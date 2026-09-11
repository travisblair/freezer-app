package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ── Lookup by barcode ─────────────────────────────────────────────────

// Sentinel errors for cross-handler classification (pitfall #17 — never
// classify by string-matching error text).
var (
	ErrInsufficientCount      = errors.New("insufficient count")
	ErrShelfNotFound          = errors.New("shelf not found")
	ErrConcurrentModification = errors.New("concurrent modification")
)

// handleLookupBarcode responds with { found: true/false, item? }.
// Preloads shelves so the frontend can show per-shelf counts.
func handleLookupBarcode(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		barcode := r.PathValue("barcode")
		var item Item
		err := db.
			Preload("Barcodes").
			Preload("Shelves").
			Joins("JOIN item_barcodes ON item_barcodes.item_id = items.id").
			Where("item_barcodes.barcode = ?", barcode).
			First(&item).Error

		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeJSON(w, http.StatusOK, map[string]interface{}{"found": false})
			} else {
				// A DB failure must not masquerade as "barcode not found" —
				// the frontend would offer to CREATE a duplicate item.
				GetLogger().Error("barcode lookup failed: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"found": true,
			"item":  item,
		})
	}
}

// ── List items ────────────────────────────────────────────────────────

func handleListItems(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		showOutOfStock := r.URL.Query().Get("showOutOfStock") == "true"
		search := strings.TrimSpace(r.URL.Query().Get("search"))

		if len(search) > maxSearchLength {
			errorJSON(w, http.StatusBadRequest, "search query too long")
			return
		}

		tx := db.Preload("Barcodes").Preload("Shelves").Order("name ASC")

		if search != "" {
			tx = tx.Where("name LIKE ? ESCAPE '\\'", "%"+escapeLike(search)+"%")
		}

		var items []Item
		if err := tx.Find(&items).Error; err != nil {
			GetLogger().Error("listItems query failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "internal server error")
			return
		}

		// Filter out-of-stock items unless explicitly requested.
		// Out-of-stock = sum of all ItemShelf counts = 0.
		if !showOutOfStock {
			filtered := make([]Item, 0, len(items))
			for _, item := range items {
				total := 0
				for _, s := range item.Shelves {
					total += s.Count
				}
				if total > 0 {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}

		writeJSON(w, http.StatusOK, items)
	}
}

// ── Search items for "Add to existing" flow ───────────────────────────

func handleSearchItems(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeJSON(w, http.StatusOK, []Item{})
			return
		}
		if len(q) > maxSearchLength {
			errorJSON(w, http.StatusBadRequest, "search query too long")
			return
		}
		var items []Item
		if err := db.Preload("Barcodes").Preload("Shelves").
			Where("name LIKE ? ESCAPE '\\'", "%"+escapeLike(q)+"%").
			Order("name ASC").
			Limit(10).
			Find(&items).Error; err != nil {
			GetLogger().Error("searchItems query failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

// ── Scan (atomic increment/decrement on a specific shelf) ─────────────

func handleScan(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Barcode  string `json:"barcode"`
			Mode     string `json:"mode"`
			Quantity int    `json:"quantity"`
			ShelfID  uint   `json:"shelfId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}

		barcode, ok := validBarcode(body.Barcode)
		if !ok {
			errorJSON(w, http.StatusBadRequest, "barcode is required")
			return
		}
		if !validQty(body.Quantity) {
			errorJSON(w, http.StatusBadRequest, "quantity must be 1–9999")
			return
		}
		if !validMode(body.Mode) {
			errorJSON(w, http.StatusBadRequest, "mode must be increment or decrement")
			return
		}

		var item Item
		err := db.
			Preload("Barcodes").
			Preload("Shelves").
			Joins("JOIN item_barcodes ON item_barcodes.item_id = items.id").
			Where("item_barcodes.barcode = ?", barcode).
			First(&item).Error

		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"action":  "create",
					"barcode": barcode,
				})
			} else {
				// A DB failure must not masquerade as "barcode unknown" —
				// the frontend would offer to CREATE a duplicate item.
				GetLogger().Error("scan barcode lookup failed: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
			return
		}

		// Determine which shelf to target.
		// If shelfId is provided, use it. Otherwise use the item's first shelf.
		// If the item is on multiple shelves and no shelfId, the frontend
		// will have prompted the user first — shelfId should always be set.
		targetShelfID := body.ShelfID
		if targetShelfID == 0 && len(item.Shelves) > 0 {
			targetShelfID = item.Shelves[0].ShelfID
		}

		// Validate the target shelf exists.  If no shelf is provided and the
		// item has no existing shelves, require an explicit shelf from the caller.
		if targetShelfID == 0 {
			errorJSON(w, http.StatusBadRequest, "shelf is required")
			return
		}
		var targetShelf Shelf
		if err := db.First(&targetShelf, targetShelfID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusBadRequest, "shelf does not exist")
			} else {
				GetLogger().Error("scan target shelf lookup failed: %v", err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		// Find or create the ItemShelf row, then atomically update count — all
		// inside a single transaction to prevent TOCTOU between find/create
		// and the count mutation.
		var itemShelf ItemShelf
		delta := body.Quantity
		if body.Mode == "decrement" {
			delta = -delta
		}

		if err := db.Transaction(func(tx *gorm.DB) error {
			firstErr := tx.Where("item_id = ? AND shelf_id = ?", item.ID, targetShelfID).First(&itemShelf).Error
			if errors.Is(firstErr, gorm.ErrRecordNotFound) {
				if body.Mode == "decrement" {
					// Decrementing a row that doesn't exist = nothing to
					// decrement. Reject instead of silently succeeding.
					return fmt.Errorf("%w: requested %d, available 0", ErrInsufficientCount, body.Quantity)
				}
				itemShelf = ItemShelf{ItemID: item.ID, ShelfID: targetShelfID, Count: 0}
				if err := tx.Create(&itemShelf).Error; err != nil {
					return err
				}
			} else if firstErr != nil {
				return firstErr
			}
			if body.Mode == "decrement" && itemShelf.Count < body.Quantity {
				// Mirror moveItem's 409 semantics: over-decrementing is a
				// client error (double-scan), not a silent clamp to zero.
				return fmt.Errorf("%w: requested %d, available %d", ErrInsufficientCount, body.Quantity, itemShelf.Count)
			}
			// Atomic update on the ItemShelf row
			if err := tx.Model(&itemShelf).Update("count",
				gorm.Expr("MAX(0, count + ?)", delta)).Error; err != nil {
				return err
			}
			// Reload count; if zero, delete the row (consistent with
			// handleSetShelfCount and handleMoveItem).
			if err := tx.Select("count").First(&itemShelf, itemShelf.ID).Error; err != nil {
				return err
			}
			if itemShelf.Count == 0 {
				return tx.Delete(&itemShelf).Error
			}
			return nil
		}); err != nil {
			if errors.Is(err, ErrInsufficientCount) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			GetLogger().Error("scan transaction failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan update failed"})
			return
		}

		// Audit BEFORE the post-commit reload: if the reload fails, the
		// mutation has already committed and must still be audited (and the
		// client retry won't double-log because the retry will 409 or scan
		// again visibly). targetShelf.Name is already loaded — no N+1 query.
		logAudit(db, r, "scan", "item", item.ID, item.Name,
			auditDetails(map[string]any{"shelf": targetShelf.Name, "mode": body.Mode, "quantity": body.Quantity}))

		// Reload item with updated shelves
		if err := db.Preload("Barcodes").Preload("Shelves").First(&item, item.ID).Error; err != nil {
			GetLogger().Error("failed to reload item %d after scan: %v", item.ID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"action": "updated",
			"item":   item,
		})
	}
}

// ── Create item ───────────────────────────────────────────────────────

func handleCreate(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string `json:"name"`
			Barcode  string `json:"barcode"`
			Quantity int    `json:"quantity"`
			ShelfID  uint   `json:"shelfId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}

		name, ok := validName(body.Name)
		if !ok {
			errorJSON(w, http.StatusBadRequest, "name is required (≤ 100 chars)")
			return
		}
		if !validQty(body.Quantity) {
			errorJSON(w, http.StatusBadRequest, "quantity must be 1–9999")
			return
		}

		bc := strings.TrimSpace(body.Barcode)
		if bc != "" {
			// Scan/link validate barcodes — create must too, or arbitrary
			// garbage lands in the unique index and the family UI.
			if _, ok := validBarcode(bc); !ok {
				errorJSON(w, http.StatusBadRequest, "barcode must be 1–255 chars")
				return
			}
		}

		// Duplicate barcode check
		if bc != "" {
			var existing ItemBarcode
			if err := db.Where("barcode = ?", bc).First(&existing).Error; err == nil {
				var parent Item
				if err := db.Preload("Barcodes").Preload("Shelves").First(&parent, existing.ItemID).Error; err != nil {
					GetLogger().Error("duplicate barcode %s references missing item %d", bc, existing.ItemID)
					errorJSON(w, http.StatusInternalServerError, "internal server error")
					return
				}
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"error": "Barcode exists",
					"item":  parent,
				})
				return
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				// A DB failure in the dup-check must not silently skip the
				// duplicate detection and proceed to create.
				GetLogger().Error("create dup-check failed: %v", err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
				return
			}
		}

		// Default to Shelf 1 if no shelf specified (frontend always sends shelfId now,
		// but the API default remains for backward compatibility with direct API users).
		shelfID := body.ShelfID
		if shelfID == 0 {
			shelfID = 1
		}

		// Validate shelf exists
		var shelf Shelf
		if err := db.First(&shelf, shelfID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusBadRequest, "shelf does not exist")
			} else {
				GetLogger().Error("create shelf lookup failed: %v", err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		item := Item{Name: name}

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&item).Error; err != nil {
				return err
			}

			is := ItemShelf{ItemID: item.ID, ShelfID: shelfID, Count: body.Quantity}
			if err := tx.Create(&is).Error; err != nil {
				return err
			}
			item.Shelves = []ItemShelf{{ID: is.ID, ItemID: item.ID, ShelfID: shelfID, Count: body.Quantity}}

			if bc != "" {
				if err := tx.Create(&ItemBarcode{ItemID: item.ID, Barcode: bc}).Error; err != nil {
					return err
				}
				item.Barcodes = []ItemBarcode{{ItemID: item.ID, Barcode: bc}}
			}

			return nil
		})
		if err != nil {
			GetLogger().Error("handleCreate transaction failed: %v", err)
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				errorJSON(w, http.StatusConflict, "barcode already exists")
				return
			}
			errorJSON(w, http.StatusInternalServerError, "failed to create item")
			return
		}

		logAudit(db, r, "create", "item", item.ID, item.Name,
			auditDetails(map[string]any{"barcode": bc, "quantity": body.Quantity, "shelf_id": shelfID}))

		writeJSON(w, http.StatusCreated, item)
	}
}

