package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testDBPath string
var testSessionCookie string // populated in TestMain by logging in as test user

func TestMain(m *testing.M) {
	// Set up a clean test database in a temp directory.
	tmpDir, err := os.MkdirTemp("", "freezer-test-*")
	if err != nil {
		panic(err)
	}
	testDBPath = filepath.Join(tmpDir, "freezer.db")
	os.Setenv("DB_PATH", testDBPath)

	// Seed a test user directly into the DB.
	db := OpenDB()
	hash, err := hashPassword("test-password")
	if err != nil {
		panic(err)
	}
	db.Create(&User{Email: "test@test.com", PasswordHash: hash})

	// Log in to get a session cookie for all tests.
	mux := http.NewServeMux()
	setupRoutes(mux, db)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"email": "test@test.com", "password": "test-password"})
	resp, err := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == getCookieName() {
			testSessionCookie = c.Value
			break
		}
	}
	if testSessionCookie == "" {
		panic("failed to get session cookie")
	}

	code := m.Run()

	os.RemoveAll(tmpDir)
	os.Exit(code)
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	clearRateLimiters()
	clearAuthRateLimiters()
	db := OpenDB()
	mux := http.NewServeMux()
	setupRoutes(mux, db)
	return httptest.NewServer(mux)
}

func authCookie() *http.Cookie {
	return &http.Cookie{
		Name:  getCookieName(),
		Value: testSessionCookie,
	}
}

func doJSON(t *testing.T, ts *httptest.Server, method, path string, body interface{}, auth bool) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, _ := http.NewRequest(method, ts.URL+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.AddCookie(authCookie())
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

// ── Auth tests ─────────────────────────────────────────────────────────

func TestAuthRejectsUnauthenticated(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "test", "quantity": 1,
	}, false)
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthRejectsWrongCookie(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/item/create", bytes.NewBufferString(`{"name":"test","quantity":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: getCookieName(), Value: "bogus-token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthAcceptsValidSession(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Chicken Breast", "barcode": "12345", "quantity": 3,
	}, true)
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var item Item
	decodeJSON(t, resp, &item)
	if item.Name != "Chicken Breast" || len(item.Shelves) != 1 || item.Shelves[0].Count != 3 {
		t.Fatalf("unexpected item: %+v", item)
	}
}

// ── Create tests ──────────────────────────────────────────────────────

func TestCreateWithoutBarcode(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Frozen Peas", "quantity": 5,
	}, true)
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestCreateRejectsEmptyName(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "", "quantity": 1,
	}, true)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateDuplicateBarcodeReturns409(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "First", "barcode": "DUP-001", "quantity": 1,
	}, true)

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Second", "barcode": "DUP-001", "quantity": 1,
	}, true)
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

// ── Link barcode tests ────────────────────────────────────────────────

func TestLinkBarcode(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Chicken Breast", "quantity": 3,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	resp = doJSON(t, ts, "POST", "/api/item/link-barcode", map[string]interface{}{
		"itemId": item.ID, "barcode": "LINK-001",
	}, true)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var linked Item
	decodeJSON(t, resp, &linked)
	if len(linked.Barcodes) != 1 || linked.Barcodes[0].Barcode != "LINK-001" {
		t.Fatalf("unexpected barcodes: %+v", linked.Barcodes)
	}
}

func TestLinkDuplicateBarcodeRejects(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Item A", "barcode": "SHARED", "quantity": 1,
	}, true)
	var itemA Item
	decodeJSON(t, resp, &itemA)

	resp = doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Item B", "quantity": 1,
	}, true)
	var itemB Item
	decodeJSON(t, resp, &itemB)

	resp = doJSON(t, ts, "POST", "/api/item/link-barcode", map[string]interface{}{
		"itemId": itemB.ID, "barcode": "SHARED",
	}, true)
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

// ── Lookup by barcode ─────────────────────────────────────────────────

func TestLookupByBarcode(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Chicken Breast", "barcode": "LOOK-001", "quantity": 3,
	}, true)

	req, _ := http.NewRequest("GET", ts.URL+"/api/item/LOOK-001", nil)
	req.AddCookie(authCookie())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	if result["found"] != true {
		t.Fatal("expected found: true")
	}
}

