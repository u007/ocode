/**
 * Single source of truth for the per-window id.
 *
 * The desktop shell threads a stable `?windowId=main` into the webview; the
 * standalone web server has no such param, so each browser tab mints its own
 * random id. Profile state (the top-right ProfileSwitcher's active profile)
 * and chat requests (the `X-Window-Id` header / body `windowId`) MUST resolve
 * the SAME id, or a profile picked for one window is never seen by the session
 * bound to another.
 *
 * This used to be duplicated: ProfileSwitcher captured the id once at module
 * load, while `api.sendMessage`/`api.chat` re-derived it per call. Because the
 * URL-param branch did not persist to sessionStorage, any later navigation that
 * dropped the query string (e.g. SessionPage's deep-link `navigate("/")`) made
 * the chat re-derive a fresh random id — so the profile pill showed the new
 * profile while the session stayed bound to a window with no profile. Resolving
 * once here and always persisting to sessionStorage keeps them identical.
 */
export const WINDOW_ID_STORAGE_KEY = "ocode.windowId"

export function getWindowId(): string {
  if (typeof window === "undefined") return "win-1"
  try {
    const fromURL = new URLSearchParams(window.location.search).get("windowId")?.trim()
    if (fromURL) {
      // Persist even though it came from the URL: a later SPA navigation (the
      // desktop deep-link redirect) replaces the location with a clean path and
      // would otherwise strand every subsequent resolution on a fresh random id.
      sessionStorage.setItem(WINDOW_ID_STORAGE_KEY, fromURL)
      return fromURL
    }
    const stored = sessionStorage.getItem(WINDOW_ID_STORAGE_KEY)
    if (stored) return stored
    const id = `win-${crypto.randomUUID().slice(0, 8)}`
    sessionStorage.setItem(WINDOW_ID_STORAGE_KEY, id)
    return id
  } catch {
    return "win-1"
  }
}
