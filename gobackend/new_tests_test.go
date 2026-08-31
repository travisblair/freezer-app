package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ── Unit: rehashIfNeeded ───────────────────────────────────────────────

func TestRehashIfNeededUpgrades(t *testing.T) {
	const oldCost = 8
	oldHash, err := bcryptGenerateWithCost("test-password", oldCost)
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := rehashIfNeeded(oldHash, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if newHash == "" {
		t.Fatal("expected rehash for cost-8 password")
	}
	cost, _ := bcrypt.Cost([]byte(newHash))
	if cost != bcryptCost {
		t.Fatalf("expected rehashed cost %d, got %d", bcryptCost, cost)
	}
}

func TestRehashIfNeededNoop(t *testing.T) {
	hash, err := hashPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := rehashIfNeeded(hash, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if newHash != "" {
		t.Fatal("expected no rehash for cost-10 password")
	}
}

func TestRehashIfNeededWrongPassword(t *testing.T) {
	// rehashIfNeeded does NOT verify the password — that's done by
	// checkPassword before calling this. A wrong password here will
	// simply be rehashed (to a cost-10 hash of the wrong password).
	const oldCost = 8
	oldHash, err := bcryptGenerateWithCost("original-password", oldCost)
	if err != nil {
		t.Fatal(err)
	}
	// This succeeds but produces a hash of "wrong-password", not "original-password"
	newHash, err := rehashIfNeeded(oldHash, "wrong-password")
	if err != nil {
		t.Fatal("rehashIfNeeded does not verify password; it should succeed", err)
	}
	if newHash == "" {
		t.Fatal("expected rehash even with different plaintext")
	}
	// The new hash should be of "wrong-password", not "original-password"
	if !checkPassword(newHash, "wrong-password") {
		t.Fatal("new hash should match the plaintext passed to rehashIfNeeded")
	}
}

func bcryptGenerateWithCost(plaintext string, cost int) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(plaintext), cost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// ── Unit: hashToken ─────────────────────────────────────────────────────

func TestHashTokenDeterministic(t *testing.T) {
	a := hashToken("my-secret-token")
	b := hashToken("my-secret-token")
	if a != b {
		t.Fatal("hashToken must be deterministic")
	}
	if len(a) != 64 {
		t.Fatalf("expected 64-char hex (SHA-256), got %d", len(a))
	}
}

func TestHashTokenDifferent(t *testing.T) {
	if hashToken("alpha") == hashToken("beta") {
		t.Fatal("different inputs must produce different hashes")
	}
}

// ── Integration: session expiry ─────────────────────────────────────────

func TestSessionExpiredReturnsUnauthorized(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	db := OpenDB()
	now := time.Now()
	session := Session{
		UserID:    1,
		TokenHash: hashToken("expired-test-token"),
		CreatedAt: now,
		ExpiresAt: now.Add(-1 * time.Hour),
		CreatedIP: "127.0.0.1",
	}
	db.Create(&session)

	req, _ := http.NewRequest("GET", ts.URL+"/api/items", nil)
	req.AddCookie(&http.Cookie{
		Name:  getCookieName(),
		Value: "expired-test-token",
	})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 for expired session, got %d", resp.StatusCode)
	}
}

// ── Unit: clientIP trusted proxy ────────────────────────────────────────

func TestClientIPTrustsProxyXFF(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	ip := clientIP(req)
	if ip != "203.0.113.5" {
		t.Fatalf("expected XFF IP from trusted proxy, got %s", ip)
	}
}

func TestClientIPIgnoresSpoofedXFF(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	ip := clientIP(req)
	if ip != "192.168.1.100" {
		t.Fatalf("expected RemoteAddr for spoofed XFF, got %s", ip)
	}
}

func TestClientIPNoXFF(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:8080"
	ip := clientIP(req)
	if ip != "10.0.0.5" {
		t.Fatalf("expected RemoteAddr without XFF, got %s", ip)
	}
}

// ── Integration: CSV export no double-escaping ──────────────────────────

func TestCSVExportDoesNotDoubleEscape(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name":     "Peas, 500g",
		"quantity": 3,
		"shelfId":  1,
	}, true)
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/export", nil)
	req.AddCookie(authCookie())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	csv := buf.String()

	if !strings.Contains(csv, "Peas, 500g") {
		t.Fatalf("CSV must contain raw name 'Peas, 500g' without double-escaping. Got:\n%s", csv)
	}
	if strings.Contains(csv, `"""`) {
		t.Fatalf("CSV must NOT contain triple-quotes (double-escaping bug). Got:\n%s", csv)
	}
}

