import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

// Helper: find an item by barcode string (works with both flat strings and objects)
const findByBarcode = (db, barcode) =>
  db.find((i) => i.barcodes && i.barcodes.some((b) => (typeof b === "string" ? b : b.barcode) === barcode));

// Out-of-stock items are searchable for restock (zero shelf rows). The shared
// ITEMS fixture deliberately omits Bacon so other specs' row expectations
// stay untouched — build the item set per-test instead.
const itemsWithBacon = () => {
  const items = cloneItems();
  items.push({ id: 5, name: "Bacon", barcodes: [] });
  return items;
};

test.describe("Manual Add Form", () => {
  test("adds a new item without barcode", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("Pizza Rolls");
    await page.getByRole("button", { name: "Add Item" }).click();

    await expect(page.getByText('Added "Pizza Rolls"')).toBeVisible();
    await expect(page.getByText("Pizza Rolls").first()).toBeVisible();
  });

  test("adds a new item with a barcode", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("Beef Steak");
    await page.getByPlaceholder("Optional").fill("99999");
    await page.getByRole("button", { name: "Add Item" }).click();

    await expect(page.getByText("Beef Steak").first()).toBeVisible();
  });

  test("adds with custom quantity", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("Family Meal");
    await page.locator(".manual-add-form input[type='number']").fill("7");
    await page.getByRole("button", { name: "Add Item" }).click();

    await expect(page.getByText("Family Meal").first()).toBeVisible();
  });

  test("shows validation error for empty name", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("");
    await page.getByRole("button", { name: "Add Item" }).click();

    await expect(page.getByText('Added "')).not.toBeVisible();
  });

  test("shows duplicate offer when barcode already exists", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("Another Chicken");
    await page.getByPlaceholder("Optional").fill("12345");
    await page.getByRole("button", { name: "Add Item" }).click();

    const offer = page.locator(".duplicate-offer");
    await expect(offer.getByText(/already exists/)).toBeVisible();
    await expect(offer.locator("p").getByText(/Chicken Breast/)).toBeVisible();
    await expect(offer.getByRole("button").first()).toBeVisible();
    await expect(offer.getByRole("button", { name: "Cancel" })).toBeVisible();
  });

  test("resolve duplicate offer via increment", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("Another Chicken");
    await page.getByPlaceholder("Optional").fill("12345");
    await page.locator(".manual-add-form input[type='number']").fill("3");
    await page.getByRole("button", { name: "Add Item" }).click();

    await page.locator(".duplicate-offer").getByRole("button").first().click();
    // Status message confirms the duplicate was resolved
    await expect(page.getByText('Updated "Chicken Breast"')).toBeVisible();
  });

  test("dismiss duplicate offer", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("Another Chicken");
    await page.getByPlaceholder("Optional").fill("12345");
    await page.getByRole("button", { name: "Add Item" }).click();

    await page.locator(".duplicate-offer").getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByText(/already exists/)).not.toBeVisible();

    // Table must still render the item list after dismissing the offer.
    await expect(page.getByText("Chicken Breast").first()).toBeVisible();
  });

  test("clears form after successful add", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("Test Clear");
    await page.getByPlaceholder("Optional").fill("test-clear");
    await page.getByRole("button", { name: "Add Item" }).click();

    await expect(page.getByPlaceholder("e.g. Chicken Breast")).toHaveValue("");
    await expect(page.getByPlaceholder("Optional")).toHaveValue("");
  });
});

