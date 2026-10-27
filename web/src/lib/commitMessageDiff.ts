import type { GitDiffFile } from "@/api/types";

/**
 * Cap in UTF-8 bytes, mirroring the server's `commitMsgMaxDiffBytes`
 * (internal/agent/commit_message.go). Sending more is wasted: the server
 * truncates to it.
 */
export const COMMIT_MESSAGE_DIFF_LIMIT = 8000;

const utf8Encoder = new TextEncoder();
const utf8Decoder = new TextDecoder();

/**
 * truncateCommitMessageDiff caps `text` to at most `limit` UTF-8 bytes, the same
 * unit the server cuts on. The cut never splits a multi-byte character: it backs
 * up over continuation bytes to the character's lead byte.
 */
export function truncateCommitMessageDiff(
  text: string,
  limit = COMMIT_MESSAGE_DIFF_LIMIT,
): string {
  // A UTF-16 code unit encodes to at most 3 UTF-8 bytes, so this fast path
  // skips the encode for any text that is certainly within the limit.
  if (text.length * 3 <= limit) return text;
  const bytes = utf8Encoder.encode(text);
  if (bytes.length <= limit) return text;
  let end = limit;
  while (end > 0 && (bytes[end] & 0xc0) === 0x80) end -= 1;
  return utf8Decoder.decode(bytes.subarray(0, end));
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
