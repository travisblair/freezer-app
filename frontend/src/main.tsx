/* @refresh reload */
import { render } from "solid-js/web";
import App from "./App";

// Register service worker for cache-busting on iOS PWA
navigator.serviceWorker?.register("/sw.js");

const root = document.getElementById("root");
if (root) {
  render(() => <App />, root);
}