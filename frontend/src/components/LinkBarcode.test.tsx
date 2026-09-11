import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent } from "@solidjs/testing-library";
import LinkBarcode from "./LinkBarcode";
import type { Item } from "../types";

vi.mock("../api", () => ({
  api: {
    searchItems: vi.fn(),
  },
}));

import { api } from "../api";

describe("LinkBarcode", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(api.searchItems).mockReset();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not repopulate results when the query is cleared mid-flight", async () => {
    // A fetch launched by the debounce can still be in flight when the
    // user clears the box; its response used to repopulate the list under
    // an empty search.
    let resolveFetch!: (v: Item[]) => void;
    const pending = new Promise<Item[]>((res) => {
      resolveFetch = res;
    });
    vi.mocked(api.searchItems).mockReturnValue(pending);

    render(() => (
      <LinkBarcode barcode="X" onConfirm={() => {}} onCancel={() => {}} />
    ));

    const input = screen.getByPlaceholderText("Type a name to find the item...");
    fireEvent.input(input, { target: { value: "milk" } });
    vi.advanceTimersByTime(500); // debounce fires -> fetch starts (pending)

    fireEvent.input(input, { target: { value: "" } }); // clear mid-flight
    resolveFetch([{ id: 1, name: "Milk" } as Item]);

    // Flush microtasks BEFORE asserting: the old code's repopulation lands
    // one tick after the resolution, and vi.waitFor's first check runs
    // synchronously — without these flushes the test passed against the
    // old code and pinned nothing.
    await Promise.resolve();
    await Promise.resolve();
    expect(screen.queryByText(/Milk/)).toBeNull();
  });
});
