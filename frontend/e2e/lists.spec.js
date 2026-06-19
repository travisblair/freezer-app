import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

test.describe("Lists", () => {
  test("shows Freezer list by default", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await expect(page.locator("h1")).toContainText("Freezer");
  });

  test("creates a new list", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Add new list" }).click();
    await page.locator(".modal-dialog input[type='text']").fill("Pantry");
    await page.getByRole("button", { name: "Create" }).click();

    // Should switch to new list
    await expect(page.locator("select.list-select")).toContainText("Pantry");
  });

  test("renames a list", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Create a list first
    await page.getByRole("button", { name: "Add new list" }).click();
    await page.locator(".modal-dialog input[type='text']").fill("Old Name");
    await page.getByRole("button", { name: "Create" }).click();

    // Rename
    await page.locator('[title="Rename list"]').click();
    await page.locator(".modal-dialog input[type='text']").fill("Renamed");
    await page.getByRole("button", { name: "Save" }).click();

    await expect(page.locator("select.list-select")).toContainText("Renamed");
  });

  test("deletes a non-default list", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Create a list to delete
    await page.getByRole("button", { name: "Add new list" }).click();
    await page.locator(".modal-dialog input[type='text']").fill("DeleteMe");
    await page.getByRole("button", { name: "Create" }).click();

    // Delete it
    await page.locator('[title="Delete list"]').click();
    await page.getByRole("button", { name: "Delete Forever" }).click();

    // After delete, currentListId resets to 1; wait for re-render
    await page.waitForTimeout(500);
    // After reverting to default list, h1 should show Freezer
    await expect(page.locator("h1")).toContainText("Freezer");
  });
});
