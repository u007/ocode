import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { api } from "../api/client";
import { eventBus } from "../lib/eventBus";
import type { PulsePage, PulseRow, PulseStatus, TodoUpdatedEvent } from "../api/types";
import { pulseRowKey, resolvePulseProjectPath, stampPulseHost } from "../lib/pulseRemoteRows";
import type { Project } from "../api/types";

const EMPTY_PROJECTS: readonly Project[] = [];

/**
 * pulseStore — the cross-project live-sessions dashboard's data layer.
 *
 * Seeded from `GET /api/pulse` and then kept live by SSE events forwarded from
 * lib/sessionEvents.ts through `pulseEventSink`. The store is deliberately NOT
 * driven by the per-tab chat slices: a dashboard must see sessions that have no
 * open tab, so the SSE router calls the sink *before* its `sessionIsTracked`
 * gate (see routeSessionScoped in that file).
 *
 * Rows are fanned out to MORE than the local server: the same request goes to
 * every remote host the SSE stream relays, so a remote project's sessions get
 * cards too. Each source is one page with its own cursor, which is why paging
 * state is `hostCursors` rather than one `nextCursor`.
 *
 * Two things it does not do:
 *  - It never synthesizes a row. An event for a session it has never seen
 *    triggers a debounced refetch instead: inventing a card the user cannot
 *    open (or worse, a card with no project) is worse than a card that arrives
 *    300ms late.
 *  - It never replaces good rows with nothing. A failed refetch keeps the last
 *    known list and records the error alongside it; for a remote host only THAT
 *    host's rows are held over, so one dead host dims instead of blanking.
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
  /** The LOCAL source's page cursor. Per-host cursors (remote sources included)
   *  live in `hostCursors`; `loadMore` reads that, never this. */
  nextCursor: string | null;
  /** One cursor per source, keyed by host ("" = local). A source whose page
   *  failed to load has a null cursor: there is no next page we can fetch. */
  hostCursors: Record<string, string | null>;
  /** True when ANY source still has another page. */
  hasMore: boolean;
  error: string | null;
  /** Remote hosts whose last fetch failed. Non-fatal: their previous rows stay
   *  on the board, so one dead host dims instead of blanking the dashboard. */
  hostErrors: string[];
  loading: boolean;
  counts: PulseCounts;
}

const initialPulseState: PulseState = {
  rows: [],
  scope: "live",
  nextCursor: null,
  hostCursors: {},
  hasMore: false,
  error: null,
  hostErrors: [],
  loading: false,
  counts: { running: 0, needsYou: 0 },
};

/**
 * This server's saved projects (`GET /api/projects`), cached for the session.
 *
 * It exists ONLY to reconcile a remote host's project path with the path this
 * server registered it under (see `resolvePulseProjectPath`). Without it every
 * remote card renders but its click is refused as "unknown project".
 */
let savedProjects: Project[] | null = null;

async function loadSavedProjects(): Promise<Project[]> {
  if (savedProjects) return savedProjects;
  try {
    const list = await api.listProjects();
    savedProjects = Array.isArray(list) ? list : [];
    return savedProjects;
  } catch (err) {
    // Non-fatal by design: the row keeps the host's own path, so its jump is
    // refused loudly rather than silently opening the wrong place. Only a
    // SUCCESSFUL response is cached: caching the failure would leave every
    // remote card un-jumpable for the whole session over one timed-out request.
    console.error("pulseStore: GET /api/projects failed — remote project paths stay unresolved:", err);
    return [];
  }
}

/** Test seam: drop the cached project list so the next fetch re-reads it, and
 *  forget which project paths were reported-unresolved. */
export function __resetPulseSavedProjectsForTests(): void {
  savedProjects = null;
  unresolvedProjectPaths.clear();
}
/** One (session_id, host) pair wins, so a cursor that replays a row cannot
 *  produce two cards for one session. */
function dedupePulseRows(rows: PulseRow[]): PulseRow[] {
  const seen = new Set<string>();
  const out: PulseRow[] = [];
  for (const row of rows) {
    const key = pulseRowKey(row.session_id, row.host);
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(row);
  }
  return out;
}

/**
 * Paths that could not be reconciled to a saved project, as `host\0path`. The
 * reconcile is pure and silent by design (an unresolvable row must still render,
 * with its jump refused rather than pointed at the wrong machine), so this is
 * the only place that would otherwise say nothing: a remote host reporting a
 * path we never registered is a configuration drift worth seeing once, not
 * 50 times per refresh.
 */
