import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

test.describe("CSV Export", () => {
  test("export CSV button is visible", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await expect(page.getByRole("button", { name: "Export CSV" })).toBeVisible();
  });

  test("export CSV downloads a CSV file", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const downloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export CSV" }).click();

    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe("freezer-inventory.csv");

    const content = await (await download.createReadStream())
      .toArray()
      .then((chunks) => Buffer.concat(chunks).toString("utf-8"));

    const lines = content.trim().split("\n");
    expect(lines[0]).toBe("id,name,count,deleted,barcodes");
    expect(lines.length).toBeGreaterThanOrEqual(4);
  });

  test("export CSV includes deleted items", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const downloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export CSV" }).click();

    const download = await downloadPromise;
    const content = await (await download.createReadStream())
      .toArray()
      .then((chunks) => Buffer.concat(chunks).toString("utf-8"));

    expect(content).toContain("Ice Cream");
    expect(content).toContain("Chicken Breast");
    expect(content).toContain("Frozen Peas");
    expect(content).toContain("Salmon Fillet");
  });

  test("export CSV contains correct values for items", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const downloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export CSV" }).click();

    const download = await downloadPromise;
    const content = await (await download.createReadStream())
      .toArray()
      .then((chunks) => Buffer.concat(chunks).toString("utf-8"));

    expect(content).toContain("1,Chicken Breast,3,0,12345|12346");
    expect(content).toContain("3,Ice Cream,0,1,67890");
  });

  test("export CSV works with empty inventory", async ({ page }) => {
    await setupApiMocks(page, []);
    await authenticate(page);

    const downloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export CSV" }).click();

    const download = await downloadPromise;
    const content = await (await download.createReadStream())
      .toArray()
      .then((chunks) => Buffer.concat(chunks).toString("utf-8"));

    expect(content.trim()).toBe("id,name,count,deleted,barcodes");
  });

  test("export CSV includes items with no barcodes as empty field", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const downloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Export CSV" }).click();

    const download = await downloadPromise;
    const content = await (await download.createReadStream())
      .toArray()
      .then((chunks) => Buffer.concat(chunks).toString("utf-8"));

    expect(content).toContain("2,Frozen Peas,5,0,");
  });
});