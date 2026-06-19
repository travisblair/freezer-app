import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";
import { clickKebabItem } from "./fixtures/kebab-helpers.js";

function itemRow(page, name) {
  return page.getByRole("row", { name: new RegExp(name) });
}

test.describe("Move Modal", () => {
  test("opens move modal via kebab with target shelf and quantity", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");
    await clickKebabItem(row, "Move");

    const dialog = page.locator(".modal-overlay");
    await expect(dialog.getByText("Move Chicken Breast")).toBeVisible();
    await expect(dialog.getByLabel("To shelf")).toBeVisible();
    await expect(dialog.getByLabel("Quantity")).toHaveValue("3");
  });

  test("cancel closes move modal", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");
    await clickKebabItem(row, "Move");

    const dialog = page.locator(".modal-overlay");
    await dialog.getByRole("button", { name: "Cancel" }).click();

    await expect(page.getByText("Move Chicken Breast")).not.toBeVisible();
  });

  test("saves move and closes modal", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const row = itemRow(page, "Chicken Breast");
    await clickKebabItem(row, "Move");

    const dialog = page.locator(".modal-overlay");
    await dialog.getByRole("button", { name: "Save" }).click();

    await expect(page.getByText("Move Chicken Breast")).not.toBeVisible();
  });
});