const unresolvedProjectPaths = new Set<string>();

function noteUnresolvedProjectPath(host: string, path: string): void {
  const key = `${host}\u0000${path}`;
  if (unresolvedProjectPaths.has(key)) return;
  unresolvedProjectPaths.add(key);
  console.error(
    `pulseStore: remote host ${host} reported project ${JSON.stringify(path)}, which is not a project saved on this server — its cards cannot be jumped to until the project is added`,
  );
}

/** Re-label a fetched page with its source, and resolve remote project paths
 *  to the form this server registered so a card's jump finds the project. */
function normalizePulsePage(rows: PulseRow[], host: string, projects: readonly Project[]): PulseRow[] {
  const stamped = stampPulseHost(rows, host);
  if (!host) return stamped;
  return stamped.map((row) => {
    const resolved = resolvePulseProjectPath(row.project_path, host, projects);
    if (!resolved.resolved) noteUnresolvedProjectPath(host, row.project_path);
    return { ...row, project_path: resolved.path };
  });
}

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
 *
 * The session-id tiebreak is now preceded by a host tiebreak: rows arrive from
 * several sources, so two of them can in principle carry the same id and the
 * order must still be total (a non-total comparator makes Array.sort's output
 * platform-dependent).
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
    if (a.host !== b.host) return a.host < b.host ? -1 : 1;
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
 * `host` is part of the row's identity: rows now come from several sources
 * (the local server plus one per relayed remote host), so a lookup by session
 * id alone could patch one host's card with another host's event.
 *
 * Returns the whole list rather than a patch so the caller has a single value
 * to hand to setState, and so sorting happens in exactly one place.
 */
