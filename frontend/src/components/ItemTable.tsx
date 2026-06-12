import { For, Show, createSignal } from "solid-js";
import type { Item, Shelf } from "../types";
import { totalCount } from "../helpers";
import { api } from "../api";
import {
  items, searchQuery, showOutOfStock, setShowOutOfStock,
  selectedIds, selectedSet, toggleSelect, selectAll, clearSelection,
  currentListId, setCurrentListId, setLists,
} from "../store";
import { useItemSearch } from "../hooks/useItemSearch";
import { useItemActions } from "../hooks/useItemActions";
import ConfirmModal from "./ConfirmModal";
import EditModal from "./EditModal";

type ShelfMap = Map<number, Map<number, number>>;

export default function ItemTable() {
  const { loading, shelves, lists, allShelves, handleSearchInput, loadItems } = useItemSearch();
  const a = useItemActions();

  const [selShelf, setSelShelf] = createSignal<number | null>(null);
  const [collapsed, setCollapsed] = createSignal<Set<number>>(new Set());
  const [newName, setNewName] = createSignal("");
  const [renameId, setRenameId] = createSignal<number | null>(null);
  const [moveState, setMoveState] = createSignal<{ item: Item; shelfId: number; count: number } | null>(null);
  const [renameVal, setRenameVal] = createSignal("");

  // Per-row kebab state — a simple number tracking which rowKey is open
  const [openKebab, setOpenKebab] = createSignal(0);

  function toggleKebab(rowKey: number) {
    setOpenKebab(prev => prev === rowKey ? 0 : rowKey);
  }

  function smap(): ShelfMap {
    const m: ShelfMap = new Map();
    for (const item of items()) {
      const im = new Map<number, number>();
      if (item.shelves) for (const s of item.shelves) im.set(s.shelfId, s.count);
      m.set(item.id, im);
    }
    return m;
  }

  function toggle(id: number) {
    setCollapsed(prev => { const s = new Set(prev); s.has(id) ? s.delete(id) : s.add(id); return s; });
  }

  function getItems(shelfId: number | null, sm: ShelfMap): { item: Item; count: number }[] {
    const r: { item: Item; count: number }[] = [];
    for (const item of items()) {
      const im = sm.get(item.id);
      if (!im) continue;
      if (shelfId === null) {
        let t = 0; for (const c of im.values()) t += c;
        if (showOutOfStock() || t > 0) r.push({ item, count: t });
      } else {
        const c = im.get(shelfId);
        if (c !== undefined && (showOutOfStock() || c > 0)) r.push({ item, count: c });
      }
    }
    return r;
  }

  async function reload() { try { await loadItems(); } catch (_) {} }
  async function createShelf(e: Event) {
    e.preventDefault(); const n = newName().trim();
    if (!n) return;
    try { await api.createShelf(n, 1); setNewName(""); await reload(); } catch (_) {}
  }
  async function renameShelf(id: number) {
    const n = renameVal().trim();
    if (!n) return;
    try { await api.updateShelf(id, n); setRenameId(null); await reload(); } catch (_) {}
  }
  async function delShelf(id: number) {
    if (id === 1) return;
    try { await api.deleteShelf(id); await reload(); } catch (_) {}
  }

  const vs = () => selShelf() === null ? shelves() : shelves().filter(s => s.id === selShelf());

  const [moveTarget, setMoveTarget] = createSignal(1);
  const [moveQty, setMoveQty] = createSignal(1);

  const otherShelves = () => shelves().filter(s => s.id !== (moveState()?.shelfId ?? 0));
  const moveTargetInit = () => otherShelves()[0]?.id ?? 1;

  async function doMove() {
    const ms = moveState();
    if (!ms) return;
    try {
      await api.moveItem(ms.item.id, ms.shelfId, moveTarget(), moveQty());
      setMoveState(null);
      await loadItems();
    } catch (err) {
      if (import.meta.env.DEV) console.error("Move failed", err);
    }
  }

  let countTimer: ReturnType<typeof setTimeout> | null = null;
  async function updateCount(shelfId: number, value: number) {
    if (countTimer) clearTimeout(countTimer);
    countTimer = setTimeout(async () => {
      try {
        await api.setShelfCount(shelfId, value);
        await loadItems();
      } catch (_) {}
    }, 400);
  }

  return (
    <div>
      <div class="grid mb-1">
        <div>
          <input type="search" placeholder="Search by name..." value={searchQuery()} onInput={handleSearchInput} class="no-mb" />
        </div>
        <div class="table-controls">
          <label>
            <span>
              <input type="checkbox" checked={showOutOfStock()} onChange={e => { setShowOutOfStock((e.target as HTMLInputElement).checked); clearSelection(); }} />
            </span>
            <span>Show out of stock</span>
          </label>
          <select value={selShelf() === null ? "" : String(selShelf())} onChange={e => {
            const v = (e.target as HTMLSelectElement).value;
            setSelShelf(v === "" ? null : Number(v));
            clearSelection();
          }}>
            <option value="">All Shelves</option>
            {shelves().map((s: Shelf) => <option value={String(s.id)}>{s.name}</option>)}
          </select>
        </div>
      </div>

      <Show when={selectedIds().length > 0}>
        <div class="bulk-actions">
          <span>{selectedIds().length} selected</span>
          <button type="button" class="secondary" onClick={() => a.setConfirmDelete({ type: "bulk" })}>Delete Selected</button>
          <button type="button" class="outline" onClick={clearSelection}>Clear</button>
        </div>
      </Show>

      <Show when={loading()}>
        <div class="center-text"><span aria-busy="true">Loading...</span></div>
      </Show>
      <Show when={!loading() && items().length === 0 && shelves().length > 0}>
        <p class="center-text">No items found.</p>
      </Show>

      <Show when={items().length > 0 && shelves().length > 0}>
        {vs().map((shelf: Shelf) => {
          const si = getItems(shelf.id, smap());
          const isCollapsed = collapsed().has(shelf.id);
          const autoHide = selShelf() === null && si.every(x => x.count === 0);
          const hidden = isCollapsed || autoHide;

          return (
            <div class="shelf-section">
              <div class="shelf-header" onClick={() => toggle(shelf.id)}>
                <span class="shelf-toggle">{hidden ? "\u25b6" : "\u25bc"}</span>
                <span class="shelf-name">
                  {renameId() === shelf.id ? (
                    <span>
                      <form onSubmit={e2 => { e2.preventDefault(); renameShelf(shelf.id); }} onClick={e2 => e2.stopPropagation()} style="display:inline">
                        <input type="text" value={renameVal()} onInput={e2 => setRenameVal((e2.target as HTMLInputElement).value)} style="width:100px;display:inline;margin:0" autofocus />
                      </form>
                      <button type="button" class="outline small-action" onClick={e2 => { e2.stopPropagation(); setRenameId(null); }} style="margin-left:4px">✕</button>
                    </span>
                  ) : shelf.name}
                </span>
                <span class="shelf-count">({si.length} items)</span>
                <div class="shelf-actions">
                  <button type="button" class="outline small-action" onClick={e2 => { e2.stopPropagation(); setRenameId(shelf.id); setRenameVal(shelf.name); }}>✏️</button>
                  {shelf.id !== 1 && <button type="button" class="outline small-action" onClick={e2 => { e2.stopPropagation(); delShelf(shelf.id); }}>🗑️</button>}
                </div>
              </div>
              <Show when={!hidden}>
                <table class="shelf-table">
                  <tbody>
                    <For each={si}>
                      {({ item, count }) => {
                      const oos = count === 0;
                      const rowKey = item.id * 10000 + shelf.id;
                      return (
                        <tr class={oos ? "deleted-row" : ""}>
                          <td class="cell-sm"><input type="checkbox" checked={selectedSet().has(item.id)} onChange={() => toggleSelect(item.id)} /></td>
                          <td>{item.name}{oos && <span class="deleted-tag">(out of stock)</span>}</td>
                          <td class="cell-sm count-cell">
                            <div class="count-editor" data-shelf-id={(() => {
                              const is = item.shelves?.find(s => s.shelfId === shelf.id);
                              return is?.id ?? 0;
                            })()}>
                              <button type="button" class="count-btn count-minus" onClick={e => {
                                const div = (e.target as HTMLElement).closest(".count-editor")!;
                                const input = div.querySelector("input")!;
                                const sid = Number(div.getAttribute("data-shelf-id"));
                                const v = Math.max(0, parseInt(input.value, 10) - 1);
                                input.value = String(v);
                                updateCount(sid, v);
                              }}>{count === 1 ? "🗑" : "−"}</button>
                              <input
                                type="number"
                                min="0" max="9999"
                                value={count}
                                class="count-input"
                                onChange={e => {
                                  const input = e.target as HTMLInputElement;
                                  const div = input.closest(".count-editor")!;
                                  const sid = Number(div.getAttribute("data-shelf-id"));
                                  const v = Math.max(0, Math.min(9999, parseInt(input.value, 10) || 0));
                                  input.value = String(v);
                                  updateCount(sid, v);
                                }}
                              />
                              <button type="button" class="count-btn count-plus" onClick={e => {
                                const div = (e.target as HTMLElement).closest(".count-editor")!;
                                const input = div.querySelector("input")!;
                                const sid = Number(div.getAttribute("data-shelf-id"));
                                const v = Math.min(9999, parseInt(input.value, 10) + 1);
                                input.value = String(v);
                                updateCount(sid, v);
                              }}>+</button>
                            </div>
                          </td>
                          <td class="cell-sm" style="position:relative">
                            <button type="button" class="outline kebab-btn" onClick={e2 => { e2.stopPropagation(); toggleKebab(rowKey); }}>⋮</button>
                            <Show when={openKebab() === rowKey}>
                              <div class="kebab-menu">
                                <div class="kebab-item" onMouseDown={() => { setOpenKebab(0); a.setEditingItem(item); }}>Edit</div>
                                <div class="kebab-item" onMouseDown={() => { setOpenKebab(0); setMoveState({ item, shelfId: shelf.id, count }); }}>Move</div>
                                <Show when={!oos}><div class="kebab-item danger" onMouseDown={() => { setOpenKebab(0); a.handleHardDelete(item); }}>Delete</div></Show>
                                <Show when={oos}><div class="kebab-item" onMouseDown={() => { setOpenKebab(0); a.handleRestore(item); }}>Restore</div></Show>
                              </div>
                            </Show>
                          </td>
                        </tr>
                      );
                    }}</For>
                  </tbody>
                </table>
              </Show>
            </div>
          );
        })}
      </Show>

      <form onSubmit={createShelf} class="create-shelf-form">
        <input type="text" placeholder="New shelf name..." value={newName()} onInput={e => setNewName((e.target as HTMLInputElement).value)} maxlength="100" />
        <button type="submit" class="outline" disabled={!newName().trim()}>+ Add Shelf</button>
      </form>

      <Show when={a.confirmDelete()}>
        <ConfirmModal
          variant={a.confirmDelete()!.type === "hard" ? "danger" : undefined}
          message={
            a.confirmDelete()!.type === "hard" ? `Permanently delete "${a.confirmDelete()!.name}"?`
            : a.confirmDelete()!.type === "single" ? `Delete "${a.confirmDelete()!.name}"?`
            : `Delete ${selectedIds().length} selected items?`
          }
          onConfirm={a.confirmDeleteAction}
          onCancel={() => a.setConfirmDelete(null)}
        />
      </Show>
      <Show when={a.editingItem()}>
        <EditModal item={a.editingItem()!} onSaved={() => a.setEditingItem(null)} onCancel={() => a.setEditingItem(null)} />
      </Show>

      <Show when={moveState()}>
        {(() => {
          const ms = moveState()!;
          setMoveTarget(moveTargetInit());
          setMoveQty(ms.count);
          return (
            <>
              <div class="modal-overlay" onClick={() => setMoveState(null)}>
                <div class="modal-dialog" onClick={e => e.stopPropagation()}>
              <article>
                <header style="display:flex;align-items:center;justify-content:space-between">
                  <strong>Move {ms.item.name}</strong>
                  <button class="pico-prev" onClick={() => setMoveState(null)} />
                </header>
                <label>
                  To shelf
                  <select value={String(moveTarget())} onChange={e => setMoveTarget(Number((e.target as HTMLSelectElement).value))}>
                    {(() => {
                      const allS = allShelves().filter(s => s.id !== ms.shelfId);
                      // Group by list
                      const listMap = new Map<number, { name: string; shelves: typeof allS }>();
                      for (const s of allS) {
                        const ln = lists().find(l => l.id === s.listId);
                        const key = s.listId;
                        if (!listMap.has(key)) listMap.set(key, { name: ln?.name || `List ${key}`, shelves: [] });
                        listMap.get(key)!.shelves.push(s);
                      }
                      return [...listMap.entries()].map(([listId, g]) => (
                        <optgroup label={g.name}>
                          {g.shelves.map(s => (
                            <option value={String(s.id)}>{s.name}</option>
                          ))}
                        </optgroup>
                      ));
                    })()}
                  </select>
                </label>
                <label>
                  Quantity
                  <input type="number" min="1" max="9999" value={moveQty()} onInput={e => { setMoveQty(parseInt((e.target as HTMLInputElement).value, 10) || 1); }} style="width:5rem" />
                </label>
                <footer style="display:flex;gap:8px;justify-content:flex-end">
                  <button type="button" class="secondary" onClick={() => setMoveState(null)}>Cancel</button>
                  <button type="button" onClick={doMove}>Save</button>
                </footer>
              </article>
              </div>
              </div>
            </>
          );
        })()}
      </Show>
    </div>
  );
}
