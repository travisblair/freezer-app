package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
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
}

func TestNewHTTPServerBaseContextCancelsRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Exercise the PRODUCTION server construction (newHTTPServer is what
	// main() calls) — a hand-built server here would only test stdlib.
	srv := newHTTPServer("127.0.0.1:0", http.NewServeMux(), ctx)
	if srv.BaseContext == nil {
		t.Fatal("newHTTPServer must wire BaseContext to the shutdown context")
	}

	returned := make(chan struct{})
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(returned)
	})

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
	// Close the fillers on EVERY exit path — without this, a failure here
	// leaves 5 streaming tarpit connections open and httptest.Server.Close
	// blocks on them for the full test timeout.
	t.Cleanup(func() {
		for _, r := range fillers {
			r.Body.Close()
		}
	})
	time.Sleep(500 * time.Millisecond) // let all five claim their slots

	// The 6th probe must hang, not complete as an instant empty 200 (which
	// scanners read as "path exists").
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(ts.URL + "/.env")
	if err == nil {
		resp.Body.Close()
		t.Fatalf("6th tarpit request completed with status %d; expected it to hang", resp.StatusCode)
	}
	// Close the fillers IN THE BODY: the deferred ts.Close() runs before
	// t.Cleanup, and it blocks on still-streaming tarpit handlers.
	for _, r := range fillers {
		r.Body.Close()
	}
}

func TestOrderedShelvesPreloadSQL(t *testing.T) {
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
	// A result-order test cannot discriminate on SQLite (rowid scan already
	// returns id order), so the honest assertion is on the generated SQL:
	// orderedShelves() must add ORDER BY id ASC to the preload query.
	// Capture the preload SQL via a query callback (DryRun does not emit
	// preload queries into Statement.SQL).
	var preloadSQL string
	cb := db.Callback().Query().After("gorm:query")
	if err := cb.Register("capture:shelves_preload_sql", func(db *gorm.DB) {
		sql := db.Statement.SQL.String()
		if strings.Contains(sql, "FROM `item_shelves`") {
			preloadSQL = sql
		}
	}); err != nil {
		t.Fatalf("register capture callback: %v", err)
	}
	defer cb.Remove("capture:shelves_preload_sql")

	var items []Item
	if err := db.Preload("Shelves", orderedShelves()).Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preloadSQL, "ORDER BY id ASC") {
		t.Fatalf("preload SQL missing ORDER BY id ASC: %q", preloadSQL)
	}
}

func TestTarpitWaiterOverflowGetsInstantResponse(t *testing.T) {
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
	t.Cleanup(func() {
		for _, r := range fillers {
			r.Body.Close()
		}
	})
	time.Sleep(500 * time.Millisecond) // let all five claim their slots

	// 32 waiters fill tarpitWaitSlots (they hang); the 33rd overflows and
	// must get an instant 200 — the bounded-resource valve.
	var mu sync.Mutex
	var wg sync.WaitGroup
	instant := 0
	timedOut := 0
	for i := 0; i < 33; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 1500 * time.Millisecond}
			resp, err := client.Get(ts.URL + "/.env")
			if err != nil {
				mu.Lock()
				timedOut++
				mu.Unlock()
				return
			}
			resp.Body.Close()
			mu.Lock()
			instant++
			mu.Unlock()
		}()
	}
	wg.Wait()

	if instant != 1 {
		t.Fatalf("expected exactly 1 instant overflow response, got %d (timedOut=%d)", instant, timedOut)
	}
	// Close the fillers IN THE BODY: the deferred ts.Close() runs before
	// t.Cleanup, and it blocks on still-streaming tarpit handlers.
	for _, r := range fillers {
		r.Body.Close()
	}
}

