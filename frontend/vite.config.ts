import { defineConfig } from "vite";
import solidPlugin from "vite-plugin-solid";

export default defineConfig({
  plugins: [solidPlugin()],
  server: {
    host: "0.0.0.0",
    allowedHosts: [
      "localhost",
      "127.0.0.1",
      ".local",
      ".ts.net",
    ],
    proxy: {
      "/api": "http://localhost:3000",
    },
  },
});