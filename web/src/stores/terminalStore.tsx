import { createContext, useContext, useCallback, useEffect, useRef, type ReactNode } from "react";
import { Store, useSelector } from "@tanstack/react-store";
import { api, authedFetch, remoteApiBase } from "@/api/client";
import { eventBus } from "@/lib/eventBus";
import {
  loadProjectTerminals,
  saveProjectTerminals,
  projectTerminalsKey,
  readAllMirrorProjects,
} from "../components/Terminal/terminalPersistence";

export const PROCESSES_TAB_ID = "processes";

export interface TerminalInstance {
  id: string;
  /** Default "Terminal N" name, or the user's explicit rename. */
  title: string;
  /** True once the user renamed the tab; a manual name always beats oscTitle. */
  renamed?: boolean;
  /** Last title the running program set via OSC 0/2 (e.g. claude code or the
   *  ocode TUI). Shown instead of `title` unless the tab was renamed. */
  oscTitle?: string;
}

/** Max length kept for an OSC-set title so a runaway program can't bloat the
 *  persisted tab metadata. */
const MAX_OSC_TITLE_LEN = 80;

/** The name a terminal tab should show: manual rename wins, then the
 *  program-set OSC title, then the default "Terminal N". */
export function terminalDisplayTitle(t: TerminalInstance): string {
  if (t.renamed) return t.title;
  return t.oscTitle || t.title;
}

/** A TerminalInstance plus the ephemeral, non-persisted alert flag surfaced by
 *  getProjectTerminals. Kept separate from TerminalInstance so persisted
 *  terminal metadata never carries the alert state. */
export type TerminalView = TerminalInstance & { alerted?: boolean };

interface ProjectTerminalState {
  terminals: TerminalInstance[];
  activeId: string;
  live: boolean;
  /** The raw project path and remote host this entry is keyed by. `byProject`
   *  is keyed by projectTerminalsKey(path, host), so the persistence and
   *  cross-window-sync effects need the parts to save/load under the same key. */
  path: string;
  host?: string;
  /** Ephemeral per-terminal alert flags: set when the terminal emitted a
   *  bell/notification while backgrounded. Kept OUT of TerminalInstance so it
   *  is never persisted to disk with the terminal metadata. */
  alerts?: Record<string, boolean>;
}

export interface TerminalStoreState {
  byProject: Record<string, ProjectTerminalState>;
  /** Bumped whenever the local mirror is rewritten from a server read.
   *
   *  A project this window never activated is a cheap "peek": `getProjectTerminals`
   *  answers it by reading the mirror out of localStorage, which is NOT reactive.
   *  Without this counter a server read that hydrates a peeked project would write
   *  the mirror but change nothing the store exposes, so nothing re-rendered and
   *  the second client kept showing an empty strip — the exact reported bug. */
  revision: number;
}

type TerminalAction =
  | { type: "SET_PROJECT_TERMINALS"; projectPath: string; host?: string; terminals: TerminalInstance[]; activeId: string }
  | { type: "CLOSE_TERMINAL"; projectPath: string; host?: string; id: string }
  | { type: "SET_ACTIVE_ID"; projectPath: string; host?: string; id: string }
  | { type: "RENAME_TERMINAL"; projectPath: string; host?: string; id: string; title: string }
  | { type: "SET_OSC_TITLE"; projectPath: string; host?: string; id: string; title: string }
  | { type: "MARK_ALERTED"; projectPath: string; host?: string; id: string }
  | { type: "CLEAR_ALERT"; projectPath: string; host?: string; id: string }
  | { type: "BUMP_REVISION" };

const initialState: TerminalStoreState = { byProject: {}, revision: 0 };

let nextTerminalSeq = 1;

/** Kills the server-side shell behind a closed tab. Unmounting the panel only
 *  detaches the shell (so reloads can resume it); an explicit close is the one
 *  place the process must actually die. 404 means it already exited.
 *
 *  For a remote project the shell lives on the host, so the DELETE goes
 *  through the local reverse proxy with the X-Ocode-Project header. */
