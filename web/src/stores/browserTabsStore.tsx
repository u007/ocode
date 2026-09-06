import { createContext, useContext, useReducer, useCallback, useMemo, type ReactNode } from "react";

export interface BrowserTab {
  id: string;
  title: string;
  /** User-typed rename (double-click). Wins over page titles until the next
   *  top-level navigation clears it. Null when the strip follows the page. */
  manualTitle: string | null;
}

interface State {
  tabsByProject: Record<string, BrowserTab[]>;
  activeByProject: Record<string, string | null>;
}

type Action =
  | { type: "OPEN"; project: string; id: string }
  | { type: "OPEN_RESTORED"; project: string; id: string; title: string; manualTitle: string | null }
  | { type: "CLOSE"; project: string; id: string }
  | { type: "RENAME"; project: string; id: string; title: string }
  | { type: "CLEAR_MANUAL"; project: string; id: string }
  | { type: "ACTIVATE"; project: string; id: string }
  | { type: "RESTORE"; project: string; tabs: BrowserTab[]; activeId: string | null };

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "OPEN": {
      const tabs = state.tabsByProject[action.project] ?? [];
      return {
        tabsByProject: { ...state.tabsByProject, [action.project]: [...tabs, { id: action.id, title: "New tab", manualTitle: null }] },
        activeByProject: { ...state.activeByProject, [action.project]: action.id },
      };
    }
    case "OPEN_RESTORED": {
      const tabs = state.tabsByProject[action.project] ?? [];
      if (tabs.some((t) => t.id === action.id)) return state;
      return {
        tabsByProject: {
          ...state.tabsByProject,
          [action.project]: [...tabs, { id: action.id, title: action.title, manualTitle: action.manualTitle }],
        },
        activeByProject: { ...state.activeByProject, [action.project]: action.id },
      };
    }
    case "RESTORE": {
      return {
        tabsByProject: { ...state.tabsByProject, [action.project]: action.tabs },
        activeByProject: { ...state.activeByProject, [action.project]: action.activeId },
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
        t.id === action.id ? { ...t, title: action.title, manualTitle: action.title } : t,
      );
      return { ...state, tabsByProject: { ...state.tabsByProject, [action.project]: tabs } };
    }
    case "CLEAR_MANUAL": {
      const tabs = (state.tabsByProject[action.project] ?? []).map((t) =>
        t.id === action.id ? { ...t, manualTitle: null } : t,
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
  const clearManualTitle = useCallback((id: string) => dispatch({ type: "CLEAR_MANUAL", project: projectPath, id }), [dispatch, projectPath]);
  const restoreBrowserTabs = useCallback(
    (tabs: BrowserTab[], activeId: string | null) => dispatch({ type: "RESTORE", project: projectPath, tabs, activeId }),
    [dispatch, projectPath],
  );
  const openRestoredBrowserTab = useCallback(
    (id: string, title: string, manualTitle: string | null) =>
      dispatch({ type: "OPEN_RESTORED", project: projectPath, id, title, manualTitle }),
    [dispatch, projectPath],
  );
  const activateBrowserTab = useCallback((id: string) => dispatch({ type: "ACTIVATE", project: projectPath, id }), [dispatch, projectPath]);

  return { tabs, activeId, openBrowserTab, closeBrowserTab, renameBrowserTab, activateBrowserTab, clearManualTitle, restoreBrowserTabs, openRestoredBrowserTab };
}
