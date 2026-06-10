import { createSignal, onMount } from "solid-js";
import { api } from "../api";
import { bumpItemsVersion } from "../store";
import type { Item, Shelf, StatusFeedback, DuplicateOfferData } from "../types";
import StatusMessage from "./StatusMessage";
import DuplicateOffer from "./DuplicateOffer";

export default function ManualAddForm() {
  const [name, setName] = createSignal("");
  const [barcode, setBarcode] = createSignal("");
  const [quantity, setQuantity] = createSignal(1);
  const [shelfId, setShelfId] = createSignal(1);
  const [shelves, setShelves] = createSignal<Shelf[]>([]);
  const [loading, setLoading] = createSignal(false);
  const [message, setMessage] = createSignal<StatusFeedback | null>(null);
  const [duplicateOffer, setDuplicateOffer] = createSignal<DuplicateOfferData | null>(null);

  onMount(async () => {
    try {
      const data = await api.getShelves(1);
      setShelves(data);
    } catch (_) {}
  });

  async function handleSubmit(e: Event) {
    e.preventDefault();
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
    } catch (err: unknown) {
      const apiErr = err as { status?: number; item?: Item; error?: string };
      if (apiErr.status === 409) {
        setDuplicateOffer({ barcode: barcode().trim(), existing: apiErr.item! });
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
        <label class="no-mb" style="flex-basis:100%">
          Name *
          <input
            type="text"
            value={name()}
            onInput={(e) => setName((e.target as HTMLInputElement).value)}
            placeholder="e.g. Chicken Breast"
            maxlength="100"
            required
            class="no-mb"
          />
        </label>
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
        </div>
        <button type="submit" aria-busy={loading()}>
          Add Item
        </button>
      </form>
    </div>
  );
}