export function applyPulseEvent(
  rows: PulseRow[],
  event: string,
  sessionId: string,
  data: unknown,
  host = "",
): PulseRow[] | null {
  if (event === "session_rekeyed") {
    const rekeyed = data as { session_id?: string; old_id?: string } | null;
    const oldId = rekeyed?.old_id || sessionId;
    const newId = rekeyed?.session_id || "";
    const idx = rows.findIndex((r) => r.session_id === oldId && r.host === host);
    if (idx < 0 || !newId) return null;
    const next = rows.slice();
    next[idx] = { ...next[idx], session_id: newId };
    return sortPulseRows(next);
  }

  const idx = rows.findIndex((r) => r.session_id === sessionId && r.host === host);
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

let sink: ((event: string, sessionId: string, data: unknown, host: string) => void) | null = null;

/** The SSE router's entry point. Safe to call before/after a provider mounts.
 *
 *  `host` is the originating host ("" for local). The dashboard's list comes
 *  from the LOCAL /api/pulse, so a remote host's session can never be in it —
 *  see the refetch guard in the sink. */
export function pulseEventSink(event: string, sessionId: string, data: unknown, host = ""): void {
  sink?.(event, sessionId, data, host);
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
  // The scope the user has ASKED for, as opposed to state.scope, which only
  // advances when a response lands. setScope's no-op guard has to compare
  // against this: two clicks inside one tick would both see the stale applied
  // scope, and the second (a real change of mind) was silently dropped.
  const requestedScopeRef = useRef<"live" | "all">(initialPulseState.scope);

  /** The hosts to ask, in a stable order: the local server first, then each
   *  remote host the SSE stream already relays. Deliberately NOT every saved
   *  project host — dialing an unsaved or disconnected one is the cold-connect
   *  hazard, and the relayed set is exactly the hosts this window already has a
   *  live connection to. */
  const pulseSources = useCallback((): string[] => ["", ...eventBus.currentHosts()], []);

  /**
   * Fetch one page from the sources and merge. `cursors` is null for a seed
   * (start every source at page one) or the previous `hostCursors` for paging.
   *
   * The fan-out is `allSettled`, never `all`: one unreachable host must not
   * reject the whole merge and empty the board. A source that fails keeps its
   * previous rows and loses its cursor, which is what `hostErrors` reports.
   *
   * Membership is decided ONCE, from the current fan-out set, and the same
   * keep/drop rule applies to a seed and to paging:
   *  - a host no longer fanned out to loses its rows (nothing can refresh them);
   *  - a host asked this round and answered has its rows REPLACED on a seed and
   *    extended on a page;
   *  - a host asked this round that failed keeps its last rows;
   *  - on a page, a host NOT asked (its cursor was exhausted) keeps its rows.
   * Paging never re-asks an exhausted source: that re-downloaded page one and
   * handed back a cursor, so `hasMore` cycled between sources forever.
   */
  const fetchSources = useCallback(
    async (scope: "live" | "all", cursors: Record<string, string | null> | null, append: boolean) => {
      const gen = ++generationRef.current;
      // Record the request synchronously, before the await, so a second click
      // in the same tick can tell it apart from a genuine no-op.
      requestedScopeRef.current = scope;
      const fanout = pulseSources();
      const hosts = append && cursors ? fanout.filter((h) => cursors[h] != null) : fanout;
      setState((s) => ({ ...s, loading: true }));
      // Only fetched when a remote source exists: a purely local dashboard
      // must not pay for a project list it does not use.
      const projects = fanout.length > 1 ? await loadSavedProjects() : EMPTY_PROJECTS;

      const settled = await Promise.allSettled(
        hosts.map(async (host) => {
          const cursor = append && cursors ? cursors[host] : null;
          const res = (await api.getPulse(scope, cursor, PAGE_SIZE, host || undefined)) as
            | PulsePage
            | undefined;
          // Validate the envelope BEFORE touching state. A mis-deployed or older
          // server (and several existing test doubles) can answer with a body
          // that is not a PulsePage at all; consuming `res.items` blindly threw
          // inside a setState updater, i.e. during render, which unmounted the
          // whole app rather than degrading this one view.
          if (!res || !Array.isArray(res.items) || (res.next_cursor != null && typeof res.next_cursor !== "string")) {
            const detail = `malformed /api/pulse response: expected {items: [], next_cursor}, got ${JSON.stringify(res)}`;
            console.error(`pulseStore: ${detail} (host=${host || "local"}, scope=${scope}, cursor=${cursor ?? "none"})`);
            return { host, ok: false as const, error: detail, items: [] as PulseRow[], cursor: null as string | null };
          }
          return { host, ok: true as const, error: null, items: normalizePulsePage(res.items, host, projects), cursor: res.next_cursor };
        }),
      );
      if (gen !== generationRef.current) return; // superseded by a newer request

      let localError: string | null = null;
      const failedHosts: string[] = [];
      const succeeded = new Set<string>();
      // Every fanned-out source has a cursor entry: a source not asked this
      // round was skipped because its cursor was already null.
      const nextCursors: Record<string, string | null> = Object.fromEntries(fanout.map((h) => [h, null]));
      const fresh: PulseRow[] = [];
      settled.forEach((r, i) => {
        const host = hosts[i];
        if (r.status === "rejected") {
          const message = r.reason instanceof Error ? r.reason.message : String(r.reason);
          console.error(`pulseStore: GET /api/pulse failed (host=${host || "local"}, scope=${scope}): ${message}`, r.reason);
          if (host === "") localError = message;
          else failedHosts.push(host);
          return;
        }
        const v = r.value;
        nextCursors[v.host] = v.cursor;
        if (v.ok) {
          succeeded.add(v.host);
          fresh.push(...v.items);
        } else if (v.host === "") localError = v.error;
        else failedHosts.push(v.host);
      });

      setState((s) => {
        // A source that failed keeps whatever it showed last: an outage must dim
        // the board, never blank it (an error banner plus a stale-but-real list
        // beats an empty dashboard). The rule is PER SOURCE: a local failure
        // keeps the local rows, but a remote page that landed in the same round
        // still replaces that host's rows.
        const inFanout = new Set(fanout);
        const kept = s.rows.filter((row) => inFanout.has(row.host) && (append || !succeeded.has(row.host)));
        // Fresh rows go FIRST so a replayed row wins over its stale copy; pages
        // are pre-sorted per source and the final sort restores the board order.
        const rows = sortPulseRows(dedupePulseRows(fresh.concat(kept)));
        const hasMore = Object.values(nextCursors).some((c) => c != null);
        return {
          ...s,
          rows,
          scope,
          nextCursor: nextCursors[""],
          hostCursors: nextCursors,
          hasMore,
          error: localError,
          hostErrors: failedHosts.sort(),
          loading: false,
          counts: countPulse(rows),
        };
      });
    },
    [pulseSources],
  );

  // Seed on mount; reseed on every reconnect. The bus dropped events while it
  // was down and a refetch is the only truthful recovery — never a replay.
  useEffect(() => {
    void fetchSources("live", null, false);
    return eventBus.onReconnect(() => {
      // A reconnect may have picked up a fresh project list (a host renamed, a
      // project added) — do not keep jumping against a stale one. Dropped
      // BEFORE the fetch: loadSavedProjects reads the cache synchronously, so
      // the other order reseeded against the stale list and only then forgot it.
      savedProjects = null;
      void fetchSources(stateRef.current.scope, null, false);
    });
  }, [fetchSources]);

  // A remote project's FIRST tab (or the last one closing) changes the set of
  // hosts the bus relays, which is the set this dashboard fans out to. Reseed
  // so its cards appear with the tab instead of a refresh later.
  useEffect(() => {
    return eventBus.onHostsChange(() => {
      void fetchSources(stateRef.current.scope, null, false);
    });
  }, [fetchSources]);

  useEffect(() => {
    sink = (event, sessionId, data, host) => {
      if (!sessionId) return;
      // Read the current list out here, not inside a setState updater: updaters
      // must stay pure (React invokes them twice in StrictMode), and the
      // unknown-session branch has a side effect (arming a timer).
      const rows = applyPulseEvent(stateRef.current.rows, event, sessionId, data, host);
      if (rows) {
        setState((s) => ({ ...s, rows, counts: countPulse(rows) }));
        return;
      }
      // A null result means EITHER "unknown session" (refetch to learn about
      // it) OR "known session, event type the dashboard does not patch". Only
      // the former may refetch: during a turn, text/thinking/tool_output/
      // turn_heartbeat for a known row would otherwise re-arm the debounce
      // continuously and poll GET /api/pulse several times a second.
      const known = stateRef.current.rows.some(
        (r) => r.session_id === sessionId && r.host === host,
      );
      if (known) return;
      // A frame from a host we are NOT fanning out to (its last tab closed
      // before the frame arrived, say) can never be learned by any fetch —
      // refetching would only repeat a request that cannot succeed.
      if (host && !eventBus.currentHosts().includes(host)) return;
      if (unknownRef.current !== undefined) return; // already armed
      unknownRef.current = window.setTimeout(() => {
        unknownRef.current = undefined;
        void fetchSources(stateRef.current.scope, null, false);
      }, UNKNOWN_SESSION_REFETCH_MS);
    };
    return () => {
      sink = null;
      if (unknownRef.current !== undefined) {
        window.clearTimeout(unknownRef.current);
        unknownRef.current = undefined;
      }
    };
  }, [fetchSources]);

  const setScope = useCallback(
    (scope: "live" | "all") => {
      if (scope === requestedScopeRef.current) return;
      void fetchSources(scope, null, false);
    },
    [fetchSources],
  );

  const loadMore = useCallback(() => {
    // `hasMore` means "at least one source still has another page", so when it
    // is false there is no cursor anywhere and the call must not spend a
    // request re-fetching page one. Per-host cursors, NOT `nextCursor`: each
    // source pages independently, and a local source with nothing left must not
    // block a remote one that does.
    if (!stateRef.current.hasMore) return;
    void fetchSources(stateRef.current.scope, stateRef.current.hostCursors, true);
  }, [fetchSources]);

  const retry = useCallback(() => {
    void fetchSources(stateRef.current.scope, null, false);
  }, [fetchSources]);

  return { ...state, setScope, loadMore, retry };
}

export function usePulse(): PulseApi {
  const ctx = useContext(PulseContext);
  if (!ctx) throw new Error("usePulse must be used within PulseProvider");
  return ctx;
}

// ── Focus mode ──
//
// Which session the dashboard is showing in detail, or null for the grid.
// Module state rather than React state or per-project persistence, on purpose:
// it must survive Cmd+J leaving and re-entering the dashboard (PulseView
// unmounts), yet must never leak into a project's saved view state, because the
// dashboard is global. An external store keeps that without a provider.

let focusedSessionId: string | null = null;
const focusListeners = new Set<() => void>();

/** Focus a session in the dashboard, or pass null to return to the grid. */
export function setPulseFocus(sessionId: string | null): void {
  if (focusedSessionId === sessionId) return;
  focusedSessionId = sessionId;
  focusListeners.forEach((l) => l());
}

function subscribeFocus(listener: () => void): () => void {
  focusListeners.add(listener);
  return () => {
    focusListeners.delete(listener);
  };
}

/** The focused session id (null = grid) and its setter. */
export function usePulseFocus(): [string | null, (sessionId: string | null) => void] {
  const id = useSyncExternalStore(subscribeFocus, () => focusedSessionId);
  return [id, setPulseFocus];
}
