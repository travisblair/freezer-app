import { describe, it, expect, vi } from "vitest";
import { render } from "@solidjs/testing-library";
import { useItemActions } from "./useItemActions";
import type { ItemActionsControls } from "./useItemActions";
import { selectAll, selectedIds } from "../store";

vi.mock("../api", () => ({
  api: {
    bulkDelete: vi.fn().mockResolvedValue(undefined),
    hardDelete: vi.fn().mockResolvedValue(undefined),
    setShelfCount: vi.fn().mockResolvedValue(undefined),
    scan: vi.fn().mockResolvedValue(undefined),
    exportCsv: vi.fn().mockResolvedValue(new Blob()),
  },
}));

function TestHarness(props: { onHook: (c: ItemActionsControls) => void }) {
  const controls = useItemActions();
  props.onHook(controls);
  return <div data-testid="ia" />;
}

describe("useItemActions", () => {
  it("hard-deleting one row keeps the rest of the selection", async () => {
    // The old code called clearSelection() on any single-row hard delete,
    // wiping an unrelated multi-row selection.
    let hook: ItemActionsControls | null = null;
    render(() => <TestHarness onHook={(c) => { hook = c; }} />);
    await vi.waitFor(() => expect(hook).not.toBeNull());

    selectAll([5, 7]);
    hook!.setConfirmDelete({ type: "hard", id: 7, name: "Gone" });
    await hook!.confirmDeleteAction();

    expect(selectedIds()).toEqual([5]);
  });
});
