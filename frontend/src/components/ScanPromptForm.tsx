import { createSignal } from "solid-js";

interface Props {
  barcode: string;
  onSubmit: (name: string, qty: number) => void;
  onCancel: () => void;
}

export default function ScanPromptForm(props: Props) {
  const [name, setName] = createSignal("");
  const [qty, setQty] = createSignal(1);

  function handleSubmit(e: Event) {
    e.preventDefault();
    const n = name().trim();
    if (!n) return;
    props.onSubmit(n, qty());
  }

  return (
    <form onSubmit={handleSubmit} class="scan-prompt-form">
      {props.barcode && (
        <div class="scan-prompt-label">
          Labeling barcode: <code>{props.barcode}</code>
        </div>
      )}
      <label>
        Item Name
        <input
          type="text"
          value={name()}
          onInput={(e) => setName((e.target as HTMLInputElement).value)}
          placeholder="e.g. Chicken Breast"
          maxlength="100"
          required
          autofocus
        />
      </label>
      <label>
        Qty
        <input
          type="number"
          min="1"
          max="9999"
          value={qty()}
          onInput={(e) => setQty(parseInt((e.target as HTMLInputElement).value, 10) || 1)}
        />
      </label>
      <button type="submit">Add</button>
      <button type="button" class="secondary" onClick={props.onCancel}>
        Cancel
      </button>
    </form>
  );
}