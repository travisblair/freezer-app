import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";
import { clickKebabItem } from "./fixtures/kebab-helpers.js";

function itemRow(page, name) {
  return page.getByRole("row", { name: new RegExp(name) });
}

test.describe("Item Table — List & Display", () => {
  test("shows non-deleted items by default", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await expect(page.getByText("Chicken Breast").first()).toBeVisible();
    await expect(page.getByText("Frozen Peas").first()).toBeVisible();
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();
    await expect(page.getByText("Ice Cream")).not.toBeVisible();
  });

  test("shows out-of-stock items when 'Show out of stock' is checked", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByLabel("Show out of stock").check();
    await expect(page.getByText("Ice Cream").first()).toBeVisible();
    await expect(page.getByText("(out of stock)")).toBeVisible();
  });

  test("shows empty state when no items", async ({ page }) => {
    await setupApiMocks(page, []);
    await authenticate(page);

    await expect(page.getByText("No items found.")).toBeVisible();
  });

  test("searches items by name (debounced)", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("Chicken");
    await expect(page.getByText("Chicken Breast").first()).toBeVisible();
    await expect(page.getByText("Frozen Peas")).not.toBeVisible();
    await expect(page.getByText("Salmon Fillet")).not.toBeVisible();
  });

  test("search is case-insensitive", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("salmon");
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();
    await expect(page.getByText("Chicken Breast").first()).not.toBeVisible();
  });

  test("search returns no results for unmatched query", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("zzzzz_nothing");
    await expect(page.getByText("No items found.")).toBeVisible();
  });

  test("search respects showOutOfStock toggle", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("Ice");
    await expect(page.getByText("No items found.")).toBeVisible();

    await page.getByLabel("Show out of stock").check();
    await expect(page.getByText("Ice Cream").first()).toBeVisible();
  });

  test("sorts items alphabetically by name", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const nameCells = page.locator("tbody td:nth-child(2)");
    const names = await nameCells.allTextContents();
    const clean = names.map((n) => n.replace(/\s*\(out of stock\)\s*/, "").trim());
    const sorted = [...clean].sort((a, b) => a.localeCompare(b));
    expect(clean).toEqual(sorted);
  });
});

test.describe("Item Table — Row Actions via Kebab", () => {
  test("hard deletes an item via kebab Delete", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Salmon Fillet");
    await clickKebabItem(row, "Delete");

    const dialog = page.locator(".modal-overlay");
    await expect(dialog.getByText("Delete Forever")).toBeVisible();
    await dialog.getByRole("button", { name: "Delete Forever" }).click();

    await expect(page.getByText("Salmon Fillet")).not.toBeVisible();
  });

  test("restores an out-of-stock item via kebab Restore", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByLabel("Show out of stock").check();
    await expect(page.getByText("Ice Cream").first()).toBeVisible();

    const row = itemRow(page, "Ice Cream");
    await clickKebabItem(row, "Restore");

    // After restore, item should still be visible (count > 0)
    await expect(page.getByText("Ice Cream").first()).toBeVisible();
  });

  test("out-of-stock rows show (out of stock) tag and Restore in kebab", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByLabel("Show out of stock").check();

    const row = itemRow(page, "Ice Cream");
    await expect(row.getByText("(out of stock)")).toBeVisible();

    // Open kebab and verify Restore is present, Delete is not
    await row.locator(".kebab-btn").click();
    const menu = row.locator(".kebab-menu");
    await expect(menu).toBeVisible();
    await expect(menu.getByText("Restore")).toBeVisible();
    await expect(menu.getByText("Delete")).not.toBeVisible();
  });

  test("active rows show Delete in kebab but not Restore", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");
    await row.locator(".kebab-btn").click();
    const menu = row.locator(".kebab-menu");
    await expect(menu).toBeVisible();
    await expect(menu.getByText("Delete")).toBeVisible();
    await expect(menu.getByText("Restore")).not.toBeVisible();
  });
});

test.describe("Item Table — Bulk Actions", () => {
  test("individual row checkbox toggles selection", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const chickenCheckbox = itemRow(page, "Chicken Breast").locator("input[type='checkbox']");
    await chickenCheckbox.check();

    await expect(page.getByText("1 selected")).toBeVisible();

    await chickenCheckbox.uncheck();
    await expect(page.getByText(/selected/)).not.toBeVisible();
  });

  test("multiple row selections work", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await itemRow(page, "Chicken Breast").locator("input[type='checkbox']").check();
    await itemRow(page, "Frozen Peas").locator("input[type='checkbox']").check();

    await expect(page.getByText("2 selected")).toBeVisible();
  });

  test("bulk delete with selected items shows confirm modal", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await itemRow(page, "Chicken Breast").locator("input[type='checkbox']").check();
    await itemRow(page, "Frozen Peas").locator("input[type='checkbox']").check();

    await page.getByRole("button", { name: "Delete Selected" }).click();

    await expect(page.locator(".modal-overlay").getByText(/selected items/)).toBeVisible();
    await page.locator(".modal-overlay").getByRole("button", { name: "Confirm" }).click();

    await expect(page.getByText(/selected/)).not.toBeVisible();
  });

  test("clear selection removes bulk actions", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await itemRow(page, "Chicken Breast").locator("input[type='checkbox']").check();
    await expect(page.getByRole("button", { name: "Delete Selected" })).toBeVisible();

    await page.getByRole("button", { name: "Clear" }).click();

    await expect(page.getByRole("button", { name: "Delete Selected" })).not.toBeVisible();
  });
});