func TestLookupUnknownBarcode(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/item/NONEXISTENT", nil)
	req.AddCookie(authCookie())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	if result["found"] != false {
		t.Fatal("expected found: false")
	}
}

// ── Scan tests ─────────────────────────────────────────────────────────

func TestScanIncrements(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Chicken", "barcode": "SCAN-001", "quantity": 3,
	}, true)

	resp := doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "SCAN-001", "mode": "increment", "quantity": 2, "shelfId": 1,
	}, true)
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	item := result["item"].(map[string]interface{})
	shelves := item["shelves"].([]interface{})
	if len(shelves) == 0 {
		t.Fatal("expected shelves array")
	}
	s := shelves[0].(map[string]interface{})
	if int(s["count"].(float64)) != 5 {
		t.Fatalf("expected shelf count 5, got %v", s["count"])
	}
}

func TestScanDecrements(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Fish", "barcode": "SCAN-002", "quantity": 5,
	}, true)

	resp := doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "SCAN-002", "mode": "decrement", "quantity": 2, "shelfId": 1,
	}, true)
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	item := result["item"].(map[string]interface{})
	shelves := item["shelves"].([]interface{})
	s := shelves[0].(map[string]interface{})
	if int(s["count"].(float64)) != 3 {
		t.Fatalf("expected shelf count 3, got %v", s["count"])
	}
}

func TestScanClampsToZeroAndSoftDeletes(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Overflow", "barcode": "SCAN-003", "quantity": 2,
	}, true)

	resp := doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "SCAN-003", "mode": "decrement", "quantity": 10, "shelfId": 1,
	}, true)
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	item := result["item"].(map[string]interface{})
	shelves := item["shelves"].([]interface{})
	s := shelves[0].(map[string]interface{})
	if int(s["count"].(float64)) != 0 {
		t.Fatalf("expected shelf count clamped to 0, got %v", s["count"])
	}
}

func TestScanUnknownBarcodeReturnsCreate(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "NEW-CODE", "mode": "increment", "quantity": 1,
	}, true)
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	if result["action"] != "create" {
		t.Fatalf("expected action create, got %v", result["action"])
	}
}

// ── List items ─────────────────────────────────────────────────────────

func TestListItemsExcludesDeleted(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Active", "quantity": 1,
	}, true)
	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Gone", "barcode": "DEL-GONE", "quantity": 1,
	}, true)

	// Soft-delete via scan decrement
	doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "DEL-GONE", "mode": "decrement", "quantity": 1, "shelfId": 1,
	}, true)
	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "ToDelete", "barcode": "DEL-002", "quantity": 1,
	}, true)
	doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "DEL-002", "mode": "decrement", "quantity": 1, "shelfId": 1,
	}, true)

	req, _ := http.NewRequest("GET", ts.URL+"/api/items", nil)
	req.AddCookie(authCookie())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var items []Item
	decodeJSON(t, resp, &items)
	for _, item := range items {
		total := 0
		for _, s := range item.Shelves {
			total += s.Count
		}
		if total == 0 {
			t.Fatalf("found out-of-stock item in results: %s", item.Name)
		}
	}
}

// ── PATCH update ──────────────────────────────────────────────────────

func TestUpdateItem(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Old Name", "quantity": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	resp = doJSON(t, ts, "PATCH", fmt.Sprintf("/api/item/%d", item.ID), map[string]interface{}{
		"name": "New Name",
	}, true)
	var updated Item
	decodeJSON(t, resp, &updated)
	if updated.Name != "New Name" {
		t.Fatalf("unexpected update: %+v", updated)
	}
}

