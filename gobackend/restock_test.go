package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// ── Restock endpoint (POST /api/item/restock) ──────────────────────────

// TestRestockCreatesRowForOutOfStockItem covers the core recovery flow:
// an item with zero shelf rows (all counts zeroed → rows deleted by
// design) gets a fresh row with exactly the requested quantity.
func TestRestockCreatesRowForOutOfStockItem(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Restock OOS", "barcode": "RESTOCK-OOS-1", "quantity": 2, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fixture: expected 201, got %d", resp.StatusCode)
	}
	var item Item
	decodeJSON(t, resp, &item)

	// Decrement to zero — the ItemShelf row is DELETED at count 0.
	resp = doJSON(t, ts, "POST", "/api/item/scan", map[string]interface{}{
		"barcode": "RESTOCK-OOS-1", "mode": "decrement", "quantity": 2, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scan to zero: expected 200, got %d", resp.StatusCode)
	}

	// Precondition: the item now has no shelf rows at all.
	check := doJSON(t, ts, "GET", "/api/item/RESTOCK-OOS-1", nil, true)
	var lookup map[string]interface{}
	decodeJSON(t, check, &lookup)
	pre := lookup["item"].(map[string]interface{})
	if shelves, _ := pre["shelves"].([]interface{}); len(shelves) != 0 {
		t.Fatalf("precondition failed: expected 0 shelf rows, got %d", len(shelves))
	}

	resp = doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
		"itemId": item.ID, "quantity": 5, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restock: expected 200, got %d", resp.StatusCode)
	}
	var updated Item
	decodeJSON(t, resp, &updated)
	if updated.ID != item.ID || updated.Name != "Restock OOS" {
		t.Fatalf("unexpected item identity in response: %+v", updated)
	}
	if len(updated.Shelves) != 1 {
		t.Fatalf("expected 1 shelf row after restock, got %+v", updated.Shelves)
	}
	if updated.Shelves[0].ShelfID != 1 || updated.Shelves[0].Count != 5 {
		t.Fatalf("expected shelf 1 count 5, got %+v", updated.Shelves[0])
	}
}

// TestRestockIncrementsExistingRow adds to an existing row and pins the
// response wire shape: the bare Item JSON (like create), not scan's
// {"action","item"} wrapper.
func TestRestockIncrementsExistingRow(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Restock Existing", "barcode": "RESTOCK-EX-1", "quantity": 3, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fixture: expected 201, got %d", resp.StatusCode)
	}
	var item Item
	decodeJSON(t, resp, &item)

	resp = doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
		"itemId": item.ID, "quantity": 4, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restock: expected 200, got %d", resp.StatusCode)
	}

	var m map[string]interface{}
	decodeJSON(t, resp, &m)
	if _, ok := m["item"]; ok {
		t.Fatalf("response must be the bare item, got wrapper key \"item\": %v", m)
	}
	if _, ok := m["action"]; ok {
		t.Fatalf("response must be the bare item, got wrapper key \"action\": %v", m)
	}
	if id, _ := m["id"].(float64); uint(id) != item.ID {
		t.Fatalf("expected id %d in response, got %v", item.ID, m["id"])
	}
	if m["name"] != "Restock Existing" {
		t.Fatalf("expected name in response, got %v", m["name"])
	}
	for _, key := range []string{"createdAt", "updatedAt", "barcodes", "shelves"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("response missing %q key: %v", key, m)
		}
	}
	barcodes := m["barcodes"].([]interface{})
	if len(barcodes) != 1 || barcodes[0].(map[string]interface{})["barcode"] != "RESTOCK-EX-1" {
		t.Fatalf("unexpected barcodes in response: %v", barcodes)
	}
	shelves := m["shelves"].([]interface{})
	if len(shelves) != 1 {
		t.Fatalf("expected 1 shelf row, got %v", shelves)
	}
	if count := int(shelves[0].(map[string]interface{})["count"].(float64)); count != 7 {
		t.Fatalf("expected count 7 after restock, got %d", count)
	}
}

func TestRestockUnknownItemReturns404(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
		"itemId": 999999, "quantity": 1, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["error"] != "item not found" {
		t.Fatalf("expected 'item not found', got %v", body["error"])
	}
}

