import { createContext, useContext, useReducer, useEffect, useCallback, useRef, type ReactNode } from "react";
import { api } from "../api/client";
import type { Project, SessionInfo } from "../api/types";

export interface Tab {
  id: string; // session ID (or `new-<ts>` temp ID before first message)
  projectPath: string;
  title: string;
}

interface ProjectState {
  projects: Project[];
  loading: boolean;
  activeProject: Project | null;
  projectSessions: SessionInfo[];
  sessionsLoading: boolean;
  /** Open tabs per project path (canonical). Never contains `new-*` temp tabs
   *  after persistence; those live in memory only. */
  tabsByProject: Record<string, Tab[]>;
  activeTabByProject: Record<string, string | null>;
  sessionPickerOpen: boolean;
  /** Persisted-tab restore bookkeeping: set once restore ran so the deep-link
   *  opener doesn't race it. */
  tabsRestored: boolean;
}

type ProjectAction =
  | { type: "SET_PROJECTS"; projects: Project[] }
  | { type: "SET_LOADING"; loading: boolean }
  | { type: "SET_ACTIVE_PROJECT"; project: Project | null }
  | { type: "SET_PROJECT_SESSIONS"; sessions: SessionInfo[] }
  | { type: "SET_SESSIONS_LOADING"; loading: boolean }
  | { type: "ADD_TAB"; tab: Tab }
  | { type: "REMOVE_TAB"; id: string }
  | { type: "SET_ACTIVE_TAB"; id: string | null }
  | { type: "UPDATE_TAB_TITLE"; id: string; title: string }
  | { type: "UPDATE_TAB_ID"; oldId: string; newId: string; newTitle?: string }
  | { type: "RESTORE_TABS"; tabsByProject: Record<string, Tab[]>; activeTabByProject: Record<string, string | null> }
  | { type: "ENSURE_NEW_TAB"; path: string }
  | { type: "SET_SESSION_PICKER"; open: boolean };

const initialState: ProjectState = {
  projects: [],
  loading: false,
  activeProject: null,
  projectSessions: [],
  sessionsLoading: false,
  tabsByProject: {},
  activeTabByProject: {},
  sessionPickerOpen: false,
  tabsRestored: false,
};

/** Tabs of the active project (derived so consumers can keep reading `tabs`). */
function activeTabs(state: ProjectState): Tab[] {
  if (!state.activeProject) return [];
  return state.tabsByProject[state.activeProject.path] || [];
}

function activeTabId(state: ProjectState): string | null {
  if (!state.activeProject) return null;
  return state.activeTabByProject[state.activeProject.path] || null;
}

function projectReducer(state: ProjectState, action: ProjectAction): ProjectState {
  const path = state.activeProject?.path || "";
  switch (action.type) {
    case "SET_PROJECTS":
      return { ...state, projects: action.projects, loading: false };
    case "SET_LOADING":
      return { ...state, loading: action.loading };
    case "SET_ACTIVE_PROJECT":
      return { ...state, activeProject: action.project };
    case "SET_PROJECT_SESSIONS":
      return { ...state, projectSessions: action.sessions, sessionsLoading: false };
    case "SET_SESSIONS_LOADING":
      return { ...state, sessionsLoading: action.loading };
    case "ADD_TAB": {
      const key = action.tab.projectPath || path;
      const list = state.tabsByProject[key] || [];
      if (list.find((t) => t.id === action.tab.id)) {
        return {
          ...state,
          activeTabByProject: { ...state.activeTabByProject, [key]: action.tab.id },
        };
      }
      return {
        ...state,
        tabsByProject: { ...state.tabsByProject, [key]: [...list, action.tab] },
        activeTabByProject: { ...state.activeTabByProject, [key]: action.tab.id },
      };
    }
    case "REMOVE_TAB": {
      if (!path) return state;
      const list = (state.tabsByProject[path] || []).filter((t) => t.id !== action.id);
      let newActive = state.activeTabByProject[path] || null;
      if (newActive === action.id) {
        newActive = list.length > 0 ? list[list.length - 1].id : null;
      }
      return {
        ...state,
        tabsByProject: { ...state.tabsByProject, [path]: list },
        activeTabByProject: { ...state.activeTabByProject, [path]: newActive },
      };
    }
    case "SET_ACTIVE_TAB":
      return path ? { ...state, activeTabByProject: { ...state.activeTabByProject, [path]: action.id } } : state;
    case "SET_SESSION_PICKER":
      return { ...state, sessionPickerOpen: action.open };
    case "UPDATE_TAB_TITLE": {
      if (!path) return state;
      const list = (state.tabsByProject[path] || []).map((t) =>
        t.id === action.id ? { ...t, title: action.title } : t
      );
      return { ...state, tabsByProject: { ...state.tabsByProject, [path]: list } };
    }
    case "UPDATE_TAB_ID": {
      const key = path || "";
      const list = state.tabsByProject[key] || [];
      const tab = list.find((t) => t.id === action.oldId);
      if (!tab) return state;
      const newTab = { ...tab, id: action.newId, title: action.newTitle || tab.title };
      return {
        ...state,
        tabsByProject: { ...state.tabsByProject, [key]: list.map((t) => (t.id === action.oldId ? newTab : t)) },
        activeTabByProject: {
          ...state.activeTabByProject,
          [key]: state.activeTabByProject[key] === action.oldId ? action.newId : state.activeTabByProject[key],
        },
      };
    }
    case "RESTORE_TABS":
      return { ...state, tabsByProject: action.tabsByProject, activeTabByProject: action.activeTabByProject, tabsRestored: true };
    case "ENSURE_NEW_TAB": {
      // Every project keeps a "New session" tab available — even when real
      // session tabs exist, so a deep-linked session always opens *beside* a
      // New tab. Idempotent: no-op when the project already has a new- tab.
      // Prepends so the New tab sits first; the active tab is left untouched
      // (a deep-link tab must stay active when this fires after it).
      const list = state.tabsByProject[action.path] || [];
      if (list.some((t) => t.id.startsWith("new-"))) {
        return state;
      }
      const newTab: Tab = {
        id: `new-${Date.now()}`,
        projectPath: action.path,
        title: "New session",
      };
      return {
        ...state,
        tabsByProject: { ...state.tabsByProject, [action.path]: [newTab, ...list] },
        activeTabByProject: {
          ...state.activeTabByProject,
          [action.path]: state.activeTabByProject[action.path] ?? newTab.id,
        },
      };
    }
    default:
      return state;
  }
}

