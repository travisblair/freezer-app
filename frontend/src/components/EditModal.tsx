import { createSignal } from "solid-js";
import { api } from "../api";
import type { Item } from "../types";
import { bumpItemsVersion } from "../store";
import BaseModal from "./BaseModal";

interface Props {
  item: Item;
  onSaved: () => void;
  onCancel: () => void;
}

export default function EditModal(props: Props) {
  const [name, setName] = createSignal(props.item.name);
  const [saving, setSaving] = createSignal(false);
  const [error, setError] = createSignal("");

  async function handleSave(e: Event) {
    e.preventDefault();
    const n = name().trim();
    if (!n) return;

    setSaving(true);
    setError("");

    try {
      await api.updateItem(props.item.id, { name: n });
      bumpItemsVersion();
      props.onSaved();
    } catch (err: unknown) {
      const apiErr = err as { error?: string };
      setError(apiErr.error || "Failed to save");
    }
    setSaving(false);
  }

  const footer = (
    <>
      <button type="button" class="secondary" onClick={props.onCancel}>
        Cancel
      </button>
      <button type="submit" form="edit-form" aria-busy={saving()}>
        Save
      </button>
    </>
  );

  return (
    <BaseModal
      title="Edit Item"
      onClose={props.onCancel}
      footer={footer}
    >
      <form id="edit-form" onSubmit={handleSave}>
        {error() && (
          <p class="edit-error-text">{error()}</p>
        )}

        <label>
          Name
          <input
            type="text"
            value={name()}
            onInput={(e) => setName((e.target as HTMLInputElement).value)}
            maxlength="100"
            required
          />
        </label>

        {props.item.barcodes && props.item.barcodes.length > 0 && (
          <p class="edit-barcode-text">
            {props.item.barcodes.length === 1
              ? `Barcode: ${props.item.barcodes[0].barcode}`
              : `Barcodes: ${props.item.barcodes.map((b) => b.barcode).join(", ")}`}
          </p>
        )}
      </form>
    </BaseModal>
  );
}
