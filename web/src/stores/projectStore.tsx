import { createContext, useContext, useEffect, useCallback, useRef, type ReactNode } from "react";
import { Store, useSelector } from "@tanstack/react-store";
import { api } from "../api/client";
import type { Project, ProjectGroup, SessionInfo, ServerProjectTabs } from "../api/types";
import { eventBus } from "../lib/eventBus";

export type SessionSubTabId = "chat" | "agents" | "changes" | "logs" | "status" | "preview";
export type ProjectMetadataStatus = "loading" | "ready" | "error";

export interface Tab {
  id: string; // session ID (or `new-<ts>` temp ID before first message)
  projectPath: string;
  title: string;
  activeSubTab: SessionSubTabId;
  /** Set once the user explicitly renames this tab (double-click). Guards
   *  against a later auto-generated title (status SSE broadcast) silently
   *  overwriting the user's own choice. */
  titleManual?: boolean;
}

export interface ProjectState {
  projects: Project[];
  loading: boolean;
  /** The project list is trusted only after at least one request succeeds. */
  projectsStatus: ProjectMetadataStatus;
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
  | { type: "SET_PROJECTS_ERROR" }
  | { type: "SET_LOADING"; loading: boolean }
  | { type: "SET_ACTIVE_PROJECT"; project: Project | null }
  | { type: "SET_PROJECT_SESSIONS"; sessions: SessionInfo[] }
  | { type: "SET_SESSIONS_LOADING"; loading: boolean }
  | { type: "ADD_TAB"; tab: Tab }
  | { type: "REMOVE_TAB"; id: string }
  | { type: "SET_ACTIVE_TAB"; id: string | null }
  | { type: "SET_TAB_SUB_TAB"; id: string; subTab: SessionSubTabId }
  | { type: "UPDATE_TAB_TITLE"; id: string; title: string; manual?: boolean }
  | { type: "UPDATE_TAB_ID"; oldId: string; newId: string; newTitle?: string }
  | { type: "RESTORE_TABS"; tabsByProject: Record<string, Tab[]>; activeTabByProject: Record<string, string | null> }
  | { type: "SET_SESSION_PICKER"; open: boolean }
  | { type: "SET_GROUPS"; groups: ProjectGroup[] };
const initialState: ProjectState = {
  projects: [],
  loading: false,
  projectsStatus: "loading",
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
      return {
        ...state,
        projects: Array.isArray(action.projects) ? action.projects : [],
        loading: false,
        projectsStatus: "ready",
      };
    case "SET_PROJECTS_ERROR":
      return {
        ...state,
        loading: false,
        // A later refresh failure must not revoke the last trusted metadata
        // snapshot or make live remote terminals look local.
        projectsStatus: state.projectsStatus === "ready" ? "ready" : "error",
      };
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
      // An auto title (e.g. the generated-title status broadcast) must not
      // clobber a title the user explicitly set — only an explicit rename
      // (action.manual) may overwrite another manual title.
      const list = state.tabsByProject[ownerPath].map((t) => {
        if (t.id !== action.id) return t;
        if (t.titleManual && !action.manual) return t;
        return { ...t, title: action.title, titleManual: !!action.manual };
      });
      return { ...state, tabsByProject: { ...state.tabsByProject, [ownerPath]: list } };
    }
    case "UPDATE_TAB_ID": {
      const ownerPath = findProjectPathForTab(state, action.oldId);
      if (!ownerPath) return state;
      const list = state.tabsByProject[ownerPath];
      const tab = list.find((t) => t.id === action.oldId);
      if (!tab) return state;
      const newTab = {
        ...tab,
        id: action.newId,
        title: tab.titleManual ? tab.title : action.newTitle || tab.title,
      };
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
    case "RESTORE_TABS": {
      // The restore is async (server fetch); a deep link or user action may
      // have already opened tabs. Restored tabs come first, locally-added
      // ones that the server doesn't know yet are kept after them.
      const tabsByProject: Record<string, Tab[]> = { ...action.tabsByProject };
      const activeTabByProject: Record<string, string | null> = { ...action.activeTabByProject };
      for (const [path, local] of Object.entries(state.tabsByProject)) {
        const merged = [...(tabsByProject[path] || [])];
        for (const t of local) if (!merged.some((m) => m.id === t.id)) merged.push(t);
        if (merged.length === 0) continue;
        tabsByProject[path] = merged;
        const localActive = state.activeTabByProject[path];
        if (localActive && merged.some((m) => m.id === localActive)) activeTabByProject[path] = localActive;
        else if (!activeTabByProject[path]) activeTabByProject[path] = merged[merged.length - 1].id;
      }
      return { ...state, tabsByProject, activeTabByProject, tabsRestored: true };
    }
    case "SET_GROUPS":
      return { ...state, groups: Array.isArray(action.groups) ? action.groups : [] };
    default:
      return state;
  }
}

