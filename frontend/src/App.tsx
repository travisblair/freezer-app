import "@picocss/pico";
import "./app.css";
import { createSignal, onCleanup, onMount, Show } from "solid-js";
import { offline, needsAuth, setNeedsAuth, currentListId, setCurrentListId, currentListName, lists, setLists, clearSelection, statusMessage, flashStatus } from "./store";
import { api } from "./api";
import OfflineBanner from "./components/OfflineBanner";
import AuthForm from "./components/AuthForm";
import Scanner from "./components/Scanner";
import ManualAddForm from "./components/ManualAddForm";
import ItemTable from "./components/ItemTable";
import PromptModal from "./components/PromptModal";
import ConfirmModal from "./components/ConfirmModal";
import StatusMessage from "./components/StatusMessage";
import NotificationsBell from "./components/NotificationsBell";
import NotificationsModal from "./components/NotificationsModal";
import type { AuditLog } from "./types";

export default function App() {
  const onAuthRequired = () => setNeedsAuth(true);
  window.addEventListener("freezer:auth-required", onAuthRequired);

  onMount(async () => {
    // Bounded boot retry: the old message promised a retry that never
    // existed (and the raw fetch bypassed the offline tracker). Two
    // retries with backoff, then an honest failure status.
    for (let attempt = 1; attempt <= 3; attempt++) {
      try {
        const res = await fetch("/api/auth/check", { credentials: "same-origin" });
        const data = await res.json();
        if (data.authenticated) {
          setNeedsAuth(false);
          // Initial notification fetch must run AFTER the gate clears —
          // fetchNotifications() skips itself while needsAuth() is true,
          // and the old mount ordering raced it every load (badge stayed
          // 0 until the first 60s tick).
          fetchNotifications();
        }
        return;
      } catch (_) {
        if (attempt < 3) {
          await new Promise((resolve) => setTimeout(resolve, 2000));
        } else {
          flashStatus("Connection failed — check your connection and reload");
        }
      }
    }
  });

  onCleanup(() => {
    window.removeEventListener("freezer:auth-required", onAuthRequired);
  });

  // List rename modal state
  const [renameOpen, setRenameOpen] = createSignal(false);
  const [newListOpen, setNewListOpen] = createSignal(false);
  const [deleteListId, setDeleteListId] = createSignal<number | null>(null);
  const [deletingList, setDeletingList] = createSignal(false);

  // Notification state
  const [notifOpen, setNotifOpen] = createSignal(false);
  const [unreadCount, setUnreadCount] = createSignal(0);
  const [notificationLogs, setNotificationLogs] = createSignal<AuditLog[]>([]);

  function openRename() {
    setRenameOpen(true);
  }

  async function doDeleteList() {
    const id = deleteListId();
    if (!id || deletingList()) return;
    // Dismiss immediately: the confirm button used to stay live through
    // the awaits, and a double-click fired DELETE twice (the second 404'd
    // and flashed a misleading failure).
    setDeleteListId(null);
    setDeletingList(true);
    try {
      await api.deleteList(id);
      setCurrentListId(1);
      const fresh = await api.getLists();
      setLists(fresh);
    } catch (_) { flashStatus("Failed to delete list"); }
    setDeletingList(false);
  }

  // ── Notifications ──────────────────────────────────────────────────

  async function fetchNotifications() {
    if (!needsAuth()) {
      try {
        const lastRead = localStorage.getItem("notif_last_read") || new Date(0).toISOString();
        const logs = await api.getNotifications(lastRead);
        setNotificationLogs(logs);
        setUnreadCount(logs.length);
      } catch (_) { /* silent — notifications are non-critical */ }
    }
  }

  async function openNotifications() {
    await fetchNotifications();
    setNotifOpen(true);
  }

  // Poll for new notifications every 60 seconds. The INITIAL fetch runs
  // in the auth onMount after the gate clears — calling it here raced
  // needsAuth() and self-skipped every load.
  onMount(() => {
    const interval = setInterval(fetchNotifications, 60000);
    onCleanup(() => clearInterval(interval));
  });

  return (
    <main class="container app-container">
      <StatusMessage message={statusMessage()} />
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
            <NotificationsBell count={unreadCount()} onClick={openNotifications} />
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
              try {
                await api.updateList(currentListId(), name);
                const fresh = await api.getLists();
                setLists(fresh);
                setRenameOpen(false);
              } catch (_) { flashStatus("Failed to rename list"); }
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
              try {
                const created = await api.createList(name) as { id: number; name: string };
                const fresh = await api.getLists();
                setLists(fresh);
                setCurrentListId(created.id);
                setNewListOpen(false);
              } catch (_) { flashStatus("Failed to create list"); }
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

        <Show when={notifOpen()}>
          <NotificationsModal
            logs={notificationLogs()}
            onClose={() => {
              localStorage.setItem("notif_last_read", new Date().toISOString());
              setUnreadCount(0);
              setNotifOpen(false);
            }}
          />
        </Show>

        <section class="section-gap">
          <Scanner />
        </section>

        <section class="section-gap">
          <ManualAddForm listId={currentListId()} />
        </section>

        <section>
          <h3 class="mb-h">Inventory</h3>
          <ItemTable />
        </section>
      </Show>
    </main>
  );
}