function killTerminalShell(id: string, host?: string, projectPath?: string) {
  const prefix = host ? remoteApiBase(host) : "";
  // Local projects keep the exact historical call shape (no headers object).
  const init: RequestInit = { method: "DELETE" };
  if (host && projectPath) {
    const headers = new Headers();
    headers.set("X-Ocode-Project", projectPath);
    init.headers = headers;
  }
  return authedFetch(`${prefix}/api/terminal/${encodeURIComponent(id)}`, init)
    .then((res) => {
      if (!res.ok && res.status !== 404) {
        console.error(`terminal: failed to kill shell ${id}: HTTP ${res.status}`);
      }
    })
    .catch((err) => console.error(`terminal: failed to kill shell ${id}:`, err));
}

/** Removes a terminal from this window's live store, or — when the project was
 *  never activated — from the persisted peek list. Returns false when the id
 *  is absent either way, so a repeated close cannot fall through to a
 *  neighbour. The persisted branch is what lets the tab strip close a terminal
 *  that is only being *peeked* after a reload (see getProjectTerminals): the
 *  reducer's CLOSE_TERMINAL no-ops without a live entry, which previously left
 *  the tab on screen and its shell alive. */
function removeTerminalLocally(
  store: Store<TerminalStoreState>,
  dispatch: (action: TerminalAction) => void,
  projectPath: string,
  id: string,
  host?: string,
): boolean {
  const key = projectTerminalsKey(projectPath, host);
  const cur = store.state.byProject[key];
  if (cur?.live) {
    if (!cur.terminals.some((t) => t.id === id)) return false;
    dispatch({ type: "CLOSE_TERMINAL", projectPath, host, id });
    return true;
  }
  const saved = loadProjectTerminals(projectPath, host);
  if (!saved?.terminals.some((t) => t.id === id)) return false;
  const terminals = saved.terminals.filter((t) => t.id !== id);
  const activeId =
    saved.activeId === id ? (terminals[terminals.length - 1]?.id ?? "") : saved.activeId;
  saveProjectTerminals(projectPath, terminals, activeId, host);
  return true;
}

function newTerminal(): TerminalInstance {
  const n = nextTerminalSeq++;
  return { id: `term-${n}-${Date.now()}`, title: `Terminal ${n}` };
}

/** Keeps the module-level title counter ahead of any restored terminal
 *  numbers, so a newly opened terminal after a restore doesn't reuse a
 *  "Terminal N" title that's already on screen. */
function bumpSeqPast(titles: string[]) {
  for (const title of titles) {
    const m = /^Terminal (\d+)$/.exec(title);
    if (m) nextTerminalSeq = Math.max(nextTerminalSeq, Number(m[1]) + 1);
  }
}

/** Restrict a project's alert map to the terminals that still exist, dropping
 *  cleared (false) entries too. A cross-window storage sync can replace the
 *  terminal list with a shrunken set (another window closed a terminal); an
 *  alert keyed to a removed id has no tab to focus, so it can never be cleared
 *  and keeps the project's attention bell lit forever. Returns undefined when
 *  nothing survives so the entry stays shape-identical to a fresh seed. */
function pruneAlerts(
  alerts: Record<string, boolean> | undefined,
  terminals: TerminalInstance[],
): Record<string, boolean> | undefined {
  if (!alerts) return undefined;
  const ids = new Set(terminals.map((t) => t.id));
  const next: Record<string, boolean> = {};
  for (const [id, on] of Object.entries(alerts)) {
    if (on && ids.has(id)) next[id] = true;
  }
  return Object.keys(next).length > 0 ? next : undefined;
}

