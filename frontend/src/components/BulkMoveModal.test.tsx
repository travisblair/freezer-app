import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import BulkMoveModal from "./BulkMoveModal";
import type { BulkMoveItemData } from "./BulkMoveModal";
import type { Shelf, List } from "../types";

// Mock api module
vi.mock("../api", () => ({
  api: {
    moveItem: vi.fn().mockResolvedValue(undefined),
  },
}));

// Mock store
vi.mock("../store", () => ({
  bumpItemsVersion: vi.fn(),
}));

const mockShelves: Shelf[] = [
  { id: 1, name: "Shelf 1", listId: 1 },
  { id: 2, name: "Shelf 2", listId: 1 },
  { id: 3, name: "Pantry Shelf", listId: 2 },
];

const mockLists: List[] = [
  { id: 1, name: "Freezer" },
  { id: 2, name: "Pantry" },
];

const mockItems: BulkMoveItemData[] = [
  { itemId: 1, name: "Chicken Breast", sourceShelfId: 1, sourceShelfName: "Shelf 1", count: 3 },
  { itemId: 2, name: "Frozen Peas", sourceShelfId: 1, sourceShelfName: "Shelf 1", count: 5 },
];

const defaultProps = {
  items: mockItems,
  allShelves: mockShelves,
  lists: mockLists,
  onDone: vi.fn(),
  onCancel: vi.fn(),
};

describe("BulkMoveModal", () => {
  it("renders title with item count", () => {
    render(() => <BulkMoveModal {...defaultProps} />);
    expect(screen.getByText("Move 2 items")).toBeTruthy();
  });

  it("renders singular title for one item", () => {
    render(() => (
      <BulkMoveModal {...defaultProps} items={[mockItems[0]]} />
    ));
    expect(screen.getByText("Move 1 item")).toBeTruthy();
  });

  it("shows each item name and source shelf info", () => {
    render(() => <BulkMoveModal {...defaultProps} />);
    expect(screen.getByText("Chicken Breast")).toBeTruthy();
    expect(screen.getByText("Frozen Peas")).toBeTruthy();
    // Both items are from Shelf 1, so "From Shelf 1" appears twice
    const fromLabels = screen.getAllByText(/From Shelf 1/);
    expect(fromLabels).toHaveLength(2);
    expect(screen.getByText(/3 available/)).toBeTruthy();
    expect(screen.getByText(/5 available/)).toBeTruthy();
  });

  it("excludes source shelf from target options when all items share same source", () => {
    // Both items from Shelf 1 — Shelf 1 should not be a target
    render(() => <BulkMoveModal {...defaultProps} />);
    const select = screen.getByRole("combobox") as HTMLSelectElement;
    const options = Array.from(select.options).map((o) => o.value);
    // Shelf 1 should NOT be in target options
    expect(options).not.toContain("1");
    // Shelf 2 and Pantry Shelf should be available
    expect(options).toContain("2");
    expect(options).toContain("3");
  });

  it("includes shelf in target options when items have different sources", () => {
    const mixedItems: BulkMoveItemData[] = [
      { itemId: 1, name: "Beef", sourceShelfId: 1, sourceShelfName: "Shelf 1", count: 2 },
      { itemId: 2, name: "Pork", sourceShelfId: 2, sourceShelfName: "Shelf 2", count: 4 },
    ];
    render(() => (
      <BulkMoveModal {...defaultProps} items={mixedItems} />
    ));
    const select = screen.getByRole("combobox") as HTMLSelectElement;
    const options = Array.from(select.options).map((o) => o.value);
    // Since not ALL items are from the same shelf, all shelves should appear
    expect(options).toContain("1");
    expect(options).toContain("2");
    expect(options).toContain("3");
  });

  it("groups target shelves by list", () => {
    render(() => <BulkMoveModal {...defaultProps} />);
    // optgroup labels are accessible as role="group" with name
    const freezerGroup = screen.getByRole("group", { name: "Freezer" });
    expect(freezerGroup).toBeTruthy();
    const pantryGroup = screen.getByRole("group", { name: "Pantry" });
    expect(pantryGroup).toBeTruthy();
  });

  it("shows quantity inputs defaulting to available count", () => {
    render(() => <BulkMoveModal {...defaultProps} />);
    const inputs = screen.getAllByRole("spinbutton") as HTMLInputElement[];
    expect(inputs).toHaveLength(2);
    // Chicken Breast defaults to 3, Frozen Peas to 5
    const values = inputs.map((i) => i.value);
    expect(values).toContain("3");
    expect(values).toContain("5");
  });

  it("clamps quantity input between 1 and available count", () => {
    render(() => <BulkMoveModal {...defaultProps} />);
    const inputs = screen.getAllByRole("spinbutton") as HTMLInputElement[];
    // Verify min/max attributes
    expect(inputs[0].min).toBe("1");
    expect(inputs[0].max).toBe(String(mockItems[0].count));
  });

  it("has Cancel and Move All buttons", () => {
    render(() => <BulkMoveModal {...defaultProps} />);
    expect(screen.getByRole("button", { name: "Cancel" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Move All" })).toBeTruthy();
  });

  it("calls onCancel when Cancel is clicked", () => {
    const onCancel = vi.fn();
    render(() => <BulkMoveModal {...defaultProps} onCancel={onCancel} />);
    screen.getByRole("button", { name: "Cancel" }).click();
    expect(onCancel).toHaveBeenCalledOnce();
  });
});
