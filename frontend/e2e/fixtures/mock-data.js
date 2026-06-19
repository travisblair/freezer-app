// ── Mock Data Fixtures (matches Go/GORM API with shelves model) ──────

import { expect } from "@playwright/test";

// Shelves for list 1 (Freezer)
const SHELVES = [
  { id: 1, name: "Shelf 1", listId: 1 },
];

// Lists
const LISTS = [
  { id: 1, name: "Freezer" },
];

let nextShelfId = 2;
let nextItemShelfId = 100;

// ITEMS use the shelves array model matching ItemShelf[]
const ITEMS = [
  { id: 1, name: "Chicken Breast", barcodes: [{ barcode: "12345" }, { barcode: "12346" }] },
  { id: 2, name: "Frozen Peas", barcodes: [] },
  { id: 3, name: "Ice Cream", barcodes: [{ barcode: "67890" }] },
  { id: 4, name: "Salmon Fillet", barcodes: [{ barcode: "11111" }] },
];

// ItemShelf entries (linking items to shelves with counts)
const ITEM_SHELVES = [
  { id: 1, itemId: 1, shelfId: 1, count: 3 },
  { id: 2, itemId: 2, shelfId: 1, count: 5 },
  { id: 3, itemId: 3, shelfId: 1, count: 0 },
  { id: 4, itemId: 4, shelfId: 1, count: 2 },
];

// Helper: attach shelves data to an item
const withShelves = (item) => {
  const shelves = ITEM_SHELVES.filter((is) => is.itemId === item.id);
  return { ...item, shelves: shelves.map((s) => ({ ...s })) };
};

// Helper: total count across all shelves for an item
const totalCount = (itemId) =>
  ITEM_SHELVES.filter((s) => s.itemId === itemId).reduce((sum, s) => sum + s.count, 0);

/** Deep-clone the initial state. Used to reset the mock DB per test. */
export function cloneItems() {
  return ITEMS.map((i) => ({
    ...i,
    barcodes: i.barcodes.map((b) => ({ ...b })),
  }));
}

/** Standard authentication helper — assumes mock returns authenticated:true. */
export async function authenticate(page) {
  await page.goto("/");
  // Auth check returns authenticated:true by default, so app renders immediately
  // h1 only renders with single list; select renders with multiple
    await expect(page.locator("h1, select.list-select").first()).toBeVisible({ timeout: 10000 });
}

/** Helper: find an item by barcode string */
const findItemByBarcode = (db, bc) =>
  db.find((i) => i.barcodes && i.barcodes.some((b) => b.barcode === bc));

/** Helper: check if any item has a given barcode */
const hasBarcode = (db, bc) => !!findItemByBarcode(db, bc);

