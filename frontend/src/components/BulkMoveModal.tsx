import { createSignal, For } from "solid-js";
import { api } from "../api";
import type { Shelf, List } from "../types";
import BaseModal from "./BaseModal";
import { bumpItemsVersion } from "../store";

export interface BulkMoveItemData {
  itemId: number;
  name: string;
  sourceShelfId: number;
  sourceShelfName: string;
  count: number;
}

interface Props {
  items: BulkMoveItemData[];
  allShelves: Shelf[];
  lists: List[];
  onDone: () => void;
  onCancel: () => void;
}

export default function BulkMoveModal(props: Props) {
  const targetShelves = () =>
    props.allShelves.filter(
      (s) => !props.items.every((i) => i.sourceShelfId === s.id)
    );

  const initTarget = () => targetShelves()[0]?.id ?? 1;
  const [moveTarget, setMoveTarget] = createSignal(initTarget());
  const [quantities, setQuantities] = createSignal<Record<number, number>>(
    Object.fromEntries(props.items.map((i) => [i.itemId, i.count]))
  );
  const [moving, setMoving] = createSignal(false);

  function setQty(itemId: number, val: number) {
    const item = props.items.find((i) => i.itemId === itemId);
    const max = item?.count ?? 1;
    setQuantities((prev) => ({ ...prev, [itemId]: Math.max(1, Math.min(val, max)) }));
  }

  async function doMove() {
    setMoving(true);
    try {
      const target = moveTarget();
      for (const item of props.items) {
        const qty = quantities()[item.itemId] ?? item.count;
        if (qty > 0) {
          await api.moveItem(item.itemId, item.sourceShelfId, target, qty);
        }
      }
      bumpItemsVersion();
      props.onDone();
    } catch (err) {
      if (import.meta.env.DEV) console.error("Bulk move failed", err);
    } finally {
      setMoving(false);
    }
  }

  const footer = (
    <>
      <button type="button" class="secondary" onClick={props.onCancel}>
        Cancel
      </button>
      <button type="button" onClick={doMove} aria-busy={moving()}>
        Move All
      </button>
    </>
  );

  return (
    <BaseModal
      title={`Move ${props.items.length} item${props.items.length !== 1 ? "s" : ""}`}
      onClose={props.onCancel}
      footer={footer}
    >
      <label>
        To shelf
        <select
          value={String(moveTarget())}
          onChange={(e) =>
            setMoveTarget(Number((e.target as HTMLSelectElement).value))
          }
        >
          {(() => {
            const allS = targetShelves();
            const listMap = new Map<
              number,
              { name: string; shelves: typeof allS }
            >();
            for (const s of allS) {
              const ln = props.lists.find((l) => l.id === s.listId);
              const key = s.listId;
              if (!listMap.has(key))
                listMap.set(key, { name: ln?.name || `List ${key}`, shelves: [] });
              listMap.get(key)!.shelves.push(s);
            }
            return [...listMap.entries()].map(([listId, g]) => (
              <optgroup label={g.name}>
                {g.shelves.map((s) => (
                  <option value={String(s.id)}>{s.name}</option>
                ))}
              </optgroup>
            ));
          })()}
        </select>
      </label>

      <div style="margin-top:1rem">
        <For each={props.items}>
          {(data) => (
            <div style="display:flex;align-items:center;gap:8px;margin-bottom:8px">
              <span style="flex:1">
                <strong>{data.name}</strong>
                <br />
                <small>
                  From {data.sourceShelfName} ({data.count} available)
                </small>
              </span>
              <label style="margin:0;display:flex;align-items:center;gap:4px">
                Qty
                <input
                  type="number"
                  min="1"
                  max={data.count}
                  value={quantities()[data.itemId] ?? data.count}
                  onInput={(e) =>
                    setQty(
                      data.itemId,
                      parseInt((e.target as HTMLInputElement).value, 10) || 1
                    )
                  }
                  style="width:5rem"
                />
              </label>
            </div>
          )}
        </For>
      </div>
    </BaseModal>
  );
}
