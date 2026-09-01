import { useEffect } from "react";
import type { NetworkEvent, StateKey } from "../../lib/browserStore";

interface Handlers {
  pushConsole: (key: StateKey, ev: { level: string; text: string; ts: number }) => void;
  pushNetwork: (key: StateKey, ev: NetworkEvent) => void;
}

// Accepts messages ONLY from the browse origin. Everything else — including the
// SPA's own origin and any other frame — is dropped. "ocode:browse:nav" is
// intentionally ignored: the address bar is driven by server nav events, so a
// page-reported URL is never trusted for display.
export function useBrowserMessages(stateKey: StateKey, browseBase: string | null, h: Handlers) {
  const { pushConsole, pushNetwork } = h;
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
          pushNetwork(stateKey, { method: String(d.method), url: String(d.url), status: Number(d.status) || 0, durationMs: Number(d.duration) || 0, ts: Number(d.ts) || Date.now() });
          break;
        // "ocode:browse:nav" intentionally not handled: display-untrusted.
        default:
          break;
      }
    }
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [stateKey, browseBase, pushConsole, pushNetwork]);
}
