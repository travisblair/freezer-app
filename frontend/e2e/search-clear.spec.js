import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

test.describe("Search Clear Button", () => {
  test("clear button is hidden when search is empty", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Empty search — no clear button
    await expect(page.locator(".search-clear-btn")).not.toBeVisible();
  });

  test("clear button appears when text is typed", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("Chicken");

    // Clear button should now be visible
    await expect(page.locator(".search-clear-btn")).toBeVisible();
    await expect(page.locator(".search-clear-btn")).toHaveText("✕");
  });

  test("clicking clear button empties search input", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const searchInput = page.getByPlaceholder("Search by name...");
    await searchInput.fill("Chicken");

    // Verify filtering happened
    await expect(page.getByText("Chicken Breast").first()).toBeVisible();

    // Click clear
    await page.locator(".search-clear-btn").click();

    // Input should be empty
    await expect(searchInput).toHaveValue("");

    // All items should be visible again
    await expect(page.getByText("Frozen Peas").first()).toBeVisible();
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();
  });

  test("clear button disappears after clearing", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("Chicken");
    await expect(page.locator(".search-clear-btn")).toBeVisible();

    await page.locator(".search-clear-btn").click();
    await expect(page.locator(".search-clear-btn")).not.toBeVisible();
  });

  test("clear button works via keyboard — clearing input hides button", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const searchInput = page.getByPlaceholder("Search by name...");
    await searchInput.fill("test");
    await expect(page.locator(".search-clear-btn")).toBeVisible();

    // Clear via keyboard (select all + delete)
    await searchInput.clear();
    // Need to trigger input event for SolidJS reactivity
    await searchInput.fill("");

    await expect(page.locator(".search-clear-btn")).not.toBeVisible();
  });

  test("clear button is accessible — has aria-label", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("test");
    await expect(page.locator("button[aria-label='Clear search']")).toBeVisible();
  });

  test("typing after clear re-shows clear button", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const searchInput = page.getByPlaceholder("Search by name...");
    await searchInput.fill("first");
    await page.locator(".search-clear-btn").click();
    await expect(page.locator(".search-clear-btn")).not.toBeVisible();

    await searchInput.fill("second");
    await expect(page.locator(".search-clear-btn")).toBeVisible();
  });
});
