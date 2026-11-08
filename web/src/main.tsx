import { runDesktopStorageMigration } from "./lib/desktopStorageMigration";

runDesktopStorageMigration().then(
  (render) => {
    if (render) {
      void import("./bootstrap").then(() => {
        if (import.meta.env.PROD) {
          void import("./lib/serviceWorker").then(({ registerServiceWorker }) => {
            void registerServiceWorker();
          });
        }
      });
    }
  },
  (err) => {
    console.error("[storage-migration] crashed; loading app without it:", err);
    void import("./bootstrap").then(() => {
      if (import.meta.env.PROD) {
        void import("./lib/serviceWorker").then(({ registerServiceWorker }) => {
          void registerServiceWorker();
        });
      }
    });
  },
);
