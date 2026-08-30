import { createContext, useContext, useReducer, useCallback, useMemo, type ReactNode } from "react";

export interface BrowserTab {
  id: string;
  title: string;
}

interface State {
  tabsByProject: Record<string, BrowserTab[]>;
  activeByProject: Record<string, string | null>;
}

type Action =
  | { type: "OPEN"; project: string; id: string }
  | { type: "CLOSE"; project: string; id: string }
  | { type: "RENAME"; project: string; id: string; title: string }
  | { type: "ACTIVATE"; project: string; id: string };

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "OPEN": {
      const tabs = state.tabsByProject[action.project] ?? [];
      return {
        tabsByProject: { ...state.tabsByProject, [action.project]: [...tabs, { id: action.id, title: "New tab" }] },
        activeByProject: { ...state.activeByProject, [action.project]: action.id },
      };
    }
    case "CLOSE": {
      const tabs = (state.tabsByProject[action.project] ?? []).filter((t) => t.id !== action.id);
      const wasActive = state.activeByProject[action.project] === action.id;
      const nextActive = wasActive ? (tabs.length ? tabs[tabs.length - 1].id : null) : state.activeByProject[action.project] ?? null;
      return {
        tabsByProject: { ...state.tabsByProject, [action.project]: tabs },
        activeByProject: { ...state.activeByProject, [action.project]: nextActive },
      };
    }
    case "RENAME": {
      const tabs = (state.tabsByProject[action.project] ?? []).map((t) =>
        t.id === action.id ? { ...t, title: action.title } : t,
      );
      return { ...state, tabsByProject: { ...state.tabsByProject, [action.project]: tabs } };
    }
    case "ACTIVATE":
      return { ...state, activeByProject: { ...state.activeByProject, [action.project]: action.id } };
    default:
      return state;
  }
}

interface Ctx {
  state: State;
  dispatch: React.Dispatch<Action>;
}
const BrowserTabsContext = createContext<Ctx | null>(null);

export function BrowserTabsProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, { tabsByProject: {}, activeByProject: {} });
  const value = useMemo(() => ({ state, dispatch }), [state]);
  return <BrowserTabsContext.Provider value={value}>{children}</BrowserTabsContext.Provider>;
}

let seq = 0;
function newId(): string {
  seq += 1;
  return `b${Date.now().toString(36)}${seq}`;
}

/** Tab-strip identity for browser tabs in one project: {id, title} + the
 *  active pointer. Live page state (URL, history, console) lives in
 *  lib/browserStore keyed `tab:{id}` — this store owns only the strip. */
export function useBrowserTabs(projectPath: string) {
  const ctx = useContext(BrowserTabsContext);
  if (!ctx) throw new Error("useBrowserTabs must be used within BrowserTabsProvider");
  const { state, dispatch } = ctx;

  const tabs = state.tabsByProject[projectPath] ?? [];
  const activeId = state.activeByProject[projectPath] ?? null;

  const openBrowserTab = useCallback(() => {
    const id = newId();
    dispatch({ type: "OPEN", project: projectPath, id });
    return id;
  }, [dispatch, projectPath]);

  const closeBrowserTab = useCallback((id: string) => dispatch({ type: "CLOSE", project: projectPath, id }), [dispatch, projectPath]);
  const renameBrowserTab = useCallback((id: string, title: string) => dispatch({ type: "RENAME", project: projectPath, id, title }), [dispatch, projectPath]);
  const activateBrowserTab = useCallback((id: string) => dispatch({ type: "ACTIVATE", project: projectPath, id }), [dispatch, projectPath]);

  return { tabs, activeId, openBrowserTab, closeBrowserTab, renameBrowserTab, activateBrowserTab };
}
