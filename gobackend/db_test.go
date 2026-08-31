package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestLegacyCountMigrationIsOneTime guards the Aug 2026 regression where
// the boot migration re-materialized stale items.count values as phantom
// Shelf 1 rows for out-of-stock items on every restart. The fix: legacy
// counts are consumed (zeroed) after the first migration, so later boots
// can never re-materialize them.
func TestLegacyCountMigrationIsOneTime(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "freezer.db")

	prev := os.Getenv("DB_PATH")
	os.Setenv("DB_PATH", dbPath)
	defer os.Setenv("DB_PATH", prev)

	// Build a pre-shelves fixture: items with counts in the dead
	// items.count column (the current Item model no longer has Count, so
	// the column is added manually) and no item_shelves rows.
	legacy, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	if err := legacy.AutoMigrate(&Item{}, &ItemBarcode{}, &Shelf{}, &ItemShelf{}, &User{}, &List{}, &ShelfAudit{}, &Session{}, &AuditLog{}); err != nil {
		t.Fatalf("fixture automigrate: %v", err)
	}
	if err := legacy.Exec("ALTER TABLE items ADD COLUMN count integer DEFAULT 0").Error; err != nil {
		t.Fatalf("add legacy count column: %v", err)
	}
	if err := legacy.Exec("ALTER TABLE items ADD COLUMN deleted integer DEFAULT 0").Error; err != nil {
		t.Fatalf("add legacy deleted column: %v", err)
	}
	pizza := Item{Name: "Legacy pizza"}
	ribs := Item{Name: "Legacy ribs"}
	gone := Item{Name: "Legacy deleted item"}
	if err := legacy.Create(&pizza).Error; err != nil {
		t.Fatalf("create pizza: %v", err)
	}
	if err := legacy.Create(&ribs).Error; err != nil {
		t.Fatalf("create ribs: %v", err)
	}
	if err := legacy.Create(&gone).Error; err != nil {
		t.Fatalf("create deleted item: %v", err)
	}
	if err := legacy.Exec("UPDATE items SET count = 3 WHERE id = ?", pizza.ID).Error; err != nil {
		t.Fatalf("set pizza count: %v", err)
	}
	if err := legacy.Exec("UPDATE items SET count = 5 WHERE id = ?", ribs.ID).Error; err != nil {
		t.Fatalf("set ribs count: %v", err)
	}
	if err := legacy.Exec("UPDATE items SET count = 2, deleted = 1 WHERE id = ?", gone.ID).Error; err != nil {
		t.Fatalf("set deleted item: %v", err)
	}
	lsql, _ := legacy.DB()
	if err := lsql.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}

	// First boot: legacy counts migrate onto Shelf 1, then are consumed.
	db := OpenDB()
	var rows []ItemShelf
	if err := db.Where("shelf_id = ?", 1).Find(&rows).Error; err != nil {
		t.Fatalf("query migrated rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("first boot: want 2 migrated rows, got %d: %+v", len(rows), rows)
	}
	counts := map[uint]int{}
	for _, r := range rows {
		counts[r.ItemID] = r.Count
	}
	if counts[pizza.ID] != 3 || counts[ribs.ID] != 5 {
		t.Fatalf("first boot counts wrong: %v", counts)
	}
	if _, ok := counts[gone.ID]; ok {
		t.Fatalf("deleted item got a shelf row: %+v", rows)
	}
	var stale []int
	if err := db.Raw("SELECT count FROM items WHERE count > 0").Scan(&stale).Error; err != nil {
		t.Fatalf("query stale counts: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("legacy counts not consumed after first boot: %v", stale)
	}

	// The incident's pathological case: every item goes out of stock, so
	// item_shelves ends up empty. Old code re-created the phantom rows on
	// the next boot; the consumed counts must make this a permanent no-op.
	if err := db.Exec("DELETE FROM item_shelves").Error; err != nil {
		t.Fatalf("empty shelves: %v", err)
	}
	sqlDB, _ := db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close first boot db: %v", err)
	}

	db2 := OpenDB()
	var rows2 []ItemShelf
	if err := db2.Find(&rows2).Error; err != nil {
		t.Fatalf("query second-boot rows: %v", err)
	}
	if len(rows2) != 0 {
		t.Fatalf("second boot re-created phantom rows: %+v", rows2)
	}
}

// TestOpenDBAppliesPragmas guards the Aug 2026 finding that the mattn-style
// DSN pragmas were silently discarded by the modernc driver, leaving the app
// on journal_mode=delete, busy_timeout=0, foreign_keys=OFF. The _pragma=
// syntax must actually take effect.
func TestOpenDBAppliesPragmas(t *testing.T) {
	tmp := t.TempDir()
	prev := os.Getenv("DB_PATH")
	os.Setenv("DB_PATH", filepath.Join(tmp, "freezer.db"))
	defer os.Setenv("DB_PATH", prev)

	db := OpenDB()

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}
	var busyTimeout int
	if err := db.Raw("PRAGMA busy_timeout").Scan(&busyTimeout).Error; err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}
	var foreignKeys int
	if err := db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

// TestShelfSeedIsIdentityBased guards the name-keyed seed bug: renaming
// "Shelf 1" used to make every boot re-create a phantom "Shelf 1". The
// seed must resolve the anchor shelf by ID and never resurrect renamed state.
func TestShelfSeedIsIdentityBased(t *testing.T) {
	tmp := t.TempDir()
	prev := os.Getenv("DB_PATH")
	os.Setenv("DB_PATH", filepath.Join(tmp, "freezer.db"))
	defer os.Setenv("DB_PATH", prev)

	db := OpenDB()
	if err := db.Model(&Shelf{}).Where("id = ?", DefaultShelfID).Update("name", "Top Drawer").Error; err != nil {
		t.Fatalf("rename shelf: %v", err)
	}
	sqlDB, _ := db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	// Second boot must not resurrect a second "Shelf 1".
	db2 := OpenDB()
	var shelves []Shelf
	if err := db2.Where("list_id = ?", DefaultListID).Find(&shelves).Error; err != nil {
		t.Fatalf("list shelves: %v", err)
	}
	if len(shelves) != 1 {
		t.Fatalf("second boot created phantom shelves: %+v", shelves)
	}
	if shelves[0].ID != DefaultShelfID || shelves[0].Name != "Top Drawer" {
		t.Fatalf("anchor shelf changed across boot: %+v", shelves[0])
	}
}
