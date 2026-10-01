import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { GitFork } from "lucide-react";
import { Progress } from "@/components/ui/progress";
import { cn } from "../../lib/utils";
import { useJumpToSession, useJumpToPendingAsk, type JumpTarget } from "../../lib/jumpToSession";
import { usePulseTail, type PulseTail } from "./usePulseTail";
import { pulseProjectBasename } from "./pulseFilter";
import type { PulseRow, PulseStatus, PulseTodoState } from "../../api/types";

/**
 * PulseCard — one session on the Pulse dashboard.
 *
 * The whole card is a single button: a dashboard is scanned, not read, so the
 * click target is the card rather than a link buried in it. Keyboard users get
 * the same target for free, and PulseView's arrow-key roving over
 * `[role="listitem"] > button` depends on the button being a direct child.
 *
 * Two click gestures, deliberately not the other way round. A single click
 * jumps immediately — no double-click timeout, because waiting to find out
 * whether a second click was coming makes the common case feel broken. A
 * double click therefore performs the same jump (idempotent) and additionally
 * opens the side pane, which is the only way to reach a pending ask without
 * hunting for it once inside the session.
 *
 * The detail overlay is absolutely positioned inside the `relative` listitem
 * and rendered as a SIBLING of the button, so showing it cannot resize the
 * card or push its neighbours — and so the overlay's own content is not part
 * of the button's click target or accessible name.
 *
 * A LIVE card (running, or paused on an ask) streams its output on the card
 * itself instead of waiting for the hover overlay: watching a turn is the
 * entire reason to open this dashboard, and making the user hover every card
 * to learn what it is doing inverts that. The stream renders in exactly ONE
 * place per card — the overlay for every other status — so the same lines are
 * never on screen twice and `pulse-tail` stays unique.
 *
 * Settled rows keep the hover-only preview deliberately: seeding one costs a
 * 200-message transcript fetch, on a dashboard that pages up to 50 rows. Live
 * rows number in single digits, so the seed fetch and the SSE `text`
 * subscription follow work that is actually happening instead of history.
 */

/** Hover must settle before an overlay appears, or a mouse crossing the grid
 *  flashes a panel over every card it passes. Focus is a deliberate act, so it
 *  expands with no delay at all. */
const EXPAND_DELAY_MS = 150;

/** Elapsed tick while running. */
const TICK_MS = 1_000;

/**
 * A turn that has been running longer than this is not a turn any more. It
 * also guards the display against `turn_started_at` being Go's zero time
 * (serialized as 0001-01-01), which would otherwise render as a ~200,000-hour
 * duration.
 */
const MAX_TURN_MS = 24 * 60 * 60 * 1_000;

/**
 * Statuses whose card carries its own always-visible streaming block.
 *
 * Needs-you rows are in here for the same reason as `running`: a paused turn is
 * live output the user is waiting on, and its `pending_ask` summary is already
 * on the card body, so the tail beside it is the context for that ask rather
 * than a historical fetch.
 */
const STREAM_ON_CARD: ReadonlySet<PulseStatus> = new Set<PulseStatus>([
  "running",
  "needs_permission",
  "needs_question",
]);

/**
 * Reserved height for the on-card stream.
 *
 * This has to be the FLOOR, not a hint: with `min-h` smaller than the content,
 * the content governs and a card grows from 224px to 240px as tail lines arrive
 * — which reflows the whole grid row on every streaming delta. Measured in
 * headless Chromium against the built CSS, a full card (status row, title,
 * task, todo bar) plus the full PULSE_TAIL_LINES-line preview is 240.5px, so
 * 16rem leaves ~15px of slack and the height is genuinely constant.
 */
const CARD_MIN_H = "min-h-[16rem]";

const STATUS_META: Record<PulseStatus, { glyph: string; label: string; className: string }> = {
  needs_permission: { glyph: "◆", label: "needs permission", className: "text-amber-400" },
  needs_question: { glyph: "◆", label: "needs question", className: "text-sky-400" },
  running: {
    glyph: "●",
    label: "running",
    // motion-reduce alongside animate-pulse: a card grid that is permanently
    // throbbing is exactly what the reduced-motion setting exists to stop.
    className: "animate-pulse motion-reduce:animate-none text-emerald-400",
  },
  error: { glyph: "✕", label: "error", className: "text-destructive" },
  idle: { glyph: "○", label: "idle", className: "text-muted-foreground" },
};

