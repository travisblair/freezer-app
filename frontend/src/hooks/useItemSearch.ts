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
  flashStatus,
} from "../store";
import { SEARCH_DEBOUNCE_MS } from "../constants";

export interface ItemSearchControls {
  loading: () => boolean;
  shelves: () => Shelf[];
  lists: () => List[];
  allShelves: () => Shelf[];
  handleSearchInput: (e: InputEvent) => void;
  clearSearch: () => void;
  loadItems: () => Promise<void>;
}

export function useItemSearch(): ItemSearchControls {
  const [loading, setLoading] = createSignal(false);
  const [debouncedSearch, setDebouncedSearch] = createSignal("");
  const [shelves, setShelves] = createSignal<Shelf[]>([]);
  const [lists, setListsLocal] = createSignal<List[]>([]);
  const [allShelves, setAllShelves] = createSignal<Shelf[]>([]);
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

  async function loadAllShelves() {
    try {
      const data = await api.allShelves();
      setAllShelves(data);
    } catch (err) {
      if (import.meta.env.DEV) console.error("Failed to load all shelves", err);
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

  function clearSearch() {
    setSearchQuery("");
    setDebouncedSearch("");
    if (debounceTimer) clearTimeout(debounceTimer);
  }

  // Monotonic request id for the stale-response guard: overlapping loads
  // (fast search keystrokes, itemsVersion bumps) used to let the LAST
  // response to ARRIVE win — a slow older request could clobber newer
  // results and flip loading off while a newer request was still in
  // flight.
  let loadSeq = 0;

  async function loadItems() {
    const seq = ++loadSeq;
    setLoading(true);
    try {
      const [itemData, _] = await Promise.all([
        api.getItems(showOutOfStock(), debouncedSearch()),
        Promise.all([loadShelves(), loadLists(), loadAllShelves()]),
      ]);
      if (seq !== loadSeq) return; // superseded by a newer request
      setItems(itemData);
    } catch (err) {
      if (seq !== loadSeq) return;
      // A 500 here used to leave a stale/empty table with zero feedback
      // (the offline banner only catches network rejections). Surface it.
      if (import.meta.env.DEV) console.error("Failed to load items", err);
      flashStatus("Failed to load items");
    }
    if (seq === loadSeq) setLoading(false);
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

  return { loading, shelves, lists, allShelves, handleSearchInput, clearSearch, loadItems };
}
