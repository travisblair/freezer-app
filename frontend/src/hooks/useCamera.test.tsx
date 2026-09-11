import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@solidjs/testing-library";
import { useCamera } from "./useCamera";
import type { CameraControls } from "./useCamera";

const mocks = vi.hoisted(() => ({
  start: vi.fn().mockResolvedValue(undefined),
  stop: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("html5-qrcode", () => ({
  Html5Qrcode: class {
    start = mocks.start;
    stop = mocks.stop;
  },
}));

function TestHarness(props: { onHook: (c: CameraControls) => void }) {
  const controls = useCamera(null);
  props.onHook(controls);
  return <div data-testid="cam" />;
}

describe("useCamera", () => {
  beforeEach(() => {
    mocks.start.mockClear();
  });

  it("ignores a second start while the first is still starting", async () => {
    // scanning() only flips true AFTER start() resolves, so without the
    // in-flight guard a double-click during the permission prompt used to
    // spawn a second (orphaned) Html5Qrcode instance.
    let resolveStart!: () => void;
    mocks.start.mockImplementationOnce(
      () =>
        new Promise<void>((res) => {
          resolveStart = res;
        }),
    );

    let hook: CameraControls | null = null;
    render(() => <TestHarness onHook={(c) => { hook = c; }} />);
    await vi.waitFor(() => expect(hook).not.toBeNull());

    const p1 = hook!.startCamera();
    const p2 = hook!.startCamera(); // must no-op while starting
    resolveStart();
    await Promise.all([p1, p2]);

    expect(mocks.start).toHaveBeenCalledTimes(1);
    expect(hook!.scanning()).toBe(true);
  });
});
