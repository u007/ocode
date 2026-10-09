import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { AlertCircle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePulse, usePulseFocus } from "@/stores/pulseStore";
import { PulseCard } from "./PulseCard";
import { PulseTerminals } from "./PulseTerminals";
import { PulseFocusPane } from "./PulseFocusPane";
import { usePulseAssistantHotkey } from "./PulseAssistantWindow";
import { usePulseAssistantPrefs } from "./pulseAssistantPrefs";
import type { PulseStatus } from "@/api/types";
import { pulseRowMatchesFilter } from "./pulseFilter";

/**
 * PulseView — the cross-project live-sessions dashboard.
 *
 * Three fixed sections (Needs you → Running → Recent), each hidden when empty,
 * because the ordering IS the value: a session blocked on the user must never
 * sit below three idle ones.
 *
 * FOCUS MODE (`usePulseFocus`) swaps the grid for a master-detail layout: a
 * `PulseFocusPane` for one session beside a narrow column of every OTHER row as
 * compact cards, under the same three headings. The focused id is module state
 * in pulseStore, so it survives a Cmd+J toggle without touching per-project view
 * state.
 *
 * The assistant is a floating window at app level (PulseAssistantWindow, mounted
 * by App), not part of this view, so it stays available on every view. This view
 * owns only the toolbar toggle and the `a` hotkey, which is scoped to the Pulse
 * view so a stray key elsewhere cannot open it. Its open state and layout are
 * per-viewer localStorage prefs (pulseAssistantPrefs), not store state.
 *
 * The row order it renders is the server's (pulseStore keeps it in step with
 * pulse_rows.go); the only thing computed here is the client-side filter, which
 * is a projection of the loaded rows and never reorders them.
 */

type Section = "needs" | "running" | "recent";

const SECTION_ORDER: { key: Section; title: string; matches: (s: PulseStatus) => boolean }[] = [
  {
    key: "needs",
    title: "Needs you",
    matches: (s) => s === "needs_permission" || s === "needs_question",
  },
  { key: "running", title: "Running", matches: (s) => s === "running" },
  { key: "recent", title: "Recent", matches: (s) => s === "idle" || s === "error" },
];