func TestShowOutOfStock(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create an item and decrement it to 0 via the API
	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Gone", "quantity": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	// Decrement to 0 using scan API
	// Need a barcode first — link one
	doJSON(t, ts, "POST", "/api/item/link-barcode", map[string]interface{}{
		"itemId": item.ID, "barcode": "GONE-001",
	}, true)

	doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "GONE-001", "mode": "decrement", "quantity": 1, "shelfId": 1,
	}, true)

	// Default list hides out-of-stock
	req, _ := http.NewRequest("GET", ts.URL+"/api/items", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var items []Item
	decodeJSON(t, resp, &items)
	for _, it := range items {
		if it.Name == "Gone" {
			t.Fatal("out-of-stock item should be hidden by default")
		}
	}

	// With showOutOfStock=true it appears
	req, _ = http.NewRequest("GET", ts.URL+"/api/items?showOutOfStock=true", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	decodeJSON(t, resp, &items)
	found := false
	for _, it := range items {
		if it.Name == "Gone" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("out-of-stock item should appear with showOutOfStock=true")
	}
}

// ── Bulk delete ───────────────────────────────────────────────────────

func TestBulkDelete(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	var ids []uint
	for _, name := range []string{"A", "B", "C"} {
		resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
			"name": name, "quantity": 1,
		}, true)
		var item Item
		decodeJSON(t, resp, &item)
		ids = append(ids, item.ID)
	}

	resp := doJSON(t, ts, "POST", "/api/items/bulk-delete", map[string]interface{}{
		"ids": ids,
	}, true)
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	if int(result["deleted"].(float64)) != 3 {
		t.Fatalf("expected deleted 3, got %v", result["deleted"])
	}
}

// ── Hard delete ───────────────────────────────────────────────────────

func TestHardDelete(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Doomed", "quantity": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/item/hard/%d", ts.URL, item.ID), nil)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(authCookie())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	if result["deleted"] != true || result["hard"] != true {
		t.Fatalf("expected hard delete, got %+v", result)
	}
}

// ── Export CSV ────────────────────────────────────────────────────────

func TestExportCSV(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Chicken", "barcode": "EXPORT-001", "quantity": 3,
	}, true)

	req, _ := http.NewRequest("GET", ts.URL+"/api/export", nil)
	req.AddCookie(authCookie())
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	body := buf.String()
	if !strings.Contains(body, "id,name,count,barcodes") {
		t.Fatal("CSV missing header")
	}
	if !strings.Contains(body, "Chicken") {
		t.Fatal("CSV missing item name")
	}
}

// ── Auth tests (bcrypt login flow) ─────────────────────────────────────

func TestAuthLoginSuccess(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create a dedicated user so we don't overwrite the global testSessionCookie
	db := OpenDB()
	hash, _ := hashPassword("login-success-password")
	db.Create(&User{Email: "login-success@test.com", PasswordHash: hash})

	body, _ := json.Marshal(map[string]string{
		"email": "login-success@test.com", "password": "login-success-password",
	})
	resp, err := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	if resp.StatusCode != 200 || result["ok"] != true {
		t.Fatalf("expected 200 ok, got %d %+v", resp.StatusCode, result)
	}

	// Verify cookie was set
	var found bool
	for _, c := range resp.Cookies() {
		if c.Name == getCookieName() && c.Value != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected session cookie to be set")
	}
}

func TestAuthLoginWrongPassword(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{
		"email": "test@test.com", "password": "wrong-password",
	})
	resp, _ := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
	resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthLoginWrongEmail(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{
		"email": "nobody@nowhere.com", "password": "anything",
	})
	resp, _ := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
	resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthLoginEmptyFields(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{
		"email": "", "password": "",
	})
	resp, _ := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
	resp.Body.Close()

	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuthLogout(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create a dedicated user so we don't invalidate the global testSessionCookie
	db := OpenDB()
	hash, _ := hashPassword("logout-test-password")
	db.Create(&User{Email: "logout-test@test.com", PasswordHash: hash})

	// Login first to get a fresh cookie
	body, _ := json.Marshal(map[string]string{
		"email": "logout-test@test.com", "password": "logout-test-password",
	})
	resp, _ := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == getCookieName() {
			sessionCookie = c
			break
		}
	}
	resp.Body.Close()

	// Verify session works
	req, _ := http.NewRequest("GET", ts.URL+"/api/auth/check", nil)
	req.AddCookie(sessionCookie)
	resp, _ = http.DefaultClient.Do(req)
	var check map[string]bool
	decodeJSON(t, resp, &check)
	if !check["authenticated"] {
		t.Fatal("should be authenticated before logout")
	}

	// Logout
	req, _ = http.NewRequest("POST", ts.URL+"/api/auth/logout", nil)
	req.AddCookie(sessionCookie)
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify session is dead
	req, _ = http.NewRequest("GET", ts.URL+"/api/auth/check", nil)
	req.AddCookie(sessionCookie)
	resp, _ = http.DefaultClient.Do(req)
	decodeJSON(t, resp, &check)
	if check["authenticated"] {
		t.Fatal("should not be authenticated after logout")
	}
}

