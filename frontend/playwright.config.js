import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: "list",
  timeout: 15000,

  use: {
    baseURL: "http://localhost:5173",
  },

  webServer: {
    command: "VITE_NO_SSL=1 npx vite --port 5173 --strictPort --host",
    url: "http://localhost:5173",
    reuseExistingServer: !process.env.CI,
    timeout: 15000,
  },
});
