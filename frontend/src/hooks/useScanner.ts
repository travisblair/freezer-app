import { createSignal, onCleanup } from "solid-js";
import { useCamera } from "./useCamera";
import { api } from "../api";
import { bumpItemsVersion } from "../store";
import { getFirstShelfId } from "../helpers";
import type { Item, DuplicateOfferData, Shelf } from "../types";
import {
  SCANNER_DUPLICATE_COOLDOWN_MS,
  SCANNER_PROCESSING_TIMEOUT_MS,
  FEEDBACK_DISPLAY_MS,
} from "../constants";

interface Feedback {
  type: "success" | "error";
}

interface ScanPrompt {
  barcode: string;
  mode?: "create" | "decrement-block";
}

export interface ScannerControls {
  // State
  expanded: () => boolean;
  scanning: () => boolean;
  mode: () => "increment" | "decrement";
  setMode: (v: "increment" | "decrement") => void;
  quantity: () => number;
  setQuantity: (v: number) => void;
  feedback: () => Feedback | null;
  prompt: () => ScanPrompt | null;
  cameraError: () => string;
  duplicateOffer: () => DuplicateOfferData | null;
  setDuplicateOffer: (v: DuplicateOfferData | null) => void;
  isProcessing: () => boolean;
  setFeedback: (v: Feedback | null) => void;
  linkBarcode: () => string | null;
  selectedShelfId: () => number;
  setSelectedShelfId: (v: number) => void;
  shelves: () => Shelf[];
  // Actions
  toggleExpanded: () => Promise<void>;
  startCamera: () => Promise<void>;
  stopCamera: () => Promise<void>;
  showCreateForm: () => void;
  handleSwitchToAddAndCreate: () => void;
  handleDuplicateResolve: (resolveMode: string) => Promise<void>;
  handleCreateNew: (name: string, qty: number) => Promise<void>;
  handleOpenLinkModal: () => void;
  handleConfirmLink: (itemId: number) => Promise<void>;
  handleCancelLink: () => void;
  handleCancelPrompt: () => void;
}

/** Shared mutable state for duplicate scan prevention across component instances. */
let lastScannedBarcode = "";
let lastScannedTime = 0;

/**
 * Encapsulates scanner orchestration: duplicate prevention, scan processing,
 * visual feedback, API calls, and the "Create new / Add to existing" flow
 * for unknown barcodes.
 *
 * When in decrement mode, unknown barcodes show a specialized prompt
 * because decrementing a non-existent item is semantically wrong.
 *
 * Camera lifecycle is delegated to useCamera.
 */
