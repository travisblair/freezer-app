import { createSignal } from "solid-js";
import { Html5Qrcode } from "html5-qrcode";

export interface CameraControls {
  scanning: () => boolean;
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
  const [cameraError, setCameraError] = createSignal("");

  let scanner: Html5Qrcode | null = null;

  async function startCamera() {
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
      setScanning(true);
    } catch (err) {
      setCameraError(
        "Camera access denied or unavailable. Please grant camera permission or use manual entry.",
      );
      if (import.meta.env.DEV) console.error(err);
    }
  }

  async function stopCamera() {
    if (scanner) {
      try { await scanner.stop(); } catch (_) { /* ignore */ }
      scanner = null;
    }
    setScanning(false);
  }

  function cleanup() {
    if (scanner) scanner.stop().catch(() => {});
  }

  return { scanning, cameraError, startCamera, stopCamera, cleanup };
}