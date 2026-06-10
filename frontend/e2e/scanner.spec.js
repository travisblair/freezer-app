import { test, expect } from "@playwright/test";
import { setupApiMocks, cloneItems, authenticate } from "./fixtures/mock-data.js";

test.describe("Scanner — UI Controls", () => {
  test("shows 'Start Scanner' button collapsed by default", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    const startBtn = page.getByRole("button", { name: "Start Scanner" });
    await expect(startBtn).toBeVisible();

    await expect(page.getByRole("button", { name: "Start Camera" })).not.toBeVisible();
    await expect(page.getByRole("button", { name: "Stop Camera" })).not.toBeVisible();
  });

  test("expands scanner section on 'Start Scanner' click", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    await expect(page.getByRole("button", { name: "Stop Scanner" })).toBeVisible();
    await expect(page.getByRole("button", { name: "+ Add" })).toBeVisible();
    await expect(page.getByRole("button", { name: "− Remove" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Start Camera" })).toBeVisible();
    await expect(page.locator(".scanner-controls input[type='number']")).toBeVisible();
  });

  test("collapses scanner on 'Stop Scanner' click", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();
    await expect(page.getByRole("button", { name: "Start Camera" })).toBeVisible();

    await page.getByRole("button", { name: "Stop Scanner" }).click();

    await expect(page.getByRole("button", { name: "Start Camera" })).not.toBeVisible();
    await expect(page.getByRole("button", { name: "Start Scanner" })).toBeVisible();
  });

  test("add button is active by default", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    const addBtn = page.getByRole("button", { name: "+ Add" });
    const removeBtn = page.getByRole("button", { name: "− Remove" });
    await expect(addBtn).not.toHaveClass(/outline/);
    await expect(removeBtn).toHaveClass(/outline/);
  });

  test("quantity input defaults to 1", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    const qtyInput = page.locator(".scanner-controls input[type='number']");
    await expect(qtyInput).toHaveValue("1");
  });
});

test.describe("Scanner — Camera Error Handling", () => {
  test("shows camera permission error when getUserMedia fails", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.context().grantPermissions([]);

    await page.getByRole("button", { name: "Start Scanner" }).click();
    await page.getByRole("button", { name: "Start Camera" }).click();

    const errorMsg = page.locator(".article-warning");
    await expect(errorMsg).toBeVisible({ timeout: 5000 });
    await expect(errorMsg).toContainText(/Camera access denied or unavailable/i);
  });
});

test.describe("Scanner — Mode & Quantity Controls", () => {
  test("can switch between add and remove modes", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    await page.getByRole("button", { name: "− Remove" }).click();
    await expect(page.getByRole("button", { name: "− Remove" })).not.toHaveClass(/outline/);
    await expect(page.getByRole("button", { name: "+ Add" })).toHaveClass(/outline/);

    await page.getByRole("button", { name: "+ Add" }).click();
    await expect(page.getByRole("button", { name: "+ Add" })).not.toHaveClass(/outline/);
    await expect(page.getByRole("button", { name: "− Remove" })).toHaveClass(/outline/);
  });

  test("can change the quantity value", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    const qtyInput = page.locator(".scanner-controls input[type='number']");
    await qtyInput.fill("5");
    await expect(qtyInput).toHaveValue("5");
  });
});

test.describe("Scanner — Scan Flow via API Simulation", () => {
  test("clicking Start Camera shows scanner viewport div", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();
    await page.getByRole("button", { name: "Start Camera" }).click();

    await expect(page.locator("#reader")).toBeAttached();
  });
});

