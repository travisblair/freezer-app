import { createSignal, onCleanup } from "solid-js";
import { api } from "../api";
import type { Item } from "../types";
import { totalCount } from "../helpers";
import BaseModal from "./BaseModal";
import { LINK_SEARCH_DEBOUNCE_MS } from "../constants";

interface Props {
  barcode: string;
  onConfirm: (itemId: number) => void;
  onCancel: () => void;
}

export default function LinkBarcode(props: Props) {
  const [query, setQuery] = createSignal("");
  const [results, setResults] = createSignal<Item[]>([]);
  const [selectedId, setSelectedId] = createSignal<number | null>(null);
  const [linking, setLinking] = createSignal(false);

  let searchTimer: ReturnType<typeof setTimeout> | null = null;

  function handleSearchInput(e: Event) {
    const target = e.target as HTMLInputElement;
    const val = target.value;
    setQuery(val);
    if (searchTimer) clearTimeout(searchTimer);
    if (!val.trim()) {
      setResults([]);
      return;
    }
    searchTimer = setTimeout(async () => {
      try {
        const items = await api.searchItems(val.trim());
        setResults(items);
      } catch {
        // Silently ignore search errors — user can retry
        setResults([]);
      }
    }, LINK_SEARCH_DEBOUNCE_MS);
  }

  async function handleConfirm() {
    const id = selectedId();
    if (!id) return;
    setLinking(true);
    try {
      await props.onConfirm(id);
    } finally {
      setLinking(false);
    }
  }

  onCleanup(() => {
    if (searchTimer) clearTimeout(searchTimer);
  });

  const footer = (
    <>
      <button type="button" class="secondary" onClick={props.onCancel}>
        Cancel
      </button>
      <button
        type="button"
        disabled={!selectedId()}
        aria-busy={linking()}
        onClick={handleConfirm}
      >
        Link & Add
      </button>
    </>
  );

  return (
    <BaseModal
      title="Add to existing item"
      onClose={props.onCancel}
      footer={footer}
    >
      <p class="mb-h">
        Scanning barcode: <code>{props.barcode}</code>
      </p>

      <input
        type="search"
        placeholder="Type a name to find the item..."
        value={query()}
        onInput={handleSearchInput}
        autofocus
        class="link-barcode-search"
      />

      {results().length > 0 && (
        <div class="link-barcode-results">
          {results().map((item) => (
            <label
              class={`link-barcode-item${selectedId() === item.id ? " selected" : ""}`}
              onClick={() => setSelectedId(item.id)}
            >
              <input
                type="radio"
                name="link-item"
                checked={selectedId() === item.id}
                onChange={() => setSelectedId(item.id)}
              />
              {item.name}
              <small>({totalCount(item)} in stock)</small>
            </label>
          ))}
        </div>
      )}

      {query().trim() && results().length === 0 && (
        <p class="center-text">No items found for "{query()}"</p>
      )}
    </BaseModal>
  );
}