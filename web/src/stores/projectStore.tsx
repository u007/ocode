import { createContext, useContext, useEffect, useCallback, useRef, type ReactNode } from "react";
import { Store, useSelector } from "@tanstack/react-store";
import { api } from "../api/client";
import type { Project, ProjectGroup, SessionInfo } from "../api/types";

export type SessionSubTabId = "chat" | "agents" | "changes" | "logs" | "status" | "terminal" | "processes";

export interface Tab {
  id: string; // session ID (or `new-<ts>` temp ID before first message)
  projectPath: string;
  title: string;
  activeSubTab: SessionSubTabId;
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
  /** Project groups for sidebar organization. */
  groups: ProjectGroup[];
}

export type ProjectAction =
  | { type: "SET_PROJECTS"; projects: Project[] }
  | { type: "SET_LOADING"; loading: boolean }
  | { type: "SET_ACTIVE_PROJECT"; project: Project | null }
  | { type: "SET_PROJECT_SESSIONS"; sessions: SessionInfo[] }
  | { type: "SET_SESSIONS_LOADING"; loading: boolean }
  | { type: "ADD_TAB"; tab: Tab }
  | { type: "REMOVE_TAB"; id: string }
  | { type: "SET_ACTIVE_TAB"; id: string | null }
  | { type: "SET_TAB_SUB_TAB"; id: string; subTab: SessionSubTabId }
  | { type: "UPDATE_TAB_TITLE"; id: string; title: string }
  | { type: "UPDATE_TAB_ID"; oldId: string; newId: string; newTitle?: string }
  | { type: "RESTORE_TABS"; tabsByProject: Record<string, Tab[]>; activeTabByProject: Record<string, string | null> }
  | { type: "SET_SESSION_PICKER"; open: boolean }
  | { type: "SET_GROUPS"; groups: ProjectGroup[] };
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
  groups: [],
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

/** Finds which project's tab list contains this tab id, regardless of which
 *  project is currently active. Needed because tab rename/rekey can be
 *  triggered by a background session (SessionTabSync, ChatPanel) whose
 *  project isn't the one the user is currently looking at. */
export function findProjectPathForTab(state: ProjectState, tabId: string): string | null {
  for (const [path, list] of Object.entries(state.tabsByProject)) {
    if (list.some((t) => t.id === tabId)) return path;
  }
  return null;
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
    case "SET_TAB_SUB_TAB": {
      const ownerPath = findProjectPathForTab(state, action.id);
      if (!ownerPath) return state;
      const list = state.tabsByProject[ownerPath].map((t) =>
        t.id === action.id ? { ...t, activeSubTab: action.subTab } : t
      );
      return { ...state, tabsByProject: { ...state.tabsByProject, [ownerPath]: list } };
    }
    case "SET_SESSION_PICKER":
      return { ...state, sessionPickerOpen: action.open };
    case "UPDATE_TAB_TITLE": {
      const ownerPath = findProjectPathForTab(state, action.id);
      if (!ownerPath) return state;
      const list = state.tabsByProject[ownerPath].map((t) =>
        t.id === action.id ? { ...t, title: action.title } : t
      );
      return { ...state, tabsByProject: { ...state.tabsByProject, [ownerPath]: list } };
    }
    case "UPDATE_TAB_ID": {
      const ownerPath = findProjectPathForTab(state, action.oldId);
      if (!ownerPath) return state;
      const list = state.tabsByProject[ownerPath];
      const tab = list.find((t) => t.id === action.oldId);
      if (!tab) return state;
      const newTab = { ...tab, id: action.newId, title: action.newTitle || tab.title };
      return {
        ...state,
        tabsByProject: {
          ...state.tabsByProject,
          [ownerPath]: list.map((t) => (t.id === action.oldId ? newTab : t)),
        },
        activeTabByProject: {
          ...state.activeTabByProject,
          [ownerPath]:
            state.activeTabByProject[ownerPath] === action.oldId
              ? action.newId
              : state.activeTabByProject[ownerPath],
        },
      };
    }
    case "RESTORE_TABS":
      return { ...state, tabsByProject: action.tabsByProject, activeTabByProject: action.activeTabByProject, tabsRestored: true };
    case "SET_GROUPS":
      return { ...state, groups: action.groups };
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
  projects: Record<string, { tabs: { id: string; title: string; subTab?: SessionSubTabId }[]; active: string | null }>;
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
        .map((t) => ({
          id: t.id,
          projectPath: path,
          title: typeof t.title === "string" ? t.title : t.id,
          activeSubTab: (t.subTab === "agents" || t.subTab === "changes" || t.subTab === "logs" || t.subTab === "status" || t.subTab === "terminal" || t.subTab === "processes" ? t.subTab : "chat") as SessionSubTabId,
        }));
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
      tabs: real.map((t) => ({ id: t.id, title: t.title, subTab: t.activeSubTab })),
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
  refreshGroups: () => Promise<void>;
  selectProject: (project: Project) => Promise<void>;
  openSessionTab: (sessionId: string, sessionTitle: string) => void;
  closeSessionTab: (sessionId: string) => void;
  addProject: (path: string) => Promise<void>;
  removeProject: (path: string) => Promise<void>;
  renameProject: (path: string, name: string) => Promise<void>;
  reorderProjects: (paths: string[]) => Promise<void>;
  setProjectGroup: (path: string, group: string) => Promise<void>;
  createGroup: (name: string) => Promise<void>;
  deleteGroup: (name: string) => Promise<void>;
  renameGroup: (oldName: string, newName: string) => Promise<void>;
  reorderGroups: (names: string[]) => Promise<void>;
  setGroupCollapsed: (name: string, collapsed: boolean) => Promise<void>;
  toggleSessionPicker: () => void;
  /** Opens the project's "New session" tab — always adds a fresh tab (keeping
   *  the current running one), unless `reuseIfEmpty` is set and the active tab
   *  is an empty `new-*` tab (no draft, no session), in which case it just
   *  activates it. */
  openNewSessionTab: (reuseIfEmpty?: boolean) => string | null;
  /** Opens a session tab for the active project. If no project is active yet
   *  (boot race), remembers it and applies once a project is selected. */
  openDeepLinkSession: (sessionId: string, sessionTitle?: string) => void;
}

