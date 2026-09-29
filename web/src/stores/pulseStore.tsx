import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { api } from "../api/client";
import { eventBus } from "../lib/eventBus";
import type { PulsePage, PulseRow, PulseStatus, TodoUpdatedEvent } from "../api/types";

/**
 * pulseStore — the cross-project live-sessions dashboard's data layer.
 *
 * Seeded from `GET /api/pulse` and then kept live by SSE events forwarded from
 * lib/sessionEvents.ts through `pulseEventSink`. The store is deliberately NOT
 * driven by the per-tab chat slices: a dashboard must see sessions that have no
 * open tab, so the SSE router calls the sink *before* its `sessionIsTracked`
 * gate (see routeSessionScoped in that file).
 *
 * Two things it does not do:
 *  - It never synthesizes a row. An event for a session it has never seen
 *    triggers a debounced refetch instead: inventing a card the user cannot
 *    open (or worse, a card with no project) is worse than a card that arrives
 *    300ms late.
 *  - It never replaces good rows with nothing. A failed refetch keeps the last
 *    known list and records the error alongside it.
 */

const PAGE_SIZE = 50;
/** Coalescing window for refetches triggered by unknown-session events. */
const UNKNOWN_SESSION_REFETCH_MS = 300;

export interface PulseCounts {
  running: number;
  needsYou: number;
}

export interface PulseState {
  rows: PulseRow[];
  scope: "live" | "all";
  nextCursor: string | null;
  hasMore: boolean;
  error: string | null;
  loading: boolean;
  counts: PulseCounts;
}

const initialPulseState: PulseState = {
  rows: [],
  scope: "live",
  nextCursor: null,
  hasMore: false,
  error: null,
  loading: false,
  counts: { running: 0, needsYou: 0 },
};

/** Mirrors the server's pulseStatusRank (internal/server/pulse_rows.go). The
 *  two must stay in step or the client re-sorts a live event into a different
 *  position than the server would have. */
function statusRank(status: PulseStatus): number {
  switch (status) {
    case "needs_permission":
    case "needs_question":
      return 0;
    case "running":
      return 1;
    default:
      return 2;
  }
}

/**
 * Re-applies the server's ordering (rank → updated_at desc → session_id asc)
 * after a local event changes a row's status. Exported for direct testing
 * because it is the one rule that silently misplaces cards when it drifts.
 */
export function sortPulseRows(rows: PulseRow[]): PulseRow[] {
  return [...rows].sort((a, b) => {
    const ra = statusRank(a.status);
    const rb = statusRank(b.status);
    if (ra !== rb) return ra - rb;
    // Compare INSTANTS, not strings: the server serializes Go time.Time with a
    // local offset (e.g. "+08:00") while a live event stamps "...Z", so a
    // lexical compare misorders a just-updated row below older ones.
    const ta = Date.parse(a.updated_at);
    const tb = Date.parse(b.updated_at);
    if (!Number.isNaN(ta) && !Number.isNaN(tb) && ta !== tb) return tb - ta;
    if (a.updated_at !== b.updated_at) return a.updated_at < b.updated_at ? 1 : -1;
    return a.session_id < b.session_id ? -1 : a.session_id > b.session_id ? 1 : 0;
  });
}

function countPulse(rows: PulseRow[]): PulseCounts {
  let running = 0;
  let needsYou = 0;
  for (const r of rows) {
    if (r.status === "running") running += 1;
    else if (r.status === "needs_permission" || r.status === "needs_question") needsYou += 1;
  }
  return { running, needsYou };
}

function isNeedsYou(status: PulseStatus): boolean {
  return status === "needs_permission" || status === "needs_question";
}

/** The task line implied by a row's plan, or null. */
function todoTask(row: PulseRow): PulseRow["current_task"] {
  return row.todo?.current ? { kind: "todo", text: row.todo.current } : null;
}

/**
 * Reduce one SSE event to the full new row list, or null when this event
 * cannot be applied to the current list (unknown session, or an event type the
 * dashboard has no interest in).
 *
 * Returns the whole list rather than a patch so the caller has a single value
 * to hand to setState, and so sorting happens in exactly one place.
 */
