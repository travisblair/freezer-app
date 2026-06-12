import { createEffect, createSignal, onCleanup } from "solid-js";
import { api } from "../api";
import type { Shelf, List } from "../types";
import {
  setItems,
  searchQuery, setSearchQuery,
  showOutOfStock,
  itemsVersion,
  currentListId,
  setLists,
} from "../store";
import { SEARCH_DEBOUNCE_MS } from "../constants";

export interface ItemSearchControls {
  loading: () => boolean;
  shelves: () => Shelf[];
  lists: () => List[];
  handleSearchInput: (e: InputEvent) => void;
  loadItems: () => Promise<void>;
}

export function useItemSearch(): ItemSearchControls {
  const [loading, setLoading] = createSignal(false);
  const [debouncedSearch, setDebouncedSearch] = createSignal("");
  const [shelves, setShelves] = createSignal<Shelf[]>([]);
  const [lists, setListsLocal] = createSignal<List[]>([]);
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
      const data = await api.getShelves(currentListId());
      setShelves(data);
    } catch (err) {
      if (import.meta.env.DEV) console.error("Failed to load shelves", err);
    }
  }

  async function loadLists() {
    try {
      const data = await api.getLists();
      setListsLocal(data);
      setLists(data);
    } catch (err) {
      if (import.meta.env.DEV) console.error("Failed to load lists", err);
    }
  }

  async function loadItems() {
    setLoading(true);
    try {
      const [itemData, _] = await Promise.all([
        api.getItems(showOutOfStock(), debouncedSearch()),
        Promise.all([loadShelves(), loadLists()]),
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
    currentListId();
    loadItems();
  });

  onCleanup(() => {
    if (debounceTimer) clearTimeout(debounceTimer);
  });

  return { loading, shelves, lists, handleSearchInput, loadItems };
}
