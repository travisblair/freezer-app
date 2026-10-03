package main

import (
	"errors"

	"gorm.io/gorm"
)

// Sentinel errors for cross-handler classification (pitfall #17 — never
// classify by string-matching error text).
var (
	ErrInsufficientCount      = errors.New("insufficient count")
	ErrShelfNotFound          = errors.New("shelf not found")
	ErrConcurrentModification = errors.New("concurrent modification")
)

// ── Shared shelf-count transaction helpers ─────────────────────────────
//
// Used by handleScan and handleRestock inside a caller-owned transaction,
// so commit/rollback semantics stay with the caller.

// findOrCreateShelfRow returns the ItemShelf row for (itemID, shelfID),
// creating a zero-count row when none exists. created reports whether the
// row was inserted by this call.
func findOrCreateShelfRow(tx *gorm.DB, itemID, shelfID uint) (*ItemShelf, bool, error) {
	row := &ItemShelf{}
	firstErr := tx.Where("item_id = ? AND shelf_id = ?", itemID, shelfID).First(row).Error
	if firstErr == nil {
		return row, false, nil
	}
	if !errors.Is(firstErr, gorm.ErrRecordNotFound) {
		return nil, false, firstErr
	}
	row = &ItemShelf{ItemID: itemID, ShelfID: shelfID, Count: 0}
	if err := tx.Create(row).Error; err != nil {
		return nil, false, err
	}
	return row, true, nil
}

// applyCountDelta atomically adds delta (negative for decrements) to the
// row's count, clamps at zero via MAX(0, ...), then deletes the row when
// the re-read count is zero — the zero-count invariant shared with
// handleSetShelfCount and handleMoveItem.
func applyCountDelta(tx *gorm.DB, rowID uint, delta int) error {
	if err := tx.Model(&ItemShelf{ID: rowID}).Update("count",
		gorm.Expr("MAX(0, count + ?)", delta)).Error; err != nil {
		return err
	}
	var row ItemShelf
	if err := tx.Select("count").First(&row, rowID).Error; err != nil {
		return err
	}
	if row.Count == 0 {
		return tx.Delete(&ItemShelf{}, rowID).Error
	}
	return nil
}