func TestAuthCheckUnauthenticated(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, _ := http.Get(ts.URL + "/api/auth/check")
	defer resp.Body.Close()
	var result map[string]bool
	decodeJSON(t, resp, &result)
	if result["authenticated"] {
		t.Fatal("expected unauthenticated")
	}
}

func TestAuthRateLimiting(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{
		"email": "test@test.com", "password": "wrong",
	})

	// First 5 failures: no delay, immediate 401
	for i := 0; i < 5; i++ {
		resp, _ := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, resp.StatusCode)
		}
	}

	// 6th failure: should get 401 after a delay (not 429)
	start := time.Now()
	resp, _ := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
	elapsed := time.Since(start)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("6th attempt: expected 401 (delayed), got %d", resp.StatusCode)
	}
	if elapsed < 2*time.Second {
		t.Fatalf("6th attempt: expected >= 2s delay, got %v", elapsed)
	}
}

func TestAuthDelaySlotExhaustion(t *testing.T) {
	// Fill all 10 delay slots, then verify authShouldDelay returns false.
	for i := 0; i < authDelaySlots; i++ {
		authDelaySem <- struct{}{}
	}

	// Pre-populate 5 failures so delays kick in
	for i := 0; i < 5; i++ {
		authRecordFailure("10.0.0.1")
	}

	// All slots full — should get false (caller should 429)
	if authShouldDelay("10.0.0.1") {
		t.Fatal("expected false when all delay slots are full")
	}

	// Clean up: drain slots so other tests aren't affected
	for i := 0; i < authDelaySlots; i++ {
		<-authDelaySem
	}
	authFailCountsMu.Lock()
	delete(authFailCounts, "10.0.0.1")
	authFailCountsMu.Unlock()
}

func TestAuthDelayDuration(t *testing.T) {
	if d := authDelayDuration(0); d != 0 {
		t.Fatalf("0 failures: expected 0, got %v", d)
	}
	if d := authDelayDuration(4); d != 0 {
		t.Fatalf("4 failures: expected 0, got %v", d)
	}
	if d := authDelayDuration(5); d != 2*time.Second {
		t.Fatalf("5 failures: expected 2s, got %v", d)
	}
	if d := authDelayDuration(6); d != 5*time.Second {
		t.Fatalf("6 failures: expected 5s, got %v", d)
	}
	if d := authDelayDuration(7); d != 15*time.Second {
		t.Fatalf("7 failures: expected 15s, got %v", d)
	}
	if d := authDelayDuration(8); d != 30*time.Second {
		t.Fatalf("8 failures: expected 30s, got %v", d)
	}
	if d := authDelayDuration(9); d != 60*time.Second {
		t.Fatalf("9+ failures: expected 60s, got %v", d)
	}
	if d := authDelayDuration(100); d != 60*time.Second {
		t.Fatalf("100 failures: expected 60s, got %v", d)
	}
}

func TestAuthSuccessResetsCounter(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create a real user to successfully authenticate against
	db := OpenDB()
	hash, _ := hashPassword("reset-test-password")
	db.Create(&User{Email: "reset-test@test.com", PasswordHash: hash})

	wrongBody, _ := json.Marshal(map[string]string{
		"email": "reset-test@test.com", "password": "wrong",
	})
	correctBody, _ := json.Marshal(map[string]string{
		"email": "reset-test@test.com", "password": "reset-test-password",
	})

	// Fail 3 times
	for i := 0; i < 3; i++ {
		resp, _ := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(wrongBody))
		resp.Body.Close()
	}

	// Succeed — should reset counter
	resp, _ := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(correctBody))
	resp.Body.Close()

	// Fail 5 more times — should NOT be delayed (counter was reset)
	for i := 0; i < 5; i++ {
		resp, _ = http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(wrongBody))
		resp.Body.Close()
	}

	// 6th failure after reset: should be delayed (>2s)
	start := time.Now()
	resp, _ = http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(wrongBody))
	elapsed := time.Since(start)
	resp.Body.Close()
	if elapsed < 2*time.Second {
		t.Fatalf("after 5 failures post-reset: expected >= 2s delay, got %v", elapsed)
	}
}

