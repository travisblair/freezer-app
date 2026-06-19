import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems } from "./fixtures/mock-data.js";

test.describe("Auth Flow (Email + Password)", () => {
  test("shows auth form when unauthenticated", async ({ page }) => {
    await setupApiMocks(page);
    // Override auth/check to deny
    await page.unroute("**/api/auth/check");
    await page.route("**/api/auth/check", (route) => {
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ authenticated: false }),
      });
    });
    await page.goto("/");

    await expect(page.getByText("Sign in to continue.")).toBeVisible();
    await expect(page.getByPlaceholder("you@example.com")).toBeVisible();
    await expect(page.getByRole("button", { name: "Sign In" })).toBeVisible();
  });

  test("signs in with valid email and password", async ({ page }) => {
    await setupApiMocks(page);
    await page.unroute("**/api/auth/check");
    await page.route("**/api/auth/check", (route) => {
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ authenticated: false }),
      });
    });
    await page.goto("/");

    await page.getByPlaceholder("you@example.com").fill("test@test.com");
    await page.getByPlaceholder("Enter password...").fill("test");
    await page.getByRole("button", { name: "Sign In" }).click();

    // Should now show the main app
    await expect(page.getByText("Sign in to continue.")).not.toBeVisible();
    await expect(page.getByText("Manual Add")).toBeVisible();
  });

  test("shows error on invalid credentials", async ({ page }) => {
    await setupApiMocks(page);
    await page.unroute("**/api/auth/check");
    await page.route("**/api/auth/check", (route) => {
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ authenticated: false }),
      });
    });
    await page.goto("/");

    await page.getByPlaceholder("you@example.com").fill("wrong@test.com");
    await page.getByPlaceholder("Enter password...").fill("wrong");
    await page.getByRole("button", { name: "Sign In" }).click();

    await expect(page.getByText("Invalid email or password")).toBeVisible();
  });

  test("skips auth form when already authenticated", async ({ page }) => {
    // Default mock returns authenticated: true
    await setupApiMocks(page, cloneItems());
    await page.goto("/");

    await expect(page.getByText("Sign in to continue.")).not.toBeVisible();
    await expect(page.getByText("Inventory")).toBeVisible();
  });
});