// ── Link barcode to existing item ─────────────────────────────────────

func handleLinkBarcode(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ItemID  uint   `json:"itemId"`
			Barcode string `json:"barcode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}

		barcode, ok := validBarcode(body.Barcode)
		if !ok {
			errorJSON(w, http.StatusBadRequest, "barcode is required")
			return
		}

		var item Item
		if err := db.First(&item, body.ItemID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "item not found")
			} else {
				GetLogger().Error("linkBarcode item lookup failed: %v", err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		var dup ItemBarcode
		if err := db.Where("barcode = ?", barcode).First(&dup).Error; err == nil {
			errorJSON(w, http.StatusConflict, "Barcode already linked to another item")
			return
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			GetLogger().Error("linkBarcode dup-check failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "internal server error")
			return
		}

		result := db.Create(&ItemBarcode{ItemID: item.ID, Barcode: barcode})
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrDuplicatedKey) {
				errorJSON(w, http.StatusConflict, "Barcode already linked to another item")
				return
			}
			GetLogger().Error("linkBarcode create failed: %v", result.Error)
			errorJSON(w, http.StatusInternalServerError, "failed to link barcode")
			return
		}
		if err := db.Preload("Barcodes").Preload("Shelves").First(&item, item.ID).Error; err != nil {
			GetLogger().Error("failed to reload item %d after linkBarcode: %v", item.ID, err)
			errorJSON(w, http.StatusInternalServerError, "internal server error")
			return
		}
		logAudit(db, r, "link_barcode", "item", item.ID, item.Name,
			auditDetails(map[string]any{"barcode": barcode}))
		writeJSON(w, http.StatusOK, item)
	}
}

// ── Update item (PATCH — name only) ────────────────────────────────────

func handleUpdateItem(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid id")
			return
		}

		var item Item
		if err := db.First(&item, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "item not found")
			} else {
				GetLogger().Error("updateItem lookup failed for %d: %v", id, err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		var body struct {
			Name *string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}

		if body.Name == nil {
			errorJSON(w, http.StatusBadRequest, "no valid fields to update")
			return
		}

		name, ok := validName(*body.Name)
		if !ok {
			errorJSON(w, http.StatusBadRequest, "name must be non-empty (≤ 100 chars)")
			return
		}

		result := db.Model(&item).Update("name", name)
		if result.Error != nil {
			GetLogger().Error("updateItem failed for item %d: %v", id, result.Error)
			errorJSON(w, http.StatusInternalServerError, "failed to update item")
			return
		}
		if err := db.Preload("Barcodes").Preload("Shelves").First(&item, id).Error; err != nil {
			GetLogger().Error("failed to reload item %d after update: %v", id, err)
			item.Name = name // at least return the name we just set
		}
		logAudit(db, r, "update", "item", item.ID, item.Name, auditDetails(map[string]any{"name": name}))
		writeJSON(w, http.StatusOK, item)
	}
}

// ── Bulk delete (set all shelf counts to 0) ────────────────────────────

func handleBulkDelete(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []uint `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if len(body.IDs) == 0 {
			errorJSON(w, http.StatusBadRequest, "ids must be a non-empty array")
			return
		}
		if len(body.IDs) > 500 {
			errorJSON(w, http.StatusBadRequest, "too many ids — maximum 500 per bulk delete")
			return
		}
		var deleted int64
		err := db.Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&ItemShelf{}).
				Where("item_id IN ?", body.IDs).
				Update("count", 0)
			if result.Error != nil {
				return result.Error
			}
			deleted = result.RowsAffected
			// Consistent with scan/setCount/moveItem: zero-count rows are
			// DELETED, not left as ghosts that resurface as phantom zero-count
			// shelf entries (the Aug 2026 phantom-row class).
			return tx.Where("item_id IN ? AND count = 0", body.IDs).Delete(&ItemShelf{}).Error
		})
		if err != nil {
			GetLogger().Error("bulkDelete transaction failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "bulk delete failed")
			return
		}

		logAudit(db, r, "delete", "item", 0, fmt.Sprintf("%d items", len(body.IDs)),
			auditDetails(map[string]any{"ids": body.IDs}))

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"deleted": deleted,
		})
	}
}