func TestAuthNewLoginInvalidatesOldSession(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create a dedicated user for this test so we don't invalidate
	// the global testSessionCookie used by all other tests.
	db := OpenDB()
	hash, _ := hashPassword("session-test-password")
	db.Create(&User{Email: "session-test@test.com", PasswordHash: hash})

	// Login first time
	body, _ := json.Marshal(map[string]string{
		"email": "session-test@test.com", "password": "session-test-password",
	})
	resp, _ := http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
	var oldCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == getCookieName() {
			oldCookie = c
			break
		}
	}
	resp.Body.Close()
	if oldCookie == nil {
		t.Fatal("no cookie from first login")
	}

	// Login second time — should get a different token
	resp, _ = http.Post(ts.URL+"/api/auth", "application/json", bytes.NewReader(body))
	var newCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == getCookieName() {
			newCookie = c
			break
		}
	}
	resp.Body.Close()
	if newCookie == nil {
		t.Fatal("no cookie from second login")
	}
	if oldCookie.Value == newCookie.Value {
		t.Fatal("expected different session tokens")
	}

	// Old cookie should be invalid
	req, _ := http.NewRequest("GET", ts.URL+"/api/auth/check", nil)
	req.AddCookie(oldCookie)
	resp, _ = http.DefaultClient.Do(req)
	var check map[string]bool
	decodeJSON(t, resp, &check)
	if check["authenticated"] {
		t.Fatal("old session should be invalid after new login")
	}

	// New cookie should work
	req, _ = http.NewRequest("GET", ts.URL+"/api/auth/check", nil)
	req.AddCookie(newCookie)
	resp, _ = http.DefaultClient.Do(req)
	decodeJSON(t, resp, &check)
	if !check["authenticated"] {
		t.Fatal("new session should be valid")
	}
}

// ── Shelf tests ────────────────────────────────────────────────────────

func TestListShelves(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/shelves?listId=1", nil)
	req.AddCookie(authCookie())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var shelves []Shelf
	decodeJSON(t, resp, &shelves)
	if len(shelves) < 1 {
		t.Fatal("expected at least one shelf (Shelf 1)")
	}
	if shelves[0].Name != "Shelf 1" || shelves[0].ListID != 1 {
		t.Fatalf("expected Shelf 1 on list 1, got %+v", shelves[0])
	}
}

func TestCreateShelf(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Door", "listId": 1,
	}, true)
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var shelf Shelf
	decodeJSON(t, resp, &shelf)
	if shelf.Name != "Door" {
		t.Fatalf("expected Door, got %s", shelf.Name)
	}

	// Verify it appears in list
	req, _ := http.NewRequest("GET", ts.URL+"/api/shelves?listId=1", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var shelves []Shelf
	decodeJSON(t, resp, &shelves)
	found := false
	for _, s := range shelves {
		if s.Name == "Door" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("created shelf not found in list")
	}
}

func TestRenameShelf(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create a shelf to rename
	resp := doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Old Name", "listId": 1,
	}, true)
	var shelf Shelf
	decodeJSON(t, resp, &shelf)

	resp = doJSON(t, ts, "PATCH", fmt.Sprintf("/api/shelf/%d", shelf.ID), map[string]interface{}{
		"name": "New Name",
	}, true)
	var updated Shelf
	decodeJSON(t, resp, &updated)
	if updated.Name != "New Name" {
		t.Fatalf("expected New Name, got %s", updated.Name)
	}
}

func TestCannotDeleteShelf1(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "DELETE", "/api/shelf/1", nil, true)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for deleting Shelf 1, got %d", resp.StatusCode)
	}
}

