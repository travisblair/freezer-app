package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ── Global rate limiter ─────────────────────────────────────────────────
//
// Simple token-bucket per-IP rate limiter applied to all authenticated
// mutating endpoints.  Read endpoints and the auth endpoint have their
// own (higher) limits.

const (
	rateLimitPerSecond = 5
	rateLimitBurst     = 10
)

var (
	rateLimiters   = map[string]*rateBucket{}
	rateLimitersMu sync.Mutex
)

type rateBucket struct {
	tokens   float64
	lastSeen time.Time
}

// globalRateLimit rejects requests that exceed the per-IP rate limit.
func globalRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if ip == "" {
			next.ServeHTTP(w, r)
			return
		}

		rateLimitersMu.Lock()
		bucket, exists := rateLimiters[ip]
		now := time.Now()
		if !exists {
			bucket = &rateBucket{tokens: float64(rateLimitBurst), lastSeen: now}
			rateLimiters[ip] = bucket
		} else {
			elapsed := now.Sub(bucket.lastSeen).Seconds()
			bucket.tokens += elapsed * float64(rateLimitPerSecond)
			if bucket.tokens > float64(rateLimitBurst) {
				bucket.tokens = float64(rateLimitBurst)
			}
			bucket.lastSeen = now
		}

		allowed := bucket.tokens >= 1
		if allowed {
			bucket.tokens--
		}
		rateLimitersMu.Unlock()

		if !allowed {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "Rate limit exceeded. Please slow down.",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the client IP from the request, respecting X-Forwarded-For.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.Split(fwd, ",")[0]
	}
	ip := r.RemoteAddr
	if i := strings.LastIndexByte(ip, ':'); i != -1 {
		return ip[:i]
	}
	return ip
}

// clearRateLimiters resets all rate limiter state. Used in tests to prevent
// cross-test contamination from the shared per-IP token buckets.
func clearRateLimiters() {
	rateLimitersMu.Lock()
	rateLimiters = map[string]*rateBucket{}
	rateLimitersMu.Unlock()
}

// setupRoutes registers all API routes and static file serving on mux.
// This is shared between main.go (production) and main_test.go (tests).
func setupRoutes(mux *http.ServeMux, db *gorm.DB) {
	// Public endpoints
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		sqlDB, err := db.DB()
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unhealthy",
				"error":  "database connection unavailable",
			})
			return
		}
		if err := sqlDB.Ping(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unhealthy",
				"error":  "database ping failed",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/auth/check", authCheckHandler(db))
	mux.HandleFunc("POST /api/auth", authHandler(db))
	mux.HandleFunc("POST /api/auth/logout", logoutHandler(db))

	// Auth-protected read endpoints
	mux.Handle("GET /api/items", requireAuth(db, http.HandlerFunc(handleListItems(db))))
	mux.Handle("GET /api/item/{barcode}", requireAuth(db, http.HandlerFunc(handleLookupBarcode(db))))
	mux.Handle("GET /api/search-items", requireAuth(db, http.HandlerFunc(handleSearchItems(db))))
	mux.Handle("GET /api/export", requireAuth(db, http.HandlerFunc(handleExport(db))))

	// Auth-protected mutating endpoints — CSRF + rate limiter
	mux.Handle("POST /api/item/scan", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleScan(db))))))
	mux.Handle("POST /api/item/create", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleCreate(db))))))
	mux.Handle("POST /api/item/link-barcode", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleLinkBarcode(db))))))
	mux.Handle("PATCH /api/item/{id}", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleUpdateItem(db))))))
	mux.Handle("POST /api/items/bulk-delete", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleBulkDelete(db))))))
	mux.Handle("DELETE /api/item/{barcode}", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleDeleteByBarcode(db))))))
	mux.Handle("DELETE /api/item/hard/{id}", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleHardDelete(db))))))

	// Shelf endpoints
	mux.Handle("GET /api/shelves", requireAuth(db, http.HandlerFunc(handleListShelves(db))))
	mux.Handle("POST /api/shelves", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleCreateShelf(db))))))
	mux.Handle("PATCH /api/shelf/{id}", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleUpdateShelf(db))))))
	mux.Handle("DELETE /api/shelf/{id}", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleDeleteShelf(db))))))

	// List endpoints
	mux.Handle("GET /api/lists", requireAuth(db, http.HandlerFunc(handleListLists(db))))
	mux.Handle("POST /api/lists", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleCreateList(db))))))
	mux.Handle("PATCH /api/lists/{id}", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleUpdateList(db))))))
	mux.Handle("DELETE /api/lists/{id}", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleDeleteList(db))))))

	// ItemShelf endpoints
	mux.Handle("PATCH /api/item-shelf/{id}", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleSetShelfCount(db))))))
	mux.Handle("POST /api/item-shelf/move", requireAuth(db, globalRateLimit(csrfProtect(http.HandlerFunc(handleMoveItem(db))))))

	// Serve static frontend build (SPA fallback)
	frontendDist := findFrontendDist()
	if dist, err := os.Stat(frontendDist); err == nil && dist.IsDir() {
		fs := http.FileServer(http.Dir(frontendDist))
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := filepath.Join(frontendDist, r.URL.Path)
			if _, err := os.Stat(path); os.IsNotExist(err) && !strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFile(w, r, filepath.Join(frontendDist, "index.html"))
				return
			}
			fs.ServeHTTP(w, r)
		}))
	}
}

// findFrontendDist locates the Vite build output relative to the binary.
func findFrontendDist() string {
	candidates := []string{
		"frontend/dist",
		"../frontend/dist",
		"../../frontend/dist",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return "frontend/dist"
}