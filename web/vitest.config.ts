import path from "path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  // Allow reading the Go-embedded capture bundle (internal/browse/capture.js)
  // from the smoke test; the app build config (vite.config.ts) is untouched.
  server: {
    fs: {
      allow: [path.resolve(__dirname, "..")],
    },
  },
  test: {
    environment: "jsdom",
    // Node 22.x ships an experimental global `localStorage` that is on by
    // default and returns undefined without --localstorage-file. vitest's
    // jsdom populate skips keys already on the global, so that accessor
    // shadowed jsdom's Storage; the setup polyfill then installed a plain
    // object, and every spy on Storage.prototype silently missed it. Turning
    // the Node global off lets jsdom's real Storage through.
    execArgv: ["--no-experimental-webstorage"],
    setupFiles: ["./src/test/setup.ts"],
    globals: true,
  },
});