// ── Persistence ────────────────────────────────────────────────────────────
// Open tabs + active tab live server-side (GET/PUT /api/tabs → internal/tabs,
// tabs.json under the global data dir), not in localStorage: localStorage is
// per-origin, so a shared Tailscale URL, a second browser, or the desktop
// shell's per-launch random port each saw an empty tab bar. The server is
// the single source of truth; every window converges via the `tabs_changed`
// bus event. Writes are debounced; failures are logged (tabs are a
// convenience, not data).
//
// LEGACY_STORAGE_KEY is the pre-server localStorage key, read once when the
// server has no state yet so existing users keep their tabs, then removed.
const LEGACY_STORAGE_KEY = "ocode.ui.tabs.v1";

interface RestoredTabs {
  tabsByProject: Record<string, Tab[]>;
  activeTabByProject: Record<string, string | null>;
}

const EMPTY_RESTORE: RestoredTabs = { tabsByProject: {}, activeTabByProject: {} };

function toSubTab(v: unknown): SessionSubTabId {
  return (v === "agents" || v === "changes" || v === "logs" || v === "status" || v === "preview" ? v : "chat") as SessionSubTabId;
}

/** Converts the server's `{root: {tabs, active}}` map into store shape,
 *  dropping malformed entries and never-persisted `new-*` tabs. */
function fromServerTabs(projects: Record<string, ServerProjectTabs> | null | undefined): RestoredTabs {
  const tabsByProject: Record<string, Tab[]> = {};
  const activeTabByProject: Record<string, string | null> = {};
  if (!projects || typeof projects !== "object") return { tabsByProject, activeTabByProject };
  for (const [path, entry] of Object.entries(projects)) {
    if (!entry || !Array.isArray(entry.tabs)) continue;
    const tabs = entry.tabs
      .filter((t) => t && typeof t.id === "string" && !t.id.startsWith("new-"))
      .map((t) => ({
        id: t.id,
        projectPath: path,
        title: typeof t.title === "string" ? t.title : t.id,
        activeSubTab: toSubTab(t.sub_tab),
      }));
    if (tabs.length === 0) continue;
    tabsByProject[path] = tabs;
    activeTabByProject[path] = entry.active && tabs.some((t) => t.id === entry.active) ? entry.active : tabs[tabs.length - 1].id;
  }
  return { tabsByProject, activeTabByProject };
}

function toServerTabs(state: ProjectState): Record<string, ServerProjectTabs> {
  const projects: Record<string, ServerProjectTabs> = {};
  for (const [path, tabs] of Object.entries(state.tabsByProject)) {
    const real = tabs.filter((t) => !t.id.startsWith("new-"));
    if (real.length === 0) continue;
    projects[path] = {
      tabs: real.map((t) => ({ id: t.id, title: t.title, sub_tab: t.activeSubTab })),
      active: state.activeTabByProject[path] ?? "",
    };
  }
  return projects;
}

/** One-time read of the pre-server localStorage state (shape
 *  `{version:1, projects:{path:{tabs:[{id,title,subTab}], active}}}`). */
function loadLegacyLocalTabs(): RestoredTabs {
  try {
    const raw = window.localStorage.getItem(LEGACY_STORAGE_KEY);
    if (!raw) return EMPTY_RESTORE;
    const parsed = JSON.parse(raw) as {
      version: number;
      projects: Record<string, { tabs: { id: string; title: string; subTab?: string }[]; active: string | null }>;
    };
    if (!parsed || parsed.version !== 1 || typeof parsed.projects !== "object") return EMPTY_RESTORE;
    const projects: Record<string, ServerProjectTabs> = {};
    for (const [path, entry] of Object.entries(parsed.projects)) {
      if (!entry || !Array.isArray(entry.tabs)) continue;
      projects[path] = {
        tabs: entry.tabs.map((t) => ({ id: t.id, title: t.title, sub_tab: t.subTab })),
        active: entry.active ?? "",
      };
    }
    return fromServerTabs(projects);
  } catch (err) {
    console.error("Failed to load legacy persisted tabs:", err);
    return EMPTY_RESTORE;
  }
}

