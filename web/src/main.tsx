import { runDesktopStorageMigration } from "./lib/desktopStorageMigration";

// The desktop storage migration must finish before any app module is
// evaluated: stores read localStorage at import time, so the app is loaded
// with a dynamic import afterwards (see lib/desktopStorageMigration.ts). A
// migration crash is logged and the app still loads — it must never leave the
// window blank.
runDesktopStorageMigration().then(
  (render) => {
    if (render) void import("./bootstrap");
  },
  (err) => {
    console.error("[storage-migration] crashed; loading app without it:", err);
    void import("./bootstrap");
  },
);
