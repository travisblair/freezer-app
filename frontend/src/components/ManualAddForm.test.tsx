import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent } from "@solidjs/testing-library";
import ManualAddForm from "./ManualAddForm";
import type { Item } from "../types";
import { LINK_SEARCH_DEBOUNCE_MS } from "../constants";

vi.mock("../api", () => ({
  api: {
    getShelves: vi.fn().mockResolvedValue([]),
    create: vi.fn(),
    scan: vi.fn().mockResolvedValue(undefined),
    searchItems: vi.fn().mockResolvedValue([]),
    restock: vi.fn(),
  },
}));

import { api } from "../api";

const bacon = (): Item => ({ id: 5, name: "Bacon", shelves: [] });

/** Flush the microtask queue the way the LinkBarcode stale-guard test does. */
async function flush() {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}

describe("ManualAddForm 409 handling", () => {
  it("shows an error instead of crashing when the 409 carries no item", async () => {
    // The backend's UNIQUE-constraint race fallback returns 409 with
    // {"error":"barcode already exists"} and NO item. The old code
    // rendered DuplicateOffer with existing=undefined and threw a
    // TypeError mid-render.
    vi.mocked(api.create).mockRejectedValue(
      Object.assign(new Error("barcode already exists"), {
        status: 409,
        error: "barcode already exists",
      }),
    );

    render(() => <ManualAddForm listId={1} />);
    const nameInput = screen.getByPlaceholderText("e.g. Chicken Breast");
    fireEvent.input(nameInput, { target: { value: "Milk" } });

    const form = nameInput.closest("form")!;
    fireEvent.submit(form);

    await vi.waitFor(() => {
      expect(screen.getByText("barcode already exists")).toBeTruthy();
    });
    // No DuplicateOffer render (the crash path).
    expect(document.querySelector(".duplicate-offer")).toBeNull();
  });
});

