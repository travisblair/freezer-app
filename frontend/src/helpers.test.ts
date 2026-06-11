import { describe, it, expect } from "vitest";
import { totalCount } from "./helpers";
import type { Item } from "./types";

describe("totalCount", () => {
  it("returns 0 when shelves is undefined", () => {
    const item: Item = { id: 1, name: "Test" };
    expect(totalCount(item)).toBe(0);
  });

  it("returns 0 when shelves is empty", () => {
    const item: Item = { id: 1, name: "Test", shelves: [] };
    expect(totalCount(item)).toBe(0);
  });

  it("sums counts across all shelves", () => {
    const item: Item = {
      id: 1,
      name: "Eggs",
      shelves: [
        { id: 1, itemId: 1, shelfId: 1, count: 3 },
        { id: 2, itemId: 1, shelfId: 2, count: 2 },
      ],
    };
    expect(totalCount(item)).toBe(5);
  });

  it("returns 0 when all counts are zero", () => {
    const item: Item = {
      id: 1,
      name: "Gone",
      shelves: [
        { id: 1, itemId: 1, shelfId: 1, count: 0 },
      ],
    };
    expect(totalCount(item)).toBe(0);
  });
});