export function PulseView() {
  const { rows, scope, setScope, loadMore, hasMore, error, retry } = usePulse();
  const [filter, setFilter] = useState("");
  const assistant = usePulseAssistantPrefs();
  usePulseAssistantHotkey(() => assistant.setOpen(!assistant.open));

  const filtered = useMemo(() => rows.filter((r) => pulseRowMatchesFilter(r, filter)), [rows, filter]);

  // Roving-tabindex arrow navigation over the rendered cards, in reading order.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    const cards = Array.from(
      // Only the header buttons: each card also has a "Focus session" icon
      // button, which is not a roving stop.
      e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="listitem"] button[aria-expanded]'),
    );
    if (cards.length === 0) return;
    const at = cards.indexOf(document.activeElement as HTMLButtonElement);
    if (at < 0) return;
    e.preventDefault();
    const next = e.key === "ArrowDown" ? at + 1 : at - 1;
    cards[Math.max(0, Math.min(cards.length - 1, next))]?.focus();
  };

  const anyRows = rows.length > 0;

  const [focusedId, setFocusedId] = usePulseFocus();
  const focusedRow = focusedId === null ? undefined : rows.find((r) => r.session_id === focusedId);
  // Reading order, the order the keyboard walks in focus mode.
  const ordered = useMemo(
    () => SECTION_ORDER.flatMap(({ matches }) => filtered.filter((r) => matches(r.status))),
    [filtered],
  );

  // Entering focus unmounts the control that was clicked, and stepping through
  // rows unmounts the side-column card that had keyboard focus. Either strands
  // focus on <body>, where no key handler below would ever see the next key.
  const paneWrapRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (focusedId === null) return;
    const active = document.activeElement;
    if (active === null || active === document.body) paneWrapRef.current?.focus();
  }, [focusedId]);

  // Escape closes focus; ArrowDown/ArrowUp (and j/k) step to the next/previous
  // row. Text fields keep their own keys (a reply being typed must not vanish
  // on Escape, nor arrows move the caret AND the focus), and a confined ask
  // dialog owns its Escape and its option navigation. `defaultPrevented` covers
  // Radix, which handles its Escape in the capture phase before React sees it.
  const onFocusKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.defaultPrevented) return;
    const target = e.target as HTMLElement;
    if (target.closest('input, textarea, select, [contenteditable="true"], [role="dialog"]')) return;
    if (e.key === "Escape") {
      setFocusedId(null);
      return;
    }
    const step = e.key === "ArrowDown" || e.key === "j" ? 1 : e.key === "ArrowUp" || e.key === "k" ? -1 : 0;
    if (step === 0 || ordered.length === 0) return;
    e.preventDefault();
    const at = ordered.findIndex((r) => r.session_id === focusedId);
    const next = Math.max(0, Math.min(ordered.length - 1, at < 0 ? (step > 0 ? 0 : ordered.length - 1) : at + step));
    setFocusedId(ordered[next].session_id);
  };

  const sections = (compactAll: boolean, excludeId: string | null) =>
    SECTION_ORDER.map(({ key, title, matches }) => {
      const inSection = filtered.filter((r) => matches(r.status) && r.session_id !== excludeId);
      if (inSection.length === 0) return null;
      return (
        <section key={key} aria-label={title}>
          <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground mb-2">
            {title} ({inSection.length})
          </h2>
          <div
            role="list"
            // The grid is capped at 2 across (no lg/xl override): a live card
            // carries a scrolling activity feed of prose and tool lines, and
            // wider cards give those entries a readable width before they wrap.
            // The focus-mode column is a single narrow stack instead.
            className={compactAll ? "flex flex-col gap-2" : "grid grid-cols-1 sm:grid-cols-2 gap-3"}
          >
            {inSection.map((r) => (
              <PulseCard key={r.session_id} row={r} compact={compactAll} />
            ))}
          </div>
        </section>
      );
    });

  return (
    <div className="flex flex-col flex-1 min-h-0 overflow-hidden" data-testid="pulse-view">
      {/* Toolbar */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-border">
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter by project or title…"
          aria-label="Filter sessions by project or title"
          className="flex-1 min-w-0 h-8 rounded-md border border-border bg-background px-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        <div
          role="group"
          aria-label="Scope"
          className="flex items-center gap-1 shrink-0"
        >
          {(["live", "all"] as const).map((s) => (
            <Button
              key={s}
              size="sm"
              variant={scope === s ? "secondary" : "ghost"}
              aria-pressed={scope === s}
              onClick={() => setScope(s)}
            >
              {s === "live" ? "Live" : "All"}
            </Button>
          ))}
        </div>
      </div>

      {/* Error banner. Deliberately NOT destructive: the rows below stay, so a
          failed refresh never blanks a dashboard the user was reading. */}
      {error && (
        <div
          role="alert"
          className="flex items-center gap-2 px-4 py-2 text-sm text-destructive border-b border-border"
        >
          <AlertCircle className="w-4 h-4 shrink-0" />
          <span className="flex-1 min-w-0 truncate">Couldn’t load sessions: {error}</span>
          <Button size="sm" variant="outline" onClick={() => retry()}>
            <RefreshCw className="w-3.5 h-3.5" />
            Retry
          </Button>
        </div>
      )}

      <div className="flex flex-1 min-h-0 min-w-0">
        {/* Horizontal scroll is the last resort when the focus pane and side
            column together outgrow the view: they keep their minimum widths
            instead of crushing. */}
        <div className="flex flex-1 min-h-0 min-w-0 flex-col overflow-x-auto">
        {focusedId !== null ? (
          <div className="flex flex-1 min-h-0" data-testid="pulse-focus-layout" onKeyDown={onFocusKeyDown}>
            <div
              ref={paneWrapRef}
              tabIndex={-1}
              className="flex-1 min-w-[18rem] min-h-0 p-4 outline-none"
            >
              {focusedRow ? (
                <PulseFocusPane key={focusedRow.session_id} row={focusedRow} onClose={() => setFocusedId(null)} />
              ) : (
                <div
                  data-testid="pulse-focus-missing"
                  className="flex items-center gap-3 rounded-md border border-border bg-card p-3 text-sm text-muted-foreground"
                >
                  <span className="flex-1">Session no longer listed</span>
                  <Button size="sm" variant="outline" onClick={() => setFocusedId(null)}>
                    Close
                  </Button>
                </div>
              )}
            </div>
            <div
              data-testid="pulse-focus-side"
              className="w-80 shrink-0 overflow-y-auto border-l border-border p-3"
            >
              <div className="flex flex-col gap-4">{sections(true, focusedId)}</div>
            </div>
          </div>
        ) : (
          <div className="flex-1 min-h-0 overflow-auto p-4" onKeyDown={onKeyDown}>
            {!anyRows && !error && scope === "live" && (
              <div className="text-center py-16 text-muted-foreground">
                <p className="text-sm">No live sessions</p>
                <Button variant="outline" size="sm" className="mt-3" onClick={() => setScope("all")}>
                  See recent sessions
                </Button>
              </div>
            )}

            {anyRows && filtered.length === 0 && (
              <p className="text-center py-16 text-sm text-muted-foreground">
                No sessions match “{filter.trim()}”
              </p>
            )}

            <div className="flex flex-col gap-6">
              <PulseTerminals />
              {sections(false, null)}
            </div>

            {hasMore && (
              <div className="mt-6 flex justify-center">
                <Button variant="outline" size="sm" onClick={() => loadMore()}>
                  Load more
                </Button>
              </div>
            )}
          </div>
        )}
        </div>
      </div>
    </div>
  );
}
