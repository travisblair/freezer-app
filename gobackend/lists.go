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

// ── Export CSV ────────────────────────────────────────────────────────

func handleExport(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var items []Item
		if err := db.Preload("Barcodes").Preload("Shelves", orderedShelves()).Order("id ASC").Find(&items).Error; err != nil {
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
