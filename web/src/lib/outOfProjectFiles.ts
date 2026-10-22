/**
 * Out-of-project file access for the editor/preview.
 *
 * The server confines the file editor/preview to the project roots, the
 * configured extra allowed paths, and the directories the user has explicitly
 * GRANTED (see internal/server/handler_file_grants.go). A grant is
 * editor-only: it never reaches the agent's permission scope.
 *
 * This module owns the client half of that contract:
 *
 *   - recognising the server's "outside the project root" refusal, so the UI
 *     can offer the grant instead of failing silently;
 *   - turning a user-typed path into a grant + an anchored open.
 */

/** The server's containment refusal message. Kept in one place because the
 *  web has to recognise it in two different shapes (the content endpoint's
 *  JSON body and the save endpoint's). */
export const OUTSIDE_ROOT_ERROR = "outside the project root";

/**
 * Reports whether a server error is the containment refusal — i.e. the file
 * exists but lives outside every root this surface may touch, so the user can
 * grant it.
 *
 * The check is a substring test on the message, not an exact match: the server
 * words the refusal slightly differently per surface ("path is outside the
 * project root" for reads, "path is outside the workspace" for saves made
 * without an anchor). Both mean the same thing to the user.
 */
export function isOutsideRootError(message: string | undefined | null): boolean {
  if (!message) return false;
  const lower = message.toLowerCase();
  return (
    lower.includes(OUTSIDE_ROOT_ERROR) ||
    lower.includes("outside the workspace") ||
    lower.includes("outside the working directory") ||
    lower.includes("not an allowed project root")
  );
}

/** A path that names a location outside the active project. Absolute paths
 *  and home-relative ("~/...") paths are the two forms a user can type. */
export function looksLikeAbsolutePath(path: string): boolean {
  const trimmed = path.trim();
  if (!trimmed) return false;
  if (trimmed.startsWith("~")) return true;
  // Windows drive letters ("C:\...") and UNC paths ("\\server\share").
  if (/^[a-zA-Z]:[\\/]/.test(trimmed)) return true;
  return trimmed.startsWith("/");
}

/** Expands a leading "~" to the user's home directory. The browser cannot
 *  read $HOME, so the server does the expansion — this only normalizes the
 *  shape so the request is well-formed. */
export function normalizeUserPath(path: string): string {
  return path.trim();
}
