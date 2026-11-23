import { useCallback, useEffect, useRef, type MutableRefObject, type RefObject } from "react";
import { isTrustedBrowseMessage } from "./browseOrigin";
import { normalizeLocalTree, type DomTreeResult } from "../../lib/domInspector";

/** A request awaiting a reply from capture.js. */
interface Pending {
  resolve: (value: unknown) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export interface LocalInspectorApi {
  /** Whole-document tree from the page (normalized). */
  fetchTree(): Promise<DomTreeResult>;
  /** Outline an element (path) or clear the outline (null). */
  highlight(path: string | null): void;
  /** Arm/disarm the in-page element picker. */
  setPick(on: boolean): void;
  /** Screenshot of the element as a PNG data URL. */
  capture(path: string): Promise<string>;
}

const TREE_TIMEOUT_MS = 10_000;
// html-to-image renders the document in-page; a heavy page can take seconds.
const CAPTURE_TIMEOUT_MS = 60_000;

/**
 * RPC to capture.js for the local reverse-proxy iframe. Replies are accepted
 * only from the tab's own page (same origin AND that iframe's window), the same
 * gate every other capture.js message passes. All state lives in the hook, so a
 * navigation (which discards the page and its script) leaves no stale resolvers.
 */
export function useLocalInspector(
  stateKey: string,
  frameRef: RefObject<HTMLIFrameElement | null>,
  originRef: MutableRefObject<string | null>,
): LocalInspectorApi {
  const pending = useRef<Map<string, Pending>>(new Map());
  const seq = useRef(0);

  const post = useCallback(
    (msg: Record<string, unknown>): boolean => {
      const frame = frameRef.current;
      const origin = originRef.current;
      if (!frame || !origin || !frame.contentWindow) return false;
      frame.contentWindow.postMessage({ ...msg, stateKey }, origin);
      return true;
    },
    [frameRef, originRef, stateKey],
  );

  const request = useCallback(
    (type: string, extra: Record<string, unknown>, timeoutMs: number): Promise<unknown> => {
      return new Promise((resolve, reject) => {
        const requestId = `${type}:${Date.now()}:${seq.current++}`;
        if (!post({ type, requestId, ...extra })) {
          reject(new Error("the page is not available"));
          return;
        }
        const timer = setTimeout(() => {
          pending.current.delete(requestId);
          reject(new Error("the page did not answer"));
        }, timeoutMs);
        pending.current.set(requestId, { resolve, reject, timer });
      });
    },
    [post],
  );

  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      const frame = frameRef.current ? frameRef.current.contentWindow : null;
      if (!isTrustedBrowseMessage(e, { origin: originRef.current, frame })) return;
      const d = e.data as { stateKey?: string; type?: string; requestId?: string; error?: string } | null;
      if (!d || d.stateKey !== stateKey || !d.requestId) return;
      if (d.type !== "ocode:browse:inspector-tree" && d.type !== "ocode:browse:inspector-capture") return;
      const entry = pending.current.get(d.requestId);
      if (!entry) return;
      pending.current.delete(d.requestId);
      clearTimeout(entry.timer);
      if (d.error) entry.reject(new Error(d.error));
      else entry.resolve(d);
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [frameRef, originRef, stateKey]);

  // A reply can never arrive after unmount; reject rather than leak timers.
  useEffect(() => {
    const entries = pending.current;
    return () => {
      for (const entry of entries.values()) {
        clearTimeout(entry.timer);
        entry.reject(new Error("closed"));
      }
      entries.clear();
    };
  }, []);

  const fetchTree = useCallback(async (): Promise<DomTreeResult> => {
    const raw = await request("ocode:browse:inspector-tree", {}, TREE_TIMEOUT_MS);
    return normalizeLocalTree(raw);
  }, [request]);

  const capture = useCallback(async (path: string): Promise<string> => {
    const raw = (await request("ocode:browse:inspector-capture", { path }, CAPTURE_TIMEOUT_MS)) as { dataUrl?: string };
    if (!raw?.dataUrl) throw new Error("the page returned no screenshot");
    return raw.dataUrl;
  }, [request]);

  const highlight = useCallback((path: string | null) => {
    post({ type: "ocode:browse:inspector-highlight", path });
  }, [post]);

  const setPick = useCallback((on: boolean) => {
    post({ type: "ocode:browse:inspector-pick-set", on });
  }, [post]);

  return { fetchTree, highlight, setPick, capture };
}