// ── Delete by barcode (set all shelf counts to 0) ─────────────────────

func handleDeleteByBarcode(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		barcode := r.PathValue("barcode")
		var link ItemBarcode
		if err := db.Where("barcode = ?", barcode).First(&link).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "item not found")
			} else {
				GetLogger().Error("deleteByBarcode lookup failed: %v", err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&ItemShelf{}).Where("item_id = ?", link.ItemID).Update("count", 0).Error; err != nil {
				return err
			}
			// Delete zero-count rows — same invariant as scan/setCount/moveItem.
			return tx.Where("item_id = ? AND count = 0", link.ItemID).Delete(&ItemShelf{}).Error
		})
		if err != nil {
			GetLogger().Error("deleteByBarcode transaction failed for item %d: %v", link.ItemID, err)
			errorJSON(w, http.StatusInternalServerError, "delete failed")
			return
		}
		logAudit(db, r, "delete", "item", link.ItemID, barcode,
			auditDetails(map[string]any{"barcode": barcode}))
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	}
}

// ── Hard delete (permanent removal, cascade) ───────────────────────────

func handleHardDelete(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid id")
			return
		}

		var item Item
		if err := db.First(&item, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "item not found")
			} else {
				GetLogger().Error("hardDelete lookup failed for %d: %v", id, err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		err = db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("item_id = ?", id).Delete(&ItemShelf{}).Error; err != nil {
				return err
			}
			if err := tx.Where("item_id = ?", id).Delete(&ItemBarcode{}).Error; err != nil {
				return err
			}
			return tx.Delete(&item).Error
		})
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, "delete failed")
			return
		}
		logAudit(db, r, "hard_delete", "item", item.ID, item.Name, "")
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
			"hard":    true,
		})
	}
}

