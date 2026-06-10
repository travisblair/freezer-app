package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	cookieName         = "__Host-freezer_token"
	bcryptCost         = 8 // Pi Zero W friendly; still computationally expensive to brute-force
	sessionTokenBytes  = 32
)

// ── Password helpers ───────────────────────────────────────────────────

func hashPassword(plaintext string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func checkPassword(hash, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}

func generateSessionToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ── Auth rate limiting ──────────────────────────────────────────────────
//
// Simple in-memory rate limiter for POST /api/auth.
// After maxFailures consecutive failures from an IP, further attempts are
// rejected for lockoutDuration. A successful auth resets the counter.

const (
	authMaxFailures    = 5
	authLockoutMinutes = 5
)

var (
	authFailCounts   = map[string]*authFailEntry{}
	authFailCountsMu sync.Mutex
)

type authFailEntry struct {
	count    int
	lockedAt time.Time
}

func authRateLimit(ip string) bool {
	authFailCountsMu.Lock()
	defer authFailCountsMu.Unlock()

	entry, exists := authFailCounts[ip]
	if !exists {
		return true
	}
	if entry.lockedAt.IsZero() {
		return true
	}
	if time.Since(entry.lockedAt) >= authLockoutMinutes*time.Minute {
		delete(authFailCounts, ip)
		return true
	}
	return false
}

func authRecordFailure(ip string) {
	authFailCountsMu.Lock()
	defer authFailCountsMu.Unlock()

	entry, exists := authFailCounts[ip]
	if !exists {
		entry = &authFailEntry{}
		authFailCounts[ip] = entry
	}
	entry.count++
	if entry.count >= authMaxFailures {
		entry.lockedAt = time.Now()
		GetLogger().Warn("Auth rate limit locked IP %s after %d failures", ip, entry.count)
	}
}

func authRecordSuccess(ip string) {
	authFailCountsMu.Lock()
	defer authFailCountsMu.Unlock()
	delete(authFailCounts, ip)
}

// authCleanup removes expired lockout entries periodically to prevent
// unbounded memory growth from unique IPs over months of uptime.
func authCleanup() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		authFailCountsMu.Lock()
		now := time.Now()
		for ip, entry := range authFailCounts {
			if !entry.lockedAt.IsZero() && now.Sub(entry.lockedAt) > 2*time.Hour {
				delete(authFailCounts, ip)
			}
		}
		authFailCountsMu.Unlock()
	}
}

// clearAuthRateLimiters resets the auth rate limit map. Test helper.
func clearAuthRateLimiters() {
	authFailCountsMu.Lock()
	authFailCounts = map[string]*authFailEntry{}
	authFailCountsMu.Unlock()
}

// ── Cookie helpers ─────────────────────────────────────────────────────

func parseCookies(header string) map[string]string {
	m := map[string]string{}
	for _, pair := range strings.Split(header, ";") {
		pair = strings.TrimSpace(pair)
		eq := strings.IndexByte(pair, '=')
		if eq == -1 {
			continue
		}
		m[pair[:eq]] = pair[eq+1:]
	}
	return m
}

func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func setSessionCookie(w http.ResponseWriter, token string, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func getSessionToken(r *http.Request) string {
	cookies := parseCookies(r.Header.Get("Cookie"))
	return cookies[cookieName]
}

// ── Handlers ───────────────────────────────────────────────────────────

// authHandler handles POST /api/auth — validates email/password and sets session cookie.
func authHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		ip := clientIP(r)
		if !authRateLimit(ip) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "Too many attempts. Try again later.",
			})
			return
		}

		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}

		body.Email = strings.TrimSpace(body.Email)
		if body.Email == "" || body.Password == "" {
			authRecordFailure(ip)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}

		var user User
		if err := db.Where("email = ?", body.Email).First(&user).Error; err != nil {
			authRecordFailure(ip)
			// Don't reveal whether the email exists — compare against a real bcrypt hash
			// to keep timing consistent with successful password checks.
			checkPassword("$2a$08$QkJCSEdFV0ZIRkxHREpBROTzgImIEMMwA4B3tA/tO5FLyC/YyRDoG", body.Password)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}

		if !checkPassword(user.PasswordHash, body.Password) {
			authRecordFailure(ip)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}

		authRecordSuccess(ip)

		token, err := generateSessionToken()
		if err != nil {
			GetLogger().Error("failed to generate session token: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}

		if err := db.Model(&user).Update("session_token", token).Error; err != nil {
			GetLogger().Error("failed to save session token: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		setSessionCookie(w, token, r)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// authCheckHandler handles GET /api/auth/check — verifies session token validity.
func authCheckHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := getSessionToken(r)
		if token == "" {
			writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
			return
		}

		var user User
		if err := db.Where("session_token = ?", token).First(&user).Error; err != nil {
			writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
	}
}

// logoutHandler handles POST /api/auth/logout — clears the session.
func logoutHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := getSessionToken(r)
		if token != "" {
			db.Model(&User{}).Where("session_token = ?", token).Update("session_token", "")
		}
		clearSessionCookie(w, r)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// requireAuth is middleware that blocks unauthenticated requests.
func requireAuth(db *gorm.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := getSessionToken(r)
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}

		var user User
		if err := db.Where("session_token = ?", token).First(&user).Error; err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// csrfProtect is middleware that enforces CSRF protection on mutating endpoints.
// Requires Content-Type: application/json for POST/PUT/PATCH/DELETE requests.
// Simple forms cannot set this header across origins without a CORS preflight,
// providing effective CSRF defense when combined with SameSite=Strict cookies.
func csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut ||
			r.Method == http.MethodPatch || r.Method == http.MethodDelete {
			ct := r.Header.Get("Content-Type")
			if ct != "application/json" {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "Content-Type must be application/json",
				})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
