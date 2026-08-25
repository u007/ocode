import { useEffect, useRef } from "react";
import { authedFetch } from "@/api/client";
import { isDesktopShell } from "@/lib/desktopShell";
import { terminalRegistrySnapshot } from "@/lib/debug/terminalRegistry";
import { useChatState } from "@/stores/chatStore";
import type { ChatState } from "@/stores/chatStore";

const REPORT_INTERVAL_MS = 30_000;

function windowId(): string {
  const fromQuery = new URLSearchParams(window.location.search).get("windowId")?.trim();
  if (fromQuery) return fromQuery;
  try {
    return sessionStorage.getItem("ocode.windowId") || "";
  } catch {
    return "";
  }
}

// Sums string-valued fields likely to hold large content (message text,
// provider reasoning-continuation blobs, tool call/output text) across every
// open session. Not a precise byte count — a cheap proxy to see whether this
// grows unbounded over a session's lifetime, which JSON.stringify-ing
// everything every 30s would too, at real CPU cost for large histories.
type ChatStats = { messageCount: number; bytes: number };

const sliceStats = new Map<string, { slice: ChatState["sessions"][string]; stats: ChatStats }>();

function estimateChatBytes(sessions: ChatState["sessions"]): { sessionCount: number; messageCount: number; bytes: number } {
  let messageCount = 0;
  let bytes = 0;
  const sessionIds = Object.keys(sessions);
  for (const id of sessionIds) {
    const slice = sessions[id];
    let stats = sliceStats.get(id);
    if (!stats || stats.slice !== slice) {
      let sliceBytes = 0;
      for (const m of slice.messages) {
        sliceBytes += m.content.length + (m.reasoning_content?.length ?? 0);
        if (m.tool_calls) {
          for (const tc of m.tool_calls) sliceBytes += tc.function.arguments.length;
        }
      }
      for (const part of slice.live) {
        if (part.kind === "thinking" || part.kind === "text" || part.kind === "status") {
          sliceBytes += part.text.length;
        } else if (part.kind === "tool") {
          sliceBytes += (part.stream?.length ?? 0) + (part.output?.length ?? 0);
        }
      }
      stats = { slice, stats: { messageCount: slice.messages.length, bytes: sliceBytes } };
      sliceStats.set(id, stats);
    }
    messageCount += stats.stats.messageCount;
    bytes += stats.stats.bytes;
  }
  for (const id of sliceStats.keys()) if (!(id in sessions)) sliceStats.delete(id);
  return { sessionCount: sessionIds.length, messageCount, bytes };
}

/**
 * Pushes periodic renderer-memory-attribution samples to the backend
 * (POST /api/debug/frontend-stats), desktop-only. Exists because the
 * WebContent process's RSS gives no attribution to what's actually growing —
 * a `vmmap`/`footprint` pass ruled out GPU/IOAccelerator, pointing at JS
 * heap / WebCore-native retention instead (see TODO.md's 2026-08-24
 * chronic-leak investigation). Pushing (rather than requiring an inspector
 * to be open and interactive) means samples keep landing right up to the
 * point the renderer wedges, which is exactly the data the prior
 * investigation was missing.
 */
export default function FrontendMemoryReporter() {
  const chatState = useChatState();
  const chatStateRef = useRef(chatState);
  chatStateRef.current = chatState;

  useEffect(() => {
    if (!isDesktopShell()) return;
    const id = windowId();

    const tick = () => {
      const terminals = terminalRegistrySnapshot();
      const chat = estimateChatBytes(chatStateRef.current.sessions);
      authedFetch("/api/debug/frontend-stats", {
        method: "POST",
        headers: { "X-Ocode-Desktop": "1" },
        body: JSON.stringify({
          window_id: id,
          terminal_count: terminals.count,
          terminal_lines: terminals.totalLines,
          session_count: chat.sessionCount,
          message_count: chat.messageCount,
          message_bytes: chat.bytes,
          dom_node_count: document.getElementsByTagName("*").length,
        }),
      }).catch((err) => console.error("frontend memory reporter: post failed", err));
    };

    tick();
    const interval = setInterval(tick, REPORT_INTERVAL_MS);
    return () => clearInterval(interval);
  }, []);

  return null;
}
