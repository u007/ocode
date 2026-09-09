import { isDesktopShell } from "./desktopShell";

type WailsRuntime = {
  Browser?: {
    OpenURL?: (url: string) => Promise<void>;
  };
};

// Keep the import out of Vite's module graph. The desktop shell serves the
// Wails runtime at this path, while a normal browser does not have it.
const importRuntime = new Function("path", "return import(path)") as (
  path: string,
) => Promise<WailsRuntime>;

function isHTTPURL(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

/**
 * Open a user-facing HTTP(S) URL outside the current ocode surface.
 *
 * Wails webviews do not reliably handle `target="_blank"` themselves, so
 * desktop uses the native Wails Browser.OpenURL runtime API. In a normal
 * browser we use a real user-gesture popup and fall back to same-tab
 * navigation when a popup blocker rejects it.
 */
export function openExternalURL(value: string): void {
  if (!isHTTPURL(value)) return;

  if (isDesktopShell()) {
    void importRuntime("/wails/runtime.js")
      .then((runtime) => {
        const openURL = runtime.Browser?.OpenURL;
        if (!openURL) throw new Error("Wails browser runtime is unavailable");
        return openURL(value);
      })
      .catch(() => {
        // The runtime may not be ready during an early click. Same-tab
        // navigation still gives the user a working login path.
        window.location.assign(value);
      });
    return;
  }

  const popup = window.open(value, "_blank", "noopener,noreferrer");
  if (!popup) window.location.assign(value);
}