function terminalReducer(state: TerminalStoreState, action: TerminalAction): TerminalStoreState {
  // BUMP_REVISION is not project-scoped, so the key is derived per case rather
  // than once up front (it read action.projectPath off every action).
  if (action.type === "BUMP_REVISION") return { ...state, revision: state.revision + 1 };
  const key = projectTerminalsKey(action.projectPath, action.host);
  switch (action.type) {
    case "SET_PROJECT_TERMINALS": {
      const cur = state.byProject[key];
      return {
        ...state,
        byProject: {
          ...state.byProject,
          [key]: {
            terminals: action.terminals,
            activeId: action.activeId,
            live: true,
            path: action.projectPath,
            host: action.host,
            // Keep any in-flight alerts across the (re)seed so a background
            // badge isn't wiped by an activate()/cross-window sync, but only
            // for terminals that survived (see pruneAlerts).
            alerts: pruneAlerts(cur?.alerts, action.terminals),
          },
        },
      };
    }
    case "CLOSE_TERMINAL": {
      const cur = state.byProject[key];
      if (!cur) return state;
      const terminals = cur.terminals.filter((t) => t.id !== action.id);
      const alerts =
        cur.alerts && cur.alerts[action.id]
          ? Object.fromEntries(Object.entries(cur.alerts).filter(([k]) => k !== action.id))
          : cur.alerts;
      const activeId =
        cur.activeId === action.id
          ? terminals.length > 0
            ? terminals[terminals.length - 1].id
            : ""
          : cur.activeId;
      return {
        ...state,
        byProject: { ...state.byProject, [key]: { ...cur, terminals, activeId, alerts } },
      };
    }
    case "SET_ACTIVE_ID": {
      const cur = state.byProject[key];
      if (!cur) return state;
      return { ...state, byProject: { ...state.byProject, [key]: { ...cur, activeId: action.id } } };
    }
    case "MARK_ALERTED": {
      const cur = state.byProject[key];
      if (!cur) return state;
      const alerts = { ...cur.alerts, [action.id]: true };
      return { ...state, byProject: { ...state.byProject, [key]: { ...cur, alerts } } };
    }
    case "CLEAR_ALERT": {
      const cur = state.byProject[key];
      if (!cur || !cur.alerts || !cur.alerts[action.id]) return state;
      const alerts = { ...cur.alerts, [action.id]: false };
      return { ...state, byProject: { ...state.byProject, [key]: { ...cur, alerts } } };
    }
    case "RENAME_TERMINAL": {
      const cur = state.byProject[key];
      if (!cur) return state;
      const terminals = cur.terminals.map((t) => (t.id === action.id ? { ...t, title: action.title, renamed: true } : t));
      return { ...state, byProject: { ...state.byProject, [key]: { ...cur, terminals } } };
    }
    case "SET_OSC_TITLE": {
      const cur = state.byProject[key];
      if (!cur) return state;
      const oscTitle = action.title.replace(/\s+/g, " ").trim().slice(0, MAX_OSC_TITLE_LEN);
      const target = cur.terminals.find((t) => t.id === action.id);
      if (!target || (target.oscTitle ?? "") === oscTitle) return state;
      const terminals = cur.terminals.map((t) => {
        if (t.id !== action.id) return t;
        if (!oscTitle) {
          const { oscTitle: _drop, ...rest } = t;
          return rest;
        }
        return { ...t, oscTitle };
      });
      return { ...state, byProject: { ...state.byProject, [key]: { ...cur, terminals } } };
    }
    default:
      return state;
  }
}

/** Live state if this project's terminal region has actually been used this
 *  session, else a cheap "peek" of what's persisted on disk (id + title
 *  only) — so a not-yet-live project's pills can still be listed and
 *  clicked without spawning any pty. See the design spec's "peek vs live"
 *  section for why this split exists. */
export function getProjectTerminals(
  state: TerminalStoreState,
  projectPath: string,
  host?: string,
): { terminals: TerminalView[]; activeId: string; live: boolean } {
  const entry = state.byProject[projectTerminalsKey(projectPath, host)];
  if (entry?.live) {
    // Merge the ephemeral alert flags onto the view-facing instances. The
    // persisted TerminalInstance objects never carry `alerted`, so this mapping
    // is the only place the badge state is surfaced and it never hits disk.
    const alerts = entry.alerts;
    const terminals = alerts
      ? entry.terminals.map((t) => (alerts[t.id] ? { ...t, alerted: true } : t))
      : entry.terminals;
    return { terminals, activeId: entry.activeId, live: true };
  }
  const saved = loadProjectTerminals(projectPath, host);
  return { terminals: saved?.terminals ?? [], activeId: saved?.activeId ?? "", live: false };
}

