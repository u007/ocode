import {
  createContext,
  useCallback,
  useContext,
  type ReactNode,
} from "react";
import { useProjectState } from "../stores/projectStore";
import type { Project } from "../api/types";
import { browserActions } from "./browserStore";
import { sideChatKey } from "./sidePaneState";

/**
 * jumpToSession — open a session from outside the current project.
 *
 * The Pulse dashboard lists sessions across every project, so a card click has
 * to cross a project boundary. Two store actions do that, and their ORDER is
 * the whole contract:
 *
 *   1. selectProject(project)  — activates the project and restores its tabs
 *   2. openSessionTab(id, title, projectPath) — binds the session to it
 *
 * Doing (2) first would attach the tab to whichever project happened to be
 * active, so the tab would show an empty transcript and bind its API calls to
 * the wrong root. selectProject is async (it revalidates the project list), so
 * the helper must await it rather than assume it settled.
 *
 * Project identity is path + host, never path alone: a local project and a
 * remote one may share a path, and picking the wrong one would send the
 * session's requests to the wrong machine.
 */

export interface JumpTarget {
  projectPath: string;
  /** "" for the local server. v1 of Pulse is local-only, so this is normally
   *  empty — the field exists so the helper does not have to be rewritten when
   *  remote fan-out lands. */
  host: string;
  sessionId: string;
  title: string;
}

/**
 * How the dashboard is left. App owns `activeView`, so the provider supplies the
 * transition rather than this module reaching into it — that keeps the jump
 * helper testable and keeps one owner of the view state.
 */
const PulseJumpContext = createContext<{ exitPulse: () => void } | null>(null);

export function PulseJumpProvider({
  exitPulse,
  children,
}: {
  exitPulse: () => void;
  children: ReactNode;
}) {
  return <PulseJumpContext.Provider value={{ exitPulse }}>{children}</PulseJumpContext.Provider>;
}

function usePulseJumpContext(): { exitPulse: () => void } {
  const ctx = useContext(PulseJumpContext);
  if (!ctx) {
    throw new Error("useJumpToSession/useJumpToPendingAsk must be used within PulseJumpProvider");
  }
  return ctx;
}

function findProject(projects: Project[], target: JumpTarget): Project | undefined {
  return projects.find(
    (p) => p.path === target.projectPath && (p.host || "") === target.host,
  );
}

/** Shared body: activate the project, open the tab, leave the dashboard.
 *  Returns false when the project is unknown (nothing was changed). */
function useJump(openSidePane: boolean) {
  const { state, selectProject, openSessionTab } = useProjectState();
  const { exitPulse } = usePulseJumpContext();

  return useCallback(
    async (target: JumpTarget) => {
      const project = findProject(state.projects, target);
      if (!project) {
        // Refuse loudly: opening a tab for a project the store does not know
        // would create a session bound to no project — a wrong tab the user
        // cannot diagnose from what they see.
        console.error(
          `jumpToSession: no project matches path=${target.projectPath} host=${target.host || "(local)"}; not opening ${target.sessionId}`,
        );
        return;
      }
      await selectProject(project);
      openSessionTab(target.sessionId, target.title, target.projectPath, target.host);
      // The pane is keyed by session id, so it can only be opened once the tab
      // exists — otherwise it would attach to nothing and silently not show.
      if (openSidePane) {
        browserActions.open(sideChatKey(target.sessionId), "");
      }
      exitPulse();
    },
    [state.projects, selectProject, openSessionTab, exitPulse, openSidePane],
  );
}

/** Jump into a session, landing on the chat surface. */
export function useJumpToSession() {
  return useJump(false);
}

/**
 * Jump into a session AND open its side pane, for a card whose whole point is
 * that it is blocked on the user: the ask itself has to be on screen.
 */
export function useJumpToPendingAsk() {
  return useJump(true);
}
