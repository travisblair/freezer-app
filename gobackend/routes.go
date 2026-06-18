package main

import (
	"fmt"
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

// ── Scanner tarpit ──────────────────────────────────────────────────────
//
// Automated scanners probe for .env, .git/config, wp-admin, and similar
// paths.  Instead of serving a 200 (SPA fallback) or 404, we stream a slow
// JSON response that ties up their connection for up to 10 minutes.
//
// Resource caps (Pi Zero safe):
//   - 5 concurrent tarpit connections (buffered channel semaphore)
//   - 512 bytes flushed every 1s
//   - Max 10 minutes per connection
//   - ~300 KB per connection, ~1.5 MB worst-case total
//   - When all slots are full, new probes get nothing (connection hangs)

var tarpitSlots = make(chan struct{}, 5)

// scannerProbes are paths that automated vulnerability scanners commonly
// probe.  Add more as you discover them in the logs.
var scannerProbes = map[string]bool{
	"/.env":             true,
	"/.env.backup":      true,
	"/.env.bak":         true,
	"/.env.local":       true,
	"/.env.production":  true,
	"/.env.development": true,
	"/.git/config":      true,
	"/.git/HEAD":        true,
	"/wp-admin":         true,
	"/wp-admin/":        true,
	"/wp-login.php":     true,
	"/admin":            true,
	"/admin/":           true,
	"/phpmyadmin":       true,
	"/phpMyAdmin":       true,
	"/.aws/credentials": true,
	"/.ssh/id_rsa":      true,
	"/config.json":      true,
	"/backup":           true,
	"/backup/":          true,
	"/.dockerenv":       true,
	"/actuator/health":  true,
	"/.vscode/sftp.json": true,
}

// serveTarpit streams a slow JSON response to waste a scanner's time.
// Callers should check scannerProbes before invoking — this does not
// re-check.
func serveTarpit(w http.ResponseWriter, r *http.Request) {
	// Claim a slot.  If none available, return without writing anything —
	// the scanner's connection hangs until their timeout fires.
	select {
	case tarpitSlots <- struct{}{}:
		defer func() { <-tarpitSlots }()
	default:
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		// No streaming support (HTTP/1.0 or proxy).  Fall back to
		// a single large JSON array — still wasteful for scanners
		// but won't hold a connection open.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 600; i++ {
			fmt.Fprintf(w,
				`{"status":"ok","config":{"env":"production","debug":false,"version":"%d.%d.%d","node":"%s"},"timestamp":"%s"}`+"\n",
				i/100, i%100, i,
				r.RemoteAddr,
				time.Now().UTC().Format(time.RFC3339Nano),
			)
		}
		fmt.Fprintf(w, `{"status":"ok","complete":true}`+"\n")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	deadline := time.Now().Add(10 * time.Minute)
	chunk := 0
	for time.Now().Before(deadline) {
		chunk++
		payload := fmt.Sprintf(
			`{"status":"ok","config":{"env":"production","debug":false,"version":"%d.%d.%d","node":"%s"},"timestamp":"%s"}`+"\n",
			chunk/100, chunk%100, chunk,
			r.RemoteAddr,
			time.Now().UTC().Format(time.RFC3339Nano),
		)
		w.Write([]byte(payload))
		flusher.Flush()
		time.Sleep(1 * time.Second)
	}
	// Graceful close — scanner thinks the transfer completed
	fmt.Fprintf(w, `{"status":"ok","complete":true}`+"\n")
	flusher.Flush()
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

	// Serve static frontend build (SPA fallback).
	// Scanner probes for .env, .git, wp-admin etc. get the tarpit.
	frontendDist := findFrontendDist()
	if dist, err := os.Stat(frontendDist); err == nil && dist.IsDir() {
		fs := http.FileServer(http.Dir(frontendDist))
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Tarpit known scanner paths before anything else
			if scannerProbes[r.URL.Path] {
				GetLogger().Info("🪤 TARPIT | %s | %s | UA=%s",
					r.URL.Path, r.RemoteAddr, truncate(r.UserAgent(), 80))
				serveTarpit(w, r)
				return
			}

			// Strip leading / so filepath.Join doesn't treat
			// r.URL.Path as absolute and drop frontendDist.
			relPath := strings.TrimPrefix(r.URL.Path, "/")
			path := filepath.Join(frontendDist, relPath)
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