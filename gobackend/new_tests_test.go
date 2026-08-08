package main

import (
	"bytes"
	"net/http"
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