// ── Persistence ────────────────────────────────────────────────────────────
// One versioned key holds every project's open tabs + active tab. Versioned so
// a future shape change can migrate rather than silently drop state. Writes
// are debounced; failures are swallowed (tabs are a convenience, not data).
const STORAGE_KEY = "ocode.ui.tabs.v1";

interface PersistedTabs {
  version: 1;
  projects: Record<string, { tabs: { id: string; title: string }[]; active: string | null }>;
}

function loadPersistedTabs(): { tabsByProject: Record<string, Tab[]>; activeTabByProject: Record<string, string | null> } {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return { tabsByProject: {}, activeTabByProject: {} };
    const parsed = JSON.parse(raw) as PersistedTabs;
    if (!parsed || parsed.version !== 1 || typeof parsed.projects !== "object") {
      return { tabsByProject: {}, activeTabByProject: {} };
    }
    const tabsByProject: Record<string, Tab[]> = {};
    const activeTabByProject: Record<string, string | null> = {};
    for (const [path, entry] of Object.entries(parsed.projects)) {
      if (!entry || !Array.isArray(entry.tabs)) continue;
      const tabs = entry.tabs
        .filter((t) => t && typeof t.id === "string" && !t.id.startsWith("new-"))
        .map((t) => ({ id: t.id, projectPath: path, title: typeof t.title === "string" ? t.title : t.id }));
      if (tabs.length === 0) continue;
      tabsByProject[path] = tabs;
      activeTabByProject[path] = entry.active && tabs.some((t) => t.id === entry.active) ? entry.active : tabs[tabs.length - 1].id;
    }
    return { tabsByProject, activeTabByProject };
  } catch (err) {
    console.error("Failed to load persisted tabs:", err);
    return { tabsByProject: {}, activeTabByProject: {} };
  }
}

function persistTabs(state: ProjectState) {
  if (state.tabsRestored === false) return; // never write before a restore settled
  const projects: PersistedTabs["projects"] = {};
  for (const [path, tabs] of Object.entries(state.tabsByProject)) {
    const real = tabs.filter((t) => !t.id.startsWith("new-"));
    if (real.length === 0) continue;
    projects[path] = {
      tabs: real.map((t) => ({ id: t.id, title: t.title })),
      active: state.activeTabByProject[path] ?? null,
    };
  }
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify({ version: 1, projects } satisfies PersistedTabs));
  } catch (err) {
    console.error("Failed to persist tabs:", err);
  }
}

interface ProjectContextType {
  state: ProjectState;
  /** Derived: tabs of the active project (kept for existing consumers). */
  tabs: Tab[];
  /** Derived: active tab id of the active project. */
  activeTabId: string | null;
  dispatch: React.Dispatch<ProjectAction>;
  refreshProjects: () => Promise<void>;
  selectProject: (project: Project) => Promise<void>;
  openSessionTab: (sessionId: string, sessionTitle: string) => void;
  closeSessionTab: (sessionId: string) => void;
  addProject: (path: string) => Promise<void>;
  removeProject: (path: string) => Promise<void>;
  toggleSessionPicker: () => void;
  /** Opens the project's "New session" tab — activates it if one exists
   *  (auto-ensured by ENSURE_NEW_TAB), otherwise creates it. */
  openNewSessionTab: () => string | null;
  /** Opens a session tab for the active project. If no project is active yet
   *  (boot race), remembers it and applies once a project is selected. */
  openDeepLinkSession: (sessionId: string, sessionTitle?: string) => void;
}

const ProjectContext = createContext<ProjectContextType | null>(null);

