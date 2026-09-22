package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"gorm.io/gorm"
)

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
		// Validate the list exists and create the shelf in ONE transaction:
		// a concurrent DELETE /api/lists/{id} between check and write used
		// to create shelves on a list that no longer existed.
		var list List
		shelf := Shelf{Name: name, ListID: listID}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.First(&list, listID).Error; err != nil {
				return err
			}
			return tx.Create(&shelf).Error
		})
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				errorJSON(w, http.StatusBadRequest, "list does not exist")
				return
			}
			GetLogger().Error("createShelf transaction failed: %v", err)
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
