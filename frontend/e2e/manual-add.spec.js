import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

// Helper: find an item by barcode string (works with both flat strings and objects)
const findByBarcode = (db, barcode) =>
  db.find((i) => i.barcodes && i.barcodes.some((b) => (typeof b === "string" ? b : b.barcode) === barcode));

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
    await expect(page.getByText('Updated "Chicken Breast"')).toBeVisible();

    expect(page.getByText("Chicken Breast").first()).toBeVisible();
  });

  test("dismiss duplicate offer", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("e.g. Chicken Breast").fill("Another Chicken");
    await page.getByPlaceholder("Optional").fill("12345");
    await page.getByRole("button", { name: "Add Item" }).click();

    await page.locator(".duplicate-offer").getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByText(/already exists/)).not.toBeVisible();

    expect(page.getByText("Chicken Breast").first()).toBeVisible();
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