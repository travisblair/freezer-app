import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

function itemRow(page, name) {
  return page.getByRole("row", { name: new RegExp(name) });
}

test.describe("Bulk Move Modal", () => {
  test("'Move Selected' button appears when items are checked", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // No bulk actions bar initially
    await expect(page.getByText(/selected/)).not.toBeVisible();

    // Check Chicken Breast
    await itemRow(page, "Chicken Breast").getByRole("checkbox").check();

    // Bulk actions bar should appear
    await expect(page.getByText("1 selected")).toBeVisible();
    await expect(page.getByRole("button", { name: "Move Selected" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Delete Selected" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Clear" })).toBeVisible();
  });

  test("opens bulk move modal with correct items", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Select Chicken Breast and Salmon Fillet
    await itemRow(page, "Chicken Breast").getByRole("checkbox").check();
    await itemRow(page, "Salmon Fillet").getByRole("checkbox").check();

    await expect(page.getByText("2 selected")).toBeVisible();

    // Click Move Selected
    await page.getByRole("button", { name: "Move Selected" }).click();

    // Bulk move modal should appear
    const dialog = page.locator(".modal-overlay");
    await expect(dialog.getByText("Move 2 items")).toBeVisible();

    // Both items should be listed
    await expect(dialog.getByText("Chicken Breast")).toBeVisible();
    await expect(dialog.getByText("Salmon Fillet")).toBeVisible();

    // Each shows source shelf info
    await expect(dialog.getByText(/From Shelf 1/).first()).toBeVisible();

    // Quantity inputs should be present
    const qtyInputs = dialog.locator("input[type='number']");
    await expect(qtyInputs).toHaveCount(2);
  });

  test("target shelf excludes source shelf when all items on same shelf", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Select two items both on Shelf 1
    await itemRow(page, "Chicken Breast").getByRole("checkbox").check();
    await itemRow(page, "Frozen Peas").getByRole("checkbox").check();

    await page.getByRole("button", { name: "Move Selected" }).click();

    const dialog = page.locator(".modal-overlay");
    // "Shelf 1" should not be in the target dropdown since both items come from it
    const select = dialog.locator("select");
    const options = await select.locator("option").allTextContents();
    expect(options).not.toContain("Shelf 1");
  });

  test("Cancel button closes modal without moving", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await itemRow(page, "Chicken Breast").getByRole("checkbox").check();
    await page.getByRole("button", { name: "Move Selected" }).click();

    const dialog = page.locator(".modal-overlay");
    await expect(dialog.getByText("Move 1 item")).toBeVisible();

    await dialog.getByRole("button", { name: "Cancel" }).click();

    // Modal should close, selection should remain (only move clears selection)
    await expect(page.getByText("Move 1 item")).not.toBeVisible();
    await expect(page.getByText("1 selected")).toBeVisible();
  });

  test("clears selection bar after successful single-item move", async ({ page }) => {
    // This tests the stale "N selected" bug fix — clearSelection() after individual move
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Check an item first so we can verify selection is cleared after move
    await itemRow(page, "Chicken Breast").getByRole("checkbox").check();
    await expect(page.getByText("1 selected")).toBeVisible();

    // Trigger individual move via kebab
    const row = itemRow(page, "Chicken Breast");
    await row.locator(".kebab-btn").click();
    const menu = row.locator(".kebab-menu");
    await expect(menu).toBeVisible();
    await menu.locator(".kebab-item").filter({ hasText: "Move" }).click();

    // Move modal appears
    const dialog = page.locator(".modal-overlay");
    await expect(dialog.getByText(/Move Chicken Breast/)).toBeVisible();

    // Click Save — this calls onDone which calls clearSelection()
    await dialog.getByRole("button", { name: "Save" }).click();

    // Wait for modal to close
    await expect(page.getByText(/Move Chicken Breast/)).not.toBeVisible();

    // Selection bar should be gone now (the bug was it persisted)
    await expect(page.getByText(/selected/)).not.toBeVisible();
  });
});
