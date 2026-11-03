/**
 * DOM find primitives for the rendered preview surfaces (Markdown, Word,
 * HTML-text fallbacks).
 *
 * The PDF viewer has its own index because pdf.js hands it a stable list of
 * text items; rendered DOM has text nodes instead. This module builds the
 * equivalent flat string + per-text-node offsets so a regex match's character
 * offsets map 1:1 onto `Range`s over the live nodes. `useDomFind` turns those
 * ranges into highlight rectangles; nothing here mutates the DOM.
 *
 * Blocks are separated with `\n` so a query can never match across a paragraph
 * boundary (browsers' own find behaves the same way). Inline elements stay
 * contiguous, which keeps a match whole across `<code>`, `<em>` and friends.
 */

export type FindOptions = { caseSensitive: boolean; wholeWord: boolean };
export type TextSegment = { node: Text; start: number; len: number };
export type TextIndex = { text: string; segments: TextSegment[] };
export type TextMatch = { start: number; end: number };

/** Elements whose boundaries break a word for search purposes. */
const BLOCK_TAGS = new Set([
  "ADDRESS",
  "ARTICLE",
  "ASIDE",
  "BLOCKQUOTE",
  "BR",
  "CAPTION",
  "DD",
  "DIV",
  "DL",
  "DT",
  "FIGCAPTION",
  "FIGURE",
  "FOOTER",
  "FORM",
  "H1",
  "H2",
  "H3",
  "H4",
  "H5",
  "H6",
  "HEADER",
  "HR",
  "LI",
  "MAIN",
  "NAV",
  "OL",
  "P",
  "PRE",
  "SECTION",
  "TABLE",
  "TBODY",
  "TD",
  "TFOOT",
  "TH",
  "THEAD",
  "TR",
  "UL",
]);

/** Non-rendered elements: their text must never be searchable. */
const SKIP_TAGS = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE"]);

export function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** Flatten `root` into a searchable string plus the text-node map back into it. */
export function buildTextIndex(root: Node): TextIndex {
  const segments: TextSegment[] = [];
  let text = "";

  const walk = (node: Node): void => {
    if (node.nodeType === 3 /* Node.TEXT_NODE */) {
      const data = (node as Text).data;
      if (!data) return;
      segments.push({ node: node as Text, start: text.length, len: data.length });
      text += data;
      return;
    }
    if (node.nodeType !== 1 /* Node.ELEMENT_NODE */) return;
    const el = node as Element;
    const tag = el.tagName.toUpperCase();
    if (SKIP_TAGS.has(tag)) return;
    const block = BLOCK_TAGS.has(tag);
    if (block) text += "\n";
    for (const child of Array.from(el.childNodes)) walk(child);
    if (block) text += "\n";
  };

  for (const child of Array.from(root.childNodes)) walk(child);
  return { text, segments };
}

/** Every match of `query` in the indexed text. Empty query → no matches. */
export function findMatches(index: TextIndex, query: string, opts: FindOptions): TextMatch[] {
  if (!query) return [];
  const flags = opts.caseSensitive ? "g" : "gi";
  const body = opts.wholeWord ? `\\b(?:${escapeRegExp(query)})\\b` : escapeRegExp(query);
  let re: RegExp;
  try {
    re = new RegExp(body, flags);
  } catch {
    return [];
  }
  const found: TextMatch[] = [];
  for (let m = re.exec(index.text); m; m = re.exec(index.text)) {
    if (m[0].length === 0) {
      re.lastIndex++;
      continue;
    }
    found.push({ start: m.index, end: m.index + m[0].length });
  }
  return found;
}

/** Map a character offset that fell inside a block separator to a real node. */
function pointAt(
  segments: TextSegment[],
  offset: number,
  preferNext: boolean,
): { node: Text; offset: number } | null {
  for (const seg of segments) {
    if (offset >= seg.start && offset < seg.start + seg.len) {
      return { node: seg.node, offset: offset - seg.start };
    }
  }
  if (preferNext) {
    for (const seg of segments) {
      if (seg.start >= offset) return { node: seg.node, offset: 0 };
    }
    const last = segments[segments.length - 1];
    return last ? { node: last.node, offset: last.len } : null;
  }
  let prev: TextSegment | null = null;
  for (const seg of segments) {
    if (seg.start < offset || (offset === 0 && seg.start === 0)) prev = seg;
  }
  return prev ? { node: prev.node, offset: prev.len } : null;
}

/** A live `Range` covering the match, or null when the nodes are gone. */
export function rangeForMatch(index: TextIndex, match: TextMatch): Range | null {
  const start = pointAt(index.segments, match.start, true);
  const end = pointAt(index.segments, match.end, false);
  if (!start || !end) return null;
  try {
    const range = document.createRange();
    range.setStart(start.node, Math.min(start.offset, start.node.data.length));
    range.setEnd(end.node, Math.min(end.offset, end.node.data.length));
    return range;
  } catch {
    return null;
  }
}

export function isEditableTarget(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null;
  if (!el || !el.tagName) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
}

/**
 * Should Ctrl/⌘F be claimed by this preview surface?
 *
 * Several find surfaces coexist (chat transcript, PDF preview, Monaco, now the
 * rendered document previews), each with a window-level shortcut. Claiming the
 * key just because the viewer is mounted would open the chat's bar too, so a
 * preview only takes it when the reader is actually in it: focused, hovering,
 * or holding a text selection inside. The visible Find button stays the
 * discoverable path for keyboard-only readers.
 */
export function previewWantsFindShortcut(root: HTMLElement | null): boolean {
  if (!root) return false;
  // `offsetParent === null` while an inactive tab's pane is CSS-hidden.
  if (root.offsetParent === null) return false;
  const active = document.activeElement;
  if (active && root.contains(active)) return true;
  const selection = window.getSelection?.();
  const anchor = selection?.anchorNode ?? null;
  if (anchor && root.contains(anchor)) return true;
  try {
    if (root.matches(":hover")) return true;
  } catch {
    // Older engines without :hover in matches() — hover is a bonus, not a gate.
  }
  return false;
}
