import { createSignal, onCleanup, onMount, Show } from "solid-js";
import { api } from "../api";
import { bumpItemsVersion, flashStatus } from "../store";
import type { Item, Shelf, StatusFeedback, DuplicateOfferData } from "../types";
import { totalCount } from "../helpers";
import { LINK_SEARCH_DEBOUNCE_MS } from "../constants";
import StatusMessage from "./StatusMessage";
import DuplicateOffer from "./DuplicateOffer";

export default function ManualAddForm(props: { listId: number }) {
  const [name, setName] = createSignal("");
  const [barcode, setBarcode] = createSignal("");
  const [quantity, setQuantity] = createSignal(1);
  const [shelfId, setShelfId] = createSignal(1);
  const [shelves, setShelves] = createSignal<Shelf[]>([]);
  const [loading, setLoading] = createSignal(false);
  const [message, setMessage] = createSignal<StatusFeedback | null>(null);
  const [duplicateOffer, setDuplicateOffer] = createSignal<DuplicateOfferData | null>(null);

  // ── Restock mode (autocomplete over existing items) ──────────────────
  const [results, setResults] = createSignal<Item[]>([]);
  const [restockItem, setRestockItem] = createSignal<Item | null>(null);
  const [showDropdown, setShowDropdown] = createSignal(false);

  let searchTimer: ReturnType<typeof setTimeout> | null = null;
  // Monotonic request id: a fetch launched by the debounce can still be in
  // flight when the query changes, the box is cleared, or an item is picked
  // — its response must never repopulate the list under a newer state.
  let searchSeq = 0;

  function stopPendingSearch() {
    if (searchTimer) {
      clearTimeout(searchTimer);
      searchTimer = null;
    }
  }

  onMount(async () => {
    try {
      const data = await api.getShelves(props.listId);
      setShelves(data);
    } catch (_) { flashStatus("Failed to load shelves"); }
  });

  function handleNameInput(e: Event) {
    const val = (e.target as HTMLInputElement).value;
    setName(val);
    if (restockItem()) return; // never search while restock mode is active
    stopPendingSearch();
    if (!val.trim()) {
      searchSeq++; // invalidate any in-flight fetch
      setResults([]);
      setShowDropdown(false);
      return;
    }
    setShowDropdown(true);
    const seq = ++searchSeq;
    searchTimer = setTimeout(async () => {
      try {
        const items = await api.searchItems(val.trim());
        if (seq !== searchSeq) return; // superseded, cleared, or picked
        setResults(items);
      } catch {
        // Silently ignore search errors — user can retry
        if (seq === searchSeq) setResults([]);
      }
    }, LINK_SEARCH_DEBOUNCE_MS);
  }

  function handleNameKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape") setShowDropdown(false); // close, keep the query
  }

  function handleNameFocusOut(e: FocusEvent) {
    // Focus moving to an element still inside the wrapper (e.g. the chip's
    // × button) must not count as a click-outside.
    const next = e.relatedTarget as Node | null;
    if (next && (e.currentTarget as HTMLElement).contains(next)) return;
    setShowDropdown(false);
  }

  function selectForRestock(item: Item) {
    stopPendingSearch();
    searchSeq++; // invalidate any in-flight fetch
    setRestockItem(item);
    setShowDropdown(false);
    setMessage(null);
  }

  function exitRestock() {
    setRestockItem(null);
    if (name().trim()) setShowDropdown(true); // re-open with the prior query
  }

  onCleanup(stopPendingSearch);

  async function handleSubmit(e: Event) {
    e.preventDefault();
    const selected = restockItem();

    if (selected) {
      setLoading(true);
      setMessage(null);

      try {
        const updated = await api.restock(selected.id, quantity(), shelfId());
        bumpItemsVersion();
        setMessage({ type: "success", text: `Restocked "${updated.name}"` });
        setName("");
        setBarcode("");
        setQuantity(1);
        setShelfId(shelves()[0]?.id ?? 1);
        setResults([]);
        setRestockItem(null); // success auto-exits restock mode
      } catch (err: unknown) {
        const apiErr = err as { error?: string };
        // Stay in restock mode: keep the selection so the user can retry.
        setMessage({ type: "error", text: apiErr.error || "Failed to restock" });
      }
      setLoading(false);
      return;
    }

    const n = name().trim();
    if (!n) return;

    setLoading(true);
    setMessage(null);

    try {
      const created = await api.create(barcode().trim() || null, n, quantity(), shelfId());
      bumpItemsVersion();
      setMessage({ type: "success", text: `Added "${created.name}"` });
      setName("");
      setBarcode("");
      setQuantity(1);
      setShelfId(shelves()[0]?.id ?? 1);
      setResults([]);
    } catch (err: unknown) {
      const apiErr = err as { status?: number; item?: Item; error?: string };
      if (apiErr.status === 409 && apiErr.item) {
        setDuplicateOffer({ barcode: barcode().trim(), existing: apiErr.item });
      } else {
        setMessage({ type: "error", text: apiErr.error || "Failed to add item" });
      }
    }
    setLoading(false);
  }

  async function handleDuplicateResolve(resolveMode: string) {
    const offer = duplicateOffer();
    setDuplicateOffer(null);
    setLoading(true);
    try {
      await api.scan(offer!.barcode, resolveMode, quantity(), shelfId());
      bumpItemsVersion();
      setMessage({ type: "success", text: `Updated "${offer!.existing.name}"` });
      setName("");
      setBarcode("");
      setQuantity(1);
    } catch (err: unknown) {
      const apiErr = err as { error?: string };
      setMessage({ type: "error", text: apiErr.error || "Failed" });
    }
    setLoading(false);
  }

  return (
    <div>
      <h3 class="mb-h">Manual Add</h3>

      <StatusMessage message={message()} />

      {duplicateOffer() && (
        <DuplicateOffer
          barcode={duplicateOffer()!.barcode}
          existing={duplicateOffer()!.existing}
          showModeToggle={true}
          onResolve={handleDuplicateResolve}
          onDismiss={() => setDuplicateOffer(null)}
        />
      )}

      <form onSubmit={handleSubmit} class="manual-add-form">
        <div class="manual-add-name" onFocusOut={handleNameFocusOut}>
          <label class="no-mb" style="flex-basis:100%">
            Name *
            <Show
              when={restockItem()}
              fallback={
                <input
                  type="text"
                  value={name()}
                  onInput={handleNameInput}
                  onKeyDown={handleNameKeyDown}
                  placeholder="e.g. Chicken Breast"
                  maxlength="100"
                  required
                  class="no-mb"
                />
              }
            >
              <div class="restock-chip">
                <span class="restock-chip-name">{restockItem()!.name}</span>
                <button
                  type="button"
                  class="remove-btn"
                  aria-label="Exit restock"
                  onClick={exitRestock}
                >
                  ×
                </button>
              </div>
            </Show>
          </label>
          <Show when={showDropdown() && !restockItem() && name().trim()}>
            <div class="link-barcode-results manual-add-dropdown">
              <Show
                when={results().length > 0}
                fallback={<p class="center-text">No items found for "{name()}"</p>}
              >
                {results().map((item) => (
                  <button
                    type="button"
                    class="link-barcode-item"
                    onMouseDown={(e) => {
                      e.preventDefault(); // keep input focus so blur can't race the pick
                      selectForRestock(item);
                    }}
                  >
                    {item.name}{" "}
                    <small>
                      ({totalCount(item) > 0 ? `${totalCount(item)} in stock` : "out of stock"})
                    </small>
                  </button>
                ))}
              </Show>
            </div>
          </Show>
        </div>
        <label class="no-mb" style="display:flex;flex-direction:column;align-items:flex-start;gap:0;max-width:120px">
          Shelf
          <select
            value={String(shelfId())}
            onChange={(e) => setShelfId(Number((e.target as HTMLSelectElement).value))}
            class="no-mb"
          >
            {shelves().map((s) => (
              <option value={String(s.id)}>{s.name}</option>
            ))}
          </select>
        </label>
        <div style="display:flex;gap:0.5rem;flex:1">
          <label class="no-mb" style="display:flex;flex-direction:column;align-items:flex-start;gap:0;flex:1">
            Qty
            <input
              type="number"
              min="1"
              max="9999"
              value={quantity()}
              onInput={(e) => setQuantity(parseInt((e.target as HTMLInputElement).value, 10) || 1)}
              class="no-mb"
              style="width:100%"
            />
          </label>
          <Show when={!restockItem()}>
            <label class="no-mb" style="display:flex;flex-direction:column;align-items:flex-start;gap:0;flex:1">
              Barcode
              <input
                type="text"
                value={barcode()}
                onInput={(e) => setBarcode((e.target as HTMLInputElement).value)}
                placeholder="Optional"
                class="no-mb"
              />
            </label>
          </Show>
        </div>
        <button type="submit" aria-busy={loading()}>
          {restockItem() ? "Restock" : "Add Item"}
        </button>
      </form>
    </div>
  );
}