// ── Shelf CRUD ──────────────────────────────────────────────────────────

func handleListShelves(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		listIDStr := r.URL.Query().Get("listId")
		tx := db.Order("id ASC")
		if listIDStr != "" {
			parsed, err := strconv.ParseUint(listIDStr, 10, 64)
			if err != nil {
				// A malformed listId used to be silently ignored, returning
				// shelves for EVERY list — fail loudly instead.
				errorJSON(w, http.StatusBadRequest, "invalid listId")
				return
			}
			tx = tx.Where("list_id = ?", parsed)
		}
		var shelves []Shelf
		if err := tx.Find(&shelves).Error; err != nil {
			GetLogger().Error("listShelves query failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, shelves)
	}
}

func handleCreateShelf(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name   string `json:"name"`
			ListID uint   `json:"listId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		name, ok := validName(body.Name)
		if !ok {
			errorJSON(w, http.StatusBadRequest, "name is required (≤ 100 chars)")
			return
		}
		listID := body.ListID
		if listID == 0 {
			listID = 1
		}
		// Validate the list exists
		var list List
		if err := db.First(&list, listID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusBadRequest, "list does not exist")
			} else {
				GetLogger().Error("createShelf list lookup failed: %v", err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		shelf := Shelf{Name: name, ListID: listID}
		result := db.Create(&shelf)
		if result.Error != nil {
			GetLogger().Error("createShelf failed: %v", result.Error)
			errorJSON(w, http.StatusInternalServerError, "failed to create shelf")
			return
		}
		if err := db.Create(&ShelfAudit{ShelfID: shelf.ID, ListID: &shelf.ListID, Name: name, Action: "created"}).Error; err != nil {
			GetLogger().Error("createShelf audit insert failed: %v", err)
		}
		logAudit(db, r, "shelf_create", "shelf", shelf.ID, name, auditDetails(map[string]any{"list_id": listID}))
		writeJSON(w, http.StatusCreated, shelf)
	}
}

func handleUpdateShelf(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid id")
			return
		}

		var shelf Shelf
		if err := db.First(&shelf, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "shelf not found")
			} else {
				GetLogger().Error("updateShelf lookup failed for %d: %v", id, err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		var body struct {
			Name *string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Name == nil {
			errorJSON(w, http.StatusBadRequest, "no valid fields to update")
			return
		}
		name, ok := validName(*body.Name)
		if !ok {
			errorJSON(w, http.StatusBadRequest, "name must be non-empty (≤ 100 chars)")
			return
		}

		shelf.Name = name
		result := db.Model(&shelf).Update("name", name)
		if result.Error != nil {
			GetLogger().Error("updateShelf failed for shelf %d: %v", shelf.ID, result.Error)
			errorJSON(w, http.StatusInternalServerError, "failed to update shelf")
			return
		}
		if err := db.Create(&ShelfAudit{ShelfID: shelf.ID, ListID: &shelf.ListID, Name: name, Action: "renamed"}).Error; err != nil {
			GetLogger().Error("updateShelf audit insert failed: %v", err)
		}
		logAudit(db, r, "shelf_update", "shelf", shelf.ID, name, "")
		writeJSON(w, http.StatusOK, shelf)
	}
}

func handleDeleteShelf(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid id")
			return
		}

		var shelf Shelf
		if err := db.First(&shelf, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "shelf not found")
			} else {
				GetLogger().Error("deleteShelf lookup failed for %d: %v", id, err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		// Find the default shelf for this shelf's list for merge target.
		// This is the first shelf (lowest ID) in the same list.
		var defaultShelf Shelf
		if err := db.Where("list_id = ?", shelf.ListID).Order("id ASC").First(&defaultShelf).Error; err != nil {
			// Shouldn't happen — every list has at least one shelf.
			errorJSON(w, http.StatusInternalServerError, "no default shelf found for this list")
			return
		}

		// Don't allow deleting the default shelf if it's the anchor for its list.
		if uint(id) == defaultShelf.ID {
			errorJSON(w, http.StatusBadRequest, "cannot delete the default shelf")
			return
		}

		// Move all items on this shelf to the list's default shelf, merging counts.
		// Run in a transaction so a crash mid-way doesn't leave orphaned rows.
		if err := db.Transaction(func(tx *gorm.DB) error {
			var itemShelves []ItemShelf
			if err := tx.Where("shelf_id = ?", id).Find(&itemShelves).Error; err != nil {
				return err
			}

			for _, is := range itemShelves {
				var existing ItemShelf
				if err := tx.Where("item_id = ? AND shelf_id = ?", is.ItemID, defaultShelf.ID).First(&existing).Error; err == nil {
					// Already on default shelf — merge counts
					if err := tx.Model(&existing).Update("count", gorm.Expr("count + ?", is.Count)).Error; err != nil {
						return err
					}
					if err := tx.Delete(&is).Error; err != nil {
						return err
					}
				} else {
					// Move to default shelf
					if err := tx.Model(&is).Update("shelf_id", defaultShelf.ID).Error; err != nil {
						return err
					}
				}
			}
			if err := tx.Delete(&shelf).Error; err != nil {
				return err
			}
			return tx.Create(&ShelfAudit{ShelfID: shelf.ID, ListID: &shelf.ListID, Name: shelf.Name, Action: "deleted"}).Error
		}); err != nil {
			GetLogger().Error("delete shelf transaction failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		logAudit(db, r, "shelf_delete", "shelf", shelf.ID, shelf.Name, "")
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	}
}

// ── Set shelf count ─────────────────────────────────────────────────────

func handleSetShelfCount(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid id")
			return
		}

		var body struct {
			Count *int `json:"count"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Count == nil {
			errorJSON(w, http.StatusBadRequest, "count is required")
			return
		}
		if !validCount(*body.Count) {
			errorJSON(w, http.StatusBadRequest, "count must be 0–9999")
			return
		}

		var is ItemShelf
		txErr := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.First(&is, id).Error; err != nil {
				return err
			}
			if err := tx.Model(&is).Update("count", *body.Count).Error; err != nil {
				return err
			}
			if *body.Count == 0 {
				// Re-read count to avoid race: if another request incremented
				// it concurrently, the row won't be zero and we skip delete.
				if err := tx.Select("count").First(&is, is.ID).Error; err != nil {
					return err
				}
				if is.Count == 0 {
					return tx.Delete(&is).Error
				}
			}
			return nil
		})
		if txErr != nil {
			if errors.Is(txErr, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "item-shelf not found")
			} else {
				GetLogger().Error("setShelfCount transaction failed for itemShelf %d: %v", id, txErr)
				errorJSON(w, http.StatusInternalServerError, "failed to update shelf count")
			}
			return
		}
		logAudit(db, r, "set_count", "item_shelf", is.ID, fmt.Sprintf("itemShelf %d", is.ID),
			auditDetails(map[string]any{"count": *body.Count}))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id":    is.ID,
			"count": *body.Count,
		})
	}
}

