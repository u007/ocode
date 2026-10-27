import type { GitDiffFile } from "@/api/types";

/**
 * Cap mirroring the server's `commitMsgMaxDiffBytes` (internal/agent/
 * commit_message.go). Sending more is wasted: the server truncates to it.
 */
export const COMMIT_MESSAGE_DIFF_LIMIT = 8000;

/**
 * truncateCommitMessageDiff caps `text` without splitting a UTF-16 surrogate
 * pair (a lone half becomes a replacement character in the request body).
 */
export function truncateCommitMessageDiff(
  text: string,
  limit = COMMIT_MESSAGE_DIFF_LIMIT,
): string {
  if (text.length <= limit) return text;
  let end = limit;
  const last = text.charCodeAt(end - 1);
  if (last >= 0xd800 && last <= 0xdbff) end -= 1;
  return text.slice(0, end);
}

/**
 * buildCommitMessageDiff assembles the text sent to the AI commit-message
 * generator, mirroring the TUI git tab: the staged patches when anything is
 * staged, otherwise the working-tree patches (which include untracked files).
 * Files whose patch is empty still contribute a header so renames/binary
 * changes are not silently invisible to the model.
 */
export function buildCommitMessageDiff(
  staged: GitDiffFile[],
  unstaged: GitDiffFile[],
): string {
  const files = staged.length > 0 ? staged : unstaged;
  const parts: string[] = [];
  for (const file of files) {
    const patch = file.patch?.trim();
    if (patch) {
      parts.push(patch);
      continue;
    }
    const suffix = file.status ? ` (${file.status})` : "";
    parts.push(`diff --git a/${file.path} b/${file.path}${suffix}`);
  }
  return truncateCommitMessageDiff(parts.join("\n"));
}
