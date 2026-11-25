import { isDesktopShell } from "./desktopShell";
import { invokeWails } from "./wails";

/**
 * Message prefix for the minimal desktop bridge. The shell's
 * `application.Options.RawMessageHandler` maps `ocode:open-external:<url>` to
 * `application.Browser.OpenURL` (the system browser). It must NOT start with
 * `wails:` — those messages are consumed by the framework itself instead of
 * reaching the raw handler.
 */
export const OPEN_EXTERNAL_MESSAGE_PREFIX = "ocode:open-external:";

/** App-level request for the embedded-vs-external URL chooser. */
export const OPEN_URL_EVENT = "ocode:open-url";

export interface OpenURLDetail {
  url: string;
  /** Project that owns the clicked surface. Embedded tabs must use this pair. */
  projectPath?: string;
  host?: string;
  /** Set by the sole listener when a chooser is already open. */
  refused?: boolean;
}

export interface URLOpenOwner {
  projectPath: string;
  host?: string;
}

export function isHTTPURL(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

/**
 * Ask the app-level chooser where to open a user-clicked HTTP(S) URL.
 *
 * Link providers outside React (Monaco, xterm, MCP widgets) cannot use a hook,
 * so they dispatch this cancelable DOM event. App prevents it after mounting
 * the chooser; an unhandled event is an error rather than a silent bypass.
 */
export function requestURLOpen(value: string, owner?: URLOpenOwner): void {
  if (!isHTTPURL(value)) return;
  const detail: OpenURLDetail = {
    url: value,
    ...(owner?.projectPath ? { projectPath: owner.projectPath } : {}),
    ...(owner?.host ? { host: owner.host } : {}),
  };
  const event = new CustomEvent<OpenURLDetail>(OPEN_URL_EVENT, {
    detail,
    cancelable: true,
  });
  if (window.dispatchEvent(event)) {
    throw new Error("requestURLOpen: no URL chooser listener is mounted");
  }
  if (detail.refused) {
    throw new Error("requestURLOpen: a URL chooser is already open");
  }
}

/**
 * Open a user-facing HTTP(S) URL outside the current ocode surface.
 *
 * Desktop: the SPA is served by ocode's own HTTP server, NOT Wails' asset
 * server, so the full runtime module at `/wails/runtime.js` is unreachable —
 * that path hits the SPA fallback and returns index.html, making the module
 * import fail. Calling `Browser.OpenURL` from the runtime is therefore
 * impossible, so we route through the minimal `window._wails.invoke` bridge
 * (the same channel as `wails:runtime:ready` and the quit-guard messages) and
 * let the shell make the native call.
 *
 * Browser: a real user-gesture popup, falling back to same-tab navigation when
 * a popup blocker rejects it.
 */
export function openExternalURL(value: string): void {
  if (!isHTTPURL(value)) return;

  if (isDesktopShell()) {
    if (invokeWails(OPEN_EXTERNAL_MESSAGE_PREFIX + value)) return;
    // The bridge is injected on every navigation, so this is at most a sub-ms
    // race right after load. Retry once rather than falling back to
    // `window.location.assign`, which would replace the entire app UI with the
    // target page instead of opening the system browser.
    window.setTimeout(() => {
      invokeWails(OPEN_EXTERNAL_MESSAGE_PREFIX + value);
    }, 50);
    return;
  }

  const popup = window.open(value, "_blank", "noopener,noreferrer");
  if (!popup) window.location.assign(value);
}