// ── Move item between shelves ────────────────────────────────────────────

func handleMoveItem(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ItemID        uint `json:"itemId"`
			SourceShelfID uint `json:"sourceShelfId"`
			TargetShelfID uint `json:"targetShelfId"`
			Quantity      int  `json:"quantity"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.SourceShelfID == body.TargetShelfID {
			errorJSON(w, http.StatusBadRequest, "source and target shelf must be different")
			return
		}
		if !validQty(body.Quantity) {
			errorJSON(w, http.StatusBadRequest, "quantity must be 1–9999")
			return
		}

		var qty int
		err := db.Transaction(func(tx *gorm.DB) error {
			// Validate source and target shelves exist
			var src, tgt Shelf
			if err := tx.First(&src, body.SourceShelfID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("%w: source", ErrShelfNotFound)
				}
				return err
			}
			if err := tx.First(&tgt, body.TargetShelfID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("%w: target", ErrShelfNotFound)
				}
				return err
			}

			var source ItemShelf
			if err := tx.Where("item_id = ? AND shelf_id = ?", body.ItemID, body.SourceShelfID).First(&source).Error; err != nil {
				return err
			}

			// Reject requests that ask for more than available
			if body.Quantity > source.Count {
				return fmt.Errorf("%w: requested %d, available %d", ErrInsufficientCount, body.Quantity, source.Count)
			}
			qty = body.Quantity

			// Decrement source (MAX(0, ...) prevents negative counts defensively)
			result := tx.Model(&source).Update("count", gorm.Expr("MAX(0, count - ?)", qty))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return fmt.Errorf("%w: source row changed", ErrConcurrentModification)
			}
			if source.Count-qty <= 0 {
				if err := tx.Delete(&source).Error; err != nil {
					return err
				}
			}

			// Increment or create target
			var target ItemShelf
			if err := tx.Where("item_id = ? AND shelf_id = ?", body.ItemID, body.TargetShelfID).First(&target).Error; err == nil {
				return tx.Model(&target).Update("count", gorm.Expr("count + ?", qty)).Error
			}
			return tx.Create(&ItemShelf{ItemID: body.ItemID, ShelfID: body.TargetShelfID, Count: qty}).Error
		})

		if err != nil {
			GetLogger().Error("move item transaction failed: %v", err)
			if errors.Is(err, ErrInsufficientCount) || errors.Is(err, ErrConcurrentModification) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			if errors.Is(err, ErrShelfNotFound) {
				errorJSON(w, http.StatusBadRequest, "source or target shelf does not exist")
				return
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "item not on source shelf")
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		logAudit(db, r, "move", "item", body.ItemID, fmt.Sprintf("item %d", body.ItemID),
			auditDetails(map[string]any{"qty": qty, "from_shelf": body.SourceShelfID, "to_shelf": body.TargetShelfID}))

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"moved": qty,
		})
	}
}

// ── Export CSV ────────────────────────────────────────────────────────

func handleExport(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var items []Item
		if err := db.Preload("Barcodes").Preload("Shelves").Order("id ASC").Find(&items).Error; err != nil {
			GetLogger().Error("export query failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "internal server error")
			return
		}

		rows := make([][]string, 0, len(items)+1)
		rows = append(rows, []string{"id", "name", "count", "barcodes"})

		for _, item := range items {
			total := 0
			for _, s := range item.Shelves {
				total += s.Count
			}
			barcodeStrs := make([]string, len(item.Barcodes))
			for i, bc := range item.Barcodes {
				barcodeStrs[i] = bc.Barcode
			}
			rows = append(rows, []string{
				fmt.Sprintf("%d", item.ID),
				csvSafe(item.Name),
				fmt.Sprintf("%d", total),
				csvSafe(strings.Join(barcodeStrs, "|")),
			})
		}
		writeCSV(w, rows)
	}
}

// ── List CRUD ─────────────────────────────────────────────────────────

func handleListLists(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var lists []List
		if err := db.Order("name ASC").Find(&lists).Error; err != nil {
			GetLogger().Error("listLists query failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, lists)
	}
}

func handleCreateList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		name, ok := validName(body.Name)
		if !ok {
			errorJSON(w, http.StatusBadRequest, "name is required (≤ 100 chars)")
			return
		}
		l := List{Name: name}
		result := db.Create(&l)
		if result.Error != nil {
			GetLogger().Error("createList failed: %v", result.Error)
			errorJSON(w, http.StatusInternalServerError, "failed to create list")
			return
		}
		logAudit(db, r, "list_create", "list", l.ID, name, "")
		writeJSON(w, http.StatusCreated, l)
	}
}

func handleUpdateList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid id")
			return
		}
		var list List
		if err := db.First(&list, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "list not found")
			} else {
				GetLogger().Error("updateList lookup failed for %d: %v", id, err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}
		var body struct {
			Name *string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if body.Name == nil {
			errorJSON(w, http.StatusBadRequest, "no valid fields to update")
			return
		}
		name, ok := validName(*body.Name)
		if !ok {
			errorJSON(w, http.StatusBadRequest, "name must be non-empty (≤ 100 chars)")
			return
		}
		result := db.Model(&list).Update("name", name)
		if result.Error != nil {
			GetLogger().Error("updateList failed for list %d: %v", list.ID, result.Error)
			errorJSON(w, http.StatusInternalServerError, "failed to update list")
			return
		}
		list.Name = name
		logAudit(db, r, "list_update", "list", list.ID, name, "")
		writeJSON(w, http.StatusOK, list)
	}
}

func handleDeleteList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid id")
			return
		}

		// Protect the default list — the app depends on list 1 existing.
		if id == DefaultListID {
			errorJSON(w, http.StatusBadRequest, "cannot delete the default list")
			return
		}

		var list List
		if err := db.First(&list, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "list not found")
			} else {
				GetLogger().Error("deleteList lookup failed for %d: %v", id, err)
				errorJSON(w, http.StatusInternalServerError, "internal server error")
			}
			return
		}

		// Cascade delete in a transaction
		userID, userName := userFromContext(r)
		err = db.Transaction(func(tx *gorm.DB) error {
			// Find all shelves in this list
			var shelfIDs []uint
			if err := tx.Model(&Shelf{}).Where("list_id = ?", id).Pluck("id", &shelfIDs).Error; err != nil {
				return err
			}

			if len(shelfIDs) > 0 {
				// Capture the items that have rows on this list's shelves BEFORE
				// deleting them. The orphan sweep below must only consider these
				// candidates — a global "no rows left" query would also destroy
				// out-of-stock items on OTHER lists (zero ItemShelf rows is their
				// normal state once a count hits 0).
				var candidateIDs []uint
				if err := tx.Model(&ItemShelf{}).Where("shelf_id IN ?", shelfIDs).Distinct().Pluck("item_id", &candidateIDs).Error; err != nil {
					return err
				}

				// Delete ItemShelf rows for this list's shelves first
				if err := tx.Where("shelf_id IN ?", shelfIDs).Delete(&ItemShelf{}).Error; err != nil {
					return err
				}

				// Find items among the candidates that now have no remaining
				// ItemShelf rows. These are items that only existed on this
				// list's shelves. Items that also exist on shelves in other
				// lists survive.
				var orphaned []Item
				if err := tx.Where("id IN ? AND id NOT IN (SELECT item_id FROM item_shelves)", candidateIDs).Find(&orphaned).Error; err != nil {
					return err
				}

				// Delete barcodes only for items that are about to be deleted
				if len(orphaned) > 0 {
					orphanedIDs := make([]uint, 0, len(orphaned))
					for _, oi := range orphaned {
						orphanedIDs = append(orphanedIDs, oi.ID)
					}
					if err := tx.Where("item_id IN ?", orphanedIDs).Delete(&ItemBarcode{}).Error; err != nil {
						return err
					}
					// Delete the orphaned items
					if err := tx.Where("id IN ?", orphanedIDs).Delete(&Item{}).Error; err != nil {
						return err
					}
					// Per-item audit rows INSIDE the transaction — the old
					// code only logged the list, so "who deleted the pork
					// chops" was unanswerable after a list delete.
					for _, oi := range orphaned {
						if err := tx.Create(&AuditLog{
							UserID: userID, UserName: userName,
							Action: "hard_delete", EntityType: "item",
							EntityID: oi.ID, EntityName: oi.Name,
							Details: auditDetails(map[string]any{"via": "list_delete", "list_id": id, "list_name": list.Name}),
						}).Error; err != nil {
							return err
						}
					}
				}
			}

			// Delete shelves
			if err := tx.Where("list_id = ?", id).Delete(&Shelf{}).Error; err != nil {
				return err
			}

			// Delete the list
			return tx.Delete(&list).Error
		})

		if err != nil {
			GetLogger().Error("delete list transaction failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "failed to delete list")
			return
		}
		logAudit(db, r, "list_delete", "list", list.ID, list.Name, "")
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	}
}

// ── Notifications ─────────────────────────────────────────────────────

func handleNotifications(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}

		var logs []AuditLog
		tx := db.Order("created_at DESC").Limit(limit)

		if since := r.URL.Query().Get("since"); since != "" {
			if t, err := time.Parse(time.RFC3339, since); err == nil {
				tx = tx.Where("created_at > ?", t)
			}
		}

		if actions := r.URL.Query().Get("actions"); actions != "" {
			parts := strings.Split(actions, ",")
			if len(parts) > 20 {
				// Cap the IN-list size so a crafted query can't exceed
				// SQLite's variable limit (and silently 500 via the Find).
				errorJSON(w, http.StatusBadRequest, "too many actions — maximum 20")
				return
			}
			for i, a := range parts {
				parts[i] = strings.TrimSpace(a)
			}
			tx = tx.Where("action IN ?", parts)
		}

		if err := tx.Find(&logs).Error; err != nil {
			GetLogger().Error("notifications query failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, logs)
	}
}
