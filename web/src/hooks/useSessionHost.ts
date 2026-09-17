import { useMemo } from "react";
import { useProjectState, findProjectPathForTab } from "../stores/projectStore";
import { getTrustedTerminalProject } from "../lib/trustedProject";
import type { ProjectState } from "../stores/projectStore";

/**
 * Resolve the SSH/WSL host for a session via the single-match trust rule.
 *
 * `sessionId` — the tab/session to resolve. `opts.fallbackToActive` — when
 * true and no registered tab owns `sessionId`, falls back to the active
 * project's path (the draft-tab path). When false (default), an unknown
 * session genuinely has no host and an omitted id yields `undefined`.
 *
 * Returns `undefined` when:
 * - the session id is unknown / not bound to a tab (and fallback is off)
 * - the project path is ambiguous (saved as both local and remote)
 * - the project is local (no host)
 */
export function resolveSessionHost(
  projectState: ProjectState,
  sessionId?: string,
  opts?: { fallbackToActive?: boolean },
): string | undefined {
  const fallbackToActive = opts?.fallbackToActive ?? false;
  const projectPath = sessionId
    ? findProjectPathForTab(projectState, sessionId) ?? (fallbackToActive ? projectState.activeProject?.path : undefined)
    : (fallbackToActive ? projectState.activeProject?.path : undefined);
  if (!projectPath) return undefined;
  const trusted = getTrustedTerminalProject(projectState.projects, projectPath);
  return trusted.known ? trusted.host : undefined;
}

/**
 * Resolve the SSH/WSL host for a session tab via the single-match trust rule.
 *
 * Returns `undefined` when:
 * - the session id is unknown / not bound to a tab
 * - the project path is ambiguous (saved as both local and remote)
 * - the project is local (no host)
 *
 * Unlike `useChat`'s call site (which passes `{fallbackToActive:true}` for
 * brand-new draft tabs), this never falls back to the active project — an
 * unknown session genuinely has no host.
 */
export function useSessionHost(sessionId?: string): string | undefined {
  const { state: projectState } = useProjectState();

  return useMemo(() => {
    return resolveSessionHost(projectState, sessionId);
  }, [sessionId, projectState]);
}
