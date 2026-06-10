import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

test.describe("Offline Banner", () => {
  test("banner is not shown when API works normally", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await expect(page.locator(".offline-banner")).not.toBeVisible();
  });

  test("shows offline banner after multiple consecutive network failures", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.route("**/api/**", (route) => route.abort("failed"));

    await page.getByPlaceholder("Search by name...").fill("fail1");
    await page.waitForTimeout(500);
    await page.getByPlaceholder("Search by name...").fill("fail2");

    await expect(page.locator(".offline-banner")).toBeVisible({ timeout: 5000 });
    await expect(page.locator(".offline-banner")).toContainText(/Server unreachable/);
  });

  test("hides offline banner when a subsequent request succeeds", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.route("**/api/**", (route) => route.abort("failed"));
    await page.getByPlaceholder("Search by name...").fill("fail1");
    await page.waitForTimeout(500);
    await page.getByPlaceholder("Search by name...").fill("fail2");
    await expect(page.locator(".offline-banner")).toBeVisible({ timeout: 5000 });

    await page.unroute("**/api/**");
    await setupApiMocks(page, cloneItems());
    await page.getByPlaceholder("Search by name...").fill("");
    await page.waitForTimeout(500);
    await page.reload();

    await expect(page.locator(".offline-banner")).not.toBeVisible({ timeout: 5000 });
  });

  test("banner has correct styling", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.route("**/api/**", (route) => route.abort("failed"));
    await page.getByPlaceholder("Search by name...").fill("fail1");
    await page.waitForTimeout(500);
    await page.getByPlaceholder("Search by name...").fill("fail2");

    const banner = page.locator(".offline-banner");
    await expect(banner).toBeVisible({ timeout: 5000 });
    await expect(banner).toHaveClass(/offline-banner/);
  });

  test("single failed request does not trigger offline banner", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    let callCount = 0;
    await page.route("**/api/items**", (route) => {
      callCount++;
      if (callCount <= 1) {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(cloneItems().filter((i) => !i.deleted)),
        });
      }
      return route.abort("failed");
    });

    await page.getByPlaceholder("Search by name...").fill("single failure");
    await page.waitForTimeout(500);

    await expect(page.locator(".offline-banner")).not.toBeVisible();
  });
});