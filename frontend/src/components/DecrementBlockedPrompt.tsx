interface PromptProps {
  barcode: string;
  onCreateNew: () => void;
  onLinkToExisting: () => void;
  onCancel: () => void;
}

/**
 * Shown when an unknown barcode is scanned in decrement mode.
 * Decrementing a non-existent item doesn't make sense, so we offer
 * to switch to Add mode or cancel.
 */
export default function DecrementBlockedPrompt(props: PromptProps) {
  return (
    <div class="barcode-prompt">
      <p class="barcode-value">
        New barcode: <strong>{props.barcode}</strong>
      </p>
      <p class="barcode-hint">
        ⚠ Remove mode is for existing items only. This barcode hasn't been
        seen before and cannot be removed.
      </p>
      <div class="barcode-actions">
        <button type="button" onClick={props.onCreateNew}>
          Switch to Add & create item
        </button>
        <button type="button" class="outline" onClick={props.onLinkToExisting}>
          Link to existing
        </button>
        <button type="button" class="secondary outline" onClick={props.onCancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}