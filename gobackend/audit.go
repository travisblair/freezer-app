package main

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// auditDetails builds a JSON string from a map, safe against injection.
// Unlike fmt.Sprintf with raw user values, json.Marshal properly escapes
// quotes, backslashes, and control characters in keys and values.
func auditDetails(args map[string]any) string {
	b, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// logAudit creates an audit log entry. Call after every successful mutation.
// Logs are fire-and-forget — failures are logged but never fail the handler.
func logAudit(db *gorm.DB, r *http.Request, action, entityType string, entityID uint, entityName, details string) {
	userID, userName := userFromContext(r)
	if userID == 0 {
		return // shouldn't happen behind requireAuth
	}

	entry := AuditLog{
		UserID:     userID,
		UserName:   userName,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		EntityName: entityName,
		Details:    details,
		CreatedAt:  time.Now(),
	}
	if err := db.Create(&entry).Error; err != nil {
		GetLogger().Error("failed to write audit log: %v", err)
	}
}
