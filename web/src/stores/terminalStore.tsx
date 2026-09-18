import { createContext, useContext, useCallback, useEffect, useRef, type ReactNode } from "react";
import { Store, useSelector } from "@tanstack/react-store";
import { authedFetch, remoteApiBase } from "@/api/client";
import { loadProjectTerminals, saveProjectTerminals, projectTerminalsKey } from "../components/Terminal/terminalPersistence";

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
}

type TerminalAction =
  | { type: "SET_PROJECT_TERMINALS"; projectPath: string; host?: string; terminals: TerminalInstance[]; activeId: string }
  | { type: "CLOSE_TERMINAL"; projectPath: string; host?: string; id: string }
  | { type: "SET_ACTIVE_ID"; projectPath: string; host?: string; id: string }
  | { type: "RENAME_TERMINAL"; projectPath: string; host?: string; id: string; title: string }
  | { type: "SET_OSC_TITLE"; projectPath: string; host?: string; id: string; title: string }
  | { type: "MARK_ALERTED"; projectPath: string; host?: string; id: string }
  | { type: "CLEAR_ALERT"; projectPath: string; host?: string; id: string };

const initialState: TerminalStoreState = { byProject: {} };

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
  authedFetch(`${prefix}/api/terminal/${encodeURIComponent(id)}`, init)
    .then((res) => {
      if (!res.ok && res.status !== 404) {
        console.error(`terminal: failed to kill shell ${id}: HTTP ${res.status}`);
      }
    })
    .catch((err) => console.error(`terminal: failed to kill shell ${id}:`, err));
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

function terminalReducer(state: TerminalStoreState, action: TerminalAction): TerminalStoreState {
  const key = projectTerminalsKey(action.projectPath, action.host);
  switch (action.type) {
    case "SET_PROJECT_TERMINALS": {
      const cur = state.byProject[key];
      return {
        byProject: {
          ...state.byProject,
          [key]: {
            terminals: action.terminals,
            activeId: action.activeId,
            live: true,
            path: action.projectPath,
            host: action.host,
            // Keep any in-flight alerts across the (re)seed so a background
            // badge isn't wiped by an activate()/cross-window sync.
            alerts: cur?.alerts,
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
        byProject: { ...state.byProject, [key]: { ...cur, terminals, activeId, alerts } },
      };
    }
    case "SET_ACTIVE_ID": {
      const cur = state.byProject[key];
      if (!cur) return state;
      return { byProject: { ...state.byProject, [key]: { ...cur, activeId: action.id } } };
    }
    case "MARK_ALERTED": {
      const cur = state.byProject[key];
      if (!cur) return state;
      const alerts = { ...cur.alerts, [action.id]: true };
      return { byProject: { ...state.byProject, [key]: { ...cur, alerts } } };
    }
    case "CLEAR_ALERT": {
      const cur = state.byProject[key];
      if (!cur || !cur.alerts || !cur.alerts[action.id]) return state;
      const alerts = { ...cur.alerts, [action.id]: false };
      return { byProject: { ...state.byProject, [key]: { ...cur, alerts } } };
    }
    case "RENAME_TERMINAL": {
      const cur = state.byProject[key];
      if (!cur) return state;
      const terminals = cur.terminals.map((t) => (t.id === action.id ? { ...t, title: action.title, renamed: true } : t));
      return { byProject: { ...state.byProject, [key]: { ...cur, terminals } } };
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
      return { byProject: { ...state.byProject, [key]: { ...cur, terminals } } };
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

  // Returns true only if a live terminal with that id actually existed and was
  // removed. Reading `store.state` (not the captured `state`) keeps the check
  // correct within a single tick — e.g. two synchronous closeActiveTerminal()
  // calls: the first removes the terminal and returns true; the second sees it
  // already gone in live state and returns false instead of removing a
  // neighbour or double-firing.
  const closeTerminal = useCallback(
    (projectPath: string, id: string, host?: string): boolean => {
      const cur = store.state.byProject[projectTerminalsKey(projectPath, host)];
      if (!cur?.live || !cur.terminals.some((t) => t.id === id)) return false;
      dispatch({ type: "CLOSE_TERMINAL", projectPath, host, id });
      killTerminalShell(id, host, projectPath);
      return true;
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

  // Persist every live project's terminals/activeId (debounced), mirroring
  // TerminalTabs.tsx's original per-project persistence effect.
  const skipNextSaveRef = useRef<Set<string>>(new Set());
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

  // Cross-window sync: another window's terminal open/close/rename updates
  // this window's already-live projects too. A project this window never
  // activated stays a cheap peek and picks up the change next render.
  useEffect(() => {
    const handler = (e: StorageEvent) => {
      if (e.key !== "ocode.ui.terminals.project.v1") return;
      for (const key of Object.keys(store.state.byProject)) {
        const cur = store.state.byProject[key];
        if (!cur.live) continue;
        const saved = loadProjectTerminals(cur.path, cur.host);
        if (!saved) {
          if (cur.terminals.length !== 0) {
            skipNextSaveRef.current.add(key);
            dispatch({ type: "SET_PROJECT_TERMINALS", projectPath: cur.path, host: cur.host, terminals: [], activeId: "" });
          }
          continue;
        }
        const same =
          saved.terminals.length === cur.terminals.length &&
          saved.terminals.every(
            (t, i) =>
              t.id === cur.terminals[i]?.id &&
              t.title === cur.terminals[i]?.title &&
              !!t.renamed === !!cur.terminals[i]?.renamed &&
              (t.oscTitle ?? "") === (cur.terminals[i]?.oscTitle ?? ""),
          ) &&
          saved.activeId === cur.activeId;
        if (same) continue;
        bumpSeqPast(saved.terminals.map((t) => t.title));
        skipNextSaveRef.current.add(key);
        const activeId =
          saved.activeId &&
          (saved.activeId === PROCESSES_TAB_ID || saved.terminals.some((t) => t.id === saved.activeId))
            ? saved.activeId
            : (saved.terminals[saved.terminals.length - 1]?.id ?? "");
        dispatch({ type: "SET_PROJECT_TERMINALS", projectPath: cur.path, host: cur.host, terminals: saved.terminals, activeId });
      }
    };
    window.addEventListener("storage", handler);
    return () => window.removeEventListener("storage", handler);
  }, [store, dispatch]);

  return (
    <TerminalContext.Provider value={{ state, activate, openTerminal, closeTerminal, setActiveId, renameTerminal, setOscTitle, markAlerted, clearAlert, attachTerminal }}>
      {children}
    </TerminalContext.Provider>
  );
}

export function useTerminalState() {
  const ctx = useContext(TerminalContext);
  if (!ctx) throw new Error("useTerminalState must be used within TerminalProvider");
  return ctx;
}