export function ProjectProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(projectReducer, initialState);

  // Debounced persistence of tabs + active tab. Runs on every tabs change.
  useEffect(() => {
    const t = setTimeout(() => persistTabs(state), 400);
    return () => clearTimeout(t);
  }, [state.tabsByProject, state.activeTabByProject, state.tabsRestored]);

  // Restore persisted tabs once on mount (before projects load; applied for
  // whatever projects the server reports).
  useEffect(() => {
    const restored = loadPersistedTabs();
    if (restored.tabsByProject && Object.keys(restored.tabsByProject).length > 0) {
      dispatch({ type: "RESTORE_TABS", ...restored });
    } else {
      dispatch({ type: "RESTORE_TABS", tabsByProject: {}, activeTabByProject: {} });
    }
  }, []);

  const refreshProjects = useCallback(async () => {
    dispatch({ type: "SET_LOADING", loading: true });
    try {
      const projects = await api.listProjects();
      dispatch({ type: "SET_PROJECTS", projects });
    } catch (err) {
      console.error("Failed to load projects:", err);
      dispatch({ type: "SET_LOADING", loading: false });
    }
  }, []);

  const selectProject = useCallback(async (project: Project) => {
    dispatch({ type: "SET_ACTIVE_PROJECT", project });
    dispatch({ type: "SET_SESSIONS_LOADING", loading: true });
    try {
      const sessions = await api.listProjectSessions(project.path);
      dispatch({ type: "SET_PROJECT_SESSIONS", sessions });
    } catch (err) {
      console.error("Failed to load project sessions:", err);
      dispatch({ type: "SET_SESSIONS_LOADING", loading: false });
    }
    // Guarantee the project's tab set exists (default "New session" tab).
    dispatch({ type: "ENSURE_NEW_TAB", path: project.path });
  }, []);

  const openSessionTab = useCallback((sessionId: string, sessionTitle: string) => {
    const path = state.activeProject?.path || "";
    const tab: Tab = {
      id: sessionId,
      projectPath: path,
      title: sessionTitle || sessionId,
    };
    dispatch({ type: "ADD_TAB", tab });
  }, [state.activeProject]);

  // Deep-link sessions (desktop -session, /session/:id) may arrive before the
  // active project is resolved. Defer via a pending marker applied when a
  // project becomes active.
  const pendingDeepLink = useCallback(
    (sessionId: string, sessionTitle?: string) => {
      if (state.activeProject) {
        openSessionTab(sessionId, sessionTitle || sessionId);
        return;
      }
      // No active project yet — stash and let selectProject apply it.
      (window as unknown as { __ocodePendingSession?: string }).__ocodePendingSession =
        sessionTitle || sessionId;
    },
    [state.activeProject, openSessionTab],
  );

  const openDeepLinkSession = useCallback(
    (sessionId: string, sessionTitle?: string) => {
      pendingDeepLink(sessionId, sessionTitle);
    },
    [pendingDeepLink],
  );

  const closeSessionTab = useCallback((sessionId: string) => {
    dispatch({ type: "REMOVE_TAB", id: sessionId });
  }, []);

  const addProject = useCallback(async (path: string) => {
    try {
      await api.addProject(path);
      await refreshProjects();
    } catch (err) {
      console.error("Failed to add project:", err);
    }
  }, [refreshProjects]);

  const removeProject = useCallback(async (path: string) => {
    try {
      await api.removeProject(path);
      await refreshProjects();
    } catch (err) {
      console.error("Failed to remove project:", err);
    }
  }, [refreshProjects]);

  const toggleSessionPicker = useCallback(() => {
    dispatch({ type: "SET_SESSION_PICKER", open: !state.sessionPickerOpen });
  }, [state.sessionPickerOpen]);

  const openNewSessionTab = useCallback((): string | null => {
    const path = state.activeProject?.path || "";
    const list = state.tabsByProject[path] || [];
    const existing = list.find((t) => t.id.startsWith("new-"));
    if (existing) {
      dispatch({ type: "SET_ACTIVE_TAB", id: existing.id });
      return existing.id;
    }
    const tempId = `new-${Date.now()}`;
    dispatch({ type: "ADD_TAB", tab: { id: tempId, projectPath: path, title: "New session" } });
    return tempId;
  }, [state.activeProject, state.tabsByProject]);

  // Apply a stashed deep-link session once the first project is selected.
  const appliedPending = useRef(false);
  useEffect(() => {
    if (appliedPending.current || !state.activeProject) return;
    const stashed = (window as unknown as { __ocodePendingSession?: string }).__ocodePendingSession;
    if (stashed) {
      appliedPending.current = true;
      openSessionTab(stashed, stashed);
    }
  }, [state.activeProject, openSessionTab]);

  return (
    <ProjectContext.Provider
      value={{
        state,
        tabs: activeTabs(state),
        activeTabId: activeTabId(state),
        dispatch,
        refreshProjects,
        selectProject,
        openSessionTab,
        closeSessionTab,
        addProject,
        removeProject,
        toggleSessionPicker,
        openNewSessionTab,
        openDeepLinkSession,
      }}
    >
      {children}
    </ProjectContext.Provider>
  );
}

export function useProjectState() {
  const ctx = useContext(ProjectContext);
  if (!ctx) throw new Error("useProjectState must be used within ProjectProvider");
  return ctx;
}