func TestDeleteShelfMovesItems(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create a shelf
	resp := doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Temp Shelf", "listId": 1,
	}, true)
	var shelf Shelf
	decodeJSON(t, resp, &shelf)

	// Create an item on that shelf with a barcode
	resp = doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Moved Item", "quantity": 3, "shelfId": shelf.ID, "barcode": "MOVE-TEST",
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	// Delete the shelf
	resp = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/shelf/%d", shelf.ID), nil, true)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify item is still findable via barcode (moved to Shelf 1)
	req, _ := http.NewRequest("GET", ts.URL+"/api/item/MOVE-TEST", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	if result["found"] != true {
		t.Fatal("item should still be findable")
	}
}

// ── Shelf Audit ───────────────────────────────────────────────────────

func TestShelfAudit(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	auditDB := OpenDB()

	// 1. Create a shelf → should log an audit row
	resp := doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Audit Test", "listId": 1,
	}, true)
	var shelf Shelf
	decodeJSON(t, resp, &shelf)

	var created []ShelfAudit
	auditDB.Where("shelf_id = ? AND action = ?", shelf.ID, "created").Find(&created)
	if len(created) != 1 {
		t.Fatalf("expected 1 created audit row, got %d", len(created))
	}
	if created[0].Name != "Audit Test" {
		t.Fatalf("expected audit name 'Audit Test', got '%s'", created[0].Name)
	}

	// 2. Rename the shelf → should log an audit row
	resp = doJSON(t, ts, "PATCH", fmt.Sprintf("/api/shelf/%d", shelf.ID), map[string]interface{}{
		"name": "Renamed",
	}, true)
	_ = resp

	var renamed []ShelfAudit
	auditDB.Where("shelf_id = ? AND action = ?", shelf.ID, "renamed").Find(&renamed)
	if len(renamed) != 1 {
		t.Fatalf("expected 1 renamed audit row, got %d", len(renamed))
	}
	if renamed[0].Name != "Renamed" {
		t.Fatalf("expected audit name 'Renamed', got '%s'", renamed[0].Name)
	}

	// 3. Delete the shelf → should log an audit row
	resp = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/shelf/%d", shelf.ID), nil, true)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var deleted []ShelfAudit
	auditDB.Where("shelf_id = ? AND action = ?", shelf.ID, "deleted").Find(&deleted)
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deleted audit row, got %d", len(deleted))
	}
	if deleted[0].Name != "Renamed" {
		t.Fatalf("expected audit name 'Renamed' (name at time of delete), got '%s'", deleted[0].Name)
	}
}

func TestCreateItemWithShelf(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Shelf Item", "quantity": 5, "shelfId": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)
	if len(item.Shelves) != 1 || item.Shelves[0].ShelfID != 1 || item.Shelves[0].Count != 5 {
		t.Fatalf("expected item on Shelf 1 with count 5, got %+v", item.Shelves)
	}
}

func TestScanWithShelfId(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Scan Shelf", "barcode": "SHELF-SCAN", "quantity": 2, "shelfId": 1,
	}, true)

	resp := doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "SHELF-SCAN", "mode": "increment", "quantity": 3, "shelfId": 1,
	}, true)
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	item := result["item"].(map[string]interface{})
	shelves := item["shelves"].([]interface{})
	s := shelves[0].(map[string]interface{})
	if int(s["count"].(float64)) != 5 {
		t.Fatalf("expected shelf count 5, got %v", s["count"])
	}
}

func TestItemOnMultipleShelves(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create Shelf 2
	resp := doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Shelf 2", "listId": 1,
	}, true)
	var s2 Shelf
	decodeJSON(t, resp, &s2)

	// Create item on Shelf 1
	resp = doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Multi Shelf", "barcode": "MULTI-001", "quantity": 3, "shelfId": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	// Scan onto Shelf 2
	doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "MULTI-001", "mode": "increment", "quantity": 2, "shelfId": s2.ID,
	}, true)

	// Verify item has two shelf entries
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/item/MULTI-001", ts.URL), nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var lookup map[string]interface{}
	decodeJSON(t, resp, &lookup)
	if lookup["found"] != true {
		t.Fatal("item should be found")
	}
	i := lookup["item"].(map[string]interface{})
	sh := i["shelves"].([]interface{})
	if len(sh) != 2 {
		t.Fatalf("expected 2 shelf entries, got %d", len(sh))
	}
}

