import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The bridge serves dist/ over HTTPS and proxies /api and /bridge to ocode and itself, so the pane
// only ever talks to its own origin. In `vite dev`, run the bridge too and point the proxy at it.
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: { outDir: "dist", emptyOutDir: true, rollupOptions: { input: "taskpane.html" } },
  server: {
    port: 3000,
    proxy: {
      "/api": process.env.BRIDGE_URL ?? "https://127.0.0.1:3443",
      "/bridge": process.env.BRIDGE_URL ?? "https://127.0.0.1:3443",
    },
  },
});