const ProjectContext = createContext<ProjectContextType | null>(null);

export function ProjectProvider({ children }: { children: ReactNode }) {
  // Backed by a @tanstack/store Store instance rather than useReducer — same
  // action-dispatch shape (projectReducer + ProjectAction) so the large body
  // of effects/callbacks below (which close over `state` and `dispatch`
  // exactly as useReducer produced them) needed no changes.
  const storeRef = useRef<Store<ProjectState> | null>(null);
  if (!storeRef.current) storeRef.current = new Store(initialState);
  const store = storeRef.current;
  const state = useSelector(store);
  const dispatch = useCallback(
    (action: ProjectAction) => store.setState((prev) => projectReducer(prev, action)),
    [store],
  );

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

  // Keep tabs in sync across same-origin windows/tabs. localStorage's
  // `storage` event fires in every *other* browsing context when one tab
  // writes — without this, opening or closing a chat tab in window A
  // never appears in window B until a full reload.
  useEffect(() => {
    const handler = (e: StorageEvent) => {
      if (e.key !== STORAGE_KEY) return;
      const external = loadPersistedTabs();
      const prev = store.state;
      // Don't clobber state before the initial restore settled.
      if (!prev.tabsRestored) return;
      const mergedByProject: Record<string, Tab[]> = {};
      const mergedActive: Record<string, string | null> = { ...prev.activeTabByProject };
      const allProjects = new Set<string>([
        ...Object.keys(prev.tabsByProject),
        ...Object.keys(external.tabsByProject),
      ]);
      for (const path of allProjects) {
        const local = prev.tabsByProject[path] || [];
        const localNew = local.filter((t: Tab) => t.id.startsWith("new-"));
        const extReal = external.tabsByProject[path] || [];
        if (extReal.length === 0 && localNew.length === 0) continue;
        // Real tabs come from storage; new-* tabs stay local.
        const merged = [...extReal];
        for (const nt of localNew) {
          if (!merged.some((t) => t.id === nt.id)) merged.push(nt);
        }
        if (merged.length > 0) mergedByProject[path] = merged;
        const extActive = external.activeTabByProject[path];
        const localActive = prev.activeTabByProject[path] || null;
        // Keep a local new-* active tab over the external active.
        if (localActive && localActive.startsWith("new-") && localNew.some((t: Tab) => t.id === localActive)) {
          mergedActive[path] = localActive;
        } else if (extActive && merged.some((t) => t.id === extActive)) {
          mergedActive[path] = extActive;
        } else if (localActive && merged.some((t) => t.id === localActive)) {
          mergedActive[path] = localActive;
        } else {
          mergedActive[path] = merged.length > 0 ? merged[merged.length - 1].id : null;
          if (merged.length === 0) delete mergedActive[path];
        }
      }
      // Include projects that only exist in external (new project added elsewhere)
      for (const [path, tabs] of Object.entries(external.tabsByProject)) {
        if (mergedByProject[path]) continue;
        mergedByProject[path] = tabs;
        mergedActive[path] = external.activeTabByProject[path] ?? tabs[tabs.length - 1]?.id ?? null;
      }
      // Remove projects that were deleted externally (no real nor new tabs)
      for (const path of Object.keys(mergedByProject)) {
        if (mergedByProject[path].length === 0) {
          delete mergedByProject[path];
          delete mergedActive[path];
        }
      }
      const prevStr = JSON.stringify({ tbp: prev.tabsByProject, atb: prev.activeTabByProject });
      const nextStr = JSON.stringify({ tbp: mergedByProject, atb: mergedActive });
      if (prevStr !== nextStr) {
        store.setState((s) => ({ ...s, tabsByProject: mergedByProject, activeTabByProject: mergedActive }));
      }
    };
    window.addEventListener("storage", handler);
    return () => window.removeEventListener("storage", handler);
  }, [store]);

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

  const refreshGroups = useCallback(async () => {
    try {
      const groups = await api.listGroups();
      dispatch({ type: "SET_GROUPS", groups });
    } catch (err) {
      console.error("Failed to load groups:", err);
    }
  }, []);

  // Load the saved project list once on mount so the sidebar is populated on
  // startup (previously only add/remove re-fetched, leaving the list empty).
  useEffect(() => {
    refreshProjects();
    refreshGroups();
  }, [refreshProjects, refreshGroups]);

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
    // No auto-ensured "New session" tab: the frontend must not force a tab
    // into existence. A New tab is created only on explicit user action
    // ("+" button, Cmd/Ctrl+N, /new) via openNewSessionTab.
  }, []);

  const openSessionTab = useCallback((sessionId: string, sessionTitle: string) => {
    const path = state.activeProject?.path || "";
    const tab: Tab = {
      id: sessionId,
      projectPath: path,
      title: sessionTitle || sessionId,
      activeSubTab: "chat",
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
    // Terminals are now project-scoped (survive session close) so closing a
    // chat session no longer drops its terminal tabs. Project-scoped
    // terminals are GC'd only when their buffers become orphaned across all
    // projects.
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

  const renameProject = useCallback(async (path: string, name: string) => {
    try {
      await api.renameProject(path, name);
      await refreshProjects();
    } catch (err) {
      console.error("Failed to rename project:", err);
    }
  }, [refreshProjects]);

  const reorderProjects = useCallback(async (paths: string[]) => {
    try {
      await api.reorderProjects(paths);
      await refreshProjects();
    } catch (err) {
      console.error("Failed to reorder projects:", err);
    }
  }, [refreshProjects]);

  const setProjectGroup = useCallback(async (path: string, group: string) => {
    try {
      await api.setProjectGroup(path, group);
      await refreshProjects();
    } catch (err) {
      console.error("Failed to set project group:", err);
    }
  }, [refreshProjects]);

  const createGroup = useCallback(async (name: string) => {
    try {
      await api.createGroup(name);
      await refreshGroups();
    } catch (err) {
      console.error("Failed to create group:", err);
    }
  }, [refreshGroups]);

  const deleteGroup = useCallback(async (name: string) => {
    try {
      await api.deleteGroup(name);
      await refreshGroups();
      await refreshProjects();
    } catch (err) {
      console.error("Failed to delete group:", err);
    }
  }, [refreshGroups, refreshProjects]);

  const renameGroup = useCallback(async (oldName: string, newName: string) => {
    try {
      await api.renameGroup(oldName, newName);
      await refreshGroups();
      await refreshProjects();
    } catch (err) {
      console.error("Failed to rename group:", err);
    }
  }, [refreshGroups, refreshProjects]);

  const reorderGroups = useCallback(async (names: string[]) => {
    try {
      await api.reorderGroups(names);
      await refreshGroups();
    } catch (err) {
      console.error("Failed to reorder groups:", err);
    }
  }, [refreshGroups]);

  const setGroupCollapsed = useCallback(async (name: string, collapsed: boolean) => {
    try {
      await api.setGroupCollapsed(name, collapsed);
      await refreshGroups();
    } catch (err) {
      console.error("Failed to set group collapsed:", err);
    }
  }, [refreshGroups]);

  const toggleSessionPicker = useCallback(() => {
    dispatch({ type: "SET_SESSION_PICKER", open: !state.sessionPickerOpen });
  }, [state.sessionPickerOpen]);

  const openNewSessionTab = useCallback((reuseIfEmpty = false): string | null => {
    const path = state.activeProject?.path || "";
    const activeId = state.activeTabByProject[path] || null;
    // Reuse the active tab when it is a completely empty new-session tab —
    // no point stacking duplicate blank tabs. Otherwise always add a fresh tab
    // and keep the current (running) tab in the bar.
    if (reuseIfEmpty && activeId && activeId.startsWith("new-")) {
      dispatch({ type: "SET_ACTIVE_TAB", id: activeId });
      return activeId;
    }
    const tempId = `new-${Date.now()}`;
    dispatch({ type: "ADD_TAB", tab: { id: tempId, projectPath: path, title: "New session", activeSubTab: "chat" } });
    return tempId;
  }, [state.activeProject, state.activeTabByProject]);

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

  // ── Boot: auto-select the server's working-directory project ─────────────
  // On open the desktop/web UI should land on the current project: the server
  // reports which saved project root matches its cwd (auto-adding it to the
  // sidebar when the cwd is a real project root). We select it once the saved
  // project list has loaded, unless the user (or a deep-link flow) already
  // picked a project.
  const bootAutoSelect = useRef<{ ran: boolean }>({ ran: false });
  const sawLoading = useRef(false);
  const activeProjectRef = useRef<Project | null>(null);
  activeProjectRef.current = state.activeProject;
  useEffect(() => {
    if (state.loading) {
      sawLoading.current = true;
      return;
    }
    if (!sawLoading.current || bootAutoSelect.current.ran) return;
    bootAutoSelect.current.ran = true;
    if (state.activeProject) return; // user already picked one — don't override
    const persistedPaths = Object.keys(store.state.tabsByProject);
    const hasPersistedTabs = persistedPaths.length > 0;
    (async () => {
      try {
        // If the user already has persisted session tabs, restore the project
        // that owns them instead of auto-switching to the server's cwd project.
        // Auto-switching to cwd steals focus to a different project's New tab
        // and is perceived as a random popup on boot. Only the cwd fallback
        // runs on a fresh install (no persisted tabs).
        if (hasPersistedTabs) {
          if (activeProjectRef.current) return;
          await refreshProjects();
          if (activeProjectRef.current) return;
          const fresh = await api.listProjects();
          // Prefer the project that owns the most-recent active tab, else the
          // first persisted path that still exists in the project list.
          const persistedActive = store.state.activeTabByProject;
          let targetPath: string | null = null;
          for (const [path, tabId] of Object.entries(persistedActive)) {
            if (tabId && persistedPaths.includes(path) && fresh.some((p) => p.path === path)) {
              targetPath = path;
              break;
            }
          }
          if (!targetPath) {
            targetPath = persistedPaths.find((p) => fresh.some((fp) => fp.path === p)) ?? null;
          }
          if (targetPath) {
            const match = fresh.find((p) => p.path === targetPath);
            if (match) {
              if (activeProjectRef.current) return;
              selectProject(match);
              return;
            }
          }
          // Persisted project no longer exists (deleted) — fall through to cwd.
        }
        const res = await api.getCurrentProject();
        const proj = res?.project;
        if (!proj) return;
        // The user may have clicked a project while the fetch was in flight.
        if (activeProjectRef.current) return;
        // Refresh so the sidebar shows the project (the server may have just
        // auto-added it), then select the freshest copy.
        if (!hasPersistedTabs) await refreshProjects();
        if (activeProjectRef.current) return;
        const fresh = await api.listProjects();
        const match = fresh.find((p) => p.path === proj.path);
        if (match) selectProject(match);
      } catch (err) {
        console.error("Failed to auto-select current project:", err);
      }
    })();
  }, [state.loading, state.activeProject, refreshProjects, selectProject]);

  return (
    <ProjectContext.Provider
      value={{
        state,
        tabs: activeTabs(state),
        activeTabId: activeTabId(state),
        dispatch,
        refreshProjects,
        refreshGroups,
        selectProject,
        openSessionTab,
        closeSessionTab,
        addProject,
        removeProject,
        renameProject,
        reorderProjects,
        setProjectGroup,
        createGroup,
        deleteGroup,
        renameGroup,
        reorderGroups,
        setGroupCollapsed,
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
