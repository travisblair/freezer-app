import { createSignal } from "solid-js";
import { Html5Qrcode } from "html5-qrcode";

export interface CameraControls {
  scanning: () => boolean;
  starting: () => boolean;
  cameraError: () => string;
  startCamera: () => Promise<void>;
  stopCamera: () => Promise<void>;
  cleanup: () => void;
}

/**
 * Camera lifecycle management.
 * Handles starting/stopping the camera and exposing camera error state.
 */
export function useCamera(onScan: ((decodedText: string) => void) | null): CameraControls {
  const [scanning, setScanning] = createSignal(false);
  const [starting, setStarting] = createSignal(false);
  const [cameraError, setCameraError] = createSignal("");

  let scanner: Html5Qrcode | null = null;
  // Generation counter: a stop() during an in-flight start() bumps it, so
  // the superseded start can't flip scanning() true with scanner === null.
  let startGen = 0;

  async function startCamera() {
    // In-flight guard: scanning() only flips true AFTER start() resolves,
    // so a double-click during the permission prompt used to spawn a
    // second Html5Qrcode instance and orphan the first (un-stoppable).
    if (scanning() || starting()) return;
    const gen = ++startGen;
    setStarting(true);
    setCameraError("");
    try {
      scanner = new Html5Qrcode("reader");
      await scanner.start(
        { facingMode: "environment" },
        {
          fps: 15,
          qrbox: (viewfinderWidth: number, viewfinderHeight: number) => {
            const size = Math.floor(Math.min(viewfinderWidth, viewfinderHeight) * 0.7);
            return { width: size, height: size };
          },
        },
        (decodedText: string) => {
          if (onScan) onScan(decodedText);
        },
        () => {},
      );
      if (gen !== startGen) return; // superseded by a stop mid-start
      setScanning(true);
    } catch (err) {
      setCameraError(
        "Camera access denied or unavailable. Please grant camera permission or use manual entry.",
      );
      if (import.meta.env.DEV) console.error(err);
    } finally {
      setStarting(false);
    }
  }

  async function stopCamera() {
    startGen++; // invalidate any in-flight start
    if (scanner) {
      try { await scanner.stop(); } catch (_) { /* ignore */ }
      scanner = null;
    }
    setScanning(false);
  }

  function cleanup() {
    startGen++;
    if (scanner) scanner.stop().catch(() => {});
  }

  return { scanning, starting, cameraError, startCamera, stopCamera, cleanup };
}