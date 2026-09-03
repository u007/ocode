import { useEffect, useMemo, useRef, useState } from "react";
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
 * Two sources, both funneled into one request state:
 * 1. `ocode:open-preview` window events (file tree "Preview in sidebar",
 *    diagram node links, manual dispatches).
 * 2. The `preview_open` agent tool: its result carries a PREVIEW_OPEN
 *    sentinel through the chat transcript, which this hook scans in the
 *    active session's messages — no extra SSE channel needed.
 */
export function usePreviewActivation(sessionId: string | null): {
  request: PreviewOpenRequest | null;
  nonce: number;
} {
  const [eventReq, setEventReq] = useState<{ req: PreviewOpenRequest; nonce: number } | null>(null);
  const nonceRef = useRef(0);

  useEffect(() => {
    const onEvent = (e: Event) => {
      const detail = (e as CustomEvent<PreviewOpenRequest>).detail;
      if (!detail?.path) return;
      nonceRef.current += 1;
      setEventReq({ req: detail, nonce: nonceRef.current });
    };
    window.addEventListener(OPEN_PREVIEW_EVENT, onEvent as EventListener);
    return () => window.removeEventListener(OPEN_PREVIEW_EVENT, onEvent as EventListener);
  }, []);

  // Select the messages array by reference (stable across unrelated
  // dispatches) and parse the sentinel with useMemo.
  const messages = useChatSelector((s) => (sessionId ? (s.sessions[sessionId]?.messages ?? EMPTY_MESSAGES) : EMPTY_MESSAGES));
  const contents = useMemo(() => messages.map((m) => m.content), [messages]);

  const toolReq = useMemo(() => parsePreviewOpen(contents), [contents]);

  // Tool directives win while fresh: compare against the last consumed one.
  const lastToolRef = useRef<string>("");
  const [toolNonce, setToolNonce] = useState(0);
  const toolKey = toolReq ? `${toolReq.path}|${toolReq.page}` : "";
  useEffect(() => {
    if (toolKey && toolKey !== lastToolRef.current) {
      lastToolRef.current = toolKey;
      setToolNonce((n) => n + 1);
    }
  }, [toolKey]);

  if (eventReq && (!toolReq || eventReq.nonce >= toolNonce)) {
    return { request: eventReq.req, nonce: eventReq.nonce };
  }
  if (toolReq) return { request: toolReq, nonce: toolNonce };
  return { request: null, nonce: 0 };
}
