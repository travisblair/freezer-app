import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

test.describe("Item Table — List & Display", () => {
  test("shows non-deleted items by default", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await expect(page.getByText("Chicken Breast").first()).toBeVisible();
    await expect(page.getByText("Frozen Peas").first()).toBeVisible();
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();
    await expect(page.getByText("Ice Cream")).not.toBeVisible();
  });

  test("shows deleted items when 'Show deleted' is checked", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByLabel("Show deleted").check();
    await expect(page.getByText("Ice Cream").first()).toBeVisible();
    await expect(page.getByText("(deleted)")).toBeVisible();
  });

  test("shows empty state when no items", async ({ page }) => {
    await setupApiMocks(page, []);
    await authenticate(page);

    await expect(page.getByText("No items found.")).toBeVisible();
  });

  test("searches items by name (debounced)", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("Chicken");
    await expect(page.getByText("Chicken Breast").first()).toBeVisible();
    await expect(page.getByText("Frozen Peas")).not.toBeVisible();
    await expect(page.getByText("Salmon Fillet")).not.toBeVisible();
  });

  test("search is case-insensitive", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("salmon");
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();
    await expect(page.getByText("Chicken Breast").first()).not.toBeVisible();
  });

  test("search returns no results for unmatched query", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("zzzzz_nothing");
    await expect(page.getByText("No items found.")).toBeVisible();
  });

  test("search respects showDeleted toggle", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByPlaceholder("Search by name...").fill("Ice");
    await expect(page.getByText("No items found.")).toBeVisible();

    await page.getByLabel("Show deleted").check();
    await expect(page.getByText("Ice Cream").first()).toBeVisible();
  });

  test("sorts items alphabetically by name", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const nameCells = page.locator("tbody td:nth-child(2)");
    const names = await nameCells.allTextContents();
    const clean = names.map((n) => n.replace(/\s*\(deleted\)\s*/, "").trim());
    const sorted = [...clean].sort((a, b) => a.localeCompare(b));
    expect(clean).toEqual(sorted);
  });
});

test.describe("Item Table — Row Actions", () => {
  test("deletes an item (soft delete via row button)", async ({ page }) => {
    const db = await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const salmonRow = page.getByRole("row", { name: /Salmon Fillet/ });
    await salmonRow.getByRole("button", { name: "Delete" }).click();

    const dialog = page.locator("dialog[open]");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Confirm" }).click();

    await expect(page.getByText("Salmon Fillet")).not.toBeVisible();

    const salmon = db.find((i) => i.name === "Salmon Fillet");
    expect(salmon.count).toBe(0);
    expect(salmon.deleted).toBe(1);
  });

  test("restores a deleted item", async ({ page }) => {
    const db = await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByLabel("Show deleted").check();
    await expect(page.getByText("Ice Cream").first()).toBeVisible();

    const iceCreamRow = page.getByRole("row", { name: /Ice Cream/ });
    await iceCreamRow.getByRole("button", { name: "Restore" }).click();

    await expect(page.getByText("Ice Cream").first()).toBeVisible();

    const iceCream = db.find((i) => i.name === "Ice Cream");
    expect(iceCream.count).toBe(1);
    expect(iceCream.deleted).toBe(0);
  });

  test("deleted rows show (deleted) tag and restore button", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByLabel("Show deleted").check();

    const iceCreamRow = page.getByRole("row", { name: /Ice Cream/ });
    await expect(iceCreamRow.getByText("(deleted)")).toBeVisible();
    await expect(iceCreamRow.getByRole("button", { name: "Restore" })).toBeVisible();
    await expect(iceCreamRow.getByRole("button", { name: "Delete" })).not.toBeVisible();
  });

  test("active rows show Delete button but not Restore", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const chickenRow = page.getByRole("row", { name: /Chicken Breast/ });
    await expect(chickenRow.getByRole("button", { name: "Delete" })).toBeVisible();
    await expect(chickenRow.getByRole("button", { name: "Restore" })).not.toBeVisible();
  });
});

test.describe("Item Table — Bulk Actions", () => {
  test("selecting all via header checkbox selects every item", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.locator("thead input[type='checkbox']").click();

    await expect(page.getByText(/selected/)).toBeVisible();
    await expect(page.getByRole("button", { name: "Delete Selected" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Clear" })).toBeVisible();
  });

  test("bulk delete soft-deletes selected items", async ({ page }) => {
    const db = await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.locator("thead input[type='checkbox']").click();
    await page.getByRole("button", { name: "Delete Selected" }).click();

    const dialog = page.locator("dialog[open]");
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Confirm" }).click();

    await expect(page.getByText("No items found.")).toBeVisible();

    const allDeleted = db.every((i) => i.deleted === 1 && i.count === 0);
    expect(allDeleted).toBe(true);
  });

  test("clear selection removes bulk actions", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.locator("thead input[type='checkbox']").click();
    await expect(page.getByRole("button", { name: "Clear" })).toBeVisible();

    await page.getByRole("button", { name: "Clear" }).click();
    await expect(page.getByRole("button", { name: "Delete Selected" })).not.toBeVisible();
  });

  test("individual row checkbox toggles selection", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const chickenCheckbox = page.getByRole("row", { name: /Chicken Breast/ }).locator("input[type='checkbox']");
    await chickenCheckbox.check();

    await expect(page.getByText("1 selected")).toBeVisible();

    await chickenCheckbox.uncheck();
    await expect(page.getByText(/selected/)).not.toBeVisible();
  });

  test("deselecting header when all selected clears all", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.locator("thead input[type='checkbox']").click();
    await expect(page.getByText(/selected/)).toBeVisible();

    await page.locator("thead input[type='checkbox']").click();
    await expect(page.getByText(/selected/)).not.toBeVisible();
  });
});