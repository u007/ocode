import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { apiPath, authHeaders } from "@/api/client";
import { eventBus } from "@/lib/eventBus";
import { useLogPrefs } from "@/hooks/useLogPrefs";
import { Trash2, Pause, Play, Filter } from "lucide-react";

// Default cap on retained entries (user-tunable in Settings → Logs via
// logViewPrefs). Every open session tab mounts a LogPanel for its whole
// lifetime; without a cap a chatty turn grows the array without bound and
// the webview's memory climbs for as long as the app runs.
export const MAX_LOGS = 1000;

export interface LogEntry {
  kind: string;
  message: string;
  session_id?: string;
}

/** Append an entry, keeping at most `max` entries (oldest dropped first).
 *  Exported for tests — this cap is what bounds the panel's memory. */
export function appendLogCapped(prev: LogEntry[], entry: LogEntry, max: number): LogEntry[] {
  if (prev.length >= max) {
    return [...prev.slice(prev.length - max + 1), entry];
  }
  return [...prev, entry];
}

const KIND_COLORS: Record<string, string> = {
  LLM: "text-purple-400",
  TOOL: "text-green-400",
  AGENT: "text-blue-400",
  ERROR: "text-red-400",
  SESSION: "text-yellow-400",
  GIT: "text-cyan-400",
  PROFILE: "text-orange-400",
};

const KIND_FILTERS = ["ALL", "LLM", "TOOL", "AGENT", "ERROR", "SESSION", "GIT", "PROFILE"];

