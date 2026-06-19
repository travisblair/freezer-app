import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";
import { openKebab } from "./fixtures/kebab-helpers.js";

function itemRow(page, name) {
  return page.getByRole("row", { name: new RegExp(name) });
}

test.describe("Kebab Menu — Consistency", () => {
  test("opens and closes via toggle button", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");

    // Open
    await row.locator(".kebab-btn").click();
    await expect(row.locator(".kebab-menu")).toBeVisible();

    // Close by clicking button again
    await row.locator(".kebab-btn").click();
    await expect(row.locator(".kebab-menu")).not.toBeAttached();
  });

  test("re-opens after closing and items remain clickable", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");

    // First open: click Edit
    await row.locator(".kebab-btn").click();
    await row.locator(".kebab-menu .kebab-item").filter({ hasText: "Edit" }).click();
    await expect(page.getByText("Edit Item")).toBeVisible();
    // Cancel
    await page.locator(".modal-overlay button[aria-label='Close']").click();
    await expect(page.getByText("Edit Item")).not.toBeVisible();

    // Second open: click Move
    await row.locator(".kebab-btn").click();
    await expect(row.locator(".kebab-menu")).toBeVisible();
    await row.locator(".kebab-menu .kebab-item").filter({ hasText: "Move" }).click();
    await expect(page.getByText(/Move Chicken Breast/)).toBeVisible();
    // Cancel
    await page.locator(".modal-overlay button[aria-label='Close']").click();
    await expect(page.getByText(/Move Chicken Breast/)).not.toBeVisible();

    // Third open: click Delete — verify confirm dialog
    await row.locator(".kebab-btn").click();
    await expect(row.locator(".kebab-menu")).toBeVisible();
    await row.locator(".kebab-menu .kebab-item").filter({ hasText: "Delete" }).click();
    await expect(page.getByText("Delete Forever")).toBeVisible();
    // Cancel
    await page.locator(".modal-overlay").getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByText("Delete Forever")).not.toBeVisible();
  });

  test.fixme("only one kebab menu open at a time", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const chickenRow = itemRow(page, "Chicken Breast");
    const peasRow = itemRow(page, "Frozen Peas");

    // Open first kebab
    await chickenRow.locator(".kebab-btn").click();
    await expect(chickenRow.locator(".kebab-menu")).toBeVisible();

    // Close by clicking elsewhere (the table body)
    await page.locator("body").click({ position: { x: 0, y: 0 } });
    await expect(chickenRow.locator(".kebab-menu")).not.toBeAttached();

    // Open second kebab
    await peasRow.locator(".kebab-btn").click();
    await expect(peasRow.locator(".kebab-menu")).toBeVisible();
    await expect(chickenRow.locator(".kebab-menu")).not.toBeAttached();
  });

  test.fixme("kebab menu shows correct items for in-stock vs out-of-stock", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // In-stock row
    const chickenRow = itemRow(page, "Chicken Breast");
    await chickenRow.locator(".kebab-btn").click();
    const chickenMenu = chickenRow.locator(".kebab-menu");
    await expect(chickenMenu).toBeVisible();
    await expect(chickenMenu.getByText("Edit")).toBeVisible();
    await expect(chickenMenu.getByText("Move")).toBeVisible();
    await expect(chickenMenu.getByText("Delete")).toBeVisible();
    await expect(chickenMenu.getByText("Restore")).not.toBeVisible();
    
    // Close before checking next
    await page.locator(".shelf-header").first().click();

    // Out-of-stock row
    await page.getByLabel("Show out of stock").check();
    const iceRow = itemRow(page, "Ice Cream");
    await iceRow.locator(".kebab-btn").click();
    const iceMenu = iceRow.locator(".kebab-menu");
    await expect(iceMenu).toBeVisible();
    await expect(iceMenu.getByText("Edit")).toBeVisible();
    await expect(iceMenu.getByText("Move")).toBeVisible();
    await expect(iceMenu.getByText("Restore")).toBeVisible();
    await expect(iceMenu.getByText("Delete")).not.toBeVisible();
  });
});
