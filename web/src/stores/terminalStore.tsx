import { createContext, useContext, useCallback, useEffect, useRef, type ReactNode } from "react";
import { Store, useSelector } from "@tanstack/react-store";
import { authedFetch } from "@/api/client";
import { loadProjectTerminals, saveProjectTerminals } from "../components/Terminal/terminalPersistence";

export const PROCESSES_TAB_ID = "processes";

export interface TerminalInstance {
  id: string;
  title: string;
}

/** A TerminalInstance plus the ephemeral, non-persisted alert flag surfaced by
 *  getProjectTerminals. Kept separate from TerminalInstance so persisted
 *  terminal metadata never carries the alert state. */
export type TerminalView = TerminalInstance & { alerted?: boolean };

interface ProjectTerminalState {
  terminals: TerminalInstance[];
  activeId: string;
  live: boolean;
  /** Ephemeral per-terminal alert flags: set when the terminal emitted a
   *  bell/notification while backgrounded. Kept OUT of TerminalInstance so it
   *  is never persisted to disk with the terminal metadata. */
  alerts?: Record<string, boolean>;
}

export interface TerminalStoreState {
  byProject: Record<string, ProjectTerminalState>;
}

type TerminalAction =
  | { type: "SET_PROJECT_TERMINALS"; projectPath: string; terminals: TerminalInstance[]; activeId: string }
  | { type: "CLOSE_TERMINAL"; projectPath: string; id: string }
  | { type: "SET_ACTIVE_ID"; projectPath: string; id: string }
  | { type: "RENAME_TERMINAL"; projectPath: string; id: string; title: string }
  | { type: "MARK_ALERTED"; projectPath: string; id: string }
  | { type: "CLEAR_ALERT"; projectPath: string; id: string };

const initialState: TerminalStoreState = { byProject: {} };

let nextTerminalSeq = 1;

/** Kills the server-side shell behind a closed tab. Unmounting the panel only
 *  detaches the shell (so reloads can resume it); an explicit close is the one
 *  place the process must actually die. 404 means it already exited. */
