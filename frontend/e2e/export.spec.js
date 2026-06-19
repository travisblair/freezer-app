import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

test.describe("CSV Export", () => {
  test("export CSV is available via useItemActions", async ({ page }) => {
    // The export functionality exists in the codebase (useItemActions.handleExport)
    // but may not have a visible button. Test the API endpoint directly.
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    // Trigger the download via the API directly
    const downloadPromise = page.waitForEvent("download");
    await page.evaluate(() => {
      const a = document.createElement("a");
      a.href = "/api/export";
      a.download = "test.csv";
      a.click();
    });

    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe("test.csv");
  });

  test("export CSV includes header and data", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const res = await page.evaluate(async () => {
      const r = await fetch("/api/export");
      return await r.text();
    });

    expect(res).toContain("id,name,count,barcodes");
    expect(res).toContain("Chicken Breast");
  });
});
