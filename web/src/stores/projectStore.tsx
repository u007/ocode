import { createContext, useContext, useReducer, useEffect, useCallback, type ReactNode } from "react";
import { api } from "../api/client";
import type { Project, SessionInfo } from "../api/types";

export interface Tab {
  id: string; // session ID
  projectPath: string;
  title: string;
}

interface ProjectState {
  projects: Project[];
  loading: boolean;
  activeProject: Project | null;
  projectSessions: SessionInfo[];
  sessionsLoading: boolean;
  tabs: Tab[];
  activeTabId: string | null;
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
  | { type: "UPDATE_TAB_TITLE"; id: string; title: string };

const initialState: ProjectState = {
  projects: [],
  loading: false,
  activeProject: null,
  projectSessions: [],
  sessionsLoading: false,
  tabs: [],
  activeTabId: null,
};

function projectReducer(state: ProjectState, action: ProjectAction): ProjectState {
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
      // Don't duplicate
      if (state.tabs.find((t) => t.id === action.tab.id && t.projectPath === action.tab.projectPath)) {
        return { ...state, activeTabId: action.tab.id };
      }
      return { ...state, tabs: [...state.tabs, action.tab], activeTabId: action.tab.id };
    }
    case "REMOVE_TAB": {
      const newTabs = state.tabs.filter((t) => t.id !== action.id);
      let newActive = state.activeTabId;
      if (state.activeTabId === action.id) {
        newActive = newTabs.length > 0 ? newTabs[newTabs.length - 1].id : null;
      }
      return { ...state, tabs: newTabs, activeTabId: newActive };
    }
    case "SET_ACTIVE_TAB":
      return { ...state, activeTabId: action.id };
    case "UPDATE_TAB_TITLE": {
      return {
        ...state,
        tabs: state.tabs.map((t) =>
          t.id === action.id ? { ...t, title: action.title } : t
        ),
      };
    }
    default:
      return state;
  }
}

interface ProjectContextType {
  state: ProjectState;
  dispatch: React.Dispatch<ProjectAction>;
  refreshProjects: () => Promise<void>;
  selectProject: (project: Project) => Promise<void>;
  openSessionTab: (sessionId: string, sessionTitle: string) => void;
  closeSessionTab: (sessionId: string) => void;
  addProject: (path: string) => Promise<void>;
  removeProject: (path: string) => Promise<void>;
}

const ProjectContext = createContext<ProjectContextType | null>(null);

export function ProjectProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(projectReducer, initialState);

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
  }, []);

  const openSessionTab = useCallback((sessionId: string, sessionTitle: string) => {
    const tab: Tab = {
      id: sessionId,
      projectPath: state.activeProject?.path || "",
      title: sessionTitle || sessionId,
    };
    dispatch({ type: "ADD_TAB", tab });
  }, [state.activeProject]);

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

  // Load projects on mount
  useEffect(() => {
    refreshProjects();
  }, [refreshProjects]);

  return (
    <ProjectContext.Provider
      value={{
        state,
        dispatch,
        refreshProjects,
        selectProject,
        openSessionTab,
        closeSessionTab,
        addProject,
        removeProject,
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
