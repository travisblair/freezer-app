package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// ── Sept 2026 audit hardening: clientIP, tarpit, BaseContext, DB path ──

func TestClientIPTrustsProxyRightmost(t *testing.T) {
	// A trusted proxy APPENDS the real client IP; the leftmost element is
	// client-supplied and attacker-controlled.
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.5")
	if ip := clientIP(req); ip != "203.0.113.5" {
		t.Fatalf("expected rightmost XFF element, got %s", ip)
	}
}

func TestClientIPTrustedProxySkipsEmptyElements(t *testing.T) {
	// A malformed header like ",1.2.3.4" used to yield "" as the bucket
	// key and bypass rate limiting entirely.
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", ", 203.0.113.5")
	if ip := clientIP(req); ip != "203.0.113.5" {
		t.Fatalf("expected non-empty rightmost element, got %q", ip)
	}
}

func TestClientIPTrustedProxyAllEmptyFallsBack(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", ", , ")
	if ip := clientIP(req); ip != "127.0.0.1" {
		t.Fatalf("expected RemoteAddr fallback for all-empty XFF, got %q", ip)
	}
}

func TestStripPasswordLineEnding(t *testing.T) {
	cases := []struct{ in, want string }{
		{" secret \n", " secret "}, // spaces preserved byte-for-byte
		{"pw\r\n", "pw"},
		{"plain", "plain"},
		{"\n", ""},
	}
	for _, c := range cases {
		if got := stripPasswordLineEnding(c.in); got != c.want {
			t.Fatalf("stripPasswordLineEnding(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDefaultDBPathNextToBinary(t *testing.T) {
	execPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(execPath), "data", "freezer.db")
	if got := defaultDBPath(); got != want {
		t.Fatalf("defaultDBPath() = %q, want %q", got, want)
	}
	// The old bug: the default resolved from the working directory, so a
	// process started from anywhere else silently opened a fresh empty DB.
	cwd, _ := os.Getwd()
	if got := defaultDBPath(); got == filepath.Join(cwd, "data", "freezer.db") {
		t.Fatalf("defaultDBPath() still resolves from cwd: %q", got)
	}
}

func TestServerBaseContextCancelsRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	returned := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/hang", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(returned)
	})

	srv := &http.Server{
		Handler:     mux,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	defer srv.Close()

	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/hang")
		if err == nil {
			resp.Body.Close()
		}
	}()

	time.Sleep(200 * time.Millisecond) // let the request reach the handler
	cancel()

	select {
	case <-returned:
		// The request context observed the cancellation via BaseContext —
		// this is what lets in-flight tarpit connections bail on shutdown.
	case <-time.After(3 * time.Second):
		t.Fatal("request context was not canceled via BaseContext")
	}
}

func TestTarpitSlotExhaustionHangs(t *testing.T) {
	if _, err := os.Stat("../frontend/dist"); err != nil {
		t.Skip("frontend dist not built — tarpit routes unregistered")
	}
	ts := newTestServer(t)
	defer ts.Close()

	// Fill all 5 tarpit slots.
	var fillers []*http.Response
	for i := 0; i < 5; i++ {
		resp, err := http.Get(ts.URL + "/.env")
		if err != nil {
			t.Fatalf("filler %d: %v", i, err)
		}
		fillers = append(fillers, resp)
	}
	time.Sleep(500 * time.Millisecond) // let all five claim their slots

	// The 6th probe must hang, not complete as an instant empty 200 (which
	// scanners read as "path exists").
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(ts.URL + "/.env")
	if err == nil {
		resp.Body.Close()
		t.Fatalf("6th tarpit request completed with status %d; expected it to hang", resp.StatusCode)
	}

	for _, r := range fillers {
		r.Body.Close()
	}
}

func TestOrderedShelvesPreloadContract(t *testing.T) {
	tmp := t.TempDir()
	prev := os.Getenv("DB_PATH")
	os.Setenv("DB_PATH", filepath.Join(tmp, "freezer.db"))
	defer os.Setenv("DB_PATH", prev)

	db := OpenDB()
	item := Item{Name: "Multi-shelf"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	secondShelf := Shelf{Name: "Second", ListID: 1}
	if err := db.Create(&secondShelf).Error; err != nil {
		t.Fatal(err)
	}
	// Explicit out-of-order IDs: 10 inserted before 9.
	if err := db.Create(&ItemShelf{ID: 10, ItemID: item.ID, ShelfID: 1, Count: 5}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ItemShelf{ID: 9, ItemID: item.ID, ShelfID: secondShelf.ID, Count: 3}).Error; err != nil {
		t.Fatal(err)
	}

	// orderedShelves() pins the scan-fallback contract: Shelves[0] is the
	// lowest row id, deterministically. Without ORDER BY the order is
	// whatever the engine happens to return.
	var got Item
	if err := db.Preload("Shelves", orderedShelves()).First(&got, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(got.Shelves) != 2 || got.Shelves[0].ID != 9 || got.Shelves[1].ID != 10 {
		t.Fatalf("shelves not ordered by id ASC: %+v", got.Shelves)
	}
}