interface TerminalContextType {
  state: TerminalStoreState;
  /** Idempotent: restores this project's persisted terminals (or spawns one
   *  fresh terminal if none were persisted) and marks it live. No-op if
   *  already live. */
  activate: (projectPath: string, host?: string) => void;
  /** Ensures the project is live (seeding from disk first if it wasn't yet
   *  live), then appends and activates one new terminal. */
  openTerminal: (projectPath: string, host?: string) => void;
  /** Closes the given terminal for a project. Returns `false` (and is a no-op)
   *  when that terminal is not currently open in this window's live state, so a
   *  repeated close of an already-removed terminal does not fall through. */
  closeTerminal: (projectPath: string, id: string, host?: string) => boolean;
  /** Kills a terminal on the host even when this window has no live tab for
   *  it (the sidebar inventory's kill X). Removes any local tab/persisted
   *  entry first — so the panel cannot reattach and respawn the shell after
   *  the DELETE — then resolves once the DELETE has been sent. */
  killTerminal: (projectPath: string, id: string, host?: string) => Promise<void>;
  /** Ensures the project is live, then sets its active id (a terminal id or
   *  PROCESSES_TAB_ID). */
  setActiveId: (projectPath: string, id: string, host?: string) => void;
  renameTerminal: (projectPath: string, id: string, title: string, host?: string) => void;
  /** Records the title the running program set via OSC 0/2. Empty clears it. */
  setOscTitle: (projectPath: string, id: string, title: string, host?: string) => void;
  /** Marks a terminal as having emitted a bell/notification while backgrounded. */
  markAlerted: (projectPath: string, id: string, host?: string) => void;
  /** Clears a terminal's background-activity badge. */
  clearAlert: (projectPath: string, id: string, host?: string) => void;
  /** Attaches an already-running terminal (from the remote host's inventory)
   *  as a tab with the given id. No-op when the id is already open. */
  attachTerminal: (projectPath: string, host: string | undefined, id: string, title: string) => void;
}

const TerminalContext = createContext<TerminalContextType | null>(null);