func TestRestockUnknownShelfReturns400(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Restock Bad Shelf", "barcode": "RESTOCK-BAD-1", "quantity": 1, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fixture: expected 201, got %d", resp.StatusCode)
	}
	var item Item
	decodeJSON(t, resp, &item)

	resp = doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
		"itemId": item.ID, "quantity": 3, "shelfId": 99999,
	}, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	decodeJSON(t, resp, &body)
	if body["error"] != "shelf does not exist" {
		t.Fatalf("expected 'shelf does not exist', got %v", body["error"])
	}

	// The failed restock must not have created or bumped any shelf row.
	check := doJSON(t, ts, "GET", "/api/item/RESTOCK-BAD-1", nil, true)
	var lookup map[string]interface{}
	decodeJSON(t, check, &lookup)
	got := lookup["item"].(map[string]interface{})
	shelves := got["shelves"].([]interface{})
	if len(shelves) != 1 {
		t.Fatalf("expected original 1 shelf row, got %d", len(shelves))
	}
	if count := int(shelves[0].(map[string]interface{})["count"].(float64)); count != 1 {
		t.Fatalf("expected untouched count 1, got %d", count)
	}
}

func TestRestockInvalidQuantityReturns400(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Restock Qty", "quantity": 1, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fixture: expected 201, got %d", resp.StatusCode)
	}
	var item Item
	decodeJSON(t, resp, &item)

	for _, qty := range []int{0, 10000} {
		resp := doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
			"itemId": item.ID, "quantity": qty, "shelfId": 1,
		}, true)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("quantity %d: expected 400, got %d", qty, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestRestockWritesAuditLog(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Audited Restock", "quantity": 1, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fixture: expected 201, got %d", resp.StatusCode)
	}
	var item Item
	decodeJSON(t, resp, &item)

	resp = doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
		"itemId": item.ID, "quantity": 6, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restock: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	db := OpenDB()
	var logs []AuditLog
	if err := db.Where("action = ? AND entity_id = ?", "restock", item.ID).Find(&logs).Error; err != nil {
		t.Fatalf("audit query failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected exactly 1 restock audit row, got %d", len(logs))
	}
	log := logs[0]
	if log.EntityType != "item" {
		t.Fatalf("expected entity_type item, got %q", log.EntityType)
	}
	if log.EntityName != "Audited Restock" {
		t.Fatalf("expected entity_name %q, got %q", "Audited Restock", log.EntityName)
	}
	var details map[string]interface{}
	if err := json.Unmarshal([]byte(log.Details), &details); err != nil {
		t.Fatalf("audit details is not valid JSON: %q (%v)", log.Details, err)
	}
	if details["shelf"] != "Shelf 1" {
		t.Fatalf("expected details shelf %q, got %v", "Shelf 1", details["shelf"])
	}
	if qty, ok := details["quantity"].(float64); !ok || qty != 6 {
		t.Fatalf("expected details quantity 6, got %v", details["quantity"])
	}
}

func TestRestockRequiresAuth(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
		"itemId": 1, "quantity": 1, "shelfId": 1,
	}, false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRestockInvalidJSONReturns400(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/item/restock", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(authCookie())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRestockMissingIDsReturn400(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Restock IDs", "quantity": 1, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fixture: expected 201, got %d", resp.StatusCode)
	}
	var item Item
	decodeJSON(t, resp, &item)

	// itemId missing (0).
	resp = doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
		"quantity": 1, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing itemId: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// shelfId missing (0).
	resp = doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
		"itemId": item.ID, "quantity": 1,
	}, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing shelfId: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestRestockCreatesRowOnExplicitShelf pins that restock never falls back
// to the item's first shelf: the caller-provided shelf is the only target.
func TestRestockCreatesRowOnExplicitShelf(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doJSON(t, ts, "POST", "/api/item/create", map[string]interface{}{
		"name": "Restock Multi", "quantity": 3, "shelfId": 1,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create fixture: expected 201, got %d", resp.StatusCode)
	}
	var item Item
	decodeJSON(t, resp, &item)

	resp = doJSON(t, ts, "POST", "/api/shelves", map[string]interface{}{
		"name": "Restock Shelf 2", "listId": 1,
	}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create shelf: expected 201, got %d", resp.StatusCode)
	}
	var shelf Shelf
	decodeJSON(t, resp, &shelf)

	resp = doJSON(t, ts, "POST", "/api/item/restock", map[string]interface{}{
		"itemId": item.ID, "quantity": 4, "shelfId": shelf.ID,
	}, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restock: expected 200, got %d", resp.StatusCode)
	}
	var updated Item
	decodeJSON(t, resp, &updated)
	counts := map[uint]int{}
	for _, s := range updated.Shelves {
		counts[s.ShelfID] = s.Count
	}
	if len(updated.Shelves) != 2 || counts[1] != 3 || counts[shelf.ID] != 4 {
		t.Fatalf("expected shelf 1=3 and shelf %d=4, got %+v", shelf.ID, updated.Shelves)
	}
}
