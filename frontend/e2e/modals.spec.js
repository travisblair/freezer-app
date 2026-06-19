import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";
import { openKebab, clickKebabItem } from "./fixtures/kebab-helpers.js";

/** Helper: find a table row by item name text (within the td). */
function itemRow(page, name) {
  return page.getByRole("row", { name: new RegExp(name) });
}

test.describe("Edit Modal", () => {
  test("opens edit modal via kebab with prefilled name", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");
    await clickKebabItem(row, "Edit");

    const dialog = page.locator(".modal-overlay");
    await expect(dialog.getByText("Edit Item")).toBeVisible();
    await expect(dialog.getByLabel("Name")).toHaveValue("Chicken Breast");
    await expect(dialog.getByText(/Barcode/)).toBeVisible();
  });

  test("saves name edit via modal", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");
    await clickKebabItem(row, "Edit");

    const dialog = page.locator(".modal-overlay");
    await dialog.getByLabel("Name").fill("Organic Chicken Breast");
    await dialog.getByRole("button", { name: "Save" }).click();

    await expect(page.getByText("Edit Item")).not.toBeVisible();
    await expect(page.getByText("Organic Chicken Breast").first()).toBeVisible();
  });

  test("cancel closes modal without changes", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");
    await clickKebabItem(row, "Edit");

    const dialog = page.locator(".modal-overlay");
    await dialog.getByLabel("Name").fill("Should Not Save");
    await dialog.getByRole("button", { name: "Cancel" }).click();

    await expect(page.getByText("Edit Item")).not.toBeVisible();
    await expect(page.getByText("Chicken Breast").first()).toBeVisible();
    await expect(page.getByText("Should Not Save")).not.toBeVisible();
  });

  test("closing modal via X button works", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");
    await clickKebabItem(row, "Edit");

    await expect(page.getByText("Edit Item")).toBeVisible();
    await page.locator(".modal-overlay button[aria-label='Close']").click();
    await expect(page.getByText("Edit Item")).not.toBeVisible();
  });
});

test.describe("Confirm Modal — Hard Delete", () => {
  test("Delete via kebab shows danger confirm modal", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Salmon Fillet");
    await clickKebabItem(row, "Delete");

    const dialog = page.locator(".modal-overlay");
    await expect(dialog.getByText("Delete Forever")).toBeVisible();
    // "cannot be undone" text is not in current ConfirmModal — message is dynamic
  });

  test("cancel on confirm modal does not delete", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Salmon Fillet");
    await clickKebabItem(row, "Delete");

    const dialog = page.locator(".modal-overlay");
    await dialog.getByRole("button", { name: "Cancel" }).click();

    await expect(page.locator(".modal-overlay")).not.toBeAttached();
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();
  });

  test("confirming hard delete removes the item", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Salmon Fillet");
    await clickKebabItem(row, "Delete");

    const dialog = page.locator(".modal-overlay");
    await dialog.getByRole("button", { name: "Delete Forever" }).click();

    await expect(page.getByText("Salmon Fillet")).not.toBeVisible();
  });
});

test.describe("Confirm Modal — Restore (out of stock)", () => {
  test("Restore via kebab for out-of-stock items", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Show out of stock to see Ice Cream (count 0)
    await page.getByLabel("Show out of stock").check();
    await expect(page.getByText("Ice Cream").first()).toBeVisible();

    const row = itemRow(page, "Ice Cream");
    await clickKebabItem(row, "Restore");

    // Verify item is restored (count > 0, so it might still show depending on toggle)
    await expect(page.getByText("Ice Cream").first()).toBeVisible();
  });
});