function escapeCsv(val) {
  const s = val == null ? "" : String(val);
  if (/[",\n\r]/.test(s)) return `"${s.replace(/"/g, '""')}"`;
  return s;
}

/**
 * Set up standard API mock routes.
 * Default: authenticated. Override /api/auth/check to test unauthenticated flows.
 */
export async function setupApiMocks(page, initialItems = null) {
  const dbItems = initialItems ?? cloneItems();

  // Reset mutable state
  SHELVES.length = 1;
  SHELVES[0] = { id: 1, name: "Shelf 1", listId: 1 };
  nextShelfId = 2;
  nextItemShelfId = 100;
  ITEM_SHELVES.length = 0;
  ITEM_SHELVES.push(
    { id: 1, itemId: 1, shelfId: 1, count: 3 },
    { id: 2, itemId: 2, shelfId: 1, count: 5 },
    { id: 3, itemId: 3, shelfId: 1, count: 0 },
    { id: 4, itemId: 4, shelfId: 1, count: 2 },
  );

  let nextId = Math.max(0, ...dbItems.map((i) => i.id)) + 1;

  // ── Auth ────────────────────────────────────────────────────────────
  await page.route("**/api/auth/check", (route) => {
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ authenticated: true }),
    });
  });

  await page.route("**/api/auth", (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const body = route.request().postDataJSON();
    if (body.email === "test@test.com" && body.password === "test") {
      route.fulfill({
        status: 200,
        headers: {
          "set-cookie":
            "__Host-freezer_token=test-session; Path=/; HttpOnly; Max-Age=31536000; SameSite=Strict",
        },
        contentType: "application/json",
        body: JSON.stringify({ ok: true }),
      });
    } else {
      route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ error: "Unauthorized" }),
      });
    }
  });

  await page.route("**/api/auth/logout", (route) => {
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ ok: true }),
    });
  });

  // ── Items ───────────────────────────────────────────────────────────
  await page.route("**/api/items**", (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    const url = new URL(route.request().url());
    const showOutOfStock = url.searchParams.get("showOutOfStock") === "true";
    const search = (url.searchParams.get("search") || "").toLowerCase();

    let results = dbItems
      .map(withShelves)
      .filter((i) => showOutOfStock || totalCount(i.id) > 0);

    if (search) {
      results = results.filter((i) => i.name.toLowerCase().includes(search));
    }
    results.sort((a, b) => a.name.localeCompare(b.name));

    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(results),
    });
  });

  // GET /api/item/:barcode
  await page.route(/\/api\/item\/(?!bulk-delete|create|scan|link-barcode|hard)([A-Za-z0-9_-]+)$/, (route) => {
    const barcode = route.request().url().split("/").pop();
    const item = findItemByBarcode(dbItems, barcode);
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        item ? { found: true, item: withShelves(item) } : { found: false },
      ),
    });
  });

  // GET /api/search-items?q=
  await page.route("**/api/search-items**", (route) => {
    if (route.request().method() !== "GET") return route.fallback();
    const url = new URL(route.request().url());
    const q = (url.searchParams.get("q") || "").toLowerCase();
    const results = dbItems
      .filter((i) => totalCount(i.id) > 0 && i.name.toLowerCase().includes(q))
      .slice(0, 10)
      .map(withShelves);
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(results),
    });
  });

  // POST /api/item/scan
  await page.route("**/api/item/scan", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const { barcode, mode, quantity, shelfId } = route.request().postDataJSON();
    const item = findItemByBarcode(dbItems, barcode);

    if (!item) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ action: "create", barcode }),
      });
    }

    const targetShelfId = shelfId || 1;
    const delta = mode === "decrement" ? -quantity : quantity;

    let is = ITEM_SHELVES.find(
      (s) => s.itemId === item.id && s.shelfId === targetShelfId,
    );
    if (!is) {
      is = {
        id: nextItemShelfId++,
        itemId: item.id,
        shelfId: targetShelfId,
        count: 0,
      };
      ITEM_SHELVES.push(is);
    }
    is.count = Math.max(0, is.count + delta);

    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ action: "updated", item: withShelves(item) }),
    });
  });

  // POST /api/item/create
  await page.route("**/api/item/create", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const body = route.request().postDataJSON();

    if (!body.name || body.name.trim() === "" || body.name.length > 100) {
      return route.fulfill({
        status: 400,
        contentType: "application/json",
        body: JSON.stringify({ error: "Invalid name" }),
      });
    }
    if (!body.quantity || body.quantity < 1 || body.quantity > 9999) {
      return route.fulfill({
        status: 400,
        contentType: "application/json",
        body: JSON.stringify({ error: "Invalid quantity" }),
      });
    }

    if (body.barcode && hasBarcode(dbItems, body.barcode)) {
      const existing = findItemByBarcode(dbItems, body.barcode);
      return route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({
          error: "Barcode exists",
          item: withShelves(existing),
        }),
      });
    }

    const barcodes = body.barcode ? [{ barcode: body.barcode.trim() }] : [];
    const item = { id: nextId++, name: body.name.trim(), barcodes };
    dbItems.push(item);

    const newIs = {
      id: nextItemShelfId++,
      itemId: item.id,
      shelfId: body.shelfId || 1,
      count: body.quantity,
    };
    ITEM_SHELVES.push(newIs);

    route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({
        ...item,
        shelves: [newIs],
      }),
    });
  });

  // POST /api/item/link-barcode
  await page.route("**/api/item/link-barcode", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const { itemId, barcode } = route.request().postDataJSON();
    const item = dbItems.find((i) => i.id === itemId);

    if (!item) {
      return route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "item not found" }),
      });
    }
    if (hasBarcode(dbItems, barcode)) {
      return route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({ error: "Barcode already linked" }),
      });
    }

    item.barcodes.push({ barcode });
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(withShelves(item)),
    });
  });

  // PATCH /api/item/:id
  await page.route(/\/api\/item\/\d+$/, async (route) => {
    if (route.request().method() !== "PATCH") return route.fallback();
    const id = parseInt(route.request().url().split("/").pop(), 10);
    const body = route.request().postDataJSON();
    const item = dbItems.find((i) => i.id === id);

    if (!item) {
      return route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "Not found" }),
      });
    }

    if (body.name !== undefined) item.name = body.name.trim();

    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(withShelves(item)),
    });
  });

  // POST /api/items/bulk-delete
  await page.route("**/api/items/bulk-delete", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const { ids } = route.request().postDataJSON();
    if (!ids || ids.length === 0) {
      return route.fulfill({
        status: 400,
        contentType: "application/json",
        body: JSON.stringify({ error: "No ids provided" }),
      });
    }
    let deleted = 0;
    for (const is of ITEM_SHELVES) {
      if (ids.includes(is.itemId)) {
        is.count = 0;
        deleted++;
      }
    }
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ deleted }),
    });
  });

  // DELETE /api/item/:barcode
  await page.route(/\/api\/item\/([A-Za-z0-9_-]+)$/, async (route) => {
    if (route.request().method() !== "DELETE") return route.fallback();
    const barcode = route.request().url().split("/").pop();
    const item = findItemByBarcode(dbItems, barcode);
    if (!item) {
      return route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "Not found" }),
      });
    }
    for (const is of ITEM_SHELVES) {
      if (is.itemId === item.id) is.count = 0;
    }
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ deleted: true }),
    });
  });

  // DELETE /api/item/hard/:id
  await page.route(/\/api\/item\/hard\/\d+$/, async (route) => {
    if (route.request().method() !== "DELETE") return route.fallback();
    const id = parseInt(route.request().url().split("/").pop(), 10);
    const idx = dbItems.findIndex((i) => i.id === id);
    if (idx === -1) {
      return route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "Not found" }),
      });
    }
    dbItems.splice(idx, 1);
    // Remove ItemShelf rows
    for (let j = ITEM_SHELVES.length - 1; j >= 0; j--) {
      if (ITEM_SHELVES[j].itemId === id) ITEM_SHELVES.splice(j, 1);
    }
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ deleted: true, hard: true }),
    });
  });

  // ── Shelves ─────────────────────────────────────────────────────────
  await page.route("**/api/shelves**", (route) => {
    if (route.request().method() === "GET") {
      const url = new URL(route.request().url());
      const listId = url.searchParams.get("listId");
      let results = SHELVES;
      if (listId) results = SHELVES.filter((s) => s.listId === Number(listId));
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(results),
      });
    } else if (route.request().method() === "POST") {
      const body = route.request().postDataJSON();
      const shelf = { id: nextShelfId++, name: body.name, listId: body.listId || 1 };
      SHELVES.push(shelf);
      route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify(shelf),
      });
    } else {
      route.fallback();
    }
  });

  // PATCH/DELETE /api/shelf/:id
  await page.route(/\/api\/shelf\/\d+$/, async (route) => {
    const id = parseInt(route.request().url().split("/").pop(), 10);
    if (route.request().method() === "PATCH") {
      const body = route.request().postDataJSON();
      const shelf = SHELVES.find((s) => s.id === id);
      if (!shelf) {
        return route.fulfill({
          status: 404,
          contentType: "application/json",
          body: JSON.stringify({ error: "Not found" }),
        });
      }
      shelf.name = body.name;
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(shelf),
      });
    } else if (route.request().method() === "DELETE") {
      if (id === 1) {
        return route.fulfill({
          status: 400,
          contentType: "application/json",
          body: JSON.stringify({ error: "cannot delete the default shelf" }),
        });
      }
      // Move items to Shelf 1
      for (const is of ITEM_SHELVES) {
        if (is.shelfId === id) {
          const existing = ITEM_SHELVES.find(
            (s) => s.itemId === is.itemId && s.shelfId === 1,
          );
          if (existing) {
            existing.count += is.count;
          } else {
            is.shelfId = 1;
          }
        }
      }
      // Remove if moved to existing (marked by keeping ref)
      for (let j = ITEM_SHELVES.length - 1; j >= 0; j--) {
        if (
          ITEM_SHELVES[j].shelfId === id ||
          (ITEM_SHELVES[j].shelfId === 1 &&
            ITEM_SHELVES.filter(
              (s) => s.itemId === ITEM_SHELVES[j].itemId && s.shelfId === id,
            ).length > 0)
        ) {
          // Actually, simpler: just delete shelf and update items on it
        }
      }
      // Simplify: just delete the shelf
      const idx = SHELVES.findIndex((s) => s.id === id);
      if (idx !== -1) SHELVES.splice(idx, 1);
      // Move items to Shelf 1
      for (const is of ITEM_SHELVES) {
        if (is.shelfId === id) {
          const existing = ITEM_SHELVES.find(
            (s) => s.itemId === is.itemId && s.shelfId === 1,
          );
          if (existing) {
            existing.count += is.count;
          } else {
            is.id = nextItemShelfId++;
            is.shelfId = 1;
          }
        }
      }
      // Remove stale ItemShelf rows for deleted shelf
      for (let j = ITEM_SHELVES.length - 1; j >= 0; j--) {
        if (ITEM_SHELVES[j].shelfId === id) ITEM_SHELVES.splice(j, 1);
      }
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ deleted: true }),
      });
    } else {
      route.fallback();
    }
  });

  // ── ItemShelf ───────────────────────────────────────────────────────
  // PATCH /api/item-shelf/:id
  await page.route(/\/api\/item-shelf\/\d+$/, async (route) => {
    const id = parseInt(route.request().url().split("/").pop(), 10);
    if (route.request().method() === "PATCH") {
      const body = route.request().postDataJSON();
      const is = ITEM_SHELVES.find((s) => s.id === id);
      if (!is) {
        return route.fulfill({
          status: 404,
          contentType: "application/json",
          body: JSON.stringify({ error: "Not found" }),
        });
      }
      is.count = body.count;
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ id: is.id, count: is.count }),
      });
    } else {
      route.fallback();
    }
  });

  // POST /api/item-shelf/move
  await page.route("**/api/item-shelf/move", async (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    const { itemId, sourceShelfId, targetShelfId, quantity } =
      route.request().postDataJSON();
    const source = ITEM_SHELVES.find(
      (s) => s.itemId === itemId && s.shelfId === sourceShelfId,
    );
    if (!source) {
      return route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "Not found" }),
      });
    }
    const qty = Math.min(quantity, source.count);
    source.count -= qty;

    const target = ITEM_SHELVES.find(
      (s) => s.itemId === itemId && s.shelfId === targetShelfId,
    );
    if (target) {
      target.count += qty;
    } else {
      ITEM_SHELVES.push({
        id: nextItemShelfId++,
        itemId,
        shelfId: targetShelfId,
        count: qty,
      });
    }
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ moved: qty }),
    });
  });

  // ── Lists ───────────────────────────────────────────────────────────
  await page.route("**/api/lists", (route) => {
    if (route.request().method() === "GET") {
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(LISTS),
      });
    } else if (route.request().method() === "POST") {
      const body = route.request().postDataJSON();
      const list = {
        id: Math.max(0, ...LISTS.map((l) => l.id)) + 1,
        name: body.name,
      };
      LISTS.push(list);
      route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify(list),
      });
    } else {
      route.fallback();
    }
  });

  await page.route(/\/api\/lists\/\d+$/, async (route) => {
    const id = parseInt(route.request().url().split("/").pop(), 10);
    if (route.request().method() === "PATCH") {
      const body = route.request().postDataJSON();
      const list = LISTS.find((l) => l.id === id);
      if (!list) {
        return route.fulfill({
          status: 404,
          contentType: "application/json",
          body: JSON.stringify({ error: "Not found" }),
        });
      }
      list.name = body.name;
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(list),
      });
    } else if (route.request().method() === "DELETE") {
      const idx = LISTS.findIndex((l) => l.id === id);
      if (idx !== -1) LISTS.splice(idx, 1);
      // Delete shelves on this list
      for (let j = SHELVES.length - 1; j >= 0; j--) {
        if (SHELVES[j].listId === id) SHELVES.splice(j, 1);
      }
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ deleted: true }),
      });
    } else {
      route.fallback();
    }
  });

  // ── Export ──────────────────────────────────────────────────────────
  await page.route("**/api/export", (route) => {
    const rows = dbItems.map((i) => {
      const bcStrs = i.barcodes.map((b) => b.barcode);
      const count = totalCount(i.id);
      return `${i.id},${escapeCsv(i.name)},${count},${bcStrs.join("|")}`;
    });
    const csv = ["id,name,count,barcodes", ...rows].join("\n") + "\n";
    route.fulfill({ status: 200, contentType: "text/csv", body: csv });
  });

  // ── Health ──────────────────────────────────────────────────────────
  await page.route("**/api/health", (route) => {
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ status: "ok" }),
    });
  });

  return dbItems;
}
