import "@picocss/pico";
import "./app.css";
import { createSignal, onCleanup, onMount, Show } from "solid-js";
import { offline, needsAuth, setNeedsAuth, currentListId, setCurrentListId, currentListName, lists, setLists, clearSelection } from "./store";
import { api } from "./api";
import OfflineBanner from "./components/OfflineBanner";
import AuthForm from "./components/AuthForm";
import Scanner from "./components/Scanner";
import ManualAddForm from "./components/ManualAddForm";
import ItemTable from "./components/ItemTable";
import PromptModal from "./components/PromptModal";
import ConfirmModal from "./components/ConfirmModal";

export default function App() {
  const onAuthRequired = () => setNeedsAuth(true);
  window.addEventListener("freezer:auth-required", onAuthRequired);

  onMount(async () => {
    try {
      const res = await fetch("/api/auth/check", { credentials: "same-origin" });
      const data = await res.json();
      if (data.authenticated) {
        setNeedsAuth(false);
      }
    } catch (_) {}
  });

  onCleanup(() => {
    window.removeEventListener("freezer:auth-required", onAuthRequired);
  });

  // List rename modal state
  const [renameOpen, setRenameOpen] = createSignal(false);
  const [newListOpen, setNewListOpen] = createSignal(false);
  const [deleteListId, setDeleteListId] = createSignal<number | null>(null);

  function openRename() {
    setRenameOpen(true);
  }

  async function doDeleteList() {
    const id = deleteListId();
    if (!id) return;
    try {
      await api.deleteList(id);
      setCurrentListId(1);
      const fresh = await api.getLists();
      setLists(fresh);
    } catch (_) {}
    setDeleteListId(null);
  }

  return (
    <main class="container app-container">
      <Show when={offline()}>
        <OfflineBanner />
      </Show>

      <Show when={needsAuth()}>
        <AuthForm />
      </Show>

      <Show when={!needsAuth()}>
        <header class="mb-1h">
          <div class="list-header">
            <Show when={lists().length <= 1}
              fallback={
                <select class="list-select" onChange={e => {
                  setCurrentListId(Number(e.target.value));
                  clearSelection();
                }}>
                  {lists().map(l => <option value={String(l.id)} selected={l.id === currentListId()}>{l.name}</option>)}
                </select>
              }>
              <h1 class="no-mb">{currentListName()}</h1>
            </Show>
            <button type="button" class="outline list-edit-btn" onClick={openRename} title="Rename list">✏️</button>
            {currentListId() !== 1 && (
              <button type="button" class="outline list-edit-btn" onClick={() => setDeleteListId(currentListId())} title="Delete list">🗑️</button>
            )}
          </div>
          <button type="button" class="add-list-btn" onClick={() => setNewListOpen(true)}>Add new list</button>
        </header>

        <Show when={renameOpen()}>
          <PromptModal
            title="Edit list name"
            initialValue={currentListName()}
            onSave={async (name) => {
              await api.updateList(currentListId(), name);
              const fresh = await api.getLists();
              setLists(fresh);
              setRenameOpen(false);
            }}
            onCancel={() => setRenameOpen(false)}
          />
        </Show>

        <Show when={newListOpen()}>
          <PromptModal
            title="Add a new list"
            placeholder="List name"
            saveLabel="Create"
            onSave={async (name) => {
              const created = await api.createList(name) as { id: number; name: string };
              const fresh = await api.getLists();
              setLists(fresh);
              setCurrentListId(created.id);
              setNewListOpen(false);
            }}
            onCancel={() => setNewListOpen(false)}
          />
        </Show>

        <Show when={deleteListId()}>
          <ConfirmModal
            variant="danger"
            message={`Permanently delete "${lists().find(l => l.id === deleteListId())?.name}" and all its items? This cannot be undone.`}
            onConfirm={doDeleteList}
            onCancel={() => setDeleteListId(null)}
          />
        </Show>

        <section class="section-gap">
          <Scanner />
        </section>

        <section class="section-gap">
          <ManualAddForm />
        </section>

        <section>
          <h3 class="mb-h">Inventory</h3>
          <ItemTable />
        </section>
      </Show>
    </main>
  );
}
