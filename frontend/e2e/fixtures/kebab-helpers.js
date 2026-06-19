// ── Kebab Menu Helpers ──────────────────────────────────────────────

import { expect } from "@playwright/test";

/**
 * Open the kebab menu on a table row and return the menu locator.
 * @param {import("@playwright/test").Locator} row
 * @returns {Promise<import("@playwright/test").Locator>}
 */
export async function openKebab(row) {
  await row.locator(".kebab-btn").click();
  const menu = row.locator(".kebab-menu");
  await expect(menu).toBeVisible();
  return menu;
}

/**
 * Click a kebab menu item by its text content.
 * @param {import("@playwright/test").Locator} row
 * @param {string} text
 */
export async function clickKebabItem(row, text) {
  const menu = await openKebab(row);
  await menu.locator(".kebab-item").filter({ hasText: text }).click();
  // Menu should close after action — verify it's gone
  await expect(menu).not.toBeAttached();
}
