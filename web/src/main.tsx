import { runDesktopStorageMigration } from "./lib/desktopStorageMigration";

// Registers the app-shell service worker, production builds only. Called after
// the bootstrap module loads, from every path that renders the app.
function registerServiceWorkerInProd() {
  if (import.meta.env.PROD) {
    void import("./lib/serviceWorker").then(({ registerServiceWorker }) => {
      void registerServiceWorker();
    });
  }
}

runDesktopStorageMigration().then(
  (render) => {
    if (render) {
      void import("./bootstrap").then(registerServiceWorkerInProd);
    }
  },
  (err) => {
    console.error("[storage-migration] crashed; loading app without it:", err);
    void import("./bootstrap").then(registerServiceWorkerInProd);
  },
);
