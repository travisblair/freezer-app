import { setOffline } from "./store";
import { OFFLINE_FAILURE_THRESHOLD } from "./constants";
import type { Item, Shelf, ApiError } from "./types";

const BASE = "/api";

/* ── Offline Detection ──────────────────────────────────────────────────
 * Tracks consecutive fetch failures.  After OFFLINE_FAILURE_THRESHOLD
 * consecutive failures the global `offline` signal is set to true.
 * A successful response resets the counter. */
let failCount = 0;

function trackOffline(ok: boolean): void {
  if (ok) {
    failCount = 0;
    setOffline(false);
  } else {
    failCount++;
    if (failCount >= OFFLINE_FAILURE_THRESHOLD) setOffline(true);
  }
}

async function request(path: string, options: RequestInit = {}): Promise<unknown> {
  let res: Response;
  try {
    res = await fetch(`${BASE}${path}`, {
      ...options,
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        ...(options.headers as Record<string, string> | undefined),
      },
    });
    trackOffline(true);
  } catch {
    trackOffline(false);
    throw new Error("Network error");
  }

  // Redirect to auth on 401
  if (res.status === 401) {
    const body = await res.json().catch(() => ({}));
    if (body.error === "Unauthorized") {
      window.dispatchEvent(new CustomEvent("freezer:auth-required"));
    }
    throw Object.assign(new Error(body.error || "Unauthorized"), {
      status: 401,
      ...body,
    }) as ApiError;
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw Object.assign(new Error(body.error || `HTTP ${res.status}`), {
      status: res.status,
      ...body,
    }) as ApiError;
  }

  const ct = res.headers.get("content-type") || "";
  if (ct.includes("text/csv")) return res.blob();

  return res.json();
}

export const api = {
  /** Authenticate: send email + password, receive HttpOnly session cookie. */
  authenticate(email: string, password: string): Promise<unknown> {
    return request("/auth", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
  },

  getItem(barcode: string): Promise<{ found: boolean; [key: string]: unknown }> {
    return request(`/item/${encodeURIComponent(barcode)}`) as Promise<{
      found: boolean;
      [key: string]: unknown;
    }>;
  },

  getItems(showOutOfStock = false, search = ""): Promise<Item[]> {
    const params = new URLSearchParams();
    if (showOutOfStock) params.set("showOutOfStock", "true");
    if (search) params.set("search", search);
    return request(`/items?${params.toString()}`) as Promise<Item[]>;
  },

  searchItems(query: string): Promise<Item[]> {
    return request(`/search-items?q=${encodeURIComponent(query)}`) as Promise<Item[]>;
  },

  scan(barcode: string, mode: string, quantity: number, shelfId?: number): Promise<unknown> {
    return request("/item/scan", {
      method: "POST",
      body: JSON.stringify({ barcode, mode, quantity, shelfId }),
    });
  },

  create(barcode: string | null, name: string, quantity: number, shelfId?: number): Promise<Item> {
    return request("/item/create", {
      method: "POST",
      body: JSON.stringify({ barcode: barcode || null, name, quantity, shelfId }),
    }) as Promise<Item>;
  },

  linkBarcode(itemId: number, barcode: string): Promise<unknown> {
    return request("/item/link-barcode", {
      method: "POST",
      body: JSON.stringify({ itemId, barcode }),
    });
  },

  updateItem(id: number, fields: { name?: string }): Promise<unknown> {
    return request(`/item/${id}`, {
      method: "PATCH",
      body: JSON.stringify(fields),
    });
  },

  bulkDelete(ids: number[]): Promise<unknown> {
    return request("/items/bulk-delete", {
      method: "POST",
      body: JSON.stringify({ ids }),
    });
  },

  deleteByBarcode(barcode: string): Promise<unknown> {
    return request(`/item/${encodeURIComponent(barcode)}`, {
      method: "DELETE",
    });
  },

  hardDelete(id: number): Promise<unknown> {
    return request(`/item/hard/${id}`, { method: "DELETE" });
  },

  exportCsv(): Promise<Blob> {
    return request("/export") as Promise<Blob>;
  },

  // ── Shelves ─────────────────────────────────────────────────────────

  getShelves(listId?: number): Promise<Shelf[]> {
    const params = listId ? `?listId=${listId}` : "";
    return request(`/shelves${params}`) as Promise<Shelf[]>;
  },

  allShelves(): Promise<Shelf[]> {
    return request("/shelves") as Promise<Shelf[]>;
  },

  createShelf(name: string, listId?: number): Promise<Shelf> {
    return request("/shelves", {
      method: "POST",
      body: JSON.stringify({ name, listId }),
    }) as Promise<Shelf>;
  },

  updateShelf(id: number, name: string): Promise<Shelf> {
    return request(`/shelf/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ name }),
    }) as Promise<Shelf>;
  },

  deleteShelf(id: number): Promise<unknown> {
    return request(`/shelf/${id}`, { method: "DELETE" });
  },

  setShelfCount(shelfId: number, count: number): Promise<unknown> {
    return request(`/item-shelf/${shelfId}`, {
      method: "PATCH",
      body: JSON.stringify({ count }),
    });
  },

  moveItem(itemId: number, sourceShelfId: number, targetShelfId: number, quantity: number): Promise<unknown> {
    return request("/item-shelf/move", {
      method: "POST",
      body: JSON.stringify({ itemId, sourceShelfId, targetShelfId, quantity }),
    });
  },

  // ── Lists ───────────────────────────────────────────────────────────

  getLists(): Promise<import("./types").List[]> {
    return request("/lists") as Promise<import("./types").List[]>;
  },

  createList(name: string): Promise<unknown> {
    return request("/lists", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
  },

  updateList(id: number, name: string): Promise<unknown> {
    return request(`/lists/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ name }),
    });
  },

  deleteList(id: number): Promise<unknown> {
    return request(`/lists/${id}`, { method: "DELETE" });
  },
};
