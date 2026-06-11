package main

import (
	"encoding/json"
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
			Preload("Shelves").
			Joins("JOIN item_barcodes ON item_barcodes.item_id = items.id").
			Where("item_barcodes.barcode = ?", barcode).
			First(&item).Error

		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"found": false})
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
			tx = tx.Where("name LIKE ?", "%"+search+"%")
		}

		var items []Item
		tx.Find(&items)

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
		db.Preload("Barcodes").Preload("Shelves").
			Where("name LIKE ?", "%"+q+"%").
			Order("name ASC").
			Limit(10).
			Find(&items)
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
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"action":  "create",
				"barcode": barcode,
			})
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

		// Find or create the ItemShelf row for this shelf (in a transaction
		// to prevent a TOCTOU race between First and Create).
		var itemShelf ItemShelf
		db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("item_id = ? AND shelf_id = ?", item.ID, targetShelfID).First(&itemShelf).Error; err != nil {
				itemShelf = ItemShelf{ItemID: item.ID, ShelfID: targetShelfID, Count: 0}
				return tx.Create(&itemShelf).Error
			}
			return nil
		})

		delta := body.Quantity
		if body.Mode == "decrement" {
			delta = -delta
		}

		// Atomic update on the ItemShelf row
		db.Model(&ItemShelf{}).Where("id = ?", itemShelf.ID).Update("count",
			gorm.Expr("MAX(0, count + ?)", delta))

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
			}
		}

		// Default to Shelf 1 if no shelf specified
		shelfID := body.ShelfID
		if shelfID == 0 {
			shelfID = 1
		}

		item := Item{Name: name}
		db.Create(&item)

		// Create the shelf link
		is := ItemShelf{ItemID: item.ID, ShelfID: shelfID, Count: body.Quantity}
		db.Create(&is)
		item.Shelves = []ItemShelf{{ID: is.ID, ItemID: item.ID, ShelfID: shelfID, Count: body.Quantity}}

		if bc != "" {
			db.Create(&ItemBarcode{ItemID: item.ID, Barcode: bc})
			item.Barcodes = []ItemBarcode{{ID: 0, ItemID: item.ID, Barcode: bc}}
		}

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
			errorJSON(w, http.StatusNotFound, "item not found")
			return
		}

		var dup ItemBarcode
		if err := db.Where("barcode = ?", barcode).First(&dup).Error; err == nil {
			errorJSON(w, http.StatusConflict, "Barcode already linked to another item")
			return
		}

		db.Create(&ItemBarcode{ItemID: item.ID, Barcode: barcode})
		db.Preload("Barcodes").Preload("Shelves").First(&item, item.ID)
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
			errorJSON(w, http.StatusNotFound, "item not found")
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

		db.Model(&item).Update("name", name)
		if err := db.Preload("Barcodes").Preload("Shelves").First(&item, id).Error; err != nil {
			GetLogger().Error("failed to reload item %d after update: %v", id, err)
			item.Name = name // at least return the name we just set
		}
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
		result := db.Model(&ItemShelf{}).
			Where("item_id IN ?", body.IDs).
			Update("count", 0)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"deleted": result.RowsAffected,
		})
	}
}

// ── Delete by barcode (set all shelf counts to 0) ─────────────────────

func handleDeleteByBarcode(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		barcode := r.PathValue("barcode")
		var link ItemBarcode
		if err := db.Where("barcode = ?", barcode).First(&link).Error; err != nil {
			errorJSON(w, http.StatusNotFound, "item not found")
			return
		}
		db.Model(&ItemShelf{}).Where("item_id = ?", link.ItemID).Update("count", 0)
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
		if db.First(&item, id).Error != nil {
			errorJSON(w, http.StatusNotFound, "item not found")
			return
		}

		db.Where("item_id = ?", id).Delete(&ItemShelf{})
		db.Where("item_id = ?", id).Delete(&ItemBarcode{})
		db.Delete(&item)
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
		listID := uint(1)
		if listIDStr != "" {
			if parsed, err := strconv.ParseUint(listIDStr, 10, 64); err == nil {
				listID = uint(parsed)
			}
		}

		var shelves []Shelf
		db.Where("list_id = ?", listID).Order("id ASC").Find(&shelves)
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

		shelf := Shelf{Name: name, ListID: listID}
		db.Create(&shelf)
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
		if db.First(&shelf, id).Error != nil {
			errorJSON(w, http.StatusNotFound, "shelf not found")
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

		db.Model(&shelf).Update("name", name)
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

		// Don't allow deleting Shelf 1 (anchor)
		if id == 1 {
			errorJSON(w, http.StatusBadRequest, "cannot delete the default shelf")
			return
		}

		var shelf Shelf
		if db.First(&shelf, id).Error != nil {
			errorJSON(w, http.StatusNotFound, "shelf not found")
			return
		}

		// Move all items on this shelf to Shelf 1, merging counts
		// for items that already exist on Shelf 1.
		var itemShelves []ItemShelf
		db.Where("shelf_id = ?", id).Find(&itemShelves)
		for _, is := range itemShelves {
			var existing ItemShelf
			if err := db.Where("item_id = ? AND shelf_id = ?", is.ItemID, uint(1)).First(&existing).Error; err == nil {
				// Already on Shelf 1 — merge counts
				db.Model(&existing).Update("count", gorm.Expr("count + ?", is.Count))
				db.Delete(&is)
			} else {
				// Move to Shelf 1
				db.Model(&is).Update("shelf_id", 1)
			}
		}

		db.Delete(&shelf)
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
		if db.First(&is, id).Error != nil {
			errorJSON(w, http.StatusNotFound, "item-shelf not found")
			return
		}

		db.Model(&is).Update("count", *body.Count)
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

		// Find source ItemShelf row
		var source ItemShelf
		if db.Where("item_id = ? AND shelf_id = ?", body.ItemID, body.SourceShelfID).First(&source).Error != nil {
			errorJSON(w, http.StatusNotFound, "item not found on source shelf")
			return
		}

		qty := body.Quantity
		if qty > source.Count {
			qty = source.Count
		}

		// Decrement source
		db.Model(&source).Update("count", gorm.Expr("count - ?", qty))
		source.Count -= qty
		if source.Count <= 0 {
			db.Delete(&source)
		}

		// Increment or create target
		var target ItemShelf
		if err := db.Where("item_id = ? AND shelf_id = ?", body.ItemID, body.TargetShelfID).First(&target).Error; err == nil {
			db.Model(&target).Update("count", gorm.Expr("count + ?", qty))
		} else {
			db.Create(&ItemShelf{ItemID: body.ItemID, ShelfID: body.TargetShelfID, Count: qty})
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"moved": qty,
		})
	}
}

// ── Export CSV ────────────────────────────────────────────────────────

func handleExport(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var items []Item
		db.Preload("Barcodes").Preload("Shelves").Order("id ASC").Find(&items)

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
				escapeCSV(item.Name),
				fmt.Sprintf("%d", total),
				escapeCSV(strings.Join(barcodeStrs, "|")),
			})
		}
		writeCSV(w, rows)
	}
}
