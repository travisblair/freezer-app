import { createSignal } from "solid-js";
import { api } from "../api";
import { selectedIds, clearSelection, bumpItemsVersion } from "../store";
import type { Item, DeleteAction } from "../types";

export interface ItemActionsControls {
  confirmDelete: () => DeleteAction | null;
  setConfirmDelete: (action: DeleteAction | null) => void;
  editingItem: () => Item | null;
  setEditingItem: (item: Item | null) => void;
  handleDeleteSingle: (item: Item) => void;
  handleHardDelete: (item: Item) => void;
  confirmDeleteAction: () => Promise<void>;
  handleRestore: (item: Item) => Promise<void>;
  handleExport: () => Promise<void>;
}

/**
 * Item mutation actions: single/bulk delete, restore, export.
 * Uses itemsVersion signal to trigger reload instead of a callback.
 */
export function useItemActions(): ItemActionsControls {
  const [confirmDelete, setConfirmDelete] = createSignal<DeleteAction | null>(null);
  const [editingItem, setEditingItem] = createSignal<Item | null>(null);

  function handleDeleteSingle(item: Item) {
    setConfirmDelete({ type: "single", id: item.id, name: item.name });
  }

  function handleHardDelete(item: Item) {
    setConfirmDelete({ type: "hard", id: item.id, name: item.name });
  }

  async function confirmDeleteAction() {
    const cd = confirmDelete();
    if (!cd) return;
    setConfirmDelete(null);

    try {
      if (cd.type === "single" && cd.id != null) {
        await api.bulkDelete([cd.id]);
      } else if (cd.type === "bulk") {
        await api.bulkDelete(selectedIds());
        clearSelection();
      } else if (cd.type === "hard" && cd.id != null) {
        await api.hardDelete(cd.id);
        clearSelection();
      }
      bumpItemsVersion();
    } catch (err) {
      if (import.meta.env.DEV) console.error("Delete failed", err);
    }
  }

  async function handleRestore(item: Item) {
    try {
      if (item.shelves && item.shelves.length > 0) {
        // Restore the first shelf's count to 1
        await api.setShelfCount(item.shelves[0].id, 1);
      } else if (item.barcodes && item.barcodes.length > 0) {
        // Fallback: scan to restore
        await api.scan(item.barcodes[0].barcode, "increment", 1, 1);
      }
      bumpItemsVersion();
    } catch (err) {
      if (import.meta.env.DEV) console.error("Restore failed", err);
    }
  }

  async function handleExport() {
    try {
      const blob = await api.exportCsv();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "freezer-inventory.csv";
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      if (import.meta.env.DEV) console.error("Export failed", err);
    }
  }

  return {
    confirmDelete,
    setConfirmDelete,
    editingItem,
    setEditingItem,
    handleDeleteSingle,
    handleHardDelete,
    confirmDeleteAction,
    handleRestore,
    handleExport,
  };
}