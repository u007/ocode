import { useEffect } from "react";
import { authedFetch } from "@/api/client";
import { activeCompactionCount } from "@/lib/compactionState";
import { isDesktopShell } from "@/lib/desktopShell";
import { getWindowId } from "@/lib/windowId";

const TICK_MS = 500;
// A timer that fires this much later than scheduled means the main thread
// was blocked for about that long.
const STALL_THRESHOLD_MS = 2_000;

/**
 * Detects renderer main-thread stalls by timer drift and POSTs each one after
 * it ends (POST /api/debug/frontend-stall → `frontend-stall:` line in
 * desktop.log). WebKit has no Long Tasks API, so a PerformanceObserver is not
 * an option. Drift while the page was hidden is timer throttling, not a
 * stall, so those ticks are ignored.
 */
export default function FrontendStallReporter() {
  useEffect(() => {
    if (!isDesktopShell()) return;
    const id = getWindowId();
    let last = performance.now();
    let hiddenSinceLast = document.hidden;

    const onVisibility = () => {
      if (document.hidden) hiddenSinceLast = true;
    };
    document.addEventListener("visibilitychange", onVisibility);

    const interval = setInterval(() => {
      const now = performance.now();
      const drift = now - last - TICK_MS;
      const ignore = hiddenSinceLast || document.hidden;
      last = now;
      hiddenSinceLast = document.hidden;
      if (ignore || drift < STALL_THRESHOLD_MS) return;
      authedFetch("/api/debug/frontend-stall", {
        method: "POST",
        headers: { "X-Ocode-Desktop": "1" },
        body: JSON.stringify({
          window_id: id,
          stall_ms: Math.round(drift),
          dom_node_count: document.getElementsByTagName("*").length,
          active_compactions: activeCompactionCount(),
        }),
      }).catch((err) => console.error("frontend stall reporter: post failed", err));
    }, TICK_MS);

    return () => {
      clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);

  return null;
}
