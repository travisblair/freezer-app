import { describe, it, expect, vi } from "vitest";
import { render } from "@solidjs/testing-library";
import { useScanner } from "./useScanner";
import type { ScannerControls } from "./useScanner";
import { api } from "../api";

const camMocks = vi.hoisted(() => ({
  start: vi.fn().mockResolvedValue(undefined),
  stop: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("html5-qrcode", () => ({
  Html5Qrcode: class {
    start = camMocks.start;
    stop = camMocks.stop;
  },
}));

vi.mock("../api", () => ({
  api: {
    getItem: vi.fn().mockResolvedValue({ found: false }),
    scan: vi.fn().mockResolvedValue(undefined),
    create: vi.fn(),
    linkBarcode: vi.fn(),
    allShelves: vi.fn().mockResolvedValue([]),
  },
}));

function TestHarness(props: { onHook: (c: ScannerControls) => void }) {
  const controls = useScanner();
  props.onHook(controls);
  return <div data-testid="sc" />;
}

describe("useScanner 409 handling", () => {
  it("shows error feedback instead of a duplicate offer when the 409 carries no item", async () => {
    // UNIQUE-race fallback: 409 with no item. The old code rendered
    // DuplicateOffer with existing=undefined and threw mid-render.
    vi.mocked(api.create).mockRejectedValue(
      Object.assign(new Error("barcode already exists"), {
        status: 409,
        error: "barcode already exists",
      }),
    );

    let hook: ScannerControls | null = null;
    render(() => <TestHarness onHook={(c) => { hook = c; }} />);
    await vi.waitFor(() => expect(hook).not.toBeNull());

    await hook!.handleCreateNew("Milk", 1);

    expect(hook!.duplicateOffer()).toBeNull();
    expect(hook!.feedback()?.type).toBe("error");
  });
});
