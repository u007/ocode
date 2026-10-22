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

/**
 * Returns `path` relative to `root` when it lies inside it, else null.
 *
 * Used to open a granted file: the server contains a path against the named
 * root, so the tab must be opened RELATIVE to the granted root rather than by
 * its absolute path.
 *
 * A plain `path.slice(root.length)` is wrong here — `root=/a/b` would match
 * `/a/bc/d` — so the boundary separator is required. Both separators are
 * accepted because a Windows host reports `C:\Users\x`.
 */
export function relativeToRoot(path: string, root: string): string | null {
  if (!path || !root) return null;
  const windows = isWindowsStylePath(root);
  const fold = (s: string) => (windows ? s.toLowerCase() : s);
  const p = splitPath(path, windows);
  const r = splitPath(root, windows);
  if (!p.absolute || !r.absolute || fold(p.anchor) !== fold(r.anchor)) return null;
  if (r.segments.length > p.segments.length) return null;
  for (let i = 0; i < r.segments.length; i++) {
    if (fold(p.segments[i]) !== fold(r.segments[i])) return null;
  }
  const rest = p.segments.slice(r.segments.length);
  return rest.length === 0 ? "." : rest.join("/");
}

/** A Windows-style root: a drive letter ("C:\..." or "C:/...") or a UNC share
 *  ("\\server\share"). Only these fold case; a POSIX root stays case-sensitive. */
function isWindowsStylePath(p: string): boolean {
  return /^[a-zA-Z]:(?:[\\/]|$)/.test(p) || p.startsWith("\\\\");
}

interface PathParts {
  /** Drive ("C:") or UNC marker ("//"), or "" for a POSIX path. */
  anchor: string;
  absolute: boolean;
  /** Normalised components: no empty, "." or ".." entries. */
  segments: string[];
}

/** Splits a path into anchor, absoluteness and normalised components. Mixed
 *  separators are folded to "/" for Windows-style paths only, so a backslash
 *  in a POSIX filename stays literal. */
function splitPath(input: string, windows: boolean): PathParts {
  let s = windows ? input.replace(/\\/g, "/") : input;
  let anchor = "";
  let absolute = s.startsWith("/");
  if (windows) {
    const drive = /^[a-zA-Z]:/.exec(s);
    if (drive) {
      anchor = drive[0].toUpperCase();
      s = s.slice(2);
      absolute = s.startsWith("/");
    } else if (s.startsWith("//")) {
      anchor = "//";
      absolute = true;
      s = s.slice(2);
    }
  }
  return { anchor, absolute, segments: normalizeSegments(s, absolute) };
}

/** Resolves "." and ".." lexically. An absolute path clamps ".." at its root
 *  (as the OS does); a relative path keeps a leading ".." so it can never match
 *  a root. */
function normalizeSegments(s: string, absolute: boolean): string[] {
  const out: string[] = [];
  for (const seg of s.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") {
      const top = out[out.length - 1];
      if (top !== undefined && top !== "..") {
        out.pop();
      } else if (!absolute) {
        out.push("..");
      }
      continue;
    }
    out.push(seg);
  }
  return out;
}
