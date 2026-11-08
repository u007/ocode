import path from "path";
import { writeFileSync, existsSync, readdirSync, readFileSync } from "node:fs";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "./",
  plugins: [
    react(),
    {
      name: "sw-precache-manifest",
      apply: "build",
      closeBundle() {
        const dist = path.resolve(__dirname, "dist");
        const swPath = path.resolve(dist, "sw.js");
        if (!existsSync(swPath)) return;
        const assets = [];
        function walk(dir) {
          for (const entry of readdirSync(dir, { withFileTypes: true })) {
            const full = path.join(dir, entry.name);
            if (entry.isDirectory()) walk(full);
            else {
              const rel = "/" + path.relative(dist, full).replace(/\\/g, "/");
              if (rel === "/index.html" || rel.startsWith("/assets/")) assets.push(rel);
            }
          }
        }
        walk(dist);
        if (!assets.includes("/index.html")) assets.unshift("/index.html");
        const manifest = JSON.stringify(assets);
        const src = readFileSync(swPath, "utf8");
        const out = src.replace("self.__PRECACHE_MANIFEST__ || ['/index.html']", manifest);
        writeFileSync(swPath, out);
      },
    },
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
