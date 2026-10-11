import {
  createContext,
  useCallback,
  useContext,
  type ReactNode,
} from "react";
import { useProjectState } from "../stores/projectStore";
import { useTerminalState } from "../stores/terminalStore";
import type { Project } from "../api/types";
import { browserActions } from "./browserStore";
import { sideChatKey } from "./sidePaneState";
import { tabFocusActions } from "./tabFocus";

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
 *
 * `targetPath` is the project the jump landed on, NOT the one the user was
 * looking at before the dashboard. App needs it because the project-switch
 * restore effect runs on the commit AFTER `selectProject` resolves and would
 * otherwise overwrite the jump's destination with the target's saved view.
 */
const PulseJumpContext = createContext<{ exitPulse: (targetPath: string) => void } | null>(null);

export function PulseJumpProvider({
  exitPulse,
  children,
}: {
  exitPulse: (targetPath: string) => void;
  children: ReactNode;
}) {
  return <PulseJumpContext.Provider value={{ exitPulse }}>{children}</PulseJumpContext.Provider>;
}

function usePulseJumpContext(): { exitPulse: (targetPath: string) => void } {
  const ctx = useContext(PulseJumpContext);
  if (!ctx) {
    throw new Error("useJumpToSession/useJumpToPendingAsk must be used within PulseJumpProvider");
  }
  return ctx;
}

function findProject(projects: Project[], projectPath: string, host: string): Project | undefined {
  return projects.find((p) => p.path === projectPath && (p.host || "") === host);
}

/** Shared body: activate the project, open the tab, leave the dashboard.
 *  Returns false when the project is unknown (nothing was changed). */
function useJump(openSidePane: boolean) {
  const { state, selectProject, openSessionTab } = useProjectState();
  const { exitPulse } = usePulseJumpContext();

  return useCallback(
    async (target: JumpTarget) => {
      const project = findProject(state.projects, target.projectPath, target.host);
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
      exitPulse(target.projectPath);
    },
    [state.projects, selectProject, openSessionTab, exitPulse, openSidePane],
  );
}

export interface TerminalJumpTarget {
  /** A LOCAL project path. Pulse terminals come from the server's own process
   *  table, so there is no host to carry. */
  projectPath: string;
  terminalId: string;
  /** The terminal's title, or "" for the placeholder name. */
  title: string;
}

/**
 * Jump to a live terminal of a local project, landing on the terminal tab.
 *
 * Same contract as `useJump`, in this order:
 *   1. selectProject(project)               — the terminal belongs to the active project
 *   2. attachTerminal(...)                  — makes the terminal a tab in this window
 *   3. tabFocusActions.request({kind:"terminal"}) — queues the reveal
 *   4. exitPulse(path)                      — leaves the dashboard
 *
 * The reveal is a queued request, not a direct view change, because App owns
 * `activeView`/`focusedKind`. App's consumer is a passive effect, which runs
 * after exitPulse's `setFocusedKind("chat")` in the same batch, so the terminal
 * reveal wins.
 *
 * attachTerminal returns early for a terminal the project already lists, so it
 * never activates one by itself; App activates the requested id from the request.
 */
export function useJumpToTerminal() {
  const { state, selectProject } = useProjectState();
  const { attachTerminal } = useTerminalState();
  const { exitPulse } = usePulseJumpContext();

  return useCallback(
    async (target: TerminalJumpTarget) => {
      const project = findProject(state.projects, target.projectPath, "");
      if (!project) {
        // Same refusal as useJump: a terminal bound to no project is a tab the
        // user cannot diagnose from what they see.
        console.error(
          `jumpToSession: no local project matches path=${target.projectPath}; not opening terminal ${target.terminalId}`,
        );
        return;
      }
      await selectProject(project);
      attachTerminal(target.projectPath, "", target.terminalId, target.title);
      tabFocusActions.request({
        kind: "terminal",
        projectPath: target.projectPath,
        host: "",
        terminalId: target.terminalId,
      });
      exitPulse(target.projectPath);
    },
    [state.projects, selectProject, attachTerminal, exitPulse],
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
