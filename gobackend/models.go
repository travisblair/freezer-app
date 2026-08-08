package main

import "time"

// Item represents a freezer inventory item.
// No count or deleted — quantities are per-shelf via ItemShelf.
// An item is "out of stock" when SUM(item_shelves.count) = 0.
// An item can exist on shelves in multiple lists (e.g. KD in upstairs and downstairs pantries).
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
	Name         string `gorm:"default:''" json:"name"`
	Email        string `gorm:"not null;uniqueIndex" json:"email"`
	PasswordHash string `gorm:"not null" json:"-"`
	SessionToken string `gorm:"index" json:"-"` // deprecated — kept for migration; replaced by sessions table
}

// Session represents an active user session.
// Replaces the single SessionToken column on users with multi-device support,
// expiry, and the ability to revoke all sessions per user.
type Session struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"not null;index:idx_session_user" json:"userId"`
	TokenHash string    `gorm:"not null;uniqueIndex" json:"-"` // SHA-256 of raw cookie token
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `gorm:"not null;index:idx_session_expires" json:"expiresAt"`
	CreatedIP string    `json:"createdIp"`
}

// List represents a named inventory list (Freezer, Pantry, Kitchen, etc.).
type List struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Name      string    `gorm:"not null" json:"name"`
}

// ShelfAudit records creation, rename, and deletion of shelves.
// Provides a permanent audit trail that survives log rotation.
type ShelfAudit struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	ShelfID   uint      `gorm:"not null;index" json:"shelfId"`
	Name      string    `gorm:"not null" json:"name"`
	Action    string    `gorm:"not null" json:"action"` // "created", "renamed", "deleted"
}

// AuditLog records every mutation for the notification feed.
// Who did what, to which entity, when.
type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"not null;index" json:"user_id"`
	UserName   string    `gorm:"not null" json:"user_name"`
	Action     string    `gorm:"not null;index" json:"action"`
	EntityType string    `gorm:"not null" json:"entity_type"`
	EntityID   uint      `json:"entity_id"`
	EntityName string    `json:"entity_name"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `gorm:"not null;index" json:"created_at"`
}