import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";
import { openKebab, clickKebabItem } from "./fixtures/kebab-helpers.js";

function itemRow(page, name) {
  return page.getByRole("row", { name: new RegExp(name) });
}

test.describe("Full User Flow (end-to-end)", () => {
  test.fixme("complete journey: add items, duplicate resolution, hard delete, restore, bulk delete, export", async ({ page }) => {
    // ── Auth ────────────────────────────────────────────────────────────
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Initial items: Chicken Breast (3), Frozen Peas (5), Salmon Fillet (2)
    await expect(page.getByText("Chicken Breast").first()).toBeVisible();
    await expect(page.getByText("Frozen Peas").first()).toBeVisible();
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();
    await expect(page.getByText("Ice Cream")).not.toBeVisible();

    // ── Add new item without barcode ────────────────────────────────────
    await page.getByPlaceholder("e.g. Chicken Breast").fill("Pizza Rolls");
    await page.getByRole("button", { name: "Add Item" }).click();

    await expect(page.getByText("Pizza Rolls").first()).toBeVisible();
    await expect(page.getByPlaceholder("e.g. Chicken Breast")).toHaveValue("");

    // ── Add new item with barcode and custom quantity ───────────────────
    await page.getByPlaceholder("e.g. Chicken Breast").fill("Beef Steak");
    await page.getByPlaceholder("Optional").fill("BEEF-001");
    await page.locator(".manual-add-form input[type='number']").fill("4");
    await page.getByRole("button", { name: "Add Item" }).click();

    await expect(page.getByText("Beef Steak").first()).toBeVisible();

    // ── Duplicate barcode ───────────────────────────────────────────────
    await page.getByPlaceholder("e.g. Chicken Breast").fill("Duplicate Chicken");
    await page.getByPlaceholder("Optional").fill("12345");
    await page.locator(".manual-add-form input[type='number']").fill("2");
    await page.getByRole("button", { name: "Add Item" }).click();

    const dupOffer = page.locator(".duplicate-offer");
    await expect(dupOffer.getByText(/already exists/)).toBeVisible();
    await expect(dupOffer.locator("em").filter({ hasText: "Chicken Breast" })).toBeVisible();

    await dupOffer.getByRole("button").first().click();

    // ── Hard delete via kebab ───────────────────────────────────────────
    const salmonRow = itemRow(page, "Salmon Fillet");
    await clickKebabItem(salmonRow, "Delete");

    const dangerDialog = page.locator(".modal-overlay");
    await expect(dangerDialog.getByText("Delete Forever")).toBeVisible();
    await expect(dangerDialog.getByText(/cannot be undone/)).toBeVisible();

    // Cancel first
    await dangerDialog.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();

    // Now actually hard delete
    await clickKebabItem(salmonRow, "Delete");
    const dangerDialog2 = page.locator(".modal-overlay");
    await dangerDialog2.getByRole("button", { name: "Delete Forever" }).click();
    await expect(page.getByText("Salmon Fillet")).not.toBeVisible();

    // ── Show out of stock + restore via kebab ───────────────────────────
    await page.getByLabel("Show out of stock").check();
    await expect(page.getByText("Ice Cream").first()).toBeVisible();

    const iceRow = itemRow(page, "Ice Cream");
    await expect(iceRow.getByText("(out of stock)")).toBeVisible();

    await clickKebabItem(iceRow, "Restore");
    await expect(page.getByText("Ice Cream").first()).toBeVisible();

    // ── Bulk Delete ─────────────────────────────────────────────────────
    // Select individual items (no thead checkbox in current UI)
    await itemRow(page, "Chicken Breast").locator("input[type='checkbox']").check();
    await itemRow(page, "Frozen Peas").locator("input[type='checkbox']").check();
    await itemRow(page, "Pizza Rolls").locator("input[type='checkbox']").check();
    await expect(page.getByText(/selected/)).toBeVisible();
    await expect(page.getByRole("button", { name: "Delete Selected" })).toBeVisible();

    await page.getByRole("button", { name: "Delete Selected" }).click();
    const bulkDialog = page.locator(".modal-overlay");
    await expect(bulkDialog).toBeVisible();
    await bulkDialog.getByRole("button", { name: "Confirm" }).click();

    await expect(page.getByText("No items found.")).toBeVisible();

    // ── Export CSV ──────────────────────────────────────────────────────
    const downloadPromise = page.waitForEvent("download");
    // Export button: trigger download via API directly
    await page.evaluate(() => {
      const a = document.createElement("a");
      a.href = "/api/export";
      a.download = "freezer-inventory.csv";
      a.click();
    });

    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe("freezer-inventory.csv");

    // Switching to raw buffer approach for cross-env compatibility
    const readable = await download.createReadStream();
    if (!readable) {
      // Fallback: read via buffer if stream not available
      const body = await (await fetch(download.url())).text();
      expect(body).toContain("id,name,count,barcodes");
      expect(body).toContain("Chicken Breast");
    } else {
      // Use stream-based read (Node.js environment)
      const chunks = [];
      for await (const chunk of readable) {
        chunks.push(chunk);
      }
      const text = Buffer.concat(chunks).toString("utf-8");
      expect(text).toContain("id,name,count,barcodes");
      expect(text).toContain("Chicken Breast");
    }
  });
});