func TestBulkDeleteSetsShelfCountsToZero(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Bulk Gone", "quantity": 1, "shelfId": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	resp = doJSON(t, ts, "POST", "/api/items/bulk-delete", map[string]interface{}{
		"ids": []uint{item.ID},
	}, true)
	var result map[string]interface{}
	decodeJSON(t, resp, &result)
	if int(result["deleted"].(float64)) < 1 {
		t.Fatal("expected at least 1 shelf row deleted")
	}
}

func TestHardDeleteCascadesShelfRows(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Hard Go", "quantity": 1, "shelfId": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/item/hard/%d", ts.URL, item.ID), nil)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var hd map[string]interface{}
	decodeJSON(t, resp, &hd)
	if hd["deleted"] != true {
		t.Fatal("expected hard delete to succeed")
	}
}

func TestSetShelfCount(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Set Count", "quantity": 3, "shelfId": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	shelfID := item.Shelves[0].ID
	resp = doJSON(t, ts, "PATCH", fmt.Sprintf("/api/item-shelf/%d", shelfID), map[string]interface{}{
		"count": 7,
	}, true)
	if resp.StatusCode != 200 {
		var errBody map[string]interface{}
		decodeJSON(t, resp, &errBody)
		t.Fatalf("expected 200, got %d: %v", resp.StatusCode, errBody)
	}
	var sc map[string]interface{}
	decodeJSON(t, resp, &sc)
	if sc["count"] == nil {
		t.Fatalf("count missing from response: %+v", sc)
	}
	if int(sc["count"].(float64)) != 7 {
		t.Fatalf("expected count 7, got %v", sc["count"])
	}
}

func TestMoveItemBetweenShelves(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create Shelf 2
	resp := doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Shelf 2", "listId": 1,
	}, true)
	var s2 Shelf
	decodeJSON(t, resp, &s2)

	// Create item on Shelf 1
	resp = doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Move Me", "quantity": 5, "shelfId": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	// Move 3 from Shelf 1 to Shelf 2
	resp = doJSON(t, ts, "POST", "/api/item-shelf/move", map[string]interface{}{
		"itemId": item.ID, "sourceShelfId": 1, "targetShelfId": s2.ID, "quantity": 3,
	}, true)
	var mv map[string]interface{}
	decodeJSON(t, resp, &mv)
	if int(mv["moved"].(float64)) != 3 {
		t.Fatalf("expected moved 3, got %v", mv["moved"])
	}

	// Verify item now has 2 on Shelf 1 and 3 on Shelf 2
	req, _ := http.NewRequest("GET", ts.URL+"/api/items?showOutOfStock=true", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var items []Item
	decodeJSON(t, resp, &items)
	for _, i := range items {
		if i.ID == item.ID {
			m := make(map[uint]int)
			for _, s := range i.Shelves {
				m[s.ShelfID] = s.Count
			}
			if m[1] != 2 || m[uint(s2.ID)] != 3 {
				t.Fatalf("expected shelf 1=2, shelf %d=3, got %+v", s2.ID, m)
			}
			return
		}
	}
	t.Fatal("item not found after move")
}

// ── Set shelf count to 0 (regression test for validQty → validCount) ──

func TestSetShelfCountToZero(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Zero Count", "quantity": 3, "shelfId": 1,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	shelfID := item.Shelves[0].ID
	resp = doJSON(t, ts, "PATCH", fmt.Sprintf("/api/item-shelf/%d", shelfID), map[string]interface{}{
		"count": 0,
	}, true)
	if resp.StatusCode != 200 {
		var errBody map[string]interface{}
		decodeJSON(t, resp, &errBody)
		t.Fatalf("expected 200 for count=0, got %d: %v", resp.StatusCode, errBody)
	}
	var sc map[string]interface{}
	decodeJSON(t, resp, &sc)
	if int(sc["count"].(float64)) != 0 {
		t.Fatalf("expected count 0, got %v", sc["count"])
	}
}

// ── Search length limit ──────────────────────────────────────────────

