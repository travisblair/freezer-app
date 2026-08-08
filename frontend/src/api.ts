import { setOffline } from "./store";
import { OFFLINE_FAILURE_THRESHOLD } from "./constants";
import type {
  Item, Shelf, List, AuditLog, ApiError,
  ScanResult, DeleteResult, MoveResult, AuthCheckResult, AuthResult,
  HealthCheckResult, LookupResult,
} from "./types";

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

async function request<T = unknown>(path: string, options: RequestInit = {}): Promise<T> {
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

  if (res.headers.get("content-type")?.includes("text/csv")) {
    return res.blob() as T;
  }

  return res.json() as T;
}

export const api = {
  /** Authenticate: send email + password, receive HttpOnly session cookie. */
  authenticate(email: string, password: string): Promise<AuthResult> {
    return request<AuthResult>("/auth", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
  },

  check(): Promise<HealthCheckResult> {
    return request<HealthCheckResult>("/health");
  },

  // ── Items ──────────────────────────────────────────────────────────

  getItem(barcode: string): Promise<LookupResult> {
    return request<LookupResult>(`/item/${encodeURIComponent(barcode)}`);
  },

  getItems(showOutOfStock = false, search = ""): Promise<Item[]> {
    const params = new URLSearchParams();
    if (showOutOfStock) params.set("showOutOfStock", "true");
    if (search) params.set("search", search);
    return request<Item[]>(`/items?${params.toString()}`);
  },

  searchItems(query: string): Promise<Item[]> {
    return request<Item[]>(`/search-items?q=${encodeURIComponent(query)}`);
  },

  scan(barcode: string, mode: string, quantity: number, shelfId?: number): Promise<ScanResult> {
    return request<ScanResult>("/item/scan", {
      method: "POST",
      body: JSON.stringify({ barcode, mode, quantity, shelfId }),
    });
  },

  create(barcode: string | null, name: string, quantity: number, shelfId?: number): Promise<Item> {
    return request<Item>("/item/create", {
      method: "POST",
      body: JSON.stringify({ barcode: barcode || null, name, quantity, shelfId }),
    });
  },

  linkBarcode(itemId: number, barcode: string): Promise<Item> {
    return request<Item>("/item/link-barcode", {
      method: "POST",
      body: JSON.stringify({ itemId, barcode }),
    });
  },

  updateItem(id: number, fields: { name?: string }): Promise<Item> {
    return request<Item>(`/item/${id}`, {
      method: "PATCH",
      body: JSON.stringify(fields),
    });
  },

  bulkDelete(ids: number[]): Promise<{ deleted: number }> {
    return request<{ deleted: number }>("/items/bulk-delete", {
      method: "POST",
      body: JSON.stringify({ ids }),
    });
  },

  deleteByBarcode(barcode: string): Promise<DeleteResult> {
    return request<DeleteResult>(`/item/${encodeURIComponent(barcode)}`, {
      method: "DELETE",
    });
  },

  hardDelete(id: number): Promise<DeleteResult> {
    return request<DeleteResult>(`/item/hard/${id}`, { method: "DELETE" });
  },

  exportCsv(): Promise<Blob> {
    return request<Blob>("/export");
  },

  // ── Shelves ─────────────────────────────────────────────────────────

  getShelves(listId?: number): Promise<Shelf[]> {
    const params = listId ? `?listId=${listId}` : "";
    return request<Shelf[]>(`/shelves${params}`);
  },

  allShelves(): Promise<Shelf[]> {
    return request<Shelf[]>("/shelves");
  },

  createShelf(name: string, listId?: number): Promise<Shelf> {
    return request<Shelf>("/shelves", {
      method: "POST",
      body: JSON.stringify({ name, listId }),
    });
  },

  updateShelf(id: number, name: string): Promise<Shelf> {
    return request<Shelf>(`/shelf/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ name }),
    });
  },

  deleteShelf(id: number): Promise<DeleteResult> {
    return request<DeleteResult>(`/shelf/${id}`, { method: "DELETE" });
  },

  setShelfCount(shelfId: number, count: number): Promise<{ id: number; count: number }> {
    return request<{ id: number; count: number }>(`/item-shelf/${shelfId}`, {
      method: "PATCH",
      body: JSON.stringify({ count }),
    });
  },

  moveItem(itemId: number, sourceShelfId: number, targetShelfId: number, quantity: number): Promise<MoveResult> {
    return request<MoveResult>("/item-shelf/move", {
      method: "POST",
      body: JSON.stringify({ itemId, sourceShelfId, targetShelfId, quantity }),
    });
  },

  // ── Lists ───────────────────────────────────────────────────────────

  getLists(): Promise<List[]> {
    return request<List[]>("/lists");
  },

  createList(name: string): Promise<List> {
    return request<List>("/lists", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
  },

  updateList(id: number, name: string): Promise<List> {
    return request<List>(`/lists/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ name }),
    });
  },

  deleteList(id: number): Promise<DeleteResult> {
    return request<DeleteResult>(`/lists/${id}`, { method: "DELETE" });
  },

  // ── Notifications ───────────────────────────────────────────────────

  getNotifications(since?: string, limit = 50): Promise<AuditLog[]> {
    const params = new URLSearchParams();
    if (since) params.set("since", since);
    params.set("limit", String(limit));
    params.set("actions", "create,delete,bulk_delete,hard_delete");
    return request<AuditLog[]>(`/notifications?${params.toString()}`);
  },

  // ── Auth ─────────────────────────────────────────────────────────────

  authCheck(): Promise<AuthCheckResult> {
    return request<AuthCheckResult>("/auth/check");
  },

  logout(): Promise<AuthResult> {
    return request<AuthResult>("/auth/logout", { method: "POST" });
  },
};
