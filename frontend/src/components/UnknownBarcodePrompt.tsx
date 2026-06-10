interface PromptProps {
  barcode: string;
  onCreateNew: () => void;
  onLinkToExisting: () => void;
  onCancel: () => void;
}

/** Shown when a normal unknown barcode is scanned in increment mode */
export default function UnknownBarcodePrompt(props: PromptProps) {
  return (
    <div class="barcode-prompt">
      <p class="barcode-value">
        New barcode: <strong>{props.barcode}</strong>
      </p>
      <p class="barcode-hint">
        This barcode hasn't been seen before. What would you like to do?
      </p>
      <div class="barcode-actions">
        <button type="button" onClick={props.onCreateNew}>
          Create new item
        </button>
        <button type="button" class="outline" onClick={props.onLinkToExisting}>
          Link to existing
        </button>
        <button type="button" class="secondary outline" onClick={props.onCancel}>
          Ignore
        </button>
      </div>
    </div>
  );
}