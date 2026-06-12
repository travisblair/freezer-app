package main

import "time"

// Item represents a freezer inventory item.
// No count or deleted — quantities are per-shelf via ItemShelf.
// An item is "out of stock" when SUM(item_shelves.count) = 0.
type Item struct {
	ID        uint          `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Name      string        `gorm:"not null" json:"name"`
	Barcodes  []ItemBarcode `gorm:"foreignKey:ItemID" json:"barcodes,omitempty"`
	Shelves   []ItemShelf   `gorm:"foreignKey:ItemID" json:"shelves,omitempty"`
}

// ItemBarcode links a barcode to an inventory item.
type ItemBarcode struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	ItemID    uint      `gorm:"not null;index" json:"-"`
	Barcode   string    `gorm:"not null;uniqueIndex" json:"barcode"`
}

// Shelf represents a physical shelf/drawer in the freezer.
// Scoped to a list (Freezer, Pantry, etc.) — each list has its own shelves.
// Ordered by creation time (ID ascending).
type Shelf struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	ListID    uint      `gorm:"not null;index" json:"listId"`
	Name      string    `gorm:"not null" json:"name"`
}

// ItemShelf links an item to a shelf with a per-shelf count.
// Unique constraint ensures an item can't be on the same shelf twice.
type ItemShelf struct {
	ID      uint `gorm:"primaryKey" json:"id"`
	ItemID  uint `gorm:"not null;index;uniqueIndex:idx_item_shelf" json:"itemId"`
	ShelfID uint `gorm:"not null;index;uniqueIndex:idx_item_shelf" json:"shelfId"`
	Count   int  `gorm:"not null;default:0" json:"count"`
}

// User represents an authenticated user of the app.
type User struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Email        string `gorm:"not null;uniqueIndex" json:"email"`
	PasswordHash string `gorm:"not null" json:"-"`
	SessionToken string `gorm:"index" json:"-"`
}

// List represents a named inventory list (Freezer, Pantry, Kitchen, etc.).
type List struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Name      string    `gorm:"not null" json:"name"`
}