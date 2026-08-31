import type { Item } from "./types";

/** Total quantity across all shelves for an item. */
export function totalCount(item: Item): number {
  if (!item.shelves || item.shelves.length === 0) return 0;
  return item.shelves.reduce((sum, s) => sum + s.count, 0);
}

/** Return the first shelf ID for an item, falling back to 1. */
export function getFirstShelfId(item: { shelves?: Partial<import("./types").ItemShelf>[] }): number {
  if (item.shelves && item.shelves.length > 0) {
    return item.shelves[0].shelfId ?? 1;
  }
  return 1;
}

export interface PerKeyDebouncer<K, A extends unknown[]> {
  /** Schedule fn(key, ...args); replaces any pending call for the SAME key. */
  schedule: (key: K, ...args: A) => void;
  /** Clear all pending timers (component unmount). */
  cancelAll: () => void;
}

/**
 * Per-key debounce. Unlike a single shared timer, rapid edits across
 * DIFFERENT keys are independent — a single timer silently drops every
 * pending edit except the last one (the count-editor data-loss bug).
 * Same-key edits still coalesce (latest wins).
 */
export function createPerKeyDebouncer<K, A extends unknown[]>(
  fn: (key: K, ...args: A) => void,
  delayMs: number,
): PerKeyDebouncer<K, A> {
  const timers = new Map<K, ReturnType<typeof setTimeout>>();
  return {
    schedule(key: K, ...args: A) {
      const existing = timers.get(key);
      if (existing) clearTimeout(existing);
      timers.set(key, setTimeout(() => {
        timers.delete(key);
        fn(key, ...args);
      }, delayMs));
    },
    cancelAll() {
      for (const t of timers.values()) clearTimeout(t);
      timers.clear();
    },
  };
}
