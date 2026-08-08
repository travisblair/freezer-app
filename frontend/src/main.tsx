/* @refresh reload */
import { render } from "solid-js/web";
import App from "./App";

// Register service worker for cache-busting on iOS PWA.
// On page load, force an update check — browsers throttle SW updates
// to every 24 hours otherwise, which means stale caches for a day.
navigator.serviceWorker?.register("/sw.js").then(reg => {
  reg.update();
});

const root = document.getElementById("root");
if (root) {
  render(() => <App />, root);
}