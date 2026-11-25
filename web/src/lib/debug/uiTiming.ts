// Attributes renderer stalls to the user action that caused them: WebKit has no Long Tasks API, so the stall reporter alone cannot say what the user just did.
import { authedFetch } from "@/api/client";
import { activeCompactionCount } from "@/lib/compactionState";
import { isDesktopShell } from "@/lib/desktopShell";
import { getWindowId } from "@/lib/windowId";

const MIN_REPORT_MS = 150;

interface PendingTiming {
  action: string;
  start: number;
  phases: Record<string, number>;
}

let pending: PendingTiming | null = null;
let lastAction: { action: string; at: number } | null = null;

/** Begins timing a user action. A newer action replaces any unfinished one. */
export function startUiTiming(action: string): void {
  const start = performance.now();
  pending = { action, start, phases: {} };
  lastAction = { action, at: start };
}

/** Records the elapsed ms since the pending action started under `phase`. */
export function markUiTiming(phase: string): void {
  if (!pending) return;
  pending.phases[phase] = Math.round(performance.now() - pending.start);
}

/**
 * Ends the pending action once the browser has painted the frame that follows
 * the current commit. If it took at least MIN_REPORT_MS, the timing is POSTed
 * (desktop shell only). A newer action that replaced this one drops the report.
 */
export function finishUiTimingAfterPaint(): void {
  const p = pending;
  if (!p) return;
  requestAnimationFrame(() =>
    setTimeout(() => {
      if (pending !== p) return;
      pending = null;
      const total = Math.round(performance.now() - p.start);
      if (total < MIN_REPORT_MS || !isDesktopShell()) return;
      const detail = Object.entries(p.phases)
        .map(([phase, ms]) => `${phase}=${ms}`)
        .join(" ");
      authedFetch("/api/debug/frontend-stall", {
        method: "POST",
        headers: { "X-Ocode-Desktop": "1" },
        body: JSON.stringify({
          window_id: getWindowId(),
          stall_ms: total,
          dom_node_count: document.getElementsByTagName("*").length,
          active_compactions: activeCompactionCount(),
          action: p.action,
          detail,
        }),
      }).catch((err) => console.error("ui timing: post failed", err));
    }, 0),
  );
}

/** Most recent user action and how long ago it started, for stall reports. */
export function describeLastUiAction(): string {
  if (!lastAction) return "";
  return `${lastAction.action} ${Math.round(performance.now() - lastAction.at)}ms ago`;
}
