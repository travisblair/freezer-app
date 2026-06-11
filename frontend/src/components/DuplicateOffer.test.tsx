import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import DuplicateOffer from "./DuplicateOffer";
import type { Item } from "../types";

const existingItem: Item = {
  id: 1,
  name: "Chicken",
  shelves: [{ id: 1, itemId: 1, shelfId: 1, count: 3 }],
};

describe("DuplicateOffer", () => {
  it("shows existing item info", () => {
    render(() => (
      <DuplicateOffer
        barcode="123"
        existing={existingItem}
        showModeToggle={true}
        onResolve={vi.fn()}
        onDismiss={vi.fn()}
      />
    ));

    expect(screen.getByText("123")).toBeTruthy();
    expect(screen.getByText(/Chicken/)).toBeTruthy();
  });

  it("shows mode toggle when showModeToggle is true", () => {
    render(() => (
      <DuplicateOffer
        barcode="123"
        existing={existingItem}
        showModeToggle={true}
        onResolve={vi.fn()}
        onDismiss={vi.fn()}
      />
    ));

    expect(screen.getByText("+ Increment")).toBeTruthy();
  });

  it("shows mode toggle when showModeToggle is undefined (default)", () => {
    render(() => (
      <DuplicateOffer
        barcode="123"
        existing={existingItem}
        onResolve={vi.fn()}
        onDismiss={vi.fn()}
      />
    ));

    // Should render the select with increment option
    expect(screen.getByText("+ Increment")).toBeTruthy();
  });

  it("hides mode toggle when showModeToggle is false", () => {
    render(() => (
      <DuplicateOffer
        barcode="123"
        existing={existingItem}
        showModeToggle={false}
        onResolve={vi.fn()}
        onDismiss={vi.fn()}
      />
    ));

    // The select should not be present
    expect(screen.queryByText("+ Increment")).toBeNull();
  });
});
