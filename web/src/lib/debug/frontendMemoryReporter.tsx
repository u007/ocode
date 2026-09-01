import { useEffect } from "react";
import { authedFetch } from "@/api/client";
import { isDesktopShell } from "@/lib/desktopShell";
import { terminalRegistrySnapshot } from "@/lib/debug/terminalRegistry";
import { estimateChatBytes } from "@/lib/memoryEstimate";
import { useChatStateRef } from "@/stores/chatStore";

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
  // Purely imperative (read every 30s inside the interval tick below), and
  // this component renders nothing — must not itself re-render (and thus
  // contribute to) the per-token churn it exists to measure.
  const chatStateRef = useChatStateRef();

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
