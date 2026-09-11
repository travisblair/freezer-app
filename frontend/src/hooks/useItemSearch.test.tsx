import { describe, it, expect, vi } from "vitest";
import { render } from "@solidjs/testing-library";
import { useItemSearch } from "./useItemSearch";
import type { ItemSearchControls } from "./useItemSearch";
import { api } from "../api";
import { items } from "../store";
import type { Item } from "../types";

// Mock api module — silence network calls
vi.mock("../api", () => ({
  api: {
    getItems: vi.fn().mockResolvedValue([]),
    getShelves: vi.fn().mockResolvedValue([]),
    allShelves: vi.fn().mockResolvedValue([]),
    getLists: vi.fn().mockResolvedValue([]),
  },
}));

// Test wrapper — renders nothing, exposes the hook controls
function TestHarness(props: { onHook: (c: ItemSearchControls) => void }) {
  const controls = useItemSearch();
  props.onHook(controls);
  return <div data-testid="harness" />;
}

describe("useItemSearch", () => {
  describe("clearSearch", () => {
    it("is a function returned in hook controls", async () => {
      let hook: ItemSearchControls | null = null;
      render(() => <TestHarness onHook={(c) => { hook = c; }} />);

      await vi.waitFor(() => {
        expect(hook).not.toBeNull();
      });

      expect(typeof hook!.clearSearch).toBe("function");
    });

    it("can be called without throwing", async () => {
      let hook: ItemSearchControls | null = null;
      render(() => <TestHarness onHook={(c) => { hook = c; }} />);

      await vi.waitFor(() => {
        expect(hook).not.toBeNull();
      });

      expect(() => hook!.clearSearch()).not.toThrow();
    });
  });

  describe("handleSearchInput", () => {
    it("is a function in controls", async () => {
      let hook: ItemSearchControls | null = null;
      render(() => <TestHarness onHook={(c) => { hook = c; }} />);

      await vi.waitFor(() => {
        expect(hook).not.toBeNull();
      });

      expect(typeof hook!.handleSearchInput).toBe("function");
    });
  });

  describe("loading", () => {
    it("settles to false after initial data load", async () => {
      let hook: ItemSearchControls | null = null;
      render(() => <TestHarness onHook={(c) => { hook = c; }} />);

      await vi.waitFor(() => {
        expect(hook).not.toBeNull();
      });

      // The createEffect triggers loadItems() which sets loading=true briefly.
      // Wait for it to settle back to false after the API mock resolves.
      await vi.waitFor(() => {
        expect(hook!.loading()).toBe(false);
      });
    });
  });

  describe("stale responses", () => {
    it("ignores an older response that lands after a newer one", async () => {
      let resolveOld!: (v: Item[]) => void;
      let resolveNew!: (v: Item[]) => void;
      const oldReq = new Promise<Item[]>((res) => { resolveOld = res; });
      const newReq = new Promise<Item[]>((res) => { resolveNew = res; });

      const getItemsMock = vi.mocked(api.getItems);
      getItemsMock.mockReset();
      getItemsMock
        .mockImplementationOnce(() => oldReq)
        .mockImplementationOnce(() => newReq);

      let hook: ItemSearchControls | null = null;
      render(() => <TestHarness onHook={(c) => { hook = c; }} />);
      await vi.waitFor(() => expect(hook).not.toBeNull());

      // The mount effect fired load #1 (oldReq in flight). Fire load #2.
      const p2 = hook!.loadItems();

      // NEW resolves first, then OLD — the stale response must not win.
      resolveNew([{ id: 2, name: "New" } as Item]);
      await vi.waitFor(() => expect(items()).toEqual([{ id: 2, name: "New" }]));

      resolveOld([{ id: 1, name: "Old" } as Item]);
      await p2;
      expect(items()).toEqual([{ id: 2, name: "New" }]);
    });
  });
});
