import { useEffect } from "react";
import type { NetworkEvent, StateKey } from "../../lib/browserStore";

interface Handlers {
  pushConsole: (key: StateKey, ev: { level: string; text: string; ts: number }) => void;
  pushNetwork: (key: StateKey, ev: NetworkEvent) => void;
  /** Local-mode page title (capture.js title observer). Display only. */
  onTitle?: (key: StateKey, title: string, url: string) => void;
  /** Local-mode scroll offset report (capture.js throttled scroll). */
  onScroll?: (key: StateKey, y: number, url: string) => void;
}

// Accepts messages ONLY from the browse origin. Everything else — including the
// SPA's own origin and any other frame — is dropped. "ocode:browse:nav" is
// intentionally ignored: the address bar is driven by server nav events, so a
// page-reported URL is never trusted for display.
export function useBrowserMessages(stateKey: StateKey, browseBase: string | null, h: Handlers) {
  const { pushConsole, pushNetwork, onTitle, onScroll } = h;
  useEffect(() => {
    if (!browseBase) return;
    let origin: string;
    try {
      origin = new URL(browseBase).origin;
    } catch (e) {
      console.error("browse: malformed browseBase", browseBase, e);
      return;
    }
    function onMessage(e: MessageEvent) {
      if (e.origin !== origin) return; // hard origin gate
      const d = e.data;
      if (!d || typeof d !== "object") return;
      if (d.stateKey !== stateKey) return;
      switch (d.type) {
        case "ocode:browse:console":
          pushConsole(stateKey, {
            level: String(d.level ?? "log"),
            text: String(Array.isArray(d.args) ? d.args.join(" ") : d.args ?? ""),
            ts: Number(d.ts) || Date.now(),
          });
          break;
        case "ocode:browse:network":
          // capture.js reports `duration`; the store's NetworkEvent is
          // `durationMs` — map at the boundary.
          pushNetwork(stateKey, {
            requestId: "",
            method: String(d.method),
            url: String(d.url),
            status: Number(d.status) || 0,
            durationMs: Number(d.duration) || 0,
            ts: Number(d.ts) || Date.now(),
          });
          break;
        case "ocode:browse:title":
          // Display-only page title (initial + JS-driven changes). The
          // address bar never renders this; setPageTitle drops it when the
          // URL doesn't match the surface (stale-event guard).
          // Empty string is an explicit clear (title removed) — the
          // store falls back instead of sticking on the old title.
          if (typeof d.title === "string") {
            onTitle?.(stateKey, d.title, typeof d.url === "string" ? d.url : "");
          }
          break;
        case "ocode:browse:scroll":
          if (typeof d.y === "number") {
            onScroll?.(stateKey, d.y, typeof d.url === "string" ? d.url : "");
          }
          break;
        // "ocode:browse:nav" intentionally not handled: display-untrusted.
        default:
          break;
      }
    }
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [stateKey, browseBase, pushConsole, pushNetwork, onTitle, onScroll]);
}
