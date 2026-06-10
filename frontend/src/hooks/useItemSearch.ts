import { createEffect, createSignal, onCleanup } from "solid-js";
import { api } from "../api";
import type { Shelf } from "../types";
import {
  setItems,
  searchQuery, setSearchQuery,
  showOutOfStock,
  itemsVersion,
} from "../store";
import { SEARCH_DEBOUNCE_MS } from "../constants";

export interface ItemSearchControls {
  loading: () => boolean;
  shelves: () => Shelf[];
  handleSearchInput: (e: InputEvent) => void;
  loadItems: () => Promise<void>;
}

/**
 * Debounced search + auto-refetch when filters change.
 * Also loads shelves alongside items so the grouped table never
 * shows an empty state due to a race condition.
 */
export function useItemSearch(): ItemSearchControls {
  const [loading, setLoading] = createSignal(false);
  const [debouncedSearch, setDebouncedSearch] = createSignal("");
  const [shelves, setShelves] = createSignal<Shelf[]>([]);
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  function handleSearchInput(e: InputEvent) {
    const target = e.target as HTMLInputElement;
    setSearchQuery(target.value);
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      setDebouncedSearch(searchQuery());
    }, SEARCH_DEBOUNCE_MS);
  }

  async function loadShelves() {
    try {
      const data = await api.getShelves(1);
      setShelves(data);
    } catch (err) {
      if (import.meta.env.DEV) console.error("Failed to load shelves", err);
    }
  }

  async function loadItems() {
    setLoading(true);
    try {
      const [itemData, _] = await Promise.all([
        api.getItems(showOutOfStock(), debouncedSearch()),
        loadShelves(),
      ]);
      setItems(itemData);
    } catch (err) {
      if (import.meta.env.DEV) console.error("Failed to load items", err);
    }
    setLoading(false);
  }

  // Reload whenever filters change or itemsVersion is bumped
  createEffect(() => {
    showOutOfStock();
    debouncedSearch();
    itemsVersion();
    loadItems();
  });

  onCleanup(() => {
    if (debounceTimer) clearTimeout(debounceTimer);
  });

  return { loading, shelves, handleSearchInput, loadItems };
}
