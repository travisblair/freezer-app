/**
 * Vitest setup — runs before each test file.
 * Registers a cleanup function for solid-testing-library.
 */
import { cleanup } from "@solidjs/testing-library";
import { afterEach } from "vitest";

afterEach(() => {
  cleanup();
});