// ── Unit: trustedOrigin CORS validation ──────────────────────────────────

func TestTrustedOrigin(t *testing.T) {
	tests := []struct {
		origin   string
		expected bool
	}{
		// Valid LAN origins
		{"http://192.168.1.1", true},
		{"http://192.168.1.50", true},
		{"http://192.168.1.1:3000", true},
		{"http://192.168.255.254", true},
		{"http://10.0.0.1", true},
		{"http://10.0.0.5:8080", true},
		{"http://172.16.0.1", true},
		{"http://172.31.255.254", true},

		// Local dev
		{"http://localhost", true},
		{"http://localhost:3000", true},
		{"http://127.0.0.1", true},
		{"http://127.0.0.1:3000", true},

		// Prefix confusion attacks — must be rejected
		{"http://192.168.1.evil.com", false},
		{"http://192.168.1.1.evil.com", false},
		{"http://192.168.foo.com", false},

		// HTTPS should not be trusted for LAN
		{"https://192.168.1.1", false},

		// Non-private IPs
		{"http://8.8.8.8", false},
		{"http://203.0.113.5", false},

		// Garbage
		{"", false},
		{"not-a-url", false},
	}

	for _, tt := range tests {
		if got := trustedOrigin(tt.origin); got != tt.expected {
			t.Errorf("trustedOrigin(%q) = %v, want %v", tt.origin, got, tt.expected)
		}
	}
}

// ── Unit: csvSafe leading-whitespace formula guard ─────────────────────

func TestCsvSafeLeadingWhitespace(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"=cmd", "'=cmd"},
		{" =cmd", "' =cmd"},
		{"	+SUM(A1)", "'	+SUM(A1)"},
		{"@import", "'@import"},
		{"normal", "normal"},
		{"-1.5", "'-1.5"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := csvSafe(tt.in); got != tt.want {
			t.Errorf("csvSafe(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ── Unit: csrfProtect requires a JSON Content-Type outright ────────────

func TestCsrfProtectRejectsMissingContentType(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

	// Bodyless cross-site fetch sends NO Content-Type — must be rejected.
	req := httptest.NewRequest(http.MethodPost, "/api/item/scan", nil)
	rec := httptest.NewRecorder()
	csrfProtect(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing Content-Type, got %d", rec.Code)
	}
	if called {
		t.Fatal("handler must not run when Content-Type is missing")
	}

	// JSON content type passes.
	req2 := httptest.NewRequest(http.MethodPost, "/api/item/scan", strings.NewReader("{}"))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	csrfProtect(next).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 for application/json, got %d", rec2.Code)
	}
	if !called {
		t.Fatal("handler must run for application/json requests")
	}
}

// ── Integration: bulkDelete removes zero-count rows (no ghosts) ────────

func TestBulkDeleteRemovesZeroRows(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	barcode := fmt.Sprintf("GHOST-%d", time.Now().UnixNano())
	createResp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Ghost bait", "barcode": barcode, "quantity": 3, "shelfId": 1,
	}, true)
	var created map[string]interface{}
	decodeJSON(t, createResp, &created)
	id := uint(created["id"].(float64))

	resp := doJSON(t, ts, "POST", "/api/items/bulk-delete", map[string]interface{}{
		"ids": []interface{}{id},
	}, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on bulk delete, got %d", resp.StatusCode)
	}

	// The ItemShelf row must be DELETED, not left as a zero-count ghost —
	// scan/setCount/moveItem all delete at zero; bulkDelete must too. The
	// lookup preloads Shelves, so a ghost would show up as a zero-count entry.
	lookupResp := doJSON(t, ts, "GET", "/api/item/"+barcode, nil, true)
	var lookup map[string]interface{}
	decodeJSON(t, lookupResp, &lookup)
	item := lookup["item"].(map[string]interface{})
	shelves, _ := item["shelves"].([]interface{})
	if len(shelves) != 0 {
		t.Fatalf("expected no shelf rows after bulk delete, got %d: %v", len(shelves), shelves)
	}
}
