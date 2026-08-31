import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { totalCount, getFirstShelfId, createPerKeyDebouncer } from "./helpers";
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

describe("createPerKeyDebouncer", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("fires each key's call independently (cross-key edits are not dropped)", () => {
    const calls: [number, number][] = [];
    const d = createPerKeyDebouncer((key: number, value: number) => calls.push([key, value]), 400);

    d.schedule(1, 5);
    d.schedule(2, 7); // different key within the window — must NOT cancel key 1

    vi.advanceTimersByTime(400);
    expect(calls).toEqual([[1, 5], [2, 7]]);
  });

  it("coalesces same-key edits to the latest value", () => {
    const calls: [number, number][] = [];
    const d = createPerKeyDebouncer((key: number, value: number) => calls.push([key, value]), 400);

    d.schedule(1, 5);
    vi.advanceTimersByTime(100);
    d.schedule(1, 6);
    vi.advanceTimersByTime(400);

    expect(calls).toEqual([[1, 6]]);
  });

  it("cancelAll clears pending timers", () => {
    const calls: [number, number][] = [];
    const d = createPerKeyDebouncer((key: number, value: number) => calls.push([key, value]), 400);

    d.schedule(1, 5);
    d.cancelAll();
    vi.advanceTimersByTime(400);

    expect(calls).toEqual([]);
  });
});