export function TerminalProvider({ children }: { children: ReactNode }) {
  const storeRef = useRef<Store<TerminalStoreState> | null>(null);
  if (!storeRef.current) storeRef.current = new Store(initialState);
  const store = storeRef.current;
  const state = useSelector(store);

  const dispatch = useCallback(
    (action: TerminalAction) => store.setState((prev) => terminalReducer(prev, action)),
    [store],
  );

  const activate = useCallback(
    (projectPath: string, host?: string) => {
      if (store.state.byProject[projectTerminalsKey(projectPath, host)]?.live) return;
      const saved = loadProjectTerminals(projectPath, host);
      if (saved) {
        bumpSeqPast(saved.terminals.map((t) => t.title));
        const activeId =
          saved.activeId &&
          (saved.activeId === PROCESSES_TAB_ID || saved.terminals.some((t) => t.id === saved.activeId))
            ? saved.activeId
            : saved.terminals[saved.terminals.length - 1].id;
        dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, host, terminals: saved.terminals, activeId });
        return;
      }
      const term = newTerminal();
      dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, host, terminals: [term], activeId: term.id });
    },
    [store, dispatch],
  );

  const openTerminal = useCallback(
    (projectPath: string, host?: string) => {
      const cur = store.state.byProject[projectTerminalsKey(projectPath, host)];
      const baseTerminals = cur?.live ? cur.terminals : loadProjectTerminals(projectPath, host)?.terminals ?? [];
      bumpSeqPast(baseTerminals.map((t) => t.title));
      const term = newTerminal();
      dispatch({
        type: "SET_PROJECT_TERMINALS",
        projectPath,
        host,
        terminals: [...baseTerminals, term],
        activeId: term.id,
      });
    },
    [store, dispatch],
  );

  // Returns true only if a terminal with that id actually existed (live or
  // peeked) and was removed. Reading `store.state` (not the captured `state`)
  // keeps the check correct within a single tick — e.g. two synchronous
  // closeActiveTerminal() calls: the first removes the terminal and returns
  // true; the second sees it already gone and returns false instead of
  // removing a neighbour or double-firing.
  const closeTerminal = useCallback(
    (projectPath: string, id: string, host?: string): boolean => {
      if (!removeTerminalLocally(store, dispatch, projectPath, id, host)) return false;
      void killTerminalShell(id, host, projectPath);
      return true;
    },
    [store, dispatch],
  );

  // Kills a terminal that may not be open in this window at all — the sidebar
  // inventory's kill X. Removes any local tab (live or peeked) first so the
  // panel cannot reattach to the id and respawn the shell right after the
  // DELETE, then awaits the DELETE so the caller can refresh the inventory
  // once the host has actually dropped the session.
  const killTerminal = useCallback(
    async (projectPath: string, id: string, host?: string): Promise<void> => {
      removeTerminalLocally(store, dispatch, projectPath, id, host);
      await killTerminalShell(id, host, projectPath);
    },
    [store, dispatch],
  );

  const setActiveId = useCallback(
    (projectPath: string, id: string, host?: string) => {
      activate(projectPath, host);
      dispatch({ type: "SET_ACTIVE_ID", projectPath, host, id });
    },
    [activate, dispatch],
  );

  const renameTerminal = useCallback(
    (projectPath: string, id: string, title: string, host?: string) =>
      dispatch({ type: "RENAME_TERMINAL", projectPath, host, id, title }),
    [dispatch],
  );

  const setOscTitle = useCallback(
    (projectPath: string, id: string, title: string, host?: string) =>
      dispatch({ type: "SET_OSC_TITLE", projectPath, host, id, title }),
    [dispatch],
  );

  const markAlerted = useCallback(
    (projectPath: string, id: string, host?: string) => dispatch({ type: "MARK_ALERTED", projectPath, host, id }),
    [dispatch],
  );

  const clearAlert = useCallback(
    (projectPath: string, id: string, host?: string) => dispatch({ type: "CLEAR_ALERT", projectPath, host, id }),
    [dispatch],
  );

  // Attach an already-running terminal by id (remote host inventory reattach).
  // No-op when that id is already open, so a double click cannot duplicate it.
  const attachTerminal = useCallback(
    (projectPath: string, host: string | undefined, id: string, title: string) => {
      const cur = store.state.byProject[projectTerminalsKey(projectPath, host)];
      const base = cur?.live ? cur.terminals : loadProjectTerminals(projectPath, host)?.terminals ?? [];
      if (base.some((t) => t.id === id)) return;
      bumpSeqPast(base.map((t) => t.title));
      const term: TerminalInstance = title.trim() ? { id, title } : { id, title: `Terminal ${nextTerminalSeq++}` };
      dispatch({
        type: "SET_PROJECT_TERMINALS",
        projectPath,
        host,
        terminals: [...base, term],
        activeId: id,
      });
    },
    [store, dispatch],
  );

  // ── Server-backed terminal tab list ───────────────────────────────────────
  //
  // The open-terminal LIST is server state (GET/PUT /api/terminal-tabs →
  // internal/termtabs, terminals.json under the global data dir), for the same
  // reason session tabs are (internal/tabs): localStorage is per-origin, so a
  // terminal started in the desktop app was invisible to a second browser even
  // though a remote project's shell is a child of the HOST's serve --remote and
  // was perfectly shared. The list was the only thing that could not cross.
  //
  // localStorage is kept only as a MIRROR of the last server state (rewritten
  // on every server read and every local mutation). It is what lets
  // getProjectTerminals answer a synchronous "peek" before hydration lands, and
  // what TopTabs/ProcessesPanel read. It is never authoritative: a server read
  // always overwrites it, and the pre-hydration persist is suppressed so a
  // mirror-derived list can never clobber the server.
  //
  // `activeId` is deliberately NOT shared — which tab a window has focused is
  // per-client view state (it can be PROCESSES_TAB_ID, which is not a terminal),
  // and sharing it would let one window yank another's selection.
  const skipNextSaveRef = useRef<Set<string>>(new Set());
  /** Keys whose next mirror→server PUT would be a pure echo of state this
   *  client just read back from the server. Separate from skipNextSaveRef so the
   *  two effects can't consume each other's marker. */
  const skipNextPutRef = useRef<Set<string>>(new Set());
  /** restored = the initial server read settled. Writes are suppressed until
   *  then so a mint-then-fetch race cannot replace the server's list with a
   *  locally-derived one (which would spawn a phantom shell). */
  const restoredRef = useRef(false);
  const syncRef = useRef({ dirty: false, writing: false, refetchQueued: false, pushFailed: false });

  /** Applies a server list to one already-live project. Ids only: a rename or an
   *  OSC title is owned by whichever client has the panel open and lands in the
   *  server on the next write, so comparing ids avoids fighting over titles. */
  const adoptServerList = useCallback(
    (key: string, incoming: TerminalInstance[]) => {
      const cur = store.state.byProject[key];
      if (!cur?.live) return;
      // Compare METADATA, not just ids. An ids-only check meant a rename or an
      // OSC title set in another client never reached this window's live tab —
      // it stayed stale until a full page reload. Titles are lost to a
      // last-writer-wins race anyway (only the attached client sees OSC output),
      // and the caller's dirty/writing guard keeps a mid-flight PUT from being
      // mistaken for a newer server state.
      const same =
        cur.terminals.length === incoming.length &&
        cur.terminals.every((t, i) => {
          const next = incoming[i];
          return (
            !!next &&
            t.id === next.id &&
            t.title === next.title &&
            !!t.renamed === !!next.renamed &&
            (t.oscTitle ?? "") === (next.oscTitle ?? "")
          );
        });
      if (same) return;
      // Two independent skips. The mirror write is redundant (refetchTerminalTabs
      // already wrote it), and the server PUT is an ECHO: this state came FROM
      // the server, so writing it straight back would trigger another
      // terminal_tabs_changed on every client and an extra round-trip per change.
      skipNextSaveRef.current.add(key);
      skipNextPutRef.current.add(key);
      // Keep a still-valid focus; the shared list may not carry this window's
      // active id (another window closed it), so fall back to the last tab.
      const activeId =
        cur.activeId && (cur.activeId === PROCESSES_TAB_ID || incoming.some((t) => t.id === cur.activeId))
          ? cur.activeId
          : incoming[incoming.length - 1]?.id ?? "";
      bumpSeqPast(incoming.map((t) => t.title));
      dispatch({
        type: "SET_PROJECT_TERMINALS",
        projectPath: cur.path,
        host: cur.host,
        terminals: incoming,
        activeId,
      });
    },
    [store, dispatch],
  );

  /** Pull the server's list, rewrite the local mirror from it, and re-seed live
   *  projects. A failed read leaves the mirror alone (a transient blip must not
   *  empty the tab strip) and is retried on the next bus reconnect. */
  const refetchTerminalTabs = useCallback(async () => {
    const sync = syncRef.current;
    if (sync.dirty || sync.writing) {
      sync.refetchQueued = true;
      return;
    }
    try {
      const res = await api.getTerminalTabs();
      if (sync.dirty || sync.writing) {
        sync.refetchQueued = true;
        return;
      }
      for (const [key, entry] of Object.entries(res.projects ?? {})) {
        const terminals = (entry?.terminals ?? [])
          .filter((t) => t && typeof t.id === "string" && t.id)
          .map((t) => ({
            id: t.id,
            title: typeof t.title === "string" ? t.title : t.id,
            ...(t.renamed ? { renamed: true } : {}),
            ...(t.osc_title ? { oscTitle: t.osc_title } : {}),
          }));
        // The mirror carries activeId too; keep whatever this window had.
        // `key` IS the host::path composite, and projectTerminalsKey(key, undefined)
        // is `key`, so this reads that project's mirror without re-splitting it.
        const current = loadProjectTerminals(key);
        saveProjectTerminals(key, terminals, current?.activeId ?? "");
        adoptServerList(key, terminals);
      }
      // A project whose LAST terminal was closed is DELETED server-side, so it
      // is absent from the response entirely rather than present with an empty
      // list. Walking only the returned keys therefore left this window showing
      // a tab for a shell that no longer exists — forever, since nothing else
      // would ever mention the key again.
      //
      // Only live projects are cleared, and only on a REFETCH: during the initial
      // restore a live project absent from the server is one minted locally
      // before hydration landed, which the restore flushes TO the server rather
      // than deleting. A failed PUT likewise leaves live projects the server never
      // heard of, so clearing waits for a push that succeeds.
      const clearAbsent = restoredRef.current && !sync.pushFailed;
      for (const [key, entry] of Object.entries(store.state.byProject)) {
        if (!clearAbsent || !entry.live || key in (res.projects ?? {}) || entry.terminals.length === 0) continue;
        skipNextPutRef.current.add(key);
        skipNextSaveRef.current.add(key);
        saveProjectTerminals(entry.path, [], "", entry.host);
        dispatch({ type: "SET_PROJECT_TERMINALS", projectPath: entry.path, host: entry.host, terminals: [], activeId: "" });
      }
      // Peeked projects read the mirror out of localStorage, which is not
      // reactive; bump so the provider re-renders and they pick the list up.
      dispatch({ type: "BUMP_REVISION" });
    } catch (err) {
      console.error("Failed to refetch terminal tabs from server:", err);
    }
  }, [adoptServerList, dispatch]);

  /** One mirror → server write of every live project. Callers debounce it (or
   *  flush it once after the restore settles) — this is the unit, not the timer.
   *
   *  Every live project is sent, INCLUDING ones left empty by a close: the server
   *  treats an empty list as a delete, and OMITTING a project reads as "this
   *  client has never heard of it", which would preserve a closed terminal
   *  forever. */
  const pushTerminalTabs = useCallback(async () => {
    const sync = syncRef.current;
    sync.dirty = false;
    sync.writing = true;
    try {
      const projects: Record<string, { terminals: { id: string; title: string; renamed?: boolean; osc_title?: string }[] }> = {};
      for (const [key, entry] of Object.entries(store.state.byProject)) {
        if (!entry.live) continue;
        if (skipNextPutRef.current.delete(key)) continue; // echo of a server read
        projects[projectTerminalsKey(entry.path, entry.host)] = {
          terminals: entry.terminals.map((term) => ({
            id: term.id,
            title: term.title,
            ...(term.renamed ? { renamed: true } : {}),
            ...(term.oscTitle ? { osc_title: term.oscTitle } : {}),
          })),
        };
      }
      // Every live project was an echo → send nothing rather than an empty merge
      // (which the server would read as "delete every project").
      if (Object.keys(projects).length === 0) return;
      await api.setTerminalTabs(projects);
      sync.pushFailed = false;
    } catch (err) {
      sync.pushFailed = true;
      console.error("Failed to persist terminal tabs to server:", err);
    } finally {
      sync.writing = false;
      if (sync.refetchQueued && !sync.dirty) {
        sync.refetchQueued = false;
        void refetchTerminalTabs();
      }
    }
  }, [store, refetchTerminalTabs]);

  // One-time migration + restore. If the server holds nothing for a project this
  // client has a local mirror for, the mirror is written THROUGH (so an existing
  // user's terminals survive the move off localStorage) and then cleared. The
  // server is consulted first, so a second browser adopts the desktop app's list
  // instead of racing it with its own.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await api.getTerminalTabs();
        if (cancelled) return;
        const projects = res.projects ?? {};
        let migrated = false;
        for (const [key, entry] of Object.entries(projects)) {
          const terminals = (entry?.terminals ?? [])
            .filter((t) => t && typeof t.id === "string" && t.id)
            .map((t) => ({
              id: t.id,
              title: typeof t.title === "string" ? t.title : t.id,
              ...(t.renamed ? { renamed: true } : {}),
              ...(t.osc_title ? { oscTitle: t.osc_title } : {}),
            }));
          saveProjectTerminals(
            key,
            terminals,
            loadProjectTerminals(key)?.activeId ?? terminals[terminals.length - 1]?.id ?? "",
          );
          adoptServerList(key, terminals);
        }
        dispatch({ type: "BUMP_REVISION" });
        // Write through any project the server has never seen. Keys are opaque
        // (host::path), so the mirror's own key is the lookup key.
        const legacy: Record<string, { terminals: TerminalInstance[] }> = {};
        for (const [key, saved] of Object.entries(readAllMirrorProjects())) {
          if (key in projects) continue;
          if (!saved || saved.length === 0) continue;
          legacy[key] = { terminals: saved };
          migrated = true;
        }
        if (migrated) {
          await api.setTerminalTabs(legacy);
          for (const key of Object.keys(legacy)) saveProjectTerminals(key, [], "");
        }
      } catch (err) {
        // Intentionally not rethrown: the store stays usable on the local
        // mirror and the next bus reconnect retries the server read.
        console.error("Failed to restore terminal tabs from server:", err);
      } finally {
        if (cancelled) return;
        restoredRef.current = true;
        // A terminal opened BEFORE this restore settled (the restore read is
        // async; the "+" button is not) is live but the server never mentioned
        // its project, so adoptServerList did not fire and no state change would
        // ever re-trigger the debounced write effect. Flush it now, or it would
        // sit in the local mirror until the next reload and no other client
        // would see it.
        const hasLive = Object.values(store.state.byProject).some((entry) => entry.live);
        if (hasLive) void pushTerminalTabs();
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [adoptServerList, dispatch, store, pushTerminalTabs]);

  // Keep the strip converged across every client on this server — the desktop
  // shell, a second browser, a shared URL. The server publishes an unscoped
  // `terminal_tabs_changed` after each PUT; on it (and on every bus reconnect,
  // which may have missed one) refetch.
  useEffect(() => {
    // Deliberately NOT gated on restoredRef: a refetch is idempotent and reads
    // only the server, so one arriving before the initial restore settles just
    // applies the same state twice. Only the WRITE path is gated, because that
    // is the one that could replace the server's list with a locally-minted one.
    const onChanged = () => {
      void refetchTerminalTabs();
    };
    const offEvent = eventBus.on("terminal_tabs_changed", onChanged);
    const offReconnect = eventBus.onReconnect(() => void refetchTerminalTabs());
    return () => {
      offEvent();
      offReconnect();
    };
  }, [refetchTerminalTabs]);

  // Debounced persistence of every live project's terminal list to the server.
  // Suppressed until the initial restore settles (see restoredRef).
  useEffect(() => {
    const timers: ReturnType<typeof setTimeout>[] = [];
    for (const [key, entry] of Object.entries(state.byProject)) {
      if (!entry.live) continue;
      if (skipNextSaveRef.current.has(key)) {
        skipNextSaveRef.current.delete(key);
        continue;
      }
      timers.push(setTimeout(() => saveProjectTerminals(entry.path, entry.terminals, entry.activeId, entry.host), 200));
    }
    return () => timers.forEach(clearTimeout);
  }, [state.byProject]);

  // Debounced persistence of every live project's terminal list to the server.
  // Suppressed until the initial restore settles (see restoredRef).
  useEffect(() => {
    if (!restoredRef.current) return;
    const sync = syncRef.current;
    sync.dirty = true;
    const t = setTimeout(() => {
      void pushTerminalTabs();
    }, 400);
    return () => clearTimeout(t);
  }, [state.byProject, pushTerminalTabs]);

  // Flush any pending debounced save synchronously when the provider unmounts
  // (e.g. a test remount, or the app tearing down) so a live project's terminal
  // layout is never lost because its 200ms timer was still pending. Reads the
  // live store state directly so the latest terminals/activeId are written.
  useEffect(() => {
    return () => {
      for (const entry of Object.values(store.state.byProject)) {
        if (entry.live) saveProjectTerminals(entry.path, entry.terminals, entry.activeId, entry.host);
      }
    };
  }, [store]);

  // The former `storage`-event cross-window sync is gone on purpose. It only
  // ever worked between tabs of ONE browser profile (the `storage` event does
  // not cross profiles), which is exactly the reported bug: a second browser
  // kept its own copy and never converged. The server-backed
  // `terminal_tabs_changed` bus event covers both cases — same-profile tabs
  // included — because every write now goes through the server, which
  // broadcasts to every connected client.

  return (
    <TerminalContext.Provider value={{ state, activate, openTerminal, closeTerminal, killTerminal, setActiveId, renameTerminal, setOscTitle, markAlerted, clearAlert, attachTerminal }}>
      {children}
    </TerminalContext.Provider>
  );
}

export function useTerminalState() {
  const ctx = useContext(TerminalContext);
  if (!ctx) throw new Error("useTerminalState must be used within TerminalProvider");
  return ctx;
}