test.describe("Manual Add — Restock Autocomplete", () => {
  test("typing shows one debounced search request and a dropdown with counts", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    const searchCalls = [];
    page.on("request", (req) => {
      if (req.url().includes("/api/search-items")) searchCalls.push(req.url());
    });
    await authenticate(page);

    const nameInput = page.getByPlaceholder("e.g. Chicken Breast");
    await nameInput.pressSequentially("chick", { delay: 20 });

    await expect(page.getByRole("button", { name: "Chicken Breast (3 in stock)" })).toBeVisible();
    // The debounce coalesces the per-keystroke input events into one request.
    expect(searchCalls.length).toBe(1);
    expect(searchCalls[0]).toContain("q=chick");
  });

  test("selecting an out-of-stock item enters restock mode", async ({ page }) => {
    await setupApiMocks(page, itemsWithBacon());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("bacon");
    await page.getByRole("button", { name: "Bacon (out of stock)" }).click();

    // Name input is replaced by the fixed chip; barcode field is hidden.
    await expect(page.locator(".restock-chip")).toContainText("Bacon");
    await expect(page.getByRole("button", { name: "Restock", exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Add Item", exact: true })).toHaveCount(0);
    await expect(page.getByPlaceholder("Optional")).toHaveCount(0);
  });

  test("submitting a restock posts the exact body and resets the form", async ({ page }) => {
    await setupApiMocks(page, itemsWithBacon());
    await authenticate(page);

    let restockBody = null;
    page.on("request", (req) => {
      if (req.url().includes("/api/item/restock")) restockBody = req.postDataJSON();
    });

    await page.getByPlaceholder("e.g. Chicken Breast").fill("bacon");
    await page.getByRole("button", { name: "Bacon (out of stock)" }).click();
    await page.locator(".manual-add-form input[type='number']").fill("2");
    await page.getByRole("button", { name: "Restock", exact: true }).click();

    await expect(page.getByText('Restocked "Bacon"')).toBeVisible();
    // Exact wire body: itemId + quantity + shelfId — nothing defaulted.
    await expect.poll(() => restockBody).toEqual({ itemId: 5, quantity: 2, shelfId: 1 });

    // Success auto-exits restock mode and resets name/qty.
    await expect(page.locator(".restock-chip")).toHaveCount(0);
    await expect(page.getByPlaceholder("e.g. Chicken Breast")).toHaveValue("");
    await expect(page.locator(".manual-add-form input[type='number']")).toHaveValue("1");
    await expect(page.getByPlaceholder("Optional")).toBeVisible();
  });

  test("restock failure keeps the chip for retry", async ({ page }) => {
    await setupApiMocks(page, itemsWithBacon());
    // Registered after the fixture, so this shadowing route wins.
    await page.route("**/api/item/restock", (route) => {
      route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "item not found" }),
      });
    });
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("bacon");
    await page.getByRole("button", { name: "Bacon (out of stock)" }).click();
    await page.getByRole("button", { name: "Restock", exact: true }).click();

    await expect(page.getByText("item not found")).toBeVisible();
    // Stay in restock mode: chip and × remain for a retry.
    await expect(page.locator(".restock-chip")).toContainText("Bacon");
    await expect(page.getByRole("button", { name: "Exit restock" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Restock", exact: true })).toBeVisible();
    await expect(page.getByPlaceholder("Optional")).toHaveCount(0);
  });

  test("x exits restock mode and restores the typed query", async ({ page }) => {
    await setupApiMocks(page, itemsWithBacon());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("bacon");
    await page.getByRole("button", { name: "Bacon (out of stock)" }).click();
    await expect(page.locator(".restock-chip")).toBeVisible();

    await page.getByRole("button", { name: "Exit restock" }).click();

    await expect(page.locator(".restock-chip")).toHaveCount(0);
    await expect(page.getByPlaceholder("e.g. Chicken Breast")).toHaveValue("bacon");
    await expect(page.getByRole("button", { name: "Add Item", exact: true })).toBeVisible();
    // Dropdown re-opens with the previous query's results.
    await expect(page.getByRole("button", { name: "Bacon (out of stock)" })).toBeVisible();
  });

  test("unmatched name still creates a new item", async ({ page }) => {
    await setupApiMocks(page, itemsWithBacon());
    await authenticate(page);

    let restockCalled = false;
    page.on("request", (req) => {
      if (req.url().includes("/api/item/restock")) restockCalled = true;
    });

    await page.getByPlaceholder("e.g. Chicken Breast").fill("Pizza Rolls");
    await expect(page.getByText('No items found for "Pizza Rolls"')).toBeVisible();
    await page.getByRole("button", { name: "Add Item", exact: true }).click();

    await expect(page.getByText('Added "Pizza Rolls"')).toBeVisible();
    await expect(page.getByPlaceholder("e.g. Chicken Breast")).toHaveValue("");
    expect(restockCalled).toBe(false);
  });

  test("Escape and click-outside close the dropdown", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const nameInput = page.getByPlaceholder("e.g. Chicken Breast");
    await nameInput.fill("chick");
    await expect(page.locator(".manual-add-dropdown")).toBeVisible();

    await nameInput.press("Escape");
    await expect(page.locator(".manual-add-dropdown")).toHaveCount(0);

    // Typing again reopens it; clicking outside closes it.
    await nameInput.pressSequentially("e", { delay: 20 });
    await expect(page.locator(".manual-add-dropdown")).toBeVisible();
    await page.getByText("Manual Add", { exact: true }).click();
    await expect(page.locator(".manual-add-dropdown")).toHaveCount(0);
  });
});