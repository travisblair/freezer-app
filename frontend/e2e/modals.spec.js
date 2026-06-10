import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

test.describe("Edit Modal", () => {
  test("opens edit modal with prefilled values", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const chickenRow = page.getByRole("row", { name: /Chicken Breast/ });
    await chickenRow.getByRole("button", { name: "Edit" }).click();

    const dialog = page.locator("dialog[open]");
    await expect(dialog.getByText("Edit Item")).toBeVisible();
    await expect(dialog.getByLabel("Name")).toHaveValue("Chicken Breast");
    await expect(dialog.getByLabel("Quantity")).toHaveValue("3");
    await expect(dialog.getByText(/Barcode/)).toBeVisible();
  });

  test("saves edits via modal", async ({ page }) => {
    const db = await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const chickenRow = page.getByRole("row", { name: /Chicken Breast/ });
    await chickenRow.getByRole("button", { name: "Edit" }).click();

    const dialog = page.locator("dialog[open]");
    await dialog.getByLabel("Name").fill("Organic Chicken Breast");
    await dialog.getByLabel("Quantity").fill("10");
    await dialog.getByRole("button", { name: "Save" }).click();

    await expect(page.getByText("Edit Item")).not.toBeVisible();
    await expect(page.getByText("Organic Chicken Breast").first()).toBeVisible();

    const chicken = db.find((i) => i.id === 1);
    expect(chicken.name).toBe("Organic Chicken Breast");
    expect(chicken.count).toBe(10);
  });

  test("setting quantity to 0 soft-deletes the item", async ({ page }) => {
    const db = await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const salmonRow = page.getByRole("row", { name: /Salmon Fillet/ });
    await salmonRow.getByRole("button", { name: "Edit" }).click();

    const dialog = page.locator("dialog[open]");
    await dialog.getByLabel("Quantity").fill("0");
    await dialog.getByRole("button", { name: "Save" }).click();

    await expect(page.getByText("Salmon Fillet")).not.toBeVisible();

    const salmon = db.find((i) => i.id === 4);
    expect(salmon.count).toBe(0);
    expect(salmon.deleted).toBe(1);
  });

  test("restores item by setting quantity > 0", async ({ page }) => {
    const db = await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByLabel("Show deleted").check();

    const iceCreamRow = page.getByRole("row", { name: /Ice Cream/ });
    await iceCreamRow.getByRole("button", { name: "Edit" }).click();

    const dialog = page.locator("dialog[open]");
    await dialog.getByLabel("Quantity").fill("5");
    await dialog.getByRole("button", { name: "Save" }).click();

    const iceCream = db.find((i) => i.id === 3);
    expect(iceCream.count).toBe(5);
    expect(iceCream.deleted).toBe(0);
  });

  test("cancel closes modal without changes", async ({ page }) => {
    const db = await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const chickenRow = page.getByRole("row", { name: /Chicken Breast/ });
    await chickenRow.getByRole("button", { name: "Edit" }).click();

    const dialog = page.locator("dialog[open]");
    await dialog.getByLabel("Name").fill("Should Not Save");
    await dialog.getByRole("button", { name: "Cancel" }).click();

    await expect(page.getByText("Edit Item")).not.toBeVisible();
    await expect(page.getByText("Chicken Breast").first()).toBeVisible();
    await expect(page.getByText("Should Not Save")).not.toBeVisible();

    const chicken = db.find((i) => i.id === 1);
    expect(chicken.name).toBe("Chicken Breast");
  });

  test("closing modal via X button works", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const chickenRow = page.getByRole("row", { name: /Chicken Breast/ });
    await chickenRow.getByRole("button", { name: "Edit" }).click();

    await expect(page.getByText("Edit Item")).toBeVisible();
    await page.locator("dialog button[aria-label='Close']").click();
    await expect(page.getByText("Edit Item")).not.toBeVisible();
  });
});

test.describe("Confirm Modal", () => {
  test("shows confirm modal on single delete and confirms", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const salmonRow = page.getByRole("row", { name: /Salmon Fillet/ });
    await salmonRow.getByRole("button", { name: "Delete" }).click();

    const dialog = page.locator("dialog[open]");
    await expect(dialog.getByText(/Salmon Fillet/)).toBeVisible();
    await expect(dialog.getByText(/hidden but not permanently removed/)).toBeVisible();

    await dialog.getByRole("button", { name: "Confirm" }).click();
    await expect(page.getByText("Salmon Fillet")).not.toBeVisible();
  });

  test("cancel on confirm modal does not delete", async ({ page }) => {
    const db = await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const salmonRow = page.getByRole("row", { name: /Salmon Fillet/ });
    await salmonRow.getByRole("button", { name: "Delete" }).click();

    const dialog = page.locator("dialog[open]");
    await dialog.getByRole("button", { name: "Cancel" }).click();

    await expect(page.locator("dialog[open]")).not.toBeAttached();
    await expect(page.getByText("Salmon Fillet").first()).toBeVisible();

    const salmon = db.find((i) => i.id === 4);
    expect(salmon.deleted).toBe(0);
  });

  test("bulk delete confirm modal shows item count", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.locator("thead input[type='checkbox']").click();
    await page.getByRole("button", { name: "Delete Selected" }).click();

    const dialog = page.locator("dialog[open]");
    await expect(dialog.getByText(/3 selected items/)).toBeVisible();
  });

  test("hard delete (Remove) shows danger confirm modal with 'Delete Forever' button", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByLabel("Show deleted").check();

    const iceCreamRow = page.getByRole("row", { name: /Ice Cream/ });
    await iceCreamRow.getByRole("button", { name: "Remove" }).click();

    const dialog = page.locator("dialog[open]");
    await expect(dialog.getByText("Delete Forever")).toBeVisible();
    await expect(dialog.getByText(/cannot be undone/)).toBeVisible();
  });
});