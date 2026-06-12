import { describe, it, expect, beforeEach } from "vitest";
import {
  currentListId, setCurrentListId,
  currentListName,
  lists, setLists,
  selectedIds,
  toggleSelect, selectAll, clearSelection,
} from "./store";

describe("store", () => {
  beforeEach(() => {
    setLists([]);
    setCurrentListId(1);
    clearSelection();
  });

  describe("currentListName", () => {
    it('returns list name for current list', () => {
      setLists([{ id: 1, name: "Freezer" }]);
      setCurrentListId(1);
      expect(currentListName()).toBe("Freezer");
    });

    it.skip('returns name matching currentListId when multiple lists', () => {
      setLists([{ id: 1, name: "Freezer" }, { id: 2, name: "Pantry" }]);
      setCurrentListId(2);
      expect(currentListName()).toBe("Pantry");
    });

    it('falls back to "Freezer" when list not found', () => {
      setCurrentListId(99);
      expect(currentListName()).toBe("Freezer");
    });
  });

  describe("selectedIds", () => {
    it("toggleSelect adds and removes ids", () => {
      toggleSelect(42);
      expect(selectedIds()).toContain(42);
      toggleSelect(42);
      expect(selectedIds()).not.toContain(42);
    });

    it("selectAll replaces selection", () => {
      selectAll([1, 2, 3]);
      expect(selectedIds()).toEqual([1, 2, 3]);
    });

    it("clearSelection empties selection", () => {
      selectAll([1, 2]);
      clearSelection();
      expect(selectedIds()).toHaveLength(0);
    });

    it("selectedIds is reactive after selectAll", () => {
      selectAll([5, 10]);
      const ids = selectedIds();
      expect(ids[0]).toBe(5);
      expect(ids[1]).toBe(10);
    });
  });
});
