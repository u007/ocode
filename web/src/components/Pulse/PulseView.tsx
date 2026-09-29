import { useMemo, useState, type KeyboardEvent } from "react";
import { AlertCircle, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { usePulse } from "@/stores/pulseStore";
import { PulseCard } from "./PulseCard";
import type { PulseStatus } from "@/api/types";
import { pulseRowMatchesFilter } from "./pulseFilter";

/**
 * PulseView — the cross-project live-sessions dashboard.
 *
 * Three fixed sections (Needs you → Running → Recent), each hidden when empty,
 * because the ordering IS the value: a session blocked on the user must never
 * sit below three idle ones.
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

  const filtered = useMemo(() => rows.filter((r) => pulseRowMatchesFilter(r, filter)), [rows, filter]);

  // Roving-tabindex arrow navigation over the rendered cards, in reading order.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    const cards = Array.from(
      e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="listitem"] > button'),
    );
    if (cards.length === 0) return;
    const at = cards.indexOf(document.activeElement as HTMLButtonElement);
    if (at < 0) return;
    e.preventDefault();
    const next = e.key === "ArrowDown" ? at + 1 : at - 1;
    cards[Math.max(0, Math.min(cards.length - 1, next))]?.focus();
  };

  const anyRows = rows.length > 0;

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
          {SECTION_ORDER.map(({ key, title, matches }) => {
            const inSection = filtered.filter((r) => matches(r.status));
            if (inSection.length === 0) return null;
            return (
              <section key={key} aria-label={title}>
                <h2 className="text-xs font-medium uppercase tracking-wide text-muted-foreground mb-2">
                  {title} ({inSection.length})
                </h2>
                <div
                  role="list"
                  className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-2"
                >
                  {inSection.map((r) => (
                    <PulseCard key={r.session_id} row={r} compact={key === "recent"} />
                  ))}
                </div>
              </section>
            );
          })}
        </div>

        {hasMore && (
          <div className="mt-6 flex justify-center">
            <Button variant="outline" size="sm" onClick={() => loadMore()}>
              Load more
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
