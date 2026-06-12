/** Shared types for the freezer app frontend. */

export interface Item {
  id: number;
  name: string;
  barcodes?: ItemBarcode[];
  shelves?: ItemShelf[];
}

export interface ItemBarcode {
  id?: number;
  barcode: string;
}

export interface ItemShelf {
  id: number;
  itemId: number;
  shelfId: number;
  count: number;
}

export interface Shelf {
  id: number;
  name: string;
  listId: number;
}

export interface List {
  id: number;
  name: string;
}

export interface ApiError extends Error {
  status?: number;
  error?: string;
  item?: Item; // carried on 409 Conflict
}

/** Status feedback shown after scan/add operations. */
export interface StatusFeedback {
  type: "success" | "error";
  text: string;
}

/** Shape carried when a duplicate-barcode conflict (409) response arrives. */
export interface DuplicateOfferData {
  barcode: string;
  existing: Item;
}

/** Describes a pending delete confirmation. */
export interface DeleteAction {
  type: "single" | "bulk" | "hard";
  id?: number;
  name?: string;
}