func TestHashPasswordCmdPreservesWhitespace(t *testing.T) {
	// Pin the CALL SITE, not just the helper: a revert to TrimSpace in
	// hashPasswordCmd must fail this test.
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	defer func() { os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr }()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = inR
	if _, err := inW.WriteString(" secret \n"); err != nil {
		t.Fatal(err)
	}
	inW.Close()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outW
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = errW

	hashPasswordCmd()

	outW.Close()
	errW.Close()
	io.ReadAll(errR) // drain the "Password: " prompt
	errR.Close()
	data, err := io.ReadAll(outR)
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimSpace(string(data))
	if hash == "" {
		t.Fatal("hashPasswordCmd produced no output")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(" secret ")); err != nil {
		t.Fatalf("hash does not match the padded password (TrimSpace regression?): %v", err)
	}
}

func TestMissingEntityMappings(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Existing barcode so the scan reaches the shelf-existence check.
	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Scannable", "barcode": "SCANME1", "quantity": 1, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fixture: expected 201, got %d", resp.StatusCode)
	}

	// Scan targeting a nonexistent shelf: 400 "shelf does not exist".
	resp = doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "SCANME1", "mode": "increment", "quantity": 1, "shelfId": 99999,
	}, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("scan: expected 400, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["error"] != "shelf does not exist" {
		t.Fatalf("scan: expected 'shelf does not exist', got %v", body["error"])
	}

	// Create with a nonexistent shelf: 400.
	resp = doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "X", "quantity": 1, "shelfId": 99999,
	}, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create: expected 400, got %d", resp.StatusCode)
	}
	decodeJSON(t, resp, &body)
	if body["error"] != "shelf does not exist" {
		t.Fatalf("create: expected 'shelf does not exist', got %v", body["error"])
	}

	// createShelf with a nonexistent list: 400.
	resp = doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Ghost Shelf", "listId": 99999,
	}, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("createShelf: expected 400, got %d", resp.StatusCode)
	}
	decodeJSON(t, resp, &body)
	if body["error"] != "list does not exist" {
		t.Fatalf("createShelf: expected 'list does not exist', got %v", body["error"])
	}
}

func TestDeleteListKeepsOwnZeroRowItems(t *testing.T) {
	// Decision (Sept 2026 review): the scoped sweep only deletes items it
	// can PROVE belonged to the deleted list (candidates = items with rows
	// on its shelves). An item on the doomed list whose count was zeroed
	// (row deleted) has no provable membership, so it SURVIVES rather than
	// risk data loss. Pin that semantics.
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/lists", map[string]interface{}{"name": "Doomed"}, true)
	var doomedList List
	decodeJSON(t, resp, &doomedList)
	resp = doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{"name": "S", "listId": doomedList.ID}, true)
	var shelf Shelf
	decodeJSON(t, resp, &shelf)
	resp = doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Ghost", "quantity": 2, "shelfId": shelf.ID,
	}, true)
	var ghost Item
	decodeJSON(t, resp, &ghost)

	// Zero the count — the row is deleted, leaving a zero-row item.
	var ghostShelfRowID uint
	req, _ := http.NewRequest("GET", ts.URL+"/api/items?showOutOfStock=true", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var items []Item
	decodeJSON(t, resp, &items)
	for _, i := range items {
		if i.ID == ghost.ID && len(i.Shelves) > 0 {
			ghostShelfRowID = i.Shelves[0].ID
		}
	}
	if ghostShelfRowID == 0 {
		t.Fatal("ghost item-shelf row not found")
	}
	resp = doJSON(t, ts, "PATCH", fmt.Sprintf("/api/item-shelf/%d", ghostShelfRowID), map[string]interface{}{"count": 0}, true)
	if resp.StatusCode != 200 {
		t.Fatalf("zero count: expected 200, got %d", resp.StatusCode)
	}

	// Delete the list. The zero-row item survives (conservative semantics).
	resp = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/lists/%d", doomedList.ID), nil, true)
	if resp.StatusCode != 200 {
		t.Fatalf("delete list: expected 200, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("GET", ts.URL+"/api/items?showOutOfStock=true", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	decodeJSON(t, resp, &items)
	for _, i := range items {
		if i.ID == ghost.ID {
			return // survived — semantics pinned
		}
	}
	t.Fatal("zero-row item on the deleted list was swept (semantics changed)")
}
