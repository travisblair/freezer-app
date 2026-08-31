package main

import (
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

	// DSN pragmas tuned for Raspberry Pi Zero W (slow SD card, single-core):
	//   _journal_mode=WAL      - Write-Ahead Logging for concurrent reads during writes
	//   _busy_timeout=5000     - Wait up to 5s when DB is locked (instead of failing)
	//   _synchronous=FULL      - Full durability; safe against power loss
	//   _foreign_keys=on       - Enforce FK constraints
	//   _cache_size=-8000      - ~8MB page cache (negative = KiB)
	//   _wal_autocheckpoint=1000 - Checkpoint WAL every 1000 pages to prevent bloat
	dsn := dbPath +
		"?_journal_mode=WAL" +
		"&_busy_timeout=5000" +
		"&_synchronous=FULL" +
		"&_foreign_keys=on" +
		"&_cache_size=-8000" +
		"&_wal_autocheckpoint=1000"

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

	// Auto-migrate models. GORM creates tables if they don't exist
	// and adds missing columns. Existing data is never dropped.
	if err := db.AutoMigrate(&Item{}, &ItemBarcode{}, &Shelf{}, &ItemShelf{}, &User{}, &List{}, &ShelfAudit{}, &Session{}, &AuditLog{}); err != nil {
		GetLogger().Fatal("auto-migration failed: %v", err)
	}

	// ── Seed default list ────────────────────────────────────────────────
	// Use ID: 1 to ensure the default Freezer list always occupies id 1.
	db.FirstOrCreate(&List{}, List{ID: DefaultListID, Name: "Freezer"})

	// ── Data migration: existing items → Shelf 1 ──────────────────────────
	// Ensure "Shelf 1" exists (default shelf, scoped to list 1 = Freezer)
	var shelf1 Shelf
	if err := db.Where("name = ? AND list_id = ?", "Shelf 1", 1).First(&shelf1).Error; err != nil {
		shelf1 = Shelf{Name: "Shelf 1", ListID: 1}
		db.Create(&shelf1)
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
	db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('items') WHERE name = 'count'").Scan(&hasLegacyCount)
	if hasLegacyCount {
		if err := db.Exec(`
			INSERT INTO item_shelves (item_id, shelf_id, count)
			SELECT id, ?, count FROM items
			WHERE count > 0 AND deleted = 0 AND id NOT IN (
				SELECT item_id FROM item_shelves
			)
		`, shelf1.ID).Error; err != nil {
			GetLogger().Error("legacy count migration failed: %v", err)
		}
		// Consume the legacy counts so later boots can never re-materialize
		// them. From here on, item_shelves is the only source of truth.
		if err := db.Exec(`UPDATE items SET count = 0 WHERE count > 0`).Error; err != nil {
			GetLogger().Error("legacy count consumption failed: %v", err)
		}
	}

	return db
}
