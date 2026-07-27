import { api } from "../api";

export default function OfflineBanner() {
  return (
    <article class="offline-banner">
      <span>Server unreachable — check your connection</span>
      <button
        type="button"
        class="outline"
        onClick={async () => {
          try {
            await api.check();
          } catch {
            // check() triggers trackOffline which updates the signal
          }
        }}
      >
        Retry
      </button>
    </article>
  );
}