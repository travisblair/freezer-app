import { describe, it, expect } from "vitest";
import { totalCount, getFirstShelfId } from "./helpers";
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

describe("getFirstShelfId", () => {
  it("returns 1 when shelves is undefined", () => {
    expect(getFirstShelfId({})).toBe(1);
  });

  it("returns 1 when shelves is empty array", () => {
    expect(getFirstShelfId({ shelves: [] })).toBe(1);
  });

  it("returns the first shelf's shelfId", () => {
    expect(getFirstShelfId({
      shelves: [{ shelfId: 3, id: 1, itemId: 1, count: 2 }],
    })).toBe(3);
  });

  it("returns first shelfId with multiple shelves", () => {
    expect(getFirstShelfId({
      shelves: [
        { shelfId: 5, id: 1, itemId: 1, count: 2 },
        { shelfId: 2, id: 2, itemId: 1, count: 1 },
      ],
    })).toBe(5);
  });
});
