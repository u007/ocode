// Preview pane live-refresh support.
//
// The sidebar PreviewHost renders ONE file; while a chat turn streams, the AI
// may write/edit that same file through the agent tools. The viewer surfaces
// fetch the file ONCE on mount (api.getFileContent / fetchFileRaw), so the
// pane used to keep showing stale content and never scrolled — the "preview
// side pane does not auto scroll" report. The fix subscribes to the existing
// per-session SSE bus (tool_start / tool_result / turn lifecycle) and bumps a
// `revision` counter whenever a mutation tool touches the previewed path;
// viewers refetch on revision change.
//
// This module is pure: extract the mutated paths from a tool frame, decide
// whether a previewed doc is affected. No React, no fetch — easy to table-test.

/** Tool names that (may) mutate file content on disk. */
const MUTATING_TOOLS = new Set([
  "write",
  "edit",
  "multiedit",
  "multi_file_edit",
  "apply_patch",
  "replace_lines",
  "delete",
]);

export function isMutatingTool(name: string): boolean {
  return MUTATING_TOOLS.has(name);
}

/** Best-effort extraction of the file path(s) a mutating tool call targets.
 *  Mirrors the tools' actual argument shapes (internal/tool):
 *  write/edit/replace_lines/delete → `path`; multiedit → `file_path`;
 *  multi_file_edit → `edits[].path`; apply_patch → patch hunks. Unknown or
 *  malformed args yield an empty array (fail quiet, never throw). */
export function mutatedPathsFromToolCall(tool: string, argsJson: string | undefined): string[] {
  if (!argsJson) return [];
  let args: unknown;
  try {
    args = JSON.parse(argsJson);
  } catch {
    return [];
  }
  const out: string[] = [];
  const push = (v: unknown) => {
    if (typeof v === "string" && v.trim()) out.push(v.trim());
  };
  const obj = args as Record<string, unknown> | null;
  if (!obj) return [];
  switch (tool) {
    case "write":
    case "edit":
    case "replace_lines":
    case "delete":
      push(obj.path);
      break;
    case "multiedit":
      push(obj.file_path);
      break;
    case "multi_file_edit": {
      const edits = obj.edits;
      if (Array.isArray(edits)) for (const e of edits) push((e as Record<string, unknown>)?.path);
      break;
    }
    case "apply_patch": {
      const text = obj.patchText;
      if (typeof text === "string") {
        const re = /^\*\*\* (?:Update File|Add File|Delete File): (.+)$/gm;
        let m: RegExpExecArray | null;
        while ((m = re.exec(text)) !== null) push(m[1]);
      }
      break;
    }
    default:
      break;
  }
  return out;
}

/** The side pane stores previewed paths RELATIVE to the project root (the
 *  `preview_open` directive and the file tree both hand repo-relative paths),
 *  while tool-call args can be either relative ("src/x.md") or absolute
 *  ("/Users/me/proj/src/x.md"). Normalize: strip the project root prefix
 *  (if the path lives under it), strip a leading "./", and collapse any
 *  "../" — then compare exactly. */
export function normalizePreviewPath(p: string, projectRoot?: string): string {
  let s = p.trim();
  if (projectRoot && (s === projectRoot || s.startsWith(projectRoot + "/"))) {
    s = s.slice(projectRoot.length + 1) || "";
  }
  // Strip a leading "~/" is NOT attempted: previews are rooted at the project,
  // home-relative previews would resolve to a foreign root anyway.
  const parts: string[] = [];
  for (const seg of s.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") {
      parts.pop();
      continue;
    }
    parts.push(seg);
  }
  return parts.join("/");
}

/** Does the mutation set touch the previewed doc? Both sides are normalized
 *  against the same project root. Empty mutation list → false. */
export function previewPathMutated(
  previewedPath: string,
  mutated: string[],
  projectRoot?: string,
): boolean {
  if (!previewedPath || mutated.length === 0) return false;
  const target = normalizePreviewPath(previewedPath, projectRoot);
  if (!target) return false;
  return mutated.some((m) => normalizePreviewPath(m, projectRoot) === target);
}