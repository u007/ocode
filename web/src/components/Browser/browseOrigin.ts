/**
 * Origin checks for window messages from the embedded browser's iframe.
 *
 * A page reports to the SPA through `window.parent.postMessage`. The SPA accepts
 * a message only when BOTH hold: its origin is the origin of the document the
 * panel last pointed the iframe at (the tab's own root-mounted origin, or the
 * prefix base in prefix mode), and its source is that iframe's window. The
 * origin alone is not enough: another frame could be on the same origin.
 */

/** Origin of a page URL, or null when the URL does not parse or its origin is
 *  opaque ("null"). A null origin never identifies a message source. */
export function browseOriginOf(url: string): string | null {
  try {
    const origin = new URL(url).origin;
    return origin === "null" ? null : origin;
  } catch {
    // intentionally not logged: a value with no origin simply has nothing to trust
    return null;
  }
}

/** The page the iframe was last pointed at: its origin, and the iframe's window. */
export interface BrowseMessageGate {
  /** Null before the first load, or when the last URL had no origin. */
  origin: string | null;
  /** The iframe's contentWindow, or null when no iframe is mounted. */
  frame: Window | null;
}

/** True when a message came from the gate's page: same origin, and the source
 *  is the gate's iframe window. Opaque origins never match. */
export function isTrustedBrowseMessage(ev: { origin: string; source: MessageEventSource | null }, gate: BrowseMessageGate): boolean {
  if (gate.origin === null || gate.frame === null) return false;
  if (ev.origin === "null" || ev.origin !== gate.origin) return false;
  return ev.source === gate.frame;
}
