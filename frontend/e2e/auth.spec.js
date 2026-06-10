import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems } from "./fixtures/mock-data.js";

test.describe("Auth Flow (HttpOnly Cookie)", () => {
  test("shows auth form when no cookie is present", async ({ page }) => {
    // Set up mocks first, then override auth/check to deny
    await setupApiMocks(page);
    await page.route("**/api/auth/check", (route) => {
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ authenticated: false }) });
    });
    await page.goto("/");

    await expect(page.getByText("Enter your access token")).toBeVisible();
    await expect(page.getByPlaceholder("Enter token...")).toBeVisible();
    await expect(page.getByRole("button", { name: "Unlock" })).toBeVisible();
    await expect(page.getByText("🧊 Freezer Inventory")).not.toBeVisible();
  });

  test("unlocks the app with a valid token (cookie set by server)", async ({ page }) => {
    await setupApiMocks(page);
    await page.route("**/api/auth/check", (route) => {
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ authenticated: false }) });
    });
    await page.goto("/");

    await page.getByPlaceholder("Enter token...").fill("test-token-123");
    await page.getByRole("button", { name: "Unlock" }).click();

    await expect(page.getByText("Enter your access token")).not.toBeVisible();
    await expect(page.getByText("🧊 Freezer Inventory")).toBeVisible();
    await expect(page.getByText("Manual Add")).toBeVisible();
  });

  test("does not unlock with empty token", async ({ page }) => {
    await setupApiMocks(page);
    await page.route("**/api/auth/check", (route) => {
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ authenticated: false }) });
    });
    await page.goto("/");

    await page.getByRole("button", { name: "Unlock" }).click();

    await expect(page.getByText("Enter your access token")).toBeVisible();
    await expect(page.getByText("🧊 Freezer Inventory")).not.toBeVisible();
  });

  test("skips auth form when cookie is already valid", async ({ page }) => {
    // Default mock returns authenticated: true — auth form skipped automatically
    await setupApiMocks(page, cloneItems());
    await page.goto("/");

    await expect(page.getByText("Enter your access token")).not.toBeVisible();
    await expect(page.getByText("🧊 Freezer Inventory")).toBeVisible();
  });

  test("shows error message on invalid token", async ({ page }) => {
    await setupApiMocks(page);
    await page.route("**/api/auth/check", (route) => {
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ authenticated: false }) });
    });
    await page.goto("/");

    await page.getByPlaceholder("Enter token...").fill("wrong-token");
    await page.getByRole("button", { name: "Unlock" }).click();

    await expect(page.getByText("Enter your access token")).toBeVisible();
    await expect(page.getByText("Invalid token")).toBeVisible();
  });
});