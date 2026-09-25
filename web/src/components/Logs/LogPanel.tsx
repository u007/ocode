import { memo, useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import ConfirmDialog from "@/components/common/ConfirmDialog";
import { api } from "@/api/client";
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

function LogPanel({ active, sessionId, host }: { active: boolean; sessionId: string; host?: string }) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [streaming, setStreaming] = useState(true);
  const [filter, setFilter] = useState("ALL");
  const [autoScroll, setAutoScroll] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // Clears the session's log file on disk, so it is gated behind a rendered
  // confirm: `window.confirm` silently returns false in the Wails/WKWebView
  // desktop webview, which made the clear unreachable there.
  const [confirmClear, setConfirmClear] = useState(false);
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
  // Last observed scrollTop. A scroll event whose offset DECREASED can only
  // come from the user (our own pins always increase it), so it is checked
  // synchronously in handleScroll to lock auto-scroll immediately — before a
  // log envelope landing in the same frame can re-pin and swallow the
  // scroll-up (same fix as ChatPanel).
  const lastScrollTopRef = useRef(0);
  // Previous rendered log count, to tell a list RESET (clear / session change /
  // shorter refetch) from an append. The retention cap keeps the length flat at
  // the limit, so appends never shrink it.
  const prevLogsCountRef = useRef<number | null>(null);
  // True when the user explicitly disabled auto-scroll with the toolbar ▼
  // toggle (as opposed to the auto-lock `handleScroll` applies when they scroll
  // up). An explicit choice must survive the reset conditions (list reset /
  // no scrollbar / session change); the scroll-up lock must not. Cleared only by
  // an explicit re-enable (toolbar) or by scrolling back to the bottom.
  const manualOffRef = useRef(false);

  // Scroll the viewport to the bottom. Instant by default — smooth scrolling
  // during streaming starts a competing animation (down/up bounce, eventual
  // lockout; same lesson as ChatPanel). The explicit ↓ button uses smooth.
  const scrollToBottom = useCallback((smooth = false) => {
    const el = containerRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth ? "smooth" : "auto" });
    lastScrollTopRef.current = el.scrollTop;
    manualOffRef.current = false;
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
      // A new session follows the tail again — unless the user explicitly
      // turned auto-scroll off, which is a choice that outlives the session.
      if (!manualOffRef.current) {
        autoScrollRef.current = true;
        setAutoScroll(true);
      }
      lastScrollTopRef.current = 0;
      savedScrollTopRef.current = 0;
      prevLogsCountRef.current = null;
    }
    let cancelled = false;
    api
      .getLogs(sessionId, host)
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
  }, [sessionId, active, host]);

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
  // The lock RESETS (autoScroll re-arms) when carrying it would be meaningless:
  // a shrunken log list (clear / session change / shorter refetch), or content
  // that no longer overflows the viewport.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const prev = prevLogsCountRef.current;
    prevLogsCountRef.current = logs.length;
    let follow = autoScroll;
    // Resets only override the auto-lock — never an explicit toolbar disable.
    if (!manualOffRef.current) {
      if (prev !== null && logs.length < prev) {
        // The list was reset/replaced under the reader — the old position no
        // longer refers to this content.
        follow = true;
      }
      // Guard on clientHeight so a hidden (display:none) tab, which reports 0/0,
      // is not mistaken for "content fits" and does not silently re-arm the lock
      // while backgrounded (background log buffering can deliver entries then).
      if (el.clientHeight > 0 && el.scrollHeight - el.clientHeight <= 1) {
        follow = true;
      }
    }
    if (follow !== autoScroll) {
      autoScrollRef.current = follow;
      setAutoScroll(follow);
    }
    if (!follow) return;
    el.scrollTop = el.scrollHeight;
    lastScrollTopRef.current = el.scrollTop;
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
      lastScrollTopRef.current = view.scrollTop;
    });
  }, [active]);

  // Cancel a pending deferred scroll check on unmount.
  useEffect(() => () => cancelAnimationFrame(rafRef.current), []);

  // Runs only from the clear confirm. A rejection propagates to ConfirmDialog,
  // which shows the reason inline and keeps the dialog open instead of
  // dropping the log list as if the file had been cleared.
  const handleClear = async () => {
    await api.clearLogs(sessionId, host);
    setLogs([]);
  };

  const handleScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    // Save the position while the element is still visible — display:none
    // resets scrollTop to 0, so this cannot wait for the tab to hide.
    savedScrollTopRef.current = el.scrollTop;
    // Synchronous upward-move check: a DECREASE can only come from the user
    // (our own pins always increase scrollTop). Unpin immediately so a log
    // envelope that lands in the same frame can't re-pin and swallow the
    // scroll-up; the deferred pass below still owns the near-bottom re-pin.
    const top = el.scrollTop;
    const prevTop = lastScrollTopRef.current;
    lastScrollTopRef.current = top;
    if (top < prevTop - 1) {
      autoScrollRef.current = false;
      setAutoScroll(false);
    }
    // Defer the pinned-to-bottom check to the next frame. Content growth
    // during streaming fires scroll events where scrollHeight has grown but
    // scrollTop has not caught up yet, which makes the distance look large
    // and wrongly flips auto-scroll off (same fix as ChatPanel).
    cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(() => {
      lastScrollTopRef.current = el.scrollTop;
      const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30;
      // Scrolling back to the bottom is an explicit-enough act to drop a manual
      // disable, matching the old position-driven behavior.
      if (atBottom) manualOffRef.current = false;
      autoScrollRef.current = atBottom;
      setAutoScroll(atBottom);
    });
  }, []);

  const filteredLogs = filter === "ALL" ? logs : logs.filter((l) => l.kind === filter);

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-border bg-card">
        <div className="flex items-center gap-2">
          <Filter className="w-4 h-4 text-muted-foreground" />
          <div className="flex gap-1">
            {KIND_FILTERS.map((kind) => (
              <button
                key={kind}
                type="button"
                onClick={() => setFilter(kind)}
                className={`px-2 py-1 rounded text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background ${
                  filter === kind
                    ? "bg-accent text-accent-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
              >
                {kind}
              </button>
            ))}
          </div>
        </div>

        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{filteredLogs.length} entries</span>
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
                manualOffRef.current = true;
                autoScrollRef.current = false;
                setAutoScroll(false);
              } else {
                scrollToBottom(true);
              }
            }}
            title={autoScroll ? "Disable auto-scroll" : "Enable auto-scroll"}
            className={autoScroll ? "text-blue-400" : "text-muted-foreground"}
          >
            ↓
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => setConfirmClear(true)}
            title="Clear logs"
            aria-label="Clear logs"
          >
            <Trash2 className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {/* Log entries */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto font-mono text-xs p-4 bg-background"
      >
        {error ? (
          <div className="text-red-400 text-center py-8">{error}</div>
        ) : filteredLogs.length === 0 ? (
          <div className="text-muted-foreground text-center py-8">
            {streaming ? "Waiting for logs..." : "No log entries"}
          </div>
        ) : (
          filteredLogs.map((log, i) => (
            <div
              key={i}
              className="flex items-start gap-3 py-1 hover:bg-card rounded"
            >
              <span
                className={`flex-shrink-0 w-16 text-right ${
                  KIND_COLORS[log.kind] || "text-muted-foreground"
                }`}
              >
                {log.kind}
              </span>
              <span className="flex-1 text-foreground whitespace-pre-wrap break-all">
                {log.message}
              </span>
            </div>
          ))
        )}
      </div>

      <ConfirmDialog
        open={confirmClear}
        title="Clear logs for this session?"
        description="The session's log file is emptied on disk. This cannot be undone."
        confirmLabel="Clear logs"
        pendingLabel="Clearing…"
        onConfirm={async () => {
          await handleClear();
          setConfirmClear(false);
        }}
        onCancel={() => setConfirmClear(false)}
      />
    </div>
  );
}

/** Props are primitives (`active`, `sessionId`), so a parent re-render — e.g.
 *  another tab becoming active — never re-renders a hidden LogPanel. */
export default memo(LogPanel);
