import type { PortMapTarget, Project } from "../api/types";

/**
 * Trusted project metadata, resolved only from the latest successful project
 * snapshot. An absent path is deliberately not treated as local: persisted tabs
 * can outlive a project, and connecting them without trusted metadata could
 * turn a previously remote shell into a local one.
 */
export type TrustedProject =
  | { known: true; host?: string; remotePort?: number }
  | { known: false };

/**
 * Resolve terminal routing from the latest successful project snapshot.
 * The terminal store is keyed by path, so a path shared by multiple hosts
 * cannot be routed safely from a path-only persisted tab. Reject ambiguity
 * rather than arbitrarily selecting a remote or local project.
 */
export function getTrustedTerminalProject(
  projects: readonly Pick<Project, "path" | "host" | "remote_port">[],
  projectPath: string,
): TrustedProject {
  const matches = projects.filter((candidate) => candidate.path === projectPath);
  if (matches.length !== 1) return { known: false };
  return { known: true, host: matches[0].host || undefined, remotePort: matches[0].remote_port || undefined };
}

/**
 * The active project as a port-forward target, or null when it is not one.
 *
 * Uses the same single-match trust rule as getTrustedTerminalProject: an
 * ambiguous or absent path yields null rather than guessing a host. WSL is
 * excluded even though it is remote — WSL2 shares the Windows loopback, so the
 * server rejects forwards for it (see internal/server/handler_portmaps.go).
 */
export function remoteForwardTarget(
  projects: readonly Pick<Project, "path" | "host" | "remote_kind">[],
  projectPath: string,
): PortMapTarget | null {
  if (!projectPath) return null;
  const matches = projects.filter((candidate) => candidate.path === projectPath);
  if (matches.length !== 1) return null;
  const project = matches[0];
  if (!project.host) return null;
  if (project.remote_kind === "wsl") return null;
  return { host: project.host, path: project.path };
}
