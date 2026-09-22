package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ── Notifications ─────────────────────────────────────────────────────

func handleNotifications(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}

		var logs []AuditLog
		tx := db.Order("created_at DESC").Limit(limit)

		if since := r.URL.Query().Get("since"); since != "" {
			if t, err := time.Parse(time.RFC3339, since); err == nil {
				tx = tx.Where("created_at > ?", t)
			}
		}

		if actions := r.URL.Query().Get("actions"); actions != "" {
			parts := strings.Split(actions, ",")
			if len(parts) > 20 {
				// Cap the IN-list size so a crafted query can't exceed
				// SQLite's variable limit (and silently 500 via the Find).
				errorJSON(w, http.StatusBadRequest, "too many actions — maximum 20")
				return
			}
			for i, a := range parts {
				parts[i] = strings.TrimSpace(a)
			}
			tx = tx.Where("action IN ?", parts)
		}

		if err := tx.Find(&logs).Error; err != nil {
			GetLogger().Error("notifications query failed: %v", err)
			errorJSON(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, logs)
	}
}