export function useScanner(): ScannerControls {
  const [expanded, setExpanded] = createSignal(false);
  const [mode, setMode] = createSignal<"increment" | "decrement">("increment");
  const [quantity, setQuantity] = createSignal(1);
  const [feedback, setFeedback] = createSignal<Feedback | null>(null);
  const [prompt, setPrompt] = createSignal<ScanPrompt | null>(null);
  const [duplicateOffer, setDuplicateOffer] = createSignal<DuplicateOfferData | null>(null);
  const [isProcessing, setIsProcessing] = createSignal(false);
  const [linkBarcode, setLinkBarcode] = createSignal<string | null>(null);
  const [selectedShelfId, setSelectedShelfId] = createSignal(1);
  const [shelves, setShelves] = createSignal<Shelf[]>([]);

  let feedbackTimer: ReturnType<typeof setTimeout> | null = null;
  let processingTimer: ReturnType<typeof setTimeout> | null = null;

  function clearFeedbackLater() {
    if (feedbackTimer) clearTimeout(feedbackTimer);
    feedbackTimer = setTimeout(() => setFeedback(null), FEEDBACK_DISPLAY_MS);
  }

  function doneProcessing() {
    setIsProcessing(false);
    if (processingTimer) {
      clearTimeout(processingTimer);
      processingTimer = null;
    }
  }

  function startProcessing() {
    setIsProcessing(true);
    processingTimer = setTimeout(doneProcessing, SCANNER_PROCESSING_TIMEOUT_MS);
  }

  /** Stop camera and collapse scanner to show table results */
  async function collapseScanner() {
    await cam.stopCamera();
    setExpanded(false);
  }

  const cam = useCamera(async (decodedText: string) => {
    const now = Date.now();
    if (decodedText === lastScannedBarcode && now - lastScannedTime < SCANNER_DUPLICATE_COOLDOWN_MS) return;
    lastScannedBarcode = decodedText;
    lastScannedTime = now;

    if (isProcessing()) return;
    startProcessing();

    try {
      const data = await api.getItem(decodedText);
      if (data.found) {
        const item = data.item as Item;
        // Default to item's first shelf, but use user-selected shelf if set
        const sId = selectedShelfId() !== 1 || getFirstShelfId(item) === 1
          ? selectedShelfId()
          : getFirstShelfId(item);
        await api.scan(decodedText, mode(), quantity(), sId);
        bumpItemsVersion();
        setFeedback({ type: "success" });
        clearFeedbackLater();
        await collapseScanner();
        doneProcessing();
      } else if (mode() === "decrement") {
        // Decrement mode + unknown barcode: show specialized prompt
        doneProcessing();
        await cam.stopCamera();
        setPrompt({ barcode: decodedText, mode: "decrement-block" });
      } else {
        // Increment mode + unknown barcode: normal create/link/ignore prompt
        doneProcessing();
        await cam.stopCamera();
        setPrompt({ barcode: decodedText });
      }
    } catch (err: unknown) {
      if (err instanceof Error && err.message !== "STALE_RESPONSE") {
        setFeedback({ type: "error" });
        clearFeedbackLater();
        if (import.meta.env.DEV) console.error(err);
      }
      doneProcessing();
    }
  });

  /** Show the create-new form (normal prompt → name + qty form) */
  function showCreateForm() {
    const p = prompt();
    if (p) setPrompt({ ...p, mode: "create" });
  }

  /**
   * Switch from decrement-blocked prompt to increment mode and show the
   * create-new form. This resolves the "Remove mode for unknown barcode" scenario.
   */
  function handleSwitchToAddAndCreate() {
    setMode("increment");
    const p = prompt();
    if (p) setPrompt({ ...p, mode: "create" });
  }

  /** "Create new item" — collect name + quantity */
  async function handleCreateNew(name: string, qty: number) {
    const bc = prompt()?.barcode;
    setPrompt(null);
    doneProcessing();
    try {
      await api.create(bc || null, name, qty, selectedShelfId());
      bumpItemsVersion();
      setFeedback({ type: "success" });
      clearFeedbackLater();
      setExpanded(false);
    } catch (err: unknown) {
      const error = err as { status?: number; item?: Item };
      if (error.status === 409) {
        setDuplicateOffer({ barcode: bc || "", existing: error.item! });
      } else {
        setFeedback({ type: "error" });
        clearFeedbackLater();
        if (import.meta.env.DEV) console.error(err);
      }
    }
  }

  /** "Add to existing" — show search/link modal */
  function handleOpenLinkModal() {
    const bc = prompt()?.barcode;
    setPrompt(null);
    doneProcessing();
    setLinkBarcode(bc || null);
  }

  /** Confirm link: attach the barcode to the selected item */
  async function handleConfirmLink(itemId: number) {
    const bc = linkBarcode();
    setLinkBarcode(null);
    doneProcessing();
    try {
      await api.linkBarcode(itemId, bc!);
      await api.scan(bc!, mode(), quantity(), selectedShelfId());
      bumpItemsVersion();
      setFeedback({ type: "success" });
      clearFeedbackLater();
      setExpanded(false);
    } catch (err) {
      setFeedback({ type: "error" });
      clearFeedbackLater();
    }
  }

  function handleCancelLink() {
    setLinkBarcode(null);
    doneProcessing();
  }

  /** Dismiss the prompt without action */
  function handleCancelPrompt() {
    setPrompt(null);
    doneProcessing();
  }

  async function toggleExpanded() {
    if (expanded()) {
      await cam.stopCamera();
      setExpanded(false);
    } else {
      setExpanded(true);
      // Load shelves for the shelf select dropdown
      api.allShelves().then(setShelves).catch(() => {});
    }
  }

  async function handleDuplicateResolve(resolveMode: string) {
    const offer = duplicateOffer();
    setDuplicateOffer(null);
    doneProcessing();
    try {
      await api.scan(offer!.barcode, resolveMode, quantity(), selectedShelfId());
      bumpItemsVersion();
      setFeedback({ type: "success" });
      clearFeedbackLater();
      setExpanded(false);
    } catch (err) {
      setFeedback({ type: "error" });
      clearFeedbackLater();
    }
  }

  onCleanup(() => {
    cam.cleanup();
    if (feedbackTimer) clearTimeout(feedbackTimer);
    if (processingTimer) clearTimeout(processingTimer);
  });

  return {
    // State
    expanded, scanning: cam.scanning, mode, setMode, quantity, setQuantity,
    feedback, prompt, cameraError: cam.cameraError, duplicateOffer, setDuplicateOffer,
    isProcessing, setFeedback, linkBarcode,
    selectedShelfId, setSelectedShelfId, shelves,
    // Actions
    toggleExpanded, startCamera: cam.startCamera, stopCamera: cam.stopCamera,
    showCreateForm, handleSwitchToAddAndCreate,
    handleDuplicateResolve, handleCreateNew,
    handleOpenLinkModal, handleConfirmLink, handleCancelLink, handleCancelPrompt,
  };
}