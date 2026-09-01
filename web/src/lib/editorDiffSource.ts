import type { GitDiffFile, GitStatus } from "@/api/types";

export interface EditorDiffSourceDecision {
  /** Which diff should decorate the file editor for the selected file.
   *  - "git": the working-tree (unstaged) diff — the selected source whenever
   *    the directory is a repo; an empty patch here means "clean tree, no
   *    decorations" and must NOT fall back to the session diff.
   *  - "session": the per-session change diff (agent edits this session) —
   *    used only when the directory is not a git repo.
   *  - "none": nothing to highlight. */
  kind: "git" | "session" | "none";
  patch: string;
}

/**
 * Decides which diff decorates the file editor for `path`.
 *
 * Supersedes the 2026-07-24 design (web-editor-context-diff-design.md) where
 * decorations were always sourced from the per-session change registry: as of
 * 2026-08-31 the file preview highlights the git WORKING-TREE (unstaged)
 * changes, so staged/committed session edits stop lighting up once they leave
 * the working tree. The session diff remains only as the fallback for
 * directories that are not git repositories (where git can't answer).
 *
 * Repo-ness comes from the explicit `is_repo` field on the git status
 * response — a clean repo and a non-repo both report no changes, so relying
 * on branch/has_changes alone would wrongly fall back to session diffs in a
 * clean repo.
 */
export function resolveEditorDiffSource(opts: {
  projectRoot?: string;
  gitStatus?: GitStatus;
  gitFiles: GitDiffFile[];
  path: string;
  sessionPatch: string;
}): EditorDiffSourceDecision {
  const { projectRoot, gitStatus, gitFiles, path, sessionPatch } = opts;
  if (projectRoot && gitStatus?.is_repo) {
    const file = gitFiles.find((f) => f.path === path);
    return { kind: "git", patch: file?.patch ?? "" };
  }
  if (sessionPatch) return { kind: "session", patch: sessionPatch };
  return { kind: "none", patch: "" };
}