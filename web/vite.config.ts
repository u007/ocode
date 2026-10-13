import path from "path";
import { writeFileSync } from "node:fs";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  // Relative asset URLs so the bundle works when /rc is served behind a
  // tailscale path prefix that is stripped before reaching the local server.
  // index.html injects a matching <base href> at runtime (see web/index.html).
  base: "./",
  plugins: [
    react(),
    // web/embed.go's //go:embed all:dist needs this committed placeholder, and
    // emptyOutDir deletes it on every build. Re-create it after each build.
    {
      name: "keep-embed-placeholder",
      apply: "build",
      closeBundle() {
        writeFileSync(path.resolve(__dirname, "dist/.gitkeep"), "");
      },
    },
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:4096",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 6000,
  },
});