export function applyPulseEvent(
  rows: PulseRow[],
  event: string,
  sessionId: string,
  data: unknown,
): PulseRow[] | null {
  if (event === "session_rekeyed") {
    const rekeyed = data as { session_id?: string; old_id?: string } | null;
    const oldId = rekeyed?.old_id || sessionId;
    const newId = rekeyed?.session_id || "";
    const idx = rows.findIndex((r) => r.session_id === oldId);
    if (idx < 0 || !newId) return null;
    const next = rows.slice();
    next[idx] = { ...next[idx], session_id: newId };
    return sortPulseRows(next);
  }

  const idx = rows.findIndex((r) => r.session_id === sessionId);
  if (idx < 0) return null;
  const row = rows[idx];
  const now = new Date().toISOString();
  let next: PulseRow;

  switch (event) {
    case "turn_started":
      next = {
        ...row,
        status: "running",
        pending_ask: null,
        current_task: todoTask(row) ?? row.current_task,
        turn_started_at: now,
        updated_at: now,
      };
      break;
    case "turn_done":
      next = { ...row, status: "idle", updated_at: now };
      break;
    case "turn_error":
      // The error text is not carried onto the row: the card renders the status
      // glyph, and the full message is one click away inside the session.
      next = { ...row, status: "error", updated_at: now };
      break;
    case "permission": {
      const p = data as { tool?: string; command?: string; summary?: string } | null;
      const summary = p?.command || p?.summary || (p?.tool ? `allow ${p.tool}?` : "");
      next = {
        ...row,
        status: "needs_permission",
        pending_ask: { kind: "permission", summary },
        // The ask is the headline; a task line would only repeat it.
        current_task: null,
        updated_at: now,
      };
      break;
    }
    case "question": {
      const q = data as { questions?: { question?: string }[] } | null;
      next = {
        ...row,
        status: "needs_question",
        pending_ask: { kind: "question", summary: q?.questions?.[0]?.question ?? "" },
        current_task: null,
        updated_at: now,
      };
      break;
    }
    case "permission_resolved":
    case "question_resolved":
      next = { ...row, status: "running", pending_ask: null, updated_at: now };
      break;
    case "todo_updated": {
      const t = data as TodoUpdatedEvent | null;
      if (!t) return null;
      const todo = {
        done: t.done ?? 0,
        total: t.total ?? 0,
        current: t.current ?? "",
        items: t.items ?? [],
      };
      next = {
        ...row,
        todo,
        // A needs-you row keeps the ask as its headline.
        current_task: isNeedsYou(row.status) ? null : todoTask({ ...row, todo }) ?? row.current_task,
      };
      break;
    }
    default:
      return null;
  }

  const out = rows.slice();
  out[idx] = next;
  return sortPulseRows(out);
}

export interface PulseApi extends PulseState {
  setScope: (scope: "live" | "all") => void;
  loadMore: () => void;
  retry: () => void;
}

// ── module-level sink ───────────────────────────────────────────────────────
// lib/sessionEvents.ts imports this directly. It is a module-level function
// rather than a prop because the SSE router is a module function with no
// component to receive props from, and it no-ops until a provider mounts so the
// router never has to know whether the dashboard is rendered.

let sink: ((event: string, sessionId: string, data: unknown) => void) | null = null;

/** The SSE router's entry point. Safe to call before/after a provider mounts. */
export function pulseEventSink(event: string, sessionId: string, data: unknown): void {
  sink?.(event, sessionId, data);
}

const PulseContext = createContext<PulseApi | null>(null);

export function PulseProvider({ children }: { children: ReactNode }) {
  const value = usePulseState();
  return <PulseContext.Provider value={value}>{children}</PulseContext.Provider>;
}