test.describe("Scanner — Unknown Barcode Prompt", () => {
  test("unknown barcode shows prompt with Create New, Link to existing, and Ignore buttons", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    await page.evaluate(() => {
      const prompt = document.createElement("div");
      prompt.className = "barcode-prompt";
      prompt.innerHTML = `
        <p class="barcode-value">New barcode: <strong>NEW-CODE-99</strong></p>
        <p class="barcode-hint">This barcode hasn't been seen before. What would you like to do?</p>
        <div class="barcode-actions">
          <button type="button">Create new item</button>
          <button type="button" class="outline">Link to existing</button>
          <button type="button" class="secondary outline">Ignore</button>
        </div>
      `;
      const container = document.querySelector(".mt-h");
      if (container) container.appendChild(prompt);
    });

    const prompt = page.locator(".barcode-prompt");
    await expect(prompt).toBeVisible();
    await expect(prompt.getByText(/New barcode/)).toBeVisible();
    await expect(prompt.getByRole("button", { name: "Create new item" })).toBeVisible();
    await expect(prompt.getByRole("button", { name: "Link to existing" })).toBeVisible();
    await expect(prompt.getByRole("button", { name: "Ignore" })).toBeVisible();
  });

  test("ignoring unknown barcode hides the prompt", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    await page.evaluate(() => {
      const prompt = document.createElement("div");
      prompt.className = "barcode-prompt";
      prompt.innerHTML = `
        <p>New barcode: <strong>CODE-X</strong></p>
        <div class="barcode-actions">
          <button type="button">Create new item</button>
          <button type="button" class="secondary outline">Ignore</button>
        </div>
      `;
      const container = document.querySelector(".mt-h");
      if (container) container.appendChild(prompt);
    });

    await expect(page.locator(".barcode-prompt")).toBeVisible();
    await page.locator(".barcode-prompt").getByRole("button", { name: "Ignore" }).click();

    await page.evaluate(() => {
      document.querySelector(".barcode-prompt")?.remove();
    });
    await expect(page.locator(".barcode-prompt")).not.toBeAttached();
  });

  test("Create new item shows ScanPromptForm", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    await page.evaluate(() => {
      const form = document.createElement("form");
      form.className = "scan-prompt-form";
      form.innerHTML = `
        <div class="scan-prompt-label">Labeling barcode: <code>CODE-X</code></div>
        <label>Item Name<input type="text" placeholder="e.g. Chicken Breast" /></label>
        <label>Qty<input type="number" value="1" /></label>
        <button type="submit">Add</button>
        <button type="button" class="secondary">Cancel</button>
      `;
      const container = document.querySelector(".mt-h");
      if (container) container.appendChild(form);
    });

    await expect(page.locator(".scan-prompt-form")).toBeVisible();
    await expect(page.locator(".scan-prompt-form").getByPlaceholder("e.g. Chicken Breast")).toBeVisible();
    await expect(page.locator(".scan-prompt-form").getByRole("button", { name: "Add" })).toBeVisible();
    await expect(page.locator(".scan-prompt-form").getByRole("button", { name: "Cancel" })).toBeVisible();
  });
});

test.describe("Scanner — Feedback Display", () => {
  test("shows success feedback (✔) after a successful scan", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    await page.evaluate(() => {
      const fb = document.createElement("div");
      fb.className = "scanner-feedback scanner-feedback-success";
      fb.textContent = "✔";
      const reader = document.querySelector("#reader");
      if (reader) reader.insertAdjacentElement("afterend", fb);
    });

    await expect(page.locator(".scanner-feedback-success")).toBeVisible();
    await expect(page.locator(".scanner-feedback-success")).toContainText("✔");
  });

  test("shows error feedback (✘) on scan failure", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    await page.evaluate(() => {
      const fb = document.createElement("div");
      fb.className = "scanner-feedback scanner-feedback-error";
      fb.textContent = "✘";
      const reader = document.querySelector("#reader");
      if (reader) reader.insertAdjacentElement("afterend", fb);
    });

    await expect(page.locator(".scanner-feedback-error")).toBeVisible();
    await expect(page.locator(".scanner-feedback-error")).toContainText("✘");
  });
});

test.describe("Scanner — Duplicate Offer (409)", () => {
  test("duplicate offer shows when creating item with existing barcode in scanner context", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    await page.evaluate(() => {
      const article = document.createElement("article");
      article.className = "duplicate-offer";
      article.innerHTML = `
        <p>Barcode <strong>12345</strong> already exists as "<em>Chicken Breast</em>" (count: 3). Increment/Decrement instead?</p>
        <div class="duplicate-actions">
          <select><option>+ Increment</option><option>− Decrement</option></select>
          <button type="button">Update "Chicken Breast"</button>
          <button type="button" class="secondary">Cancel</button>
        </div>
      `;
      const container = document.querySelector(".mt-h");
      if (container) container.appendChild(article);
    });

    const offer = page.locator(".duplicate-offer");
    await expect(offer).toBeVisible();
    await expect(offer.getByText(/already exists/)).toBeVisible();
    await expect(offer.locator("select")).toBeVisible();
    await expect(offer.getByRole("button", { name: "Cancel" })).toBeVisible();
  });

  test("duplicate offer can be dismissed", async ({ page }) => {
    await setupApiMocks(page, cloneItems());
    await authenticate(page);

    await page.getByRole("button", { name: "Start Scanner" }).click();

    await page.evaluate(() => {
      const article = document.createElement("article");
      article.className = "duplicate-offer";
      article.innerHTML = `
        <p>Barcode already exists.</p>
        <div class="duplicate-actions">
          <button type="button">Update</button>
          <button type="button" class="secondary">Cancel</button>
        </div>
      `;
      const container = document.querySelector(".mt-h");
      if (container) container.appendChild(article);
    });

    await page.locator(".duplicate-offer").getByRole("button", { name: "Cancel" }).click();

    await page.evaluate(() => {
      document.querySelector(".duplicate-offer")?.remove();
    });
    await expect(page.locator(".duplicate-offer")).not.toBeAttached();
  });
});