export default function LogPanel({ active, sessionId }: { active: boolean; sessionId: string }) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [streaming, setStreaming] = useState(true);
  const [filter, setFilter] = useState("ALL");
  const [autoScroll, setAutoScroll] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  // Settings → Logs: whether hidden tabs keep buffering, and the retention
  // cap. Read through refs so pref changes don't re-subscribe the bus handler.
  const prefs = useLogPrefs();
  const prefsRef = useRef(prefs);
  useEffect(() => {
    prefsRef.current = prefs;
  }, [prefs]);

  // Mirror of `autoScroll` for handlers that must read the current value
  // without re-running their effects (scroll handler, tab-visibility effect).
  const autoScrollRef = useRef(true);
  useEffect(() => {
    autoScrollRef.current = autoScroll;
  }, [autoScroll]);

  // Mirror of `active` for the bus handler below (a stable closure that must
  // not re-subscribe on every visibility flip).
  const activeRef = useRef(active);
  useEffect(() => {
    activeRef.current = active;
  }, [active]);

  // Tracks the previous session id so the load effect only clears the list
  // when the session actually changed (not on every visibility flip).
  const prevSessionRef = useRef(sessionId);

  // Tab-visibility bookkeeping. The panel is force-mounted and hidden with
  // `display: none` while the tab is inactive, so "mounted" !== "open". The
  // scroll position is saved on every scroll (display:none resets scrollTop
  // to 0, so it can only be read while the panel is visible), and when the
  // tab opens:
  //   - first open            -> jump to the bottom (show the latest logs)
  //   - re-open while pinned  -> catch up to the latest logs
  //   - re-open while reading -> restore the saved position
  const hasOpenedRef = useRef(false);
  const savedScrollTopRef = useRef(0);
  const rafRef = useRef(0);

  // Scroll the viewport to the bottom. Instant by default — smooth scrolling
  // during streaming starts a competing animation (down/up bounce, eventual
  // lockout; same lesson as ChatPanel). The explicit ↓ button uses smooth.
  const scrollToBottom = useCallback((smooth = false) => {
    const el = containerRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth ? "smooth" : "auto" });
    autoScrollRef.current = true;
    setAutoScroll(true);
  }, []);

  useEffect(() => {
    // Load logs scoped to this session (plus process-global entries — see
    // HandleGetLogs). Runs on session change AND whenever the tab becomes
    // visible: while hidden, live entries are dropped instead of accumulated
    // (see the bus subscription below), so activation refetches the backlog
    // to catch up. The server keeps a bounded ring buffer (debuglog cap=500),
    // so this returns recent history; older entries remain on the disk mirror.
    if (prevSessionRef.current !== sessionId) {
      prevSessionRef.current = sessionId;
      setLogs([]); // fresh session — don't flash the previous tab's logs
    }
    let cancelled = false;
    fetch(apiPath(`/api/logs?session_id=${encodeURIComponent(sessionId)}`), { headers: authHeaders() })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then((data) => {
        if (cancelled) return;
        setLogs(data as LogEntry[]);
        setError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        console.error("Failed to fetch logs:", err);
        setError("Failed to load logs");
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, active]);

  // Live logs arrive as `logs` envelopes on the shared event bus (the single
  // /api/events connection). The `streaming` toggle pauses consumption (the
  // bus itself stays connected — other consumers share it). Only entries for
  // this session (or untagged process-global ones) are kept — see
  // logBusForwardLoop.
  //
  // While the tab is hidden the panel drops entries instead of accumulating
  // them: every open session tab keeps one of these mounted for its whole
  // lifetime, so unconditional accumulation grew memory (and hidden-list
  // re-render churn) without bound. Activation refetches the backlog above.
  // Settings → Logs → "Buffer logs in background tabs" restores the old
  // keep-everything behavior for users who want full history in-panel.
  useEffect(() => {
    if (!streaming) return;
    const off = eventBus.on("logs", (env) => {
      const entry = env.data as LogEntry;
      if (!entry || typeof entry.message !== "string") return;
      if (entry.session_id && entry.session_id !== sessionId) return;
      if (!activeRef.current && !prefsRef.current.backgroundBuffering) return;
      setLogs((prev) => appendLogCapped(prev, entry, prefsRef.current.maxEntries));
    });
    return off;
  }, [streaming, sessionId]);

  // Follow the tail on new logs, but only while the user is pinned to the
  // bottom (autoScroll enabled). Instant, not smooth — see scrollToBottom.
  useEffect(() => {
    if (!autoScroll) return;
    const el = containerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [logs, autoScroll]);

  // React to the log tab becoming visible. The first open jumps to the bottom
  // to show the latest entries; later opens restore the saved position unless
  // the user was following the tail (then catch up to the latest).
  useEffect(() => {
    if (!active) return;
    const el = containerRef.current;
    if (!el) return;
    const firstOpen = !hasOpenedRef.current;
    hasOpenedRef.current = true;
    const jumpToBottom = firstOpen || autoScrollRef.current;
    if (jumpToBottom) {
      autoScrollRef.current = true;
      setAutoScroll(true);
    }
    requestAnimationFrame(() => {
      const view = containerRef.current;
      if (!view) return;
      view.scrollTop = jumpToBottom ? view.scrollHeight : savedScrollTopRef.current;
    });
  }, [active]);

  // Cancel a pending deferred scroll check on unmount.
  useEffect(() => () => cancelAnimationFrame(rafRef.current), []);

  const handleClear = async () => {
    if (!window.confirm("Clear logs for this session?")) return;
    try {
      await fetch(apiPath(`/api/logs?session_id=${encodeURIComponent(sessionId)}`), {
        method: "DELETE",
        headers: authHeaders(),
      });
      setLogs([]);
    } catch (err) {
      console.error("Failed to clear logs:", err);
    }
  };

  const handleScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    // Save the position while the element is still visible — display:none
    // resets scrollTop to 0, so this cannot wait for the tab to hide.
    savedScrollTopRef.current = el.scrollTop;
    // Defer the pinned-to-bottom check to the next frame. Content growth
    // during streaming fires scroll events where scrollHeight has grown but
    // scrollTop has not caught up yet, which makes the distance look large
    // and wrongly flips auto-scroll off (same fix as ChatPanel).
    cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(() => {
      const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30;
      autoScrollRef.current = atBottom;
      setAutoScroll(atBottom);
    });
  }, []);

  const filteredLogs = filter === "ALL" ? logs : logs.filter((l) => l.kind === filter);

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-700 bg-zinc-900">
        <div className="flex items-center gap-2">
          <Filter className="w-4 h-4 text-zinc-500" />
          <div className="flex gap-1">
            {KIND_FILTERS.map((kind) => (
              <button
                key={kind}
                type="button"
                onClick={() => setFilter(kind)}
                className={`px-2 py-1 rounded text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-zinc-900 ${
                  filter === kind
                    ? "bg-zinc-700 text-white"
                    : "text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800"
                }`}
              >
                {kind}
              </button>
            ))}
          </div>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-xs text-zinc-500">{filteredLogs.length} entries</span>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => setStreaming(!streaming)}
            title={streaming ? "Pause streaming" : "Resume streaming"}
          >
            {streaming ? (
              <Pause className="w-4 h-4" />
            ) : (
              <Play className="w-4 h-4" />
            )}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              if (autoScroll) {
                autoScrollRef.current = false;
                setAutoScroll(false);
              } else {
                scrollToBottom(true);
              }
            }}
            title={autoScroll ? "Disable auto-scroll" : "Enable auto-scroll"}
            className={autoScroll ? "text-blue-400" : "text-zinc-500"}
          >
            ↓
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={handleClear}
            title="Clear logs"
          >
            <Trash2 className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {/* Log entries */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto font-mono text-xs p-4 bg-zinc-950"
      >
        {error ? (
          <div className="text-red-400 text-center py-8">{error}</div>
        ) : filteredLogs.length === 0 ? (
          <div className="text-zinc-500 text-center py-8">
            {streaming ? "Waiting for logs..." : "No log entries"}
          </div>
        ) : (
          filteredLogs.map((log, i) => (
            <div
              key={i}
              className="flex items-start gap-3 py-1 hover:bg-zinc-900 rounded"
            >
              <span
                className={`flex-shrink-0 w-16 text-right ${
                  KIND_COLORS[log.kind] || "text-zinc-400"
                }`}
              >
                {log.kind}
              </span>
              <span className="flex-1 text-zinc-300 whitespace-pre-wrap break-all">
                {log.message}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
