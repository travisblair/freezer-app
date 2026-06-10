import type { Item } from "./types";

/** Total quantity across all shelves for an item. */
export function totalCount(item: Item): number {
  if (!item.shelves || item.shelves.length === 0) return 0;
  return item.shelves.reduce((sum, s) => sum + s.count, 0);
}