function killTerminalShell(id: string) {
  authedFetch(`/api/terminal/${encodeURIComponent(id)}`, { method: "DELETE" })
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
  switch (action.type) {
    case "SET_PROJECT_TERMINALS": {
      const cur = state.byProject[action.projectPath];
      return {
        byProject: {
          ...state.byProject,
          [action.projectPath]: {
            terminals: action.terminals,
            activeId: action.activeId,
            live: true,
            // Keep any in-flight alerts across the (re)seed so a background
            // badge isn't wiped by an activate()/cross-window sync.
            alerts: cur?.alerts,
          },
        },
      };
    }
    case "CLOSE_TERMINAL": {
      const cur = state.byProject[action.projectPath];
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
        byProject: { ...state.byProject, [action.projectPath]: { ...cur, terminals, activeId, alerts } },
      };
    }
    case "SET_ACTIVE_ID": {
      const cur = state.byProject[action.projectPath];
      if (!cur) return state;
      return { byProject: { ...state.byProject, [action.projectPath]: { ...cur, activeId: action.id } } };
    }
    case "MARK_ALERTED": {
      const cur = state.byProject[action.projectPath];
      if (!cur) return state;
      const alerts = { ...cur.alerts, [action.id]: true };
      return { byProject: { ...state.byProject, [action.projectPath]: { ...cur, alerts } } };
    }
    case "CLEAR_ALERT": {
      const cur = state.byProject[action.projectPath];
      if (!cur || !cur.alerts || !cur.alerts[action.id]) return state;
      const alerts = { ...cur.alerts, [action.id]: false };
      return { byProject: { ...state.byProject, [action.projectPath]: { ...cur, alerts } } };
    }
    case "RENAME_TERMINAL": {
      const cur = state.byProject[action.projectPath];
      if (!cur) return state;
      const terminals = cur.terminals.map((t) => (t.id === action.id ? { ...t, title: action.title } : t));
      return { byProject: { ...state.byProject, [action.projectPath]: { ...cur, terminals } } };
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
): { terminals: TerminalView[]; activeId: string; live: boolean } {
  const entry = state.byProject[projectPath];
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
  const saved = loadProjectTerminals(projectPath);
  return { terminals: saved?.terminals ?? [], activeId: saved?.activeId ?? "", live: false };
}

interface TerminalContextType {
  state: TerminalStoreState;
  /** Idempotent: restores this project's persisted terminals (or spawns one
   *  fresh terminal if none were persisted) and marks it live. No-op if
   *  already live. */
  activate: (projectPath: string) => void;
  /** Ensures the project is live (seeding from disk first if it wasn't yet
   *  live), then appends and activates one new terminal. */
  openTerminal: (projectPath: string) => void;
  /** Closes the given terminal for a project. Returns `false` (and is a no-op)
   *  when that terminal is not currently open in this window's live state, so a
   *  repeated close of an already-removed terminal does not fall through. */
  closeTerminal: (projectPath: string, id: string) => boolean;
  /** Ensures the project is live, then sets its active id (a terminal id or
   *  PROCESSES_TAB_ID). */
  setActiveId: (projectPath: string, id: string) => void;
  renameTerminal: (projectPath: string, id: string, title: string) => void;
  /** Marks a terminal as having emitted a bell/notification while backgrounded. */
  markAlerted: (projectPath: string, id: string) => void;
  /** Clears a terminal's background-activity badge. */
  clearAlert: (projectPath: string, id: string) => void;
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
    (projectPath: string) => {
      if (store.state.byProject[projectPath]?.live) return;
      const saved = loadProjectTerminals(projectPath);
      if (saved) {
        bumpSeqPast(saved.terminals.map((t) => t.title));
        const activeId =
          saved.activeId &&
          (saved.activeId === PROCESSES_TAB_ID || saved.terminals.some((t) => t.id === saved.activeId))
            ? saved.activeId
            : saved.terminals[saved.terminals.length - 1].id;
        dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, terminals: saved.terminals, activeId });
        return;
      }
      const term = newTerminal();
      dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, terminals: [term], activeId: term.id });
    },
    [store, dispatch],
  );

  const openTerminal = useCallback(
    (projectPath: string) => {
      const cur = store.state.byProject[projectPath];
      const baseTerminals = cur?.live ? cur.terminals : loadProjectTerminals(projectPath)?.terminals ?? [];
      bumpSeqPast(baseTerminals.map((t) => t.title));
      const term = newTerminal();
      dispatch({
        type: "SET_PROJECT_TERMINALS",
        projectPath,
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
    (projectPath: string, id: string): boolean => {
      const cur = store.state.byProject[projectPath];
      if (!cur?.live || !cur.terminals.some((t) => t.id === id)) return false;
      dispatch({ type: "CLOSE_TERMINAL", projectPath, id });
      killTerminalShell(id);
      return true;
    },
    [store, dispatch],
  );

  const setActiveId = useCallback(
    (projectPath: string, id: string) => {
      activate(projectPath);
      dispatch({ type: "SET_ACTIVE_ID", projectPath, id });
    },
    [activate, dispatch],
  );

  const renameTerminal = useCallback(
    (projectPath: string, id: string, title: string) =>
      dispatch({ type: "RENAME_TERMINAL", projectPath, id, title }),
    [dispatch],
  );

  const markAlerted = useCallback(
    (projectPath: string, id: string) => dispatch({ type: "MARK_ALERTED", projectPath, id }),
    [dispatch],
  );

  const clearAlert = useCallback(
    (projectPath: string, id: string) => dispatch({ type: "CLEAR_ALERT", projectPath, id }),
    [dispatch],
  );

  // Persist every live project's terminals/activeId (debounced), mirroring
  // TerminalTabs.tsx's original per-project persistence effect.
  const skipNextSaveRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const timers: ReturnType<typeof setTimeout>[] = [];
    for (const [projectPath, entry] of Object.entries(state.byProject)) {
      if (!entry.live) continue;
      if (skipNextSaveRef.current.has(projectPath)) {
        skipNextSaveRef.current.delete(projectPath);
        continue;
      }
      timers.push(setTimeout(() => saveProjectTerminals(projectPath, entry.terminals, entry.activeId), 200));
    }
    return () => timers.forEach(clearTimeout);
  }, [state.byProject]);

  // Flush any pending debounced save synchronously when the provider unmounts
  // (e.g. a test remount, or the app tearing down) so a live project's terminal
  // layout is never lost because its 200ms timer was still pending. Reads the
  // live store state directly so the latest terminals/activeId are written.
  useEffect(() => {
    return () => {
      for (const [projectPath, entry] of Object.entries(store.state.byProject)) {
        if (entry.live) saveProjectTerminals(projectPath, entry.terminals, entry.activeId);
      }
    };
  }, [store]);

  // Cross-window sync: another window's terminal open/close/rename updates
  // this window's already-live projects too. A project this window never
  // activated stays a cheap peek and picks up the change next render.
  useEffect(() => {
    const handler = (e: StorageEvent) => {
      if (e.key !== "ocode.ui.terminals.project.v1") return;
      for (const projectPath of Object.keys(store.state.byProject)) {
        const cur = store.state.byProject[projectPath];
        if (!cur.live) continue;
        const saved = loadProjectTerminals(projectPath);
        if (!saved) {
          if (cur.terminals.length !== 0) {
            skipNextSaveRef.current.add(projectPath);
            dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, terminals: [], activeId: "" });
          }
          continue;
        }
        const same =
          saved.terminals.length === cur.terminals.length &&
          saved.terminals.every((t, i) => t.id === cur.terminals[i]?.id && t.title === cur.terminals[i]?.title) &&
          saved.activeId === cur.activeId;
        if (same) continue;
        bumpSeqPast(saved.terminals.map((t) => t.title));
        skipNextSaveRef.current.add(projectPath);
        const activeId =
          saved.activeId &&
          (saved.activeId === PROCESSES_TAB_ID || saved.terminals.some((t) => t.id === saved.activeId))
            ? saved.activeId
            : (saved.terminals[saved.terminals.length - 1]?.id ?? "");
        dispatch({ type: "SET_PROJECT_TERMINALS", projectPath, terminals: saved.terminals, activeId });
      }
    };
    window.addEventListener("storage", handler);
    return () => window.removeEventListener("storage", handler);
  }, [store, dispatch]);

  return (
    <TerminalContext.Provider value={{ state, activate, openTerminal, closeTerminal, setActiveId, renameTerminal, markAlerted, clearAlert }}>
      {children}
    </TerminalContext.Provider>
  );
}

export function useTerminalState() {
  const ctx = useContext(TerminalContext);
  if (!ctx) throw new Error("useTerminalState must be used within TerminalProvider");
  return ctx;
}
