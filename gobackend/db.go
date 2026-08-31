package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	DefaultListID  = 1 // Seeded Freezer list — do not delete
	DefaultShelfID = 1 // Seeded Shelf 1 in list 1 — do not delete
)

// OpenDB initializes the SQLite database with GORM.
// Uses pure-Go SQLite (no CGO) for easy ARM64 cross-compilation.
// The DB_PATH is relative to the binary unless absolute.
func OpenDB() *gorm.DB {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		// Default: data directory next to binary, mirroring Pi deployment layout.
		execDir, _ := os.Getwd()
		dbPath = filepath.Join(execDir, "data", "freezer.db")
	}

	// Ensure the parent directory exists so sqlite can create the file.
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		GetLogger().Fatal("cannot create data directory %s: %v", dir, err)
	}

	// DSN pragmas tuned for Raspberry Pi Zero W (slow SD card, single-core).
	// CRITICAL: glebarez/sqlite (modernc) only honors the `_pragma=` query
	// parameter. mattn-style params (`_journal_mode=WAL`, `_busy_timeout`,
	// `_foreign_keys`) are silently DISCARDED by the driver — the app ran
	// journal_mode=delete, busy_timeout=0, foreign_keys=OFF for months
	// before this was caught (Aug 2026 audit). The values are verified
	// after connect below so a future regression is loud, not silent.
	//   journal_mode=WAL     - write-ahead logging for concurrent reads
	//   busy_timeout=5000    - wait up to 5s on lock contention
	//   synchronous=FULL     - full durability against power loss
	//   foreign_keys=1       - enforce FK constraints (orphan-row defense)
	//   cache_size=-8000     - ~8MB page cache (negative = KiB)
	//   wal_autocheckpoint=1000 - checkpoint WAL every 1000 pages
	dsn := dbPath +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(FULL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=cache_size(-8000)" +
		"&_pragma=wal_autocheckpoint(1000)"

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		GetLogger().Fatal("failed to connect to database: %v", err)
	}

	// Connection pool: SQLite is single-writer — more than 1 open conn
	// causes "database is locked" errors under concurrent access.
	sqlDB, err := db.DB()
	if err != nil {
		GetLogger().Fatal("failed to get underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Verify the critical pragmas actually applied. With the _pragma=
	// syntax a failure is loud at connect time, but verify anyway and
	// warn — the Aug 2026 incident taught us that guards must actually
	// guard. journal_mode persists in the DB file, busy_timeout and
	// foreign_keys are per-connection.
	var journalMode string
	var busyTimeout int
	var foreignKeys int
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err == nil {
		if journalMode != "wal" {
			GetLogger().Warn("journal_mode is %q, expected wal — DSN pragma not applied", journalMode)
		}
	}
	db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout)
	if busyTimeout != 5000 {
		GetLogger().Warn("busy_timeout is %d, expected 5000 — DSN pragma not applied", busyTimeout)
	}
	db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys)
	if foreignKeys != 1 {
		GetLogger().Warn("foreign_keys is %d, expected 1 — DSN pragma not applied", foreignKeys)
	}

	// Auto-migrate models. GORM creates tables if they don't exist
	// and adds missing columns. Existing data is never dropped.
	if err := db.AutoMigrate(&Item{}, &ItemBarcode{}, &Shelf{}, &ItemShelf{}, &User{}, &List{}, &ShelfAudit{}, &Session{}, &AuditLog{}); err != nil {
		GetLogger().Fatal("auto-migration failed: %v", err)
	}

	// ── Seed default list ────────────────────────────────────────────────
	// Use ID: 1 to ensure the default Freezer list always occupies id 1.
	// Identity-based, NOT name-based: the old FirstOrCreate condition
	// (ID=1 AND name='Freezer') missed forever once the user renamed the
	// list, silently re-attempting a doomed INSERT on every boot (the
	// UNIQUE error was swallowed by the unchecked result until Aug 2026).
	if err := db.First(&List{}, DefaultListID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		if err := db.Create(&List{ID: DefaultListID, Name: "Freezer"}).Error; err != nil {
			GetLogger().Fatal("failed to seed default list: %v", err)
		}
	} else if err != nil {
		GetLogger().Fatal("default list lookup failed: %v", err)
	}

	// ── Data migration: existing items → Shelf 1 ──────────────────────────
	// Resolve the anchor shelf by IDENTITY (the shelf at DefaultShelfID),
	// NOT by name. The old name-keyed seed let a rename of "Shelf 1" cause
	// a phantom "Shelf 1" to be re-created on every boot — the same
	// wrong-guard class as the legacy-count migration (Aug 2026 audit).
	var shelf1 Shelf
	seedErr := db.First(&shelf1, DefaultShelfID).Error
	if errors.Is(seedErr, gorm.ErrRecordNotFound) {
		shelf1 = Shelf{ID: DefaultShelfID, Name: "Shelf 1", ListID: DefaultListID}
		if err := db.Create(&shelf1).Error; err != nil {
			GetLogger().Fatal("failed to seed default shelf: %v", err)
		}
	} else if seedErr != nil {
		GetLogger().Fatal("default shelf lookup failed: %v", seedErr)
	}

	// One-time: move existing item counts from the legacy `items.count`
	// column into ItemShelf rows. GORM AutoMigrate adds columns but never
	// drops them, so `count` persists. After migrating, the legacy counts
	// are zeroed ("consumed") — the app never reads or writes items.count
	// anymore, and zeroing makes this idempotent even when every item is
	// out of stock.
	//
	// Aug 2026 incident: the old guard (column exists + "no rows") re-ran
	// this INSERT on EVERY boot, re-materializing stale June/July counts as
	// phantom Shelf 1 rows for each out-of-stock item (19 rows after the
	// post-cleanup deploy restart). The NOT IN guard can never be relied
	// on alone: rows are deleted at count 0, so an out-of-stock item is
	// always "missing a row" by design.
	var hasLegacyCount bool
	if err := db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('items') WHERE name = 'count'").Scan(&hasLegacyCount).Error; err != nil {
		GetLogger().Error("legacy count column check failed: %v", err)
		hasLegacyCount = false
	}
	if hasLegacyCount {
		// INSERT and consumption run in ONE transaction: if the INSERT
		// fails (locked DB, SD error at boot), the legacy counts must
		// survive so the next boot can retry. Zeroing them after a failed
		// INSERT would permanently destroy unmigrated inventory.
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(`
				INSERT INTO item_shelves (item_id, shelf_id, count)
				SELECT id, ?, count FROM items
				WHERE count > 0 AND deleted = 0 AND id NOT IN (
					SELECT item_id FROM item_shelves
				)
			`, shelf1.ID).Error; err != nil {
				return err
			}
			// Consume the legacy counts so later boots can never
			// re-materialize them. From here on, item_shelves is the only
			// source of truth.
			return tx.Exec(`UPDATE items SET count = 0 WHERE count > 0`).Error
		}); err != nil {
			GetLogger().Error("legacy count migration failed (counts preserved for retry): %v", err)
		}
	}

	return db
}
