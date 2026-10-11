/**
 * Session/tab titles are labels, not transcripts. A title can be mirrored from
 * an oversized first user message (megabytes — e.g. a pasted standup prompt
 * carrying full commit diffs), and the web UI renders it into the tab strip as
 * a `white-space:nowrap; text-overflow:ellipsis` line, which forces the browser
 * to lay out the ENTIRE string on every reflow. That is the difference between
 * ~11ms and ~300ms of layout for one tab. Every title must therefore be bounded
 * before it reaches React/DOM state.
 *
 * This also bounds the WORK: `Array.from(s)` on a multi-megabyte string
 * allocates one array slot per character, so the rune walk must never see more
 * than a bounded prefix.
 */
export const MAX_TITLE_CHARS = 80;

/**
 * Larger cap for the hover tooltip / stored label. The visible tab label is
 * clamped to {@link MAX_TITLE_CHARS}, but a user-renamed or longer title should
 * still be readable on hover. Bounded so a pathological title (e.g. mirrored
 * from an oversized first user message) can never reach the DOM unbounded.
 */
export const MAX_TITLE_TOOLTIP_CHARS = 300;

/**
 * Collapse a title to a single line and truncate to `maxLen` characters with an
 * ellipsis. Safe (and cheap) for multi-megabyte input: work is bounded to a
 * prefix before any per-character expansion.
 */
export function truncateTitle(s: string, maxLen: number = MAX_TITLE_CHARS): string {
  if (!s) return s;
  if (maxLen <= 0) return "";
  // Fast path: nothing to cut. Still collapse newlines so the label stays on
  // one line, but avoid Array.from on the whole string.
  if (s.length <= maxLen) return s.replace(/[\r\n]+/g, " ").trim();
  // A rune is at most 2 UTF-16 code units, so 2*maxLen units always contains at
  // least maxLen runes — enough to produce the truncated label without
  // materializing the rest of the string.
  const window = s.slice(0, maxLen * 2).replace(/[\r\n]+/g, " ").trim();
  const runes = Array.from(window);
  if (runes.length <= maxLen) return runes.join("");
  const keep = Math.max(0, maxLen - 3);
  return runes.slice(0, keep).join("") + "...";
}
