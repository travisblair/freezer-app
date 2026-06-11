import { createSignal } from "solid-js";
import type { Item } from "../types";
import { totalCount } from "../helpers";

interface Props {
  barcode: string;
  existing: Item;
  showModeToggle?: boolean;
  onResolve: (mode: string) => void;
  onDismiss: () => void;
}

export default function DuplicateOffer(props: Props) {
  const [mode, setMode] = createSignal("increment");
  const cnt = totalCount(props.existing);

  return (
    <article class="duplicate-offer">
      <p>
        Barcode <strong>{props.barcode}</strong> already exists as "
        <em>{props.existing.name}</em>" (count: {cnt}).
        {props.showModeToggle !== false ? " Increment/Decrement instead?" : ""}
      </p>
      <div class="duplicate-actions">
        {props.showModeToggle !== false && (
          <select
            value={mode()}
            onInput={(e) => setMode((e.target as HTMLSelectElement).value)}
          >
            <option value="increment">+ Increment</option>
            <option value="decrement">− Decrement</option>
          </select>
        )}
        <button
          type="button"
          onClick={() => props.onResolve(mode())}
        >
          {cnt > 0
            ? `Update "${props.existing.name}"`
            : `Restore "${props.existing.name}"`}
        </button>
        <button type="button" class="secondary" onClick={props.onDismiss}>
          Cancel
        </button>
      </div>
    </article>
  );
}
