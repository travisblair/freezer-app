package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// ── Lookup by barcode ─────────────────────────────────────────────────

// handleLookupBarcode responds with { found: true/false, item? }.
// Preloads shelves so the frontend can show per-shelf counts.
func handleLookupBarcode(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		barcode := r.PathValue("barcode")
		var item Item
		err := db.
			Preload("Barcodes").
			Preload("Shelves", orderedShelves()).
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

		tx := db.Preload("Barcodes").Preload("Shelves", orderedShelves()).Order("name ASC")

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
		if err := db.Preload("Barcodes").Preload("Shelves", orderedShelves()).
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
			Preload("Shelves", orderedShelves()).
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
		// Find or create the ItemShelf row, then atomically update count — all
		// inside a single transaction to prevent TOCTOU between find/create
		// and the count mutation. The target-shelf existence check is inside
		// the transaction too: a concurrent DELETE /api/shelf/{id} between a
		// separate check and the write used to create rows on a shelf that no
		// longer existed.
		var targetShelf Shelf
		var itemShelf ItemShelf
		delta := body.Quantity
		if body.Mode == "decrement" {
			delta = -delta
		}

		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.First(&targetShelf, targetShelfID).Error; err != nil {
				return err
			}
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
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusBadRequest, "shelf does not exist")
				return
			}
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
		if err := db.Preload("Barcodes").Preload("Shelves", orderedShelves()).First(&item, item.ID).Error; err != nil {
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
				if err := db.Preload("Barcodes").Preload("Shelves", orderedShelves()).First(&parent, existing.ItemID).Error; err != nil {
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

		// Validate shelf exists INSIDE the transaction: a concurrent
		// DELETE /api/shelf/{id} between check and write used to create
		// items on a shelf that no longer existed.
		var shelf Shelf
		item := Item{Name: name}

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.First(&shelf, shelfID).Error; err != nil {
				return err
			}
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
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusBadRequest, "shelf does not exist")
				return
			}
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				errorJSON(w, http.StatusConflict, "barcode already exists")
				return
			}
			GetLogger().Error("handleCreate transaction failed: %v", err)
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

		// Same TOCTOU class as scan/create/createShelf: the item-existence
		// check, the dup check, and the INSERT must share one transaction,
		// or a concurrent DELETE /api/item/hard/{id} between check and
		// create leaves an ItemBarcode pointing at a missing item.
		var item Item
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.First(&item, body.ItemID).Error; err != nil {
				return err
			}
			var dup ItemBarcode
			if err := tx.Where("barcode = ?", barcode).First(&dup).Error; err == nil {
				return gorm.ErrDuplicatedKey
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return tx.Create(&ItemBarcode{ItemID: item.ID, Barcode: barcode}).Error
		})
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusNotFound, "item not found")
				return
			}
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				errorJSON(w, http.StatusConflict, "Barcode already linked to another item")
				return
			}
			GetLogger().Error("linkBarcode transaction failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "failed to link barcode")
			return
		}
		if err := db.Preload("Barcodes").Preload("Shelves", orderedShelves()).First(&item, item.ID).Error; err != nil {
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
		if err := db.Preload("Barcodes").Preload("Shelves", orderedShelves()).First(&item, id).Error; err != nil {
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