const TODO_MARK: Record<PulseTodoState, string> = {
  pending: "☐",
  in_progress: "▸",
  done: "☑",
};

/** "45s" / "3m 20s" / "1h 04m". */
function formatDuration(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1_000));
  const h = Math.floor(total / 3_600);
  const m = Math.floor((total % 3_600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}h ${String(m).padStart(2, "0")}m`;
  if (m > 0) return `${m}m ${String(s).padStart(2, "0")}s`;
  return `${s}s`;
}

function formatAgo(iso: string, now: number): string {
  const then = Date.parse(iso);
  if (!Number.isFinite(then)) return "";
  const min = Math.floor((now - then) / 60_000);
  if (min < 1) return "just now";
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}

/**
 * The streaming / last-assistant preview, in one place so the SAME block can be
 * laid out on the card for a live row and in the overlay for every other
 * status. Rendering both at once would duplicate the lines on screen and make
 * `pulse-tail` ambiguous to the tests.
 */
function PulseStream({ tail, className }: { tail: PulseTail; className?: string }) {
  return (
    <div data-testid="pulse-tail" className={cn("flex flex-col gap-0.5", className)}>
      {tail.error ? (
        <span className="text-[11px] text-destructive">{tail.error}</span>
      ) : (
        tail.lines.map((line, i) => (
          <div key={`${i}-${line}`} className="truncate font-mono text-[11px] text-muted-foreground">
            {line}
          </div>
        ))
      )}
    </div>
  );
}

export function PulseCard({ row, compact }: { row: PulseRow; compact: boolean }) {
  const jump = useJumpToSession();
  const jumpAsk = useJumpToPendingAsk();

  const [expanded, setExpanded] = useState(false);
  const expandTimer = useRef<number | null>(null);
  const [now, setNow] = useState(() => Date.now());

  // Only a running row needs a per-second clock; a settled row's recency
  // label refreshes on the next render (any bus event touches the dashboard),
  // which keeps 50 cards from holding 50 intervals.
  useEffect(() => {
    if (row.status !== "running") return;
    const timer = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(timer);
  }, [row.status]);

  const clearExpandTimer = useCallback(() => {
    if (expandTimer.current === null) return;
    window.clearTimeout(expandTimer.current);
    expandTimer.current = null;
  }, []);
  // A pending expand must not fire into a card that has been unmounted.
  useEffect(() => clearExpandTimer, [clearExpandTimer]);

  const expandAfterDelay = useCallback(() => {
    clearExpandTimer();
    expandTimer.current = window.setTimeout(() => {
      expandTimer.current = null;
      setExpanded(true);
    }, EXPAND_DELAY_MS);
  }, [clearExpandTimer]);
  const expandNow = useCallback(() => {
    clearExpandTimer();
    setExpanded(true);
  }, [clearExpandTimer]);
  const collapse = useCallback(() => {
    clearExpandTimer();
    setExpanded(false);
  }, [clearExpandTimer]);

  // A live card reads its tail whether or not it is hovered — which is also the
  // only way it is reachable at all on a touch device, where there is no hover.
  // Every other status stays gated on `expanded`, so a collapsed card
  // subscribes to nothing and fetches nothing.
  const streamOnCard = !compact && STREAM_ON_CARD.has(row.status);
  const tail = usePulseTail(row.session_id, expanded || streamOnCard, row.status);

  const target: JumpTarget = useMemo(
    () => ({
      projectPath: row.project_path,
      // Pulse is local-only in v1, so the jump always targets this server. The
      // field is still explicit so a card can carry a host when remote fan-out
      // lands (see JumpTarget).
      host: "",
      sessionId: row.session_id,
      title: row.title,
    }),
    [row.project_path, row.session_id, row.title],
  );

  const status = STATUS_META[row.status];
  const label = row.title || row.session_id;
  const basename = pulseProjectBasename(row.project_path);
  const startedAt = Date.parse(row.turn_started_at);
  const elapsedMs = now - startedAt;
  const showDuration =
    row.status === "running" && Number.isFinite(elapsedMs) && elapsedMs >= 0 && elapsedMs < MAX_TURN_MS;
  const elapsedText = showDuration ? formatDuration(elapsedMs) : formatAgo(row.updated_at, now);
  // A needs-you row's ask IS the thing to do; the task line would just repeat
  // it (and the server already nulls current_task for these rows).
  const taskText = row.pending_ask ? row.pending_ask.summary : (row.current_task?.text ?? "");
  const todoPercent = row.todo && row.todo.total > 0 ? Math.round((row.todo.done / row.todo.total) * 100) : 0;

  // What the hover overlay can actually put on screen, and whether that is
  // anything at all.
  //
  // `tool` AND `text` are both rendered. Gating on `tool` alone dropped the
  // server's last-resort task — kind "text", the last assistant line, which
  // derivePulseTask returns for exactly the idle and error rows that make up
  // most of the dashboard — so those cards expanded into an empty box while the
  // very same line was visible on the card body just above it.
  //
  // `todo` is deliberately excluded: the items list already contains the current
  // item, so rendering it again would duplicate a line the user just read.
  const overlayTask =
    row.current_task?.kind === "tool" || row.current_task?.kind === "text"
      ? row.current_task.text
      : "";
  // The tail only counts toward the overlay's content when it is NOT already on
  // the card body: a live row shows its stream there, so repeating the same
  // lines in the overlay would be redundant as well as a second `pulse-tail`.
  const tailOnOverlay = !streamOnCard && (tail.lines.length > 0 || tail.error !== null);
  const overlayHasContent =
    overlayTask !== "" ||
    tailOnOverlay ||
    !!row.pending_ask ||
    (row.todo?.items.length ?? 0) > 0;

  return (
    <div role="listitem" className="relative min-w-0">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => jump(target)}
        onDoubleClick={() => {
          if (row.pending_ask) jumpAsk(target);
        }}
        onPointerEnter={expandAfterDelay}
        onPointerLeave={collapse}
        onFocus={expandNow}
        onBlur={collapse}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            collapse();
            return;
          }
          // Enter is spelled out because a click handler alone leaves the
          // keyboard path to the browser's synthetic click, which carries no
          // pointer bookkeeping we can rely on for the overlay.
          if (e.key === "Enter") jump(target);
        }}
        // h-full fills the grid row the card is stretched into, so every card in
        // a row shares one height; the min-height is the floor that reserves the
        // live region. Both are needed: `min-h` alone still lets a taller card
        // stretch its row, and `h-full` alone gives no room to stream into.
        className={cn(
          "flex h-full w-full min-w-0 flex-col gap-1.5 rounded-md border border-border bg-card p-2 text-left transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          !compact && CARD_MIN_H,
        )}
      >
        {compact ? (
          <div data-pulse-line="" className="flex min-w-0 items-center gap-2">
            <span aria-label={status.label} className={cn("shrink-0 text-xs leading-none", status.className)}>
              {status.glyph}
            </span>
            <span title={row.project_path} className="shrink-0 truncate text-xs text-muted-foreground">
              {basename}
            </span>
            <span className="min-w-0 flex-1 truncate text-xs">{label}</span>
            <span
              data-testid="pulse-elapsed"
              className="shrink-0 text-[11px] tabular-nums text-muted-foreground"
            >
              {elapsedText}
            </span>
          </div>
        ) : (
          <>
            <div className="flex min-w-0 items-center gap-2">
              <span
                aria-label={status.label}
                className={cn("shrink-0 text-xs leading-none", status.className)}
              >
                {status.glyph}
              </span>
              <span title={row.project_path} className="shrink-0 truncate text-xs text-muted-foreground">
                {basename}
              </span>
              {row.child_count > 0 && (
                // A span, not the ui Badge: a Badge renders a div, and a
                // button's content must stay phrasing content.
                <span
                  data-testid="pulse-children"
                  className="inline-flex shrink-0 items-center gap-0.5 rounded-full border border-border px-1.5 py-0.5 text-[10px] text-muted-foreground"
                >
                  <GitFork className="h-2.5 w-2.5" aria-hidden />
                  {row.child_count}
                </span>
              )}
              <span
                data-testid="pulse-elapsed"
                className="ml-auto shrink-0 text-[11px] tabular-nums text-muted-foreground"
              >
                {elapsedText}
              </span>
            </div>
            <span className="truncate text-sm font-medium">{label}</span>
            {taskText && (
              <span data-testid="pulse-task" className="truncate text-xs text-muted-foreground">
                {taskText}
              </span>
            )}
            {row.todo && (
              <div className="flex items-center gap-2">
                <Progress
                  value={todoPercent}
                  // The ui Progress wrapper destructures `value` out and only
                  // forwards it to the Indicator, so Radix's Root never sees
                  // it and renders `data-state="indeterminate"` with NO
                  // aria-valuenow — a pre-existing gap in the shared
                  // primitive, deliberately not fixed here (it would change
                  // every other Progress consumer). Passing the value
                  // explicitly through props is what makes this bar announce
                  // "50 of 100" instead of nothing.
                  aria-valuenow={todoPercent}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-label="Todo progress"
                  className="h-1 flex-1"
                />
                <span
                  data-testid="pulse-todo-count"
                  className="shrink-0 text-[11px] tabular-nums text-muted-foreground"
                >
                  {row.todo.done}/{row.todo.total}
                </span>
              </div>
            )}
            {/* The reserved live region. flex-1 + justify-end pins the newest
                line to the bottom edge, nearest the eye, the way a terminal
                tail reads. min-h-0 lets it shrink rather than push the card
                taller if the budget is ever exceeded; truncate per line stops
                a long streamed line from wrapping. */}
            {streamOnCard && (
              <PulseStream
                tail={tail}
                className="mt-0.5 min-h-0 flex-1 justify-end overflow-hidden"
              />
            )}
          </>
        )}
      </button>

      {expanded && (
        // bottom-full: grows upward so it is never clipped by the bottom of the
        // scrolling list. Absolute, so the card keeps its exact box.
        <div
          role="group"
          aria-label={`Details for ${label}`}
          data-testid="pulse-overlay"
          className="absolute bottom-full left-0 right-0 z-20 mb-1 flex flex-col gap-1.5 rounded-md border border-border bg-popover p-2 text-left shadow-lg"
        >
          {/* Hover still owns the preview for every non-live status; a live row
              already carries its stream on the card, so the overlay shows only
              what the card body cannot — the plan and the ask. */}
          {!streamOnCard && <PulseStream tail={tail} />}
          {row.todo && row.todo.items.length > 0 && (
            <div data-testid="pulse-todo-items" className="flex flex-col gap-0.5">
              {row.todo.items.map((item, i) => (
                <span
                  key={`${i}-${item.text}`}
                  className={cn(
                    "truncate text-[11px]",
                    item.state === "done" && "text-muted-foreground line-through",
                  )}
                >
                  <span aria-hidden>{TODO_MARK[item.state]} </span>
                  {item.text}
                </span>
              ))}
            </div>
          )}
          {overlayTask && (
            <div className="truncate font-mono text-[11px] text-muted-foreground">
              {overlayTask}
            </div>
          )}
          {row.pending_ask && (
            <div className="truncate text-[11px] text-amber-400">{row.pending_ask.summary}</div>
          )}
          {/* A blank bordered panel reads as a broken card, so an overlay with
              nothing in it says which of the two real situations it is: the seed
              fetch still in flight, or genuinely no preview for this row (a
              disk-only scope=all row, or a session whose assistant only ever
              made tool calls and so has no last assistant line). */}
          {!overlayHasContent &&
            (tail.loading ? (
              <div data-testid="pulse-overlay-loading" className="text-[11px] text-muted-foreground">
                Loading…
              </div>
            ) : (
              <div data-testid="pulse-overlay-empty" className="text-[11px] text-muted-foreground">
                Nothing to preview for this session
              </div>
            ))}
        </div>
      )}
    </div>
  );
}
