import { createSignal } from "solid-js";
import { api } from "../api";
import type { Item, Shelf, List } from "../types";
import BaseModal from "./BaseModal";
import { bumpItemsVersion } from "../store";

interface Props {
  item: Item;
  shelfId: number;
  count: number;
  allShelves: () => Shelf[];
  lists: () => List[];
  onDone: () => void;
  onCancel: () => void;
}

export default function MoveModal(props: Props) {
  const otherShelves = () =>
    props.allShelves().filter((s) => s.id !== props.shelfId);

  const initTarget = () => otherShelves()[0]?.id ?? 1;
  const [moveTarget, setMoveTarget] = createSignal(initTarget());
  const [moveQty, setMoveQty] = createSignal(props.count);

  async function doMove() {
    try {
      await api.moveItem(props.item.id, props.shelfId, moveTarget(), moveQty());
      bumpItemsVersion();
      props.onDone();
    } catch (err) {
      if (import.meta.env.DEV) console.error("Move failed", err);
    }
  }

  const footer = (
    <>
      <button type="button" class="secondary" onClick={props.onCancel}>
        Cancel
      </button>
      <button type="button" onClick={doMove}>
        Save
      </button>
    </>
  );

  return (
    <BaseModal
      title={`Move ${props.item.name}`}
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
            const allS = otherShelves();
            const listMap = new Map<
              number,
              { name: string; shelves: typeof allS }
            >();
            for (const s of allS) {
              const ln = props.lists().find((l) => l.id === s.listId);
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
      <label>
        Quantity
        <input
          type="number"
          min="1"
          max="9999"
          value={moveQty()}
          onInput={(e) => {
            setMoveQty(parseInt((e.target as HTMLInputElement).value, 10) || 1);
          }}
          style="width:5rem"
        />
      </label>
    </BaseModal>
  );
}