describe("ManualAddForm restock autocomplete", () => {
  beforeEach(() => {
    vi.mocked(api.searchItems).mockReset();
    vi.mocked(api.restock).mockReset();
    vi.mocked(api.create).mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  /** Render the form and pick `item` out of the debounced results. */
  async function renderAndSelect(query: string, item: Item) {
    vi.useFakeTimers();
    vi.mocked(api.searchItems).mockResolvedValue([item]);
    render(() => <ManualAddForm listId={1} />);
    const input = screen.getByPlaceholderText("e.g. Chicken Breast");
    fireEvent.input(input, { target: { value: query } });
    await vi.advanceTimersByTimeAsync(LINK_SEARCH_DEBOUNCE_MS);
    await flush();
    fireEvent.mouseDown(screen.getByRole("button", { name: new RegExp(item.name) }));
  }

  it("shows debounced results with stock counts after a single search request", async () => {
    vi.useFakeTimers();
    vi.mocked(api.searchItems).mockResolvedValue([
      { id: 1, name: "Chicken Breast", shelves: [{ id: 1, itemId: 1, shelfId: 1, count: 3 }] } as Item,
      bacon(),
    ]);

    render(() => <ManualAddForm listId={1} />);
    const input = screen.getByPlaceholderText("e.g. Chicken Breast");
    fireEvent.input(input, { target: { value: "c" } });
    fireEvent.input(input, { target: { value: "ch" } });
    fireEvent.input(input, { target: { value: "chick" } });
    expect(api.searchItems).not.toHaveBeenCalled(); // debounce not elapsed yet

    await vi.advanceTimersByTimeAsync(LINK_SEARCH_DEBOUNCE_MS);
    await flush();

    expect(api.searchItems).toHaveBeenCalledTimes(1);
    expect(api.searchItems).toHaveBeenCalledWith("chick");
    expect(screen.getByText(/Chicken Breast/)).toBeTruthy();
    expect(screen.getByText("(3 in stock)")).toBeTruthy();
    expect(screen.getByText("(out of stock)")).toBeTruthy();
  });

  it("shows the no-results message and hides the dropdown when the query is cleared", async () => {
    vi.useFakeTimers();
    vi.mocked(api.searchItems).mockResolvedValue([]);

    render(() => <ManualAddForm listId={1} />);
    const input = screen.getByPlaceholderText("e.g. Chicken Breast");
    fireEvent.input(input, { target: { value: "zzz" } });
    await vi.advanceTimersByTimeAsync(LINK_SEARCH_DEBOUNCE_MS);
    await flush();

    expect(screen.getByText('No items found for "zzz"')).toBeTruthy();

    fireEvent.input(input, { target: { value: "" } });
    expect(screen.queryByText('No items found for "zzz"')).toBeNull();
    expect(document.querySelector(".manual-add-dropdown")).toBeNull();
  });

  it("selecting a result enters restock mode: chip, Restock button, barcode hidden", async () => {
    await renderAndSelect("bacon", bacon());

    expect(screen.queryByPlaceholderText("e.g. Chicken Breast")).toBeNull();
    expect(document.querySelector(".restock-chip")!.textContent).toContain("Bacon");
    expect(screen.getByRole("button", { name: "Restock" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add Item" })).toBeNull();
    expect(screen.queryByPlaceholderText("Optional")).toBeNull(); // barcode field hidden
  });

  it("cancels a pending debounce and issues no searches while restock mode is active", async () => {
    vi.useFakeTimers();
    vi.mocked(api.searchItems).mockResolvedValue([bacon()]);

    render(() => <ManualAddForm listId={1} />);
    const input = screen.getByPlaceholderText("e.g. Chicken Breast");
    fireEvent.input(input, { target: { value: "ba" } });
    await vi.advanceTimersByTimeAsync(LINK_SEARCH_DEBOUNCE_MS);
    await flush();
    expect(api.searchItems).toHaveBeenCalledTimes(1);

    // Re-typing schedules a fresh debounce; picking from the still-visible
    // old results must cancel it rather than search under the chip.
    fireEvent.input(input, { target: { value: "baco" } });
    fireEvent.mouseDown(screen.getByRole("button", { name: /Bacon/ }));

    await vi.advanceTimersByTimeAsync(2000);
    expect(api.searchItems).toHaveBeenCalledTimes(1);
  });

  it("submits a restock with itemId/quantity/shelfId and resets on success", async () => {
    await renderAndSelect("bacon", bacon());
    vi.mocked(api.restock).mockResolvedValue({ id: 5, name: "Bacon" } as Item);

    fireEvent.input(screen.getByRole("spinbutton"), { target: { value: "4" } });
    fireEvent.submit(screen.getByRole("button", { name: "Restock" }).closest("form")!);
    await flush();

    expect(api.restock).toHaveBeenCalledWith(5, 4, 1);
    expect(screen.getByText('Restocked "Bacon"')).toBeTruthy();
    // Success auto-exits restock mode and resets the form.
    expect(screen.queryByText("Bacon")).toBeNull(); // chip gone
    expect((screen.getByPlaceholderText("e.g. Chicken Breast") as HTMLInputElement).value).toBe("");
    expect(screen.getByPlaceholderText("Optional")).toBeTruthy(); // barcode field back
    expect((screen.getByRole("spinbutton") as HTMLInputElement).value).toBe("1");
  });

  it("keeps the selection and stays in restock mode when restock fails", async () => {
    await renderAndSelect("bacon", bacon());
    vi.mocked(api.restock).mockRejectedValue(
      Object.assign(new Error("shelf does not exist"), {
        status: 400,
        error: "shelf does not exist",
      }),
    );

    fireEvent.submit(screen.getByRole("button", { name: "Restock" }).closest("form")!);
    await flush();

    expect(screen.getByText("shelf does not exist")).toBeTruthy();
    expect(screen.queryByText("Bacon")).not.toBeNull(); // chip preserved for retry
    expect(screen.getByRole("button", { name: "Restock" })).toBeTruthy();
    expect(screen.queryByPlaceholderText("e.g. Chicken Breast")).toBeNull();
    expect(screen.getByRole("button", { name: "Exit restock" })).toBeTruthy(); // × still available
  });

  it("exiting restock mode restores the typed query and reopens the dropdown", async () => {
    await renderAndSelect("bacon", bacon());

    fireEvent.click(screen.getByRole("button", { name: "Exit restock" }));

    expect(document.querySelector(".restock-chip")).toBeNull();
    const input = screen.getByPlaceholderText("e.g. Chicken Breast") as HTMLInputElement;
    expect(input.value).toBe("bacon");
    expect(screen.getByRole("button", { name: "Add Item" })).toBeTruthy();
    expect(screen.getByText(/Bacon/)).toBeTruthy(); // dropdown re-opened with prior results
  });

  it("leaves the create path unchanged when no item is selected", async () => {
    vi.useFakeTimers();
    vi.mocked(api.create).mockResolvedValue({ id: 9, name: "Pizza Rolls" } as Item);

    render(() => <ManualAddForm listId={1} />);
    const input = screen.getByPlaceholderText("e.g. Chicken Breast");
    fireEvent.input(input, { target: { value: "Pizza Rolls" } });
    fireEvent.submit(input.closest("form")!);
    await flush();

    expect(api.create).toHaveBeenCalledWith(null, "Pizza Rolls", 1, 1);
    expect(api.restock).not.toHaveBeenCalled();
    expect(screen.getByText('Added "Pizza Rolls"')).toBeTruthy();
  });

  it("a slow search response cannot repopulate results after the query is cleared", async () => {
    vi.useFakeTimers();
    let resolveFetch!: (v: Item[]) => void;
    const pending = new Promise<Item[]>((res) => {
      resolveFetch = res;
    });
    vi.mocked(api.searchItems).mockReturnValue(pending);

    render(() => <ManualAddForm listId={1} />);
    const input = screen.getByPlaceholderText("e.g. Chicken Breast");
    fireEvent.input(input, { target: { value: "milk" } });
    vi.advanceTimersByTime(LINK_SEARCH_DEBOUNCE_MS); // debounce fires -> fetch starts (pending)

    fireEvent.input(input, { target: { value: "" } }); // clear mid-flight
    resolveFetch([{ id: 1, name: "Milk" } as Item]);

    // Flush microtasks BEFORE asserting: the stale repopulation would land
    // one tick after the resolution.
    await flush();
    expect(screen.queryByText(/Milk/)).toBeNull();
  });
});
