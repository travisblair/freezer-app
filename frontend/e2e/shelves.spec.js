import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

test.describe("Shelf Management", () => {
  test("creates a new shelf", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("New shelf name...").fill("Door Shelf");
    await page.getByRole("button", { name: "+ Add Shelf" }).click();

    await expect(page.locator(".shelf-header").filter({ hasText: "Door Shelf" })).toBeVisible();
    await expect(page.getByPlaceholder("New shelf name...")).toHaveValue("");
  });

  test.fixme("renames a shelf via pencil button", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Find the shelf header and click pencil
    const shelfHeader = page.locator(".shelf-header").filter({ hasText: "Shelf 1" });
    await shelfHeader.locator("button").first().click(); // pencil button

    // Input should appear for renaming
    const input = shelfHeader.locator("input[type='text']");
    await expect(input).toBeVisible();
    await input.fill("Main Shelf");
    await input.press("Enter");
    // Wait for rename to take effect

    await expect(page.getByText("Main Shelf")).toBeVisible();
  });

  test("deletes a shelf via trash button", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Create a second shelf so we can delete it
    await page.getByPlaceholder("New shelf name...").fill("Temp Shelf");
    await page.getByRole("button", { name: "+ Add Shelf" }).click();
    await expect(page.locator(".shelf-header").filter({ hasText: "Temp Shelf" })).toBeVisible();

    // Delete it
    const tempHeader = page.locator(".shelf-header").filter({ hasText: "Temp Shelf" });
    // The trash button is the second button (after pencil)
    const buttons = tempHeader.locator("button");
    await buttons.nth(1).click();

    await expect(page.locator(".shelf-header").filter({ hasText: "Temp Shelf" })).not.toBeAttached();
  });

  test("cannot delete Shelf 1", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Shelf 1 should have no trash button
    const shelf1Header = page.locator(".shelf-header").filter({ hasText: "Shelf 1" });
    const buttons = shelf1Header.locator("button");
    const count = await buttons.count();
    // Only pencil button (no trash) for shelf 1
    expect(count).toBe(1);
  });

  test.fixme("shelf filter dropdown works", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Create a second shelf with an item
    await page.getByPlaceholder("New shelf name...").fill("Door Shelf");
    await page.getByRole("button", { name: "+ Add Shelf" }).click();

    // Switch to "All Shelves" first
    const dropdown = page.locator(".table-controls select");
    await dropdown.selectOption("");

    // Verify both shelves visible
    await expect(page.locator(".shelf-header").filter({ hasText: "Shelf 1" })).toBeVisible();
    await expect(page.locator(".shelf-header").filter({ hasText: "Door Shelf" })).toBeVisible();

    // Filter to Door Shelf
    await dropdown.selectOption({ label: "Door Shelf" });

    // Should show Door Shelf but not Shelf 1
    await expect(page.locator(".shelf-header").filter({ hasText: "Door Shelf" })).toBeVisible();
    // Shelf 1 section header shouldn't be visible when filtered
    await expect.poll(() => page.locator(".shelf-section").count()).toBe(1);
  });
});
