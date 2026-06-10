package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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
	if err := db.AutoMigrate(&Item{}, &ItemBarcode{}, &Shelf{}, &ItemShelf{}, &User{}); err != nil {
		GetLogger().Fatal("auto-migration failed: %v", err)
	}

	// ── Data migration: existing items → Shelf 1 ──────────────────────────
	// Ensure "Shelf 1" exists (default shelf, scoped to list 1 = Freezer)
	var shelf1 Shelf
	if err := db.Where("name = ? AND list_id = ?", "Shelf 1", 1).First(&shelf1).Error; err != nil {
		shelf1 = Shelf{Name: "Shelf 1", ListID: 1}
		db.Create(&shelf1)
	}

	// One-time: move all existing item counts into ItemShelf rows.
	// Idempotent: silently fails if the count column was already dropped
	// by a prior AutoMigrate. Only items with the old count column present
	// will be migrated.
	db.Exec(`
		INSERT INTO item_shelves (item_id, shelf_id, count)
		SELECT id, ?, count FROM items
		WHERE count > 0 AND id NOT IN (
			SELECT item_id FROM item_shelves
		)
	`, shelf1.ID)

	return db
}