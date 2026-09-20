import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useChatSelector } from "../../stores/chatStore";
import type { Message } from "../../api/types";
import {
  OPEN_PREVIEW_EVENT,
  parsePreviewOpen,
  type PreviewOpenRequest,
} from "../../lib/previewKind";

const EMPTY_MESSAGES: Message[] = [];

/**
 * Listens for AI-driven preview activations and surfaces the latest one.
 * Two sources, both funneled into one pending request:
 * 1. `ocode:open-preview` window events (file tree "Preview in sidebar",
 *    diagram node links, manual dispatches).
 * 2. The `preview_open` agent tool: its result carries a PREVIEW_OPEN
 *    sentinel through the chat transcript, which this hook scans in the
 *    active session's messages — no extra SSE channel needed.
 *
 * An activation is an EVENT, not durable state: the returned request is
 * one-shot and must be acknowledged with `consume()` (the sidebar PreviewHost
 * does this as it applies it). Without consume, remounting the panel — which a
 * project/session switch does, since the panel is keyed by the active tab —
 * would replay an old activation and clobber the state that was restored for
 * the newly-active project.
 */
export function usePreviewActivation(sessionId: string | null): {
  request: PreviewOpenRequest | null;
  nonce: number;
  /** Acknowledge the pending activation; it will not be surfaced again. */
  consume: () => void;
} {
  // Monotonic across the app's lifetime (the hook lives in App, which does not
  // remount) so a later activation always outranks an earlier one.
  const nonceRef = useRef(0);
  const [pending, setPending] = useState<{ req: PreviewOpenRequest; nonce: number } | null>(null);
  const nextNonce = useCallback(() => {
    nonceRef.current += 1;
    return nonceRef.current;
  }, []);

  useEffect(() => {
    const onEvent = (e: Event) => {
      const detail = (e as CustomEvent<PreviewOpenRequest>).detail;
      if (!detail?.path) return;
      setPending({ req: detail, nonce: nextNonce() });
    };
    window.addEventListener(OPEN_PREVIEW_EVENT, onEvent as EventListener);
    return () => window.removeEventListener(OPEN_PREVIEW_EVENT, onEvent as EventListener);
  }, [nextNonce]);

  // Select the messages array by reference (stable across unrelated
  // dispatches) and parse the sentinel with useMemo.
  const messages = useChatSelector((s) => (sessionId ? (s.sessions[sessionId]?.messages ?? EMPTY_MESSAGES) : EMPTY_MESSAGES));
  const contents = useMemo(() => messages.map((m) => m.content), [messages]);

  const toolReq = useMemo(() => parsePreviewOpen(contents), [contents]);

  // Fire once per new tool directive: compare against the last one raised.
  const lastToolRef = useRef<string>("");
  const toolKey = toolReq ? `${toolReq.path}|${toolReq.page}` : "";
  useEffect(() => {
    if (toolKey && toolKey !== lastToolRef.current) {
      lastToolRef.current = toolKey;
      setPending({ req: toolReq as PreviewOpenRequest, nonce: nextNonce() });
    }
  }, [toolKey, toolReq, nextNonce]);

  const consume = useCallback(() => setPending(null), []);

  return { request: pending?.req ?? null, nonce: pending?.nonce ?? 0, consume };
}
