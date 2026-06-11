/**
 * Vitest setup — runs before each test file.
 * Polyfill browser APIs for jsdom + SolidJS compatibility.
 */
import { cleanup } from "@solidjs/testing-library";
import { afterEach } from "vitest";

// jsdom doesn't support HTMLDialogElement — polyfill it
HTMLDialogElement.prototype.showModal = function () {
  this.setAttribute("open", "");
};
HTMLDialogElement.prototype.close = function () {
  this.removeAttribute("open");
};

// SolidJS needs these to detect a browser environment
Object.defineProperty(globalThis, "window", { value: globalThis });
Object.defineProperty(globalThis, "document", { value: document });

afterEach(() => {
  cleanup();
});