func TestSearchRejectsLongQuery(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Build a 201-character search query
	longQ := strings.Repeat("x", 201)

	req, _ := http.NewRequest("GET", ts.URL+"/api/items?search="+longQ, nil)
	req.AddCookie(authCookie())
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for long search, got %d", resp.StatusCode)
	}

	req2, _ := http.NewRequest("GET", ts.URL+"/api/search-items?q="+longQ, nil)
	req2.AddCookie(authCookie())
	resp2, _ := http.DefaultClient.Do(req2)
	if resp2.StatusCode != 400 {
		t.Fatalf("expected 400 for long search query, got %d", resp2.StatusCode)
	}
}

// ── List tests ────────────────────────────────────────────────────────

func TestListCRUD(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create
	resp := doJSON(t, ts, "POST", "/api/lists", map[string]interface{}{
		"name": "Pantry",
	}, true)
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var list List
	decodeJSON(t, resp, &list)
	if list.Name != "Pantry" {
		t.Fatalf("expected Pantry, got %s", list.Name)
	}

	// List all
	req, _ := http.NewRequest("GET", ts.URL+"/api/lists", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var lists []List
	decodeJSON(t, resp, &lists)
	if len(lists) < 2 { // "Freezer" seed + "Pantry"
		t.Fatalf("expected at least 2 lists, got %d", len(lists))
	}

	// Rename
	resp = doJSON(t, ts, "PATCH", fmt.Sprintf("/api/lists/%d", list.ID), map[string]interface{}{
		"name": "Kitchen",
	}, true)
	var updated List
	decodeJSON(t, resp, &updated)
	if updated.Name != "Kitchen" {
		t.Fatalf("expected Kitchen, got %s", updated.Name)
	}

	// Delete
	resp = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/lists/%d", list.ID), nil, true)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDeleteListCascades(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create list
	resp := doJSON(t, ts, "POST", "/api/lists", map[string]interface{}{
		"name": "ToDelete",
	}, true)
	var list List
	decodeJSON(t, resp, &list)

	// Create shelf on this list
	resp = doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Shelf A", "listId": list.ID,
	}, true)
	var shelf Shelf
	decodeJSON(t, resp, &shelf)

	// Create item on that shelf
	resp = doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Doomed", "quantity": 3, "shelfId": shelf.ID,
	}, true)
	var item Item
	decodeJSON(t, resp, &item)

	// Delete list
	resp = doJSON(t, ts, "DELETE", fmt.Sprintf("/api/lists/%d", list.ID), nil, true)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify shelf gone
	resp = doJSON(t, ts, "GET", fmt.Sprintf("/api/shelves?listId=%d", list.ID), nil, true)
	var shelves []Shelf
	decodeJSON(t, resp, &shelves)
	if len(shelves) != 0 {
		t.Fatalf("expected 0 shelves, got %d", len(shelves))
	}

	// Verify item unreachable via barcode (we created it without a barcode,
	// but the item should be deleted since it only existed on this shelf)
	req, _ := http.NewRequest("GET", ts.URL+"/api/items?showOutOfStock=true", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var items []Item
	decodeJSON(t, resp, &items)
	for _, i := range items {
		if i.ID == item.ID {
			t.Fatal("item should be deleted")
		}
	}
}

func TestListScoping(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// Create a second list
	resp := doJSON(t, ts, "POST", "/api/lists", map[string]interface{}{
		"name": "Pantry",
	}, true)
	var pantry List
	decodeJSON(t, resp, &pantry)

	// Create shelf on Pantry
	resp = doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Pantry Shelf", "listId": pantry.ID,
	}, true)

	// Create item on Freezer (list 1)
	resp = doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Ice Cream", "quantity": 2, "shelfId": 1,
	}, true)

	// Verify shelves for Freezer only
	req, _ := http.NewRequest("GET", ts.URL+"/api/shelves?listId=1", nil)
	req.AddCookie(authCookie())
	resp, _ = http.DefaultClient.Do(req)
	var shelves []Shelf
	decodeJSON(t, resp, &shelves)
	for _, s := range shelves {
		if s.ListID != 1 {
			t.Fatalf("expected list 1 only, got shelf on list %d", s.ListID)
		}
	}
}