function usePulseState(): PulseApi {
  const [state, setState] = useState<PulseState>(initialPulseState);
  const stateRef = useRef(state);
  stateRef.current = state;

  // Generation guard: a fetch whose scope changed while it was in flight must
  // not land its rows, or switching Live→All would flash the old list.
  const generationRef = useRef(0);
  const unknownRef = useRef<number | undefined>(undefined);

  const fetchPage = useCallback(
    async (scope: "live" | "all", cursor: string | null, append: boolean) => {
      const gen = ++generationRef.current;
      setState((s) => ({ ...s, loading: true }));
      try {
        const res = (await api.getPulse(scope, cursor, PAGE_SIZE)) as PulsePage | undefined;
        if (gen !== generationRef.current) return; // superseded by a newer request
        // Validate the envelope BEFORE touching state. A mis-deployed or older
        // server (and several existing test doubles) can answer with a body
        // that is not a PulsePage at all; consuming `res.items` blindly threw
        // inside a setState updater, i.e. during render, which unmounted the
        // whole app rather than degrading this one view.
        if (!res || !Array.isArray(res.items) || (res.next_cursor != null && typeof res.next_cursor !== "string")) {
          const detail = `malformed /api/pulse response: expected {items: [], next_cursor}, got ${JSON.stringify(res)}`;
          console.error(`pulseStore: ${detail} (scope=${scope}, cursor=${cursor ?? "none"})`);
          setState((s) => ({ ...s, error: detail, loading: false }));
          return;
        }
        // The generation counter above is the real guard against a stale
        // response landing; re-checking the scope here would be wrong, because
        // a legitimate scope change has not updated state.scope yet (that only
        // happens once this very response is applied).
        setState((s) => {
          const rows = append ? s.rows.concat(res.items) : res.items;
          return {
            ...s,
            rows,
            scope,
            nextCursor: res.next_cursor,
            hasMore: res.next_cursor != null,
            error: null,
            loading: false,
            counts: countPulse(rows),
          };
        });
      } catch (err) {
        if (gen !== generationRef.current) return;
        const message = err instanceof Error ? err.message : String(err);
        // Rows are left as-is on purpose: an error banner plus a stale-but-real
        // list beats an empty dashboard.
        console.error(
          `pulseStore: GET /api/pulse failed (scope=${scope}, cursor=${cursor ?? "none"}): ${message}`,
          err,
        );
        setState((s) => ({ ...s, error: message, loading: false }));
      }
    },
    [],
  );

  // Seed on mount; reseed on every reconnect. The bus dropped events while it
  // was down and a refetch is the only truthful recovery — never a replay.
  useEffect(() => {
    void fetchPage("live", null, false);
    return eventBus.onReconnect(() => {
      void fetchPage(stateRef.current.scope, null, false);
    });
  }, [fetchPage]);

  useEffect(() => {
    sink = (event, sessionId, data) => {
      if (!sessionId) return;
      // Read the current list out here, not inside a setState updater: updaters
      // must stay pure (React invokes them twice in StrictMode), and the
      // unknown-session branch has a side effect (arming a timer).
      // A null result means EITHER "unknown session" (refetch to learn about
      // it) OR "known session, event type the dashboard does not patch". Only
      // the former may refetch: during a turn, text/thinking/tool_output/
      // turn_heartbeat for a known row would otherwise re-arm the debounce
      // continuously and poll GET /api/pulse several times a second.
      const known = stateRef.current.rows.some((r) => r.session_id === sessionId);
      const rows = applyPulseEvent(stateRef.current.rows, event, sessionId, data);
      if (rows) {
        setState((s) => ({ ...s, rows, counts: countPulse(rows) }));
        return;
      }
      if (known) return;
      if (unknownRef.current !== undefined) return; // already armed
      unknownRef.current = window.setTimeout(() => {
        unknownRef.current = undefined;
        void fetchPage(stateRef.current.scope, null, false);
      }, UNKNOWN_SESSION_REFETCH_MS);
    };
    return () => {
      sink = null;
      if (unknownRef.current !== undefined) {
        window.clearTimeout(unknownRef.current);
        unknownRef.current = undefined;
      }
    };
  }, [fetchPage]);

  const setScope = useCallback(
    (scope: "live" | "all") => {
      if (scope === stateRef.current.scope) return;
      void fetchPage(scope, null, false);
    },
    [fetchPage],
  );

  const loadMore = useCallback(() => {
    const s = stateRef.current;
    if (!s.hasMore || s.nextCursor == null) return;
    void fetchPage(s.scope, s.nextCursor, true);
  }, [fetchPage]);

  const retry = useCallback(() => {
    void fetchPage(stateRef.current.scope, null, false);
  }, [fetchPage]);

  return { ...state, setScope, loadMore, retry };
}

export function usePulse(): PulseApi {
  const ctx = useContext(PulseContext);
  if (!ctx) throw new Error("usePulse must be used within PulseProvider");
  return ctx;
}
