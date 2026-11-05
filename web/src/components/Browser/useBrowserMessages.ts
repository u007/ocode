import { useEffect, type MutableRefObject, type RefObject } from "react";
import type { NetworkEvent, PageURLKind, StateKey } from "../../lib/browserStore";
import { isTrustedBrowseMessage } from "./browseOrigin";

interface Handlers {
  pushConsole: (key: StateKey, ev: { level: string; text: string; ts: number }) => void;
  pushNetwork: (key: StateKey, ev: NetworkEvent) => void;
  pushResponseBody: (key: StateKey, requestId: string, body: { body: string; base64Encoded: boolean; truncated: boolean }) => void;
  /** Local-mode page title (capture.js title observer). Display only. */
  onTitle?: (key: StateKey, title: string, url: string) => void;
  /** Local-mode scroll offset report (capture.js throttled scroll). */
  onScroll?: (key: StateKey, y: number, url: string) => void;
  /** Same-document navigation hint (capture.js pushState/replaceState/popstate/
   *  hashchange). Display-untrusted; the receiver must apply its own same-origin check. */
  onNav?: (key: StateKey, url: string, kind: PageURLKind) => void;
}

/** The hint kind capture.js sent. Anything unrecognised is a replace, the
 *  least disruptive entry change. */
function navKind(kind: unknown): PageURLKind {
  return kind === "push" || kind === "traverse" ? kind : "replace";
}

/** Where a panel's messages may come from: the origin of the document it last
 *  pointed the iframe at (the tab's own origin, or the prefix base), and the
 *  iframe itself. Refs, so the listener reads the current page on every event. */
export interface BrowseMessageSource {
  originRef: MutableRefObject<string | null>;
  frameRef: RefObject<HTMLIFrameElement | null>;
}

// Accepts messages ONLY from the current page: its origin AND the iframe's own
// window (isTrustedBrowseMessage). Everything else — the SPA's own origin, other
// tabs' origins, other frames — is dropped. "ocode:browse:nav" is a same-document
// hint only: the receiver accepts it solely for the tab's current origin.
export function useBrowserMessages(stateKey: StateKey, browseBase: string | null, source: BrowseMessageSource, h: Handlers) {
  const { pushConsole, pushNetwork, pushResponseBody, onTitle, onScroll, onNav } = h;
  const { originRef, frameRef } = source;
  useEffect(() => {
    if (!browseBase) return;
    function onMessage(e: MessageEvent) {
      const frame = frameRef.current ? frameRef.current.contentWindow : null;
      if (!isTrustedBrowseMessage(e, { origin: originRef.current, frame })) return; // hard origin + source gate
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
        case "ocode:browse:network": {
          // capture.js reports `duration`; the store's NetworkEvent is
          // `durationMs` — map at the boundary.
          const requestId = String(d.requestId ?? "");
          pushNetwork(stateKey, {
            requestId,
            method: String(d.method),
            url: String(d.url),
            status: Number(d.status) || 0,
            durationMs: Number(d.durationMs ?? d.duration) || 0,
            ts: Number(d.ts) || Date.now(),
            requestHeaders: d.requestHeaders && typeof d.requestHeaders === "object" ? d.requestHeaders : undefined,
            responseHeaders: d.responseHeaders && typeof d.responseHeaders === "object" ? d.responseHeaders : undefined,
            postData: typeof d.postData === "string" ? d.postData : undefined,
            contentType: typeof d.contentType === "string" ? d.contentType : undefined,
            size: typeof d.size === "number" ? d.size : undefined,
          });
          // Two body delivery shapes reach this boundary:
          //  - fetch reads the response stream asynchronously, so it posts the
          //    body LATER as an `ocode:browse:networkbody` message;
          //  - XHR already has `responseText` synchronously at `loadend` and
          //    carries it inline on the row.
          // Storing the inline one here (after the row, so `retainBodies` keeps
          // it) is what gives an XHR row its Body tab.
          if (requestId && typeof d.responseBody === "string") {
            pushResponseBody(stateKey, requestId, {
              body: d.responseBody,
              base64Encoded: false,
              truncated: !!d.responseBodyTruncated,
            });
          }
          break;
        }
        case "ocode:browse:networkbody":
          // Local-mode body, posted after its network row (same requestId). It
          // is a separate message so it never creates a second row.
          if (typeof d.responseBody === "string" && d.requestId) {
            pushResponseBody(stateKey, String(d.requestId), {
              body: d.responseBody,
              base64Encoded: false,
              truncated: !!d.responseBodyTruncated,
            });
          }
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
        case "ocode:browse:nav":
          if (typeof d.url === "string" && d.url) onNav?.(stateKey, d.url, navKind(d.kind));
          break;
        default:
          break;
      }
    }
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [stateKey, browseBase, originRef, frameRef, pushConsole, pushNetwork, pushResponseBody, onTitle, onScroll, onNav]);
}
