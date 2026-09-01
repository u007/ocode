import type { ChatState, SessionSlice } from "@/stores/chatStore";
import type { BrowserTabState } from "@/lib/browserStore";

/**
 * Frontend memory attribution helpers for the Processes tab and the debug
 * memory reporter.
 *
 * IMPORTANT: none of these are precise byte counts. The browser tab and the
 * chat session live inside the same renderer process as the whole SPA (the
 * desktop shell is one WKWebView window), so OS-level RSS cannot be attributed
 * to either surface. These functions estimate the *retained JS state* the
 * surface owns (message content, live stream parts, browse console/network
 * logs, URL history) — a cheap, honest-to-their-limits proxy. Values are
 * labeled `est.` in the UI.
 */

// Sums string-valued fields likely to hold large content (message text,
// provider reasoning-continuation blobs, tool call/output text) across a
// session slice. Not a precise byte count — a cheap proxy to see whether the
// chat grows unbounded over a session's lifetime, which JSON.stringify-ing
// everything on every tick would too, at real CPU cost for large histories.
export type ChatStats = { messageCount: number; bytes: number };

// Per-session cache: recompute a slice's bytes only when the slice object
// itself changes identity (updateSession replaces the touched slice), so the
// per-token churn during streaming costs one Map lookup per tick.
const sliceStats = new Map<string, { slice: SessionSlice; stats: ChatStats }>();

/** Bytes of one session slice (messages + in-flight live turn). */
export function estimateSessionSliceBytes(key: string, slice: SessionSlice): ChatStats {
  let stats = sliceStats.get(key);
  if (!stats || stats.slice !== slice) {
    let bytes = 0;
    for (const m of slice.messages) {
      bytes += m.content.length + (m.reasoning_content?.length ?? 0);
      if (m.tool_calls) {
        for (const tc of m.tool_calls) bytes += tc.function.arguments.length;
      }
    }
    for (const part of slice.live) {
      if (part.kind === "thinking" || part.kind === "text" || part.kind === "status") {
        bytes += part.text.length;
      } else if (part.kind === "tool") {
        bytes += (part.stream?.length ?? 0) + (part.output?.length ?? 0);
      }
    }
    stats = { slice, stats: { messageCount: slice.messages.length, bytes } };
    sliceStats.set(key, stats);
  }
  return stats.stats;
}

/** Bytes summed across every open session. */
export function estimateChatBytes(
  sessions: ChatState["sessions"],
): { sessionCount: number; messageCount: number; bytes: number } {
  let messageCount = 0;
  let bytes = 0;
  const sessionIds = Object.keys(sessions);
  for (const id of sessionIds) {
    const slice = sessions[id];
    const stats = estimateSessionSliceBytes(id, slice);
    messageCount += stats.messageCount;
    bytes += stats.bytes;
  }
  // Drop cache entries for sessions that are gone.
  for (const id of sliceStats.keys()) if (!(id in sessions)) sliceStats.delete(id);
  return { sessionCount: sessionIds.length, messageCount, bytes };
}

/**
 * Bytes of one browser surface's retained state: the canonical URL, the
 * navigation history, and the in-memory console/network telemetry lists
 * (each capped at CONSOLE_CAP/NETWORK_CAP — see browserStore). The page
 * itself lives in a proxied iframe, which is why this measures the SPA-side
 * bookkeeping rather than page DOM: the iframe is only mounted while its tab
 * is focused, so page-DOM counting would produce "—" exactly when the
 * Processes tab is on screen.
 */
export function estimateBrowseSurfaceBytes(
  surface: BrowserTabState,
): { bytes: number; consoleCount: number; networkCount: number } {
  let bytes = surface.url.length;
  for (const u of surface.history) bytes += u.length;
  for (const ev of surface.consoleEvents) bytes += ev.text.length;
  for (const ev of surface.networkEvents) bytes += ev.url.length + ev.method.length;
  return { bytes, consoleCount: surface.consoleEvents.length, networkCount: surface.networkEvents.length };
}