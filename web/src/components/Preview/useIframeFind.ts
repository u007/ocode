import { useCallback, useEffect, useRef, useState } from "react";
import type { RefObject } from "react";
import type { FindOptions } from "./findDom";
import { HTML_FIND_MESSAGE, HTML_FIND_READY, HTML_FIND_RESULT } from "./htmlFindAgent";

/** How long the bar shows "…" before giving up on a frame that never replies
 *  (a CSP that blocks the injected agent, or a page that replaced itself). */
const IFRAME_FIND_REPLY_TIMEOUT_MS = 1200;

/**
 * Find for the HTML preview.
 *
 * The iframe is sandboxed without `allow-same-origin`, so the parent cannot
 * inspect its DOM. `htmlFindAgent` is injected into the `srcDoc` and the two
 * sides speak the small postMessage protocol documented there. The parent owns
 * the bar state and the query; the frame owns matching, painting and scrolling.
 */
export function useIframeFind(params: { iframeRef: RefObject<HTMLIFrameElement | null> }) {
  const { iframeRef } = params;
  const [active, setActive] = useState(false);
  const [query, setQuery] = useState("");
  const [opts, setOpts] = useState<FindOptions>({ caseSensitive: false, wholeWord: false });
  const [count, setCount] = useState(0);
  const [current, setCurrent] = useState(0);
  const [searching, setSearching] = useState(false);
  // Bumped on every frame load (and on the agent's ready message): both mean
  // the document inside was replaced, so an open query must be re-sent.
  const [frameGeneration, setFrameGeneration] = useState(0);
  const replyTimer = useRef<number | null>(null);

  const clearReplyTimer = () => {
    if (replyTimer.current !== null) {
      window.clearTimeout(replyTimer.current);
      replyTimer.current = null;
    }
  };

  const awaitReply = useCallback(() => {
    setSearching(true);
    clearReplyTimer();
    replyTimer.current = window.setTimeout(() => {
      replyTimer.current = null;
      setSearching(false);
    }, IFRAME_FIND_REPLY_TIMEOUT_MS);
  }, []);

  const post = useCallback(
    (payload: Record<string, unknown>) => {
      const win = iframeRef.current?.contentWindow;
      if (!win) return;
      win.postMessage({ [HTML_FIND_MESSAGE]: true, ...payload }, "*");
    },
    [iframeRef],
  );

  const onFrameLoad = useCallback(() => {
    setFrameGeneration((g) => g + 1);
  }, []);

  // Search whenever the bar opens, the query/options change, or the frame
  // reloads (srcDoc edits reload it on every debounced keystroke).
  useEffect(() => {
    if (!active) return;
    awaitReply();
    setCount(0);
    setCurrent(0);
    post({
      action: "search",
      query,
      caseSensitive: opts.caseSensitive,
      wholeWord: opts.wholeWord,
    });
  }, [active, query, opts.caseSensitive, opts.wholeWord, frameGeneration, awaitReply, post]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const frame = iframeRef.current;
      if (!frame || event.source !== frame.contentWindow) return;
      const data = event.data as Record<string, unknown> | null;
      if (!data) return;
      if (data[HTML_FIND_READY] === true) {
        // The agent installed after our last send — re-run the open query.
        setFrameGeneration((g) => g + 1);
        return;
      }
      if (data[HTML_FIND_RESULT] !== true) return;
      clearReplyTimer();
      setSearching(false);
      setCount(typeof data.count === "number" ? data.count : 0);
      setCurrent(typeof data.current === "number" ? data.current : 0);
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [iframeRef]);

  useEffect(() => clearReplyTimer, []);

  const show = useCallback(() => setActive(true), []);
  const hide = useCallback(() => {
    post({ action: "close" });
    setActive(false);
    setSearching(false);
    setCount(0);
    setCurrent(0);
    clearReplyTimer();
  }, [post]);

  const go = useCallback(
    (dir: 1 | -1) => {
      if (!count) return;
      awaitReply();
      post({ action: "step", dir });
    },
    [awaitReply, post, count],
  );

  const toggleOption = useCallback((key: keyof FindOptions) => {
    setOpts((o) => ({ ...o, [key]: !o[key] }));
  }, []);

  return {
    active,
    show,
    hide,
    query,
    setQuery,
    opts,
    toggleOption,
    count,
    current,
    searching,
    go,
    onFrameLoad,
  };
}
