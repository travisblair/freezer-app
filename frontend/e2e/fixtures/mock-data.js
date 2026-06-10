// ── Mock Data Fixtures (matches real Go/GORM API response shapes) ─────

import { expect } from "@playwright/test";

// Barcode helper: returns [ { barcode: "x" }, ... ] like GORM Preload
const toObj = (arr) => arr.map((b) => ({ barcode: b }));

export const ITEMS = [
  { id: 1, name: "Chicken Breast", count: 3, deleted: 0, barcodes: toObj(["12345", "12346"]) },
  { id: 2, name: "Frozen Peas", count: 5, deleted: 0, barcodes: [] },
  { id: 3, name: "Ice Cream", count: 0, deleted: 1, barcodes: toObj(["67890"]) },
  { id: 4, name: "Salmon Fillet", count: 2, deleted: 0, barcodes: toObj(["11111"]) },
];

export const CSV_HEADER = "id,name,count,deleted,barcodes";

export function cloneItems() {
  return ITEMS.map((i) => ({ ...i, barcodes: [...i.barcodes] }));
}

/** Standard authentication helper. */
export async function authenticate(page) {
  await page.goto("/");
  await expect(page.getByText("🧊 Freezer Inventory")).toBeVisible();
}

/** Helper: setup standard API mock routes on a page. */
export async function setupApiMocks(page, initialItems = null) {
  const db = initialItems ?? cloneItems();
  let nextId = Math.max(0, ...db.map((i) => i.id)) + 1;

  // Helper: check if any item has a given barcode
  const hasBarcode = (bc) => db.some((i) =>
    i.barcodes.some((b) => (typeof b === "string" ? b : b.barcode) === bc)
  );
  const findItemByBarcode = (bc) => db.find((i) =>
    i.barcodes.some((b) => (typeof b === "string" ? b : b.barcode) === bc)
  );

  // GET /api/auth/check
  await page.route("**/api/auth/check", (route) => {
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ authenticated: true }) });
  });

  // POST /api/auth
  await page.route("**/api/auth", (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const body = route.request().postDataJSON();
    if (body.token === "test-token-123") {
      route.fulfill({
        status: 200,
        headers: { "set-cookie": "freezer_token=test-token-123; Path=/; HttpOnly; Max-Age=31536000" },
        contentType: "application/json",
        body: JSON.stringify({ ok: true }),
      });
    } else {
      route.fulfill({ status: 401, contentType: "application/json", body: JSON.stringify({ error: "Unauthorized" }) });
    }
  });

  // GET /api/items
  await page.route("**/api/items**", (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    const url = new URL(route.request().url());
    const showDeleted = url.searchParams.get("showDeleted") === "true";
    const search = (url.searchParams.get("search") || "").toLowerCase();

    let results = db.filter((i) => showDeleted || !i.deleted);
    if (search) results = results.filter((i) => i.name.toLowerCase().includes(search));
    results.sort((a, b) => a.name.localeCompare(b.name));

    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(results) });
  });

  // GET /api/item/:barcode
  await page.route(/\/api\/item\/(?!bulk-delete|create|scan|link-barcode)([A-Za-z0-9_-]+)$/, (route) => {
    const barcode = route.request().url().split("/").pop();
    const item = findItemByBarcode(barcode);
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(item ? { found: true, item } : { found: false }),
    });
  });

  // GET /api/search-items?q=
  await page.route("**/api/search-items**", (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    const url = new URL(route.request().url());
    const q = (url.searchParams.get("q") || "").toLowerCase();
    const results = db.filter((i) => !i.deleted && i.name.toLowerCase().includes(q)).slice(0, 10);
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(results) });
  });

  // POST /api/item/scan
  await page.route("**/api/item/scan", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const { barcode, mode, quantity } = route.request().postDataJSON();
    const item = findItemByBarcode(barcode);

    if (!item) {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ action: "create", barcode }) });
    }

    const delta = mode === "decrement" ? -quantity : quantity;
    item.count = Math.max(0, item.count + delta);
    item.deleted = item.count === 0 ? 1 : 0;

    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ action: "updated", item: { ...item, barcodes: [...item.barcodes] } }),
    });
  });

  // POST /api/item/create
  await page.route("**/api/item/create", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const body = route.request().postDataJSON();

    if (!body.name || body.name.trim() === "" || body.name.length > 100) {
      return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: "Invalid name" }) });
    }
    if (!body.quantity || body.quantity < 1 || body.quantity > 9999) {
      return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: "Invalid quantity" }) });
    }

    if (body.barcode && hasBarcode(body.barcode)) {
      const existing = findItemByBarcode(body.barcode);
      return route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({ error: "Barcode exists", item: existing }),
      });
    }

    const barcodes = body.barcode ? [{ barcode: body.barcode.trim() }] : [];
    const item = { id: nextId++, name: body.name.trim(), count: body.quantity, deleted: 0, barcodes };
    db.push(item);

    route.fulfill({ status: 201, contentType: "application/json", body: JSON.stringify(item) });
  });

  // POST /api/item/link-barcode
  await page.route("**/api/item/link-barcode", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const { itemId, barcode } = route.request().postDataJSON();
    const item = db.find((i) => i.id === itemId);

    if (!item) {
      return route.fulfill({ status: 404, contentType: "application/json", body: JSON.stringify({ error: "item not found" }) });
    }
    if (hasBarcode(barcode)) {
      return route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ error: "Barcode already linked" }) });
    }

    item.barcodes.push({ barcode });
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...item, barcodes: [...item.barcodes] }) });
  });

  // PATCH /api/item/:id
  await page.route(/\/api\/item\/\d+$/, async (route) => {
    if (route.request().method() !== "PATCH") return route.fallback();
    const id = parseInt(route.request().url().split("/").pop(), 10);
    const body = route.request().postDataJSON();
    const item = db.find((i) => i.id === id);

    if (!item) {
      return route.fulfill({ status: 404, contentType: "application/json", body: JSON.stringify({ error: "Not found" }) });
    }

    if (body.name !== undefined) item.name = body.name.trim();
    if (body.count !== undefined) {
      item.count = Math.max(0, body.count);
      item.deleted = item.count === 0 ? 1 : 0;
    }

    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...item, barcodes: [...item.barcodes] }) });
  });

  // POST /api/items/bulk-delete
  await page.route("**/api/items/bulk-delete", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const { ids } = route.request().postDataJSON();
    if (!ids || ids.length === 0) {
      return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: "No ids provided" }) });
    }
    let deleted = 0;
    for (const id of ids) {
      const item = db.find((i) => i.id === id);
      if (item) { item.count = 0; item.deleted = 1; deleted++; }
    }
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ deleted }) });
  });

  // DELETE /api/item/:barcode
  await page.route(/\/api\/item\/([A-Za-z0-9_-]+)$/, async (route) => {
    if (route.request().method() !== "DELETE") return route.fallback();
    const barcode = route.request().url().split("/").pop();
    const item = findItemByBarcode(barcode);
    if (!item) {
      return route.fulfill({ status: 404, contentType: "application/json", body: JSON.stringify({ error: "Not found" }) });
    }
    item.count = 0;
    item.deleted = 1;
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ deleted: true }) });
  });

  // GET /api/export
  await page.route("**/api/export", (route) => {
    const rows = db.map((i) => {
      const bcStrs = i.barcodes.map((bc) => (typeof bc === "string" ? bc : bc.barcode));
      return `${i.id},${escapeCsv(i.name)},${i.count},${i.deleted},${bcStrs.join("|")}`;
    });
    const csv = [CSV_HEADER, ...rows].join("\n") + "\n";
    route.fulfill({ status: 200, contentType: "text/csv", body: csv });
  });

  return db;
}

function escapeCsv(val) {
  const s = val == null ? "" : String(val);
  if (/[",\n\r]/.test(s)) return `"${s.replace(/"/g, '""')}"`;
  return s;
}