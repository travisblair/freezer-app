// @ts-check
const { test, expect } = require("@playwright/test");

test.describe("Lists", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    // Log in if needed
    const authForm = page.locator(".auth-container");
    if (await authForm.isVisible()) {
      await page.fill('input[type="email"]', "test@test.com");
      await page.fill('input[type="password"]', "test");
      await page.click('button[type="submit"]');
      await page.waitForSelector("text=Freezer", { timeout: 5000 });
    }
  });

  test("shows Freezer list by default", async ({ page }) => {
    await expect(page.locator("h1")).toContainText("Freezer");
  });

  test("creates a new list and switches to it", async ({ page }) => {
    await page.click("text=Add new list");
    await page.fill('.modal-dialog input[type="text"]', "Pantry");
    await page.click("text=Create");
    // Should switch to new list
    await expect(page.locator("select.list-select")).toContainText("Pantry");
  });

  test("renames a list", async ({ page }) => {
    // Create a list first
    await page.click("text=Add new list");
    await page.fill('.modal-dialog input[type="text"]', "Old Name");
    await page.click("text=Create");

    // Click pencil to rename
    await page.click('[title="Rename list"]');
    await page.fill('.modal-dialog input[type="text"]', "Renamed");
    await page.click("text=Save");

    await expect(page.locator("select.list-select")).toContainText("Renamed");
  });

  test("deletes a non-default list", async ({ page }) => {
    // Create a list to delete
    await page.click("text=Add new list");
    await page.fill('.modal-dialog input[type="text"]', "DeleteMe");
    await page.click("text=Create");

    // Delete it
    await page.click('[title="Delete list"]');
    await page.click("text=Delete Forever");

    // Should no longer be in the select
    await expect(page.locator("select.list-select")).not.toContainText("DeleteMe");
  });
});
