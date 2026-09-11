import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@solidjs/testing-library";
import ManualAddForm from "./ManualAddForm";
import { api } from "../api";

vi.mock("../api", () => ({
  api: {
    getShelves: vi.fn().mockResolvedValue([]),
    create: vi.fn(),
    scan: vi.fn().mockResolvedValue(undefined),
  },
}));

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
