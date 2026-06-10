import "@picocss/pico";
import "./app.css";
import { onCleanup, onMount, Show } from "solid-js";
import { offline, needsAuth, setNeedsAuth } from "./store";
import OfflineBanner from "./components/OfflineBanner";
import AuthForm from "./components/AuthForm";
import Scanner from "./components/Scanner";
import ManualAddForm from "./components/ManualAddForm";
import ItemTable from "./components/ItemTable";

export default function App() {
  // Listen for auth-required events (emitted on 401 from api.ts)
  const onAuthRequired = () => setNeedsAuth(true);
  window.addEventListener("freezer:auth-required", onAuthRequired);

  // Check cookie on mount: if valid, skip auth form
  onMount(async () => {
    try {
      const res = await fetch("/api/auth/check", { credentials: "same-origin" });
      const data = await res.json();
      if (data.authenticated) {
        setNeedsAuth(false);
      }
    } catch (_) {
      // Network error — stay on auth form
    }
  });

  onCleanup(() => {
    window.removeEventListener("freezer:auth-required", onAuthRequired);
  });

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
          <h1 class="no-mb">🧊 Freezer Inventory</h1>
        </header>

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