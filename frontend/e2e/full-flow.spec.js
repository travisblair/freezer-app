import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, ITEMS, authenticate } from "./fixtures/mock-data.js";

/**
 * Complete end-to-end user journey.
 * Covers: login → add items → duplicate resolution →
 *         soft delete → restore → hard delete → bulk delete → export
 */
test.describe("Full User Flow (end-to-end)", () => {
  test("complete journey: login, add items, duplicate resolution, soft/hard delete", async ({ page }) => {
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

    await expect(page.getByText('Added "Pizza Rolls"')).toBeVisible();
    await expect(page.getByText("Pizza Rolls").first()).toBeVisible();
    await expect(page.getByPlaceholder("e.g. Chicken Breast")).toHaveValue("");

    // ── Add new item with barcode and custom quantity ───────────────────
    await page.getByPlaceholder("e.g. Chicken Breast").fill("Beef Steak");
    await page.getByPlaceholder("Optional").fill("BEEF-001");
    await page.locator(".manual-add-form input[type='number']").fill("4");
    await page.getByRole("button", { name: "Add Item" }).click();

    await expect(page.getByText('Added "Beef Steak"')).toBeVisible();
    await expect(page.getByText("Beef Steak").first()).toBeVisible();

    // ── Duplicate barcode ───────────────────────────────────────────────
    await page.getByPlaceholder("e.g. Chicken Breast").fill("Duplicate Chicken");
    await page.getByPlaceholder("Optional").fill("12345");
    await page.locator(".manual-add-form input[type='number']").fill("2");
    await page.getByRole("button", { name: "Add Item" }).click();

    const dupOffer = page.locator(".duplicate-offer");
    await expect(dupOffer.getByText(/already exists/)).toBeVisible();
    await expect(dupOffer.locator("p").getByText(/Chicken Breast/)).toBeVisible();

    await dupOffer.getByRole("button").first().click();
    await expect(page.getByText('Updated "Chicken Breast"')).toBeVisible();

    // ── Another item without barcode ────────────────────────────────────
    await page.getByPlaceholder("e.g. Chicken Breast").fill("Mystery Meat");
    await page.getByRole("button", { name: "Add Item" }).click();
    await expect(page.getByText("Mystery Meat").first()).toBeVisible();
    await expect(page.getByRole("cell", { name: "Mystery Meat" })).toBeVisible();

    // ── Soft delete ─────────────────────────────────────────────────────
    const salmonRow = page.getByRole("row", { name: /Salmon Fillet/ });
    await salmonRow.getByRole("button", { name: "Delete" }).click();

    const dialog = page.locator("dialog[open]");
    await expect(dialog.getByText(/Salmon Fillet/)).toBeVisible();
    await dialog.getByRole("button", { name: "Confirm" }).click();
    await expect(page.getByText("Salmon Fillet")).not.toBeVisible();

    // ── Show deleted ────────────────────────────────────────────────────
    await page.getByLabel("Show deleted").check();
    await expect(page.getByText("Ice Cream").first()).toBeVisible();
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();

    const iceRow = page.getByRole("row", { name: /Ice Cream/ });
    await expect(iceRow.getByText("(deleted)")).toBeVisible();

    const deletedSalmonRow = page.getByRole("row", { name: /Salmon Fillet/ });
    await expect(deletedSalmonRow.getByText("(deleted)")).toBeVisible();

    // ── Restore ─────────────────────────────────────────────────────────
    await iceRow.getByRole("button", { name: "Restore" }).click();
    await expect(iceRow.getByText("(deleted)")).not.toBeVisible();
    await expect(iceRow.getByRole("button", { name: "Delete" })).toBeVisible();

    // ── Hard delete ─────────────────────────────────────────────────────
    await deletedSalmonRow.getByRole("button", { name: "Remove" }).click();

    const dangerDialog = page.locator("dialog[open]");
    await expect(dangerDialog.getByText("Delete Forever")).toBeVisible();
    await expect(dangerDialog.getByText(/cannot be undone/)).toBeVisible();

    // Cancel first
    await dangerDialog.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();

    // Now actually hard delete
    await deletedSalmonRow.getByRole("button", { name: "Remove" }).click();
    const dangerDialog2 = page.locator("dialog[open]");
    await dangerDialog2.getByRole("button", { name: "Delete Forever" }).click();

    // Active items should still be visible
    await expect(page.getByText("Chicken Breast").first()).toBeVisible();
    await expect(page.getByText("Frozen Peas").first()).toBeVisible();

    // ── Bulk Delete ─────────────────────────────────────────────────────
    await page.locator("thead input[type='checkbox']").click();
    await expect(page.getByText(/selected/)).toBeVisible();
    await expect(page.getByRole("button", { name: "Delete Selected" })).toBeVisible();

    await page.getByRole("button", { name: "Delete Selected" }).click();
    const bulkDialog = page.locator("dialog[open]");
    await expect(bulkDialog).toBeVisible();
    await bulkDialog.getByRole("button", { name: "Confirm" }).click();

    await expect(page.getByText("No items found.")).toBeVisible();

    // ── Export CSV ──────────────────────────────────────────────────────
    const downloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export CSV" }).click();

    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe("freezer-inventory.csv");

    const content = await (await download.createReadStream())
      .toArray()
      .then((chunks) => Buffer.concat(chunks).toString("utf-8"));

    expect(content).toContain("id,name,count,deleted,barcodes");
    expect(content).toContain("Chicken Breast");
    expect(content).toContain("Frozen Peas");
    expect(content).toContain("Pizza Rolls");
  });
});