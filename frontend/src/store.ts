import { createSignal, createMemo } from "solid-js";
import type { Item, List } from "./types";

export const [items, setItems] = createSignal<Item[]>([]);
export const [searchQuery, setSearchQuery] = createSignal("");
export const [showOutOfStock, setShowOutOfStock] = createSignal(false);
export const [selectedIds, setSelectedIds] = createSignal<number[]>([]);
export const [offline, setOffline] = createSignal(false);
export const [currentListId, setCurrentListId] = createSignal(1);
export const [lists, setLists] = createSignal<List[]>([]);

export const currentListName = createMemo(() => {
  return lists().find(l => l.id === currentListId())?.name || "Freezer";
});

/** Start unauthenticated. The app will try a GET /api/items on mount.
 *  If it succeeds, auth is established (cookie present). If 401, show auth form.
 *  The `freezer:auth-required` event forces re-auth at any time. */
export const [needsAuth, setNeedsAuth] = createSignal(true);

/** Bump to trigger table reload after mutations (scan, link, create) */
export const [itemsVersion, setItemsVersion] = createSignal(0);

export const selectedSet = createMemo(() => new Set(selectedIds()));

export function toggleSelect(id: number): void {
  setSelectedIds((prev) =>
    prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
  );
}

export function selectAll(ids: number[]): void {
  setSelectedIds(ids);
}

export function clearSelection(): void {
  setSelectedIds([]);
}

export function bumpItemsVersion(): void {
  setItemsVersion((v) => v + 1);
}