function clearLegacyLocalTabs() {
  try {
    window.localStorage.removeItem(LEGACY_STORAGE_KEY);
  } catch (err) {
    console.error("Failed to clear legacy persisted tabs:", err);
  }
}

/** Merges tab state fetched from the server into the current state: real
 *  tabs come from the server, local `new-*` tabs stay. Returns null when the
 *  merge would not change anything. */
function mergeExternalTabs(prev: ProjectState, external: RestoredTabs): RestoredTabs | null {
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
  // Remove projects that were deleted externally (no real nor new tabs)
  for (const path of Object.keys(mergedActive)) {
    if (!mergedByProject[path]) delete mergedActive[path];
  }
  const prevStr = JSON.stringify({ tbp: prev.tabsByProject, atb: prev.activeTabByProject });
  const nextStr = JSON.stringify({ tbp: mergedByProject, atb: mergedActive });
  if (prevStr === nextStr) return null;
  return { tabsByProject: mergedByProject, activeTabByProject: mergedActive };
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
  addRemoteProject: (host: string, path: string, port?: number) => Promise<void>;
  updateRemoteProject: (input: {
    old_host: string;
    old_path: string;
    path: string;
    kind: "ssh" | "wsl";
    user?: string;
    host?: string;
    port?: number;
    distro?: string;
  }) => Promise<void>;
  removeProject: (path: string, host?: string) => Promise<void>;
  renameProject: (path: string, name: string, host?: string) => Promise<void>;
  reorderProjects: (refs: Array<{ path: string; host?: string }>) => Promise<void>;
  setProjectGroup: (path: string, group: string, host?: string) => Promise<void>;
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
  openNewSessionTab: (reuseIfEmpty?: boolean, projectPath?: string) => string | null;
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
  const projectsRequestRef = useRef(0);

  // Server sync state shared by the persist, restore, and bus effects below.
  // `dirty` = a debounced write is scheduled, `writing` = a PUT is in flight,
  // `refetchQueued` = a tabs_changed event arrived mid-write; the refetch is
  // deferred until the write lands so the server reply can't clobber newer
  // local state with what it held a moment earlier.
  const syncRef = useRef({ dirty: false, writing: false, refetchQueued: false });

  const applyServerTabs = useCallback(
    (projects: Record<string, ServerProjectTabs>) => {
      const merged = mergeExternalTabs(store.state, fromServerTabs(projects));
      if (merged) store.setState((s) => ({ ...s, ...merged }));
    },
    [store],
  );

  const refetchTabs = useCallback(async () => {
    const sync = syncRef.current;
    if (sync.dirty || sync.writing) {
      sync.refetchQueued = true;
      return;
    }
    try {
      const res = await api.getTabs();
      // A write may have started while the GET was in flight; its completion
      // re-runs this via refetchQueued, so don't apply a now-stale snapshot.
      if (sync.dirty || sync.writing) {
        sync.refetchQueued = true;
        return;
      }
      applyServerTabs(res.projects);
    } catch (err) {
      console.error("Failed to refetch tabs from server:", err);
    }
  }, [applyServerTabs]);

  // Debounced persistence of tabs + active tab to the server. Runs on every
  // tabs change once the initial restore settled.
  useEffect(() => {
    if (!state.tabsRestored) return; // never write before a restore settled
    const sync = syncRef.current;
    sync.dirty = true;
    const t = setTimeout(async () => {
      sync.dirty = false;
      sync.writing = true;
      try {
        await api.setTabs(toServerTabs(store.state));
      } catch (err) {
        console.error("Failed to persist tabs to server:", err);
      } finally {
        sync.writing = false;
        if (sync.refetchQueued && !sync.dirty) {
          sync.refetchQueued = false;
          void refetchTabs();
        }
      }
    }, 400);
    return () => clearTimeout(t);
  }, [state.tabsByProject, state.activeTabByProject, state.tabsRestored, store, refetchTabs]);

  // Restore persisted tabs once on mount (before projects load; applied for
  // whatever projects the server reports). If the server holds nothing yet,
  // the pre-server localStorage state is migrated once; the persist effect
  // then writes it through. A failed fetch leaves tabsRestored false (so no
  // write can wipe the server copy) and retries on the next bus reconnect.
  const restoreTabs = useCallback(async () => {
    try {
      const res = await api.getTabs();
      let restored = fromServerTabs(res.projects);
      if (Object.keys(restored.tabsByProject).length === 0) {
        const legacy = loadLegacyLocalTabs();
        if (Object.keys(legacy.tabsByProject).length > 0) restored = legacy;
      }
      dispatch({ type: "RESTORE_TABS", ...restored });
      clearLegacyLocalTabs();
    } catch (err) {
      console.error("Failed to restore tabs from server (will retry on reconnect):", err);
    }
  }, [dispatch]);

  useEffect(() => {
    void restoreTabs();
  }, [restoreTabs]);

  // Keep tabs in sync across every window on this server — another browser,
  // a shared Tailscale URL, the desktop shell. The server publishes an
  // unscoped `tabs_changed` bus event after each PUT; on it (and on every bus
  // reconnect, which may have missed one) refetch and merge.
  useEffect(() => {
    const onChanged = () => {
      if (!store.state.tabsRestored) return; // initial restore still pending
      void refetchTabs();
    };
    const offEvent = eventBus.on("tabs_changed", onChanged);
    const offReconnect = eventBus.onReconnect(() => {
      if (!store.state.tabsRestored) void restoreTabs();
      else void refetchTabs();
    });
    return () => {
      offEvent();
      offReconnect();
    };
  }, [store, refetchTabs, restoreTabs]);

  const refreshProjects = useCallback(async () => {
    const requestId = ++projectsRequestRef.current;
    dispatch({ type: "SET_LOADING", loading: true });
    try {
      const projects = await api.listProjects();
      if (requestId !== projectsRequestRef.current) return;
      dispatch({ type: "SET_PROJECTS", projects });
    } catch (err) {
      if (requestId !== projectsRequestRef.current) return;
      console.error("Failed to load projects:", err);
      dispatch({ type: "SET_PROJECTS_ERROR" });
    }
  }, [dispatch]);

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
      const sessions = await api.listProjectSessions(project.path, project.host);
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

  const addRemoteProject = useCallback(async (host: string, path: string, port?: number) => {
    try {
      await api.addRemoteProject(host, path, port);
      await refreshProjects();
    } catch (err) {
      console.error("Failed to add remote project:", err);
      throw err;
    }
  }, [refreshProjects]);

  const updateRemoteProject = useCallback(async (input: Parameters<NonNullable<typeof api.updateRemoteProject>>[0]) => {
    const updated = await api.updateRemoteProject(input);
    dispatch({
      type: "SET_PROJECTS",
      projects: state.projects.map((project) =>
        project.host === input.old_host && project.path === input.old_path ? updated : project,
      ),
    });
    const active = state.activeProject;
    if (active?.host === input.old_host && active.path === input.old_path) {
      dispatch({ type: "SET_ACTIVE_PROJECT", project: updated });
    }
    await refreshProjects();
  }, [dispatch, refreshProjects, state.activeProject, state.projects]);

  const removeProject = useCallback(async (path: string, host?: string) => {
    try {
      if (host) {
        await api.removeRemoteProject(path, host);
      } else {
        await api.removeProject(path);
      }
      await refreshProjects();
    } catch (err) {
      console.error("Failed to remove project:", err);
    }
  }, [refreshProjects]);

  const renameProject = useCallback(async (path: string, name: string, host?: string) => {
    try {
      await api.renameProject(path, name, host);
      await refreshProjects();
    } catch (err) {
      console.error("Failed to rename project:", err);
      throw err;
    }
  }, [refreshProjects]);

  const reorderProjects = useCallback(async (refs: Array<{ path: string; host?: string }>) => {
    try {
      await api.reorderProjects(refs);
      await refreshProjects();
    } catch (err) {
      console.error("Failed to reorder projects:", err);
    }
  }, [refreshProjects]);

  const setProjectGroup = useCallback(async (path: string, group: string, host?: string) => {
    try {
      await api.setProjectGroup(path, group, host);
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

  const openNewSessionTab = useCallback((reuseIfEmpty = false, projectPath?: string): string | null => {
    const path = projectPath ?? state.activeProject?.path ?? "";
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
        addRemoteProject,
        updateRemoteProject,
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
