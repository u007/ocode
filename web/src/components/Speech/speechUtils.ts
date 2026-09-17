export function sanitizeSpeechText(text: string) {
  return text
    .replace(/\u001b\][^\u0007]*(?:\u0007|\u001b\\)/g, "")
    .replace(/\u001b(?:\[[0-?]*[ -/]*[@-~]|[@-_])/g, "")
    .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, "")
    .trim();
}

/**
 * Computes the next playback mode in the two-state cycle:
 * manual ↔ at-bottom. Any unknown or legacy value (e.g. "auto")
 * normalizes to "at-bottom" on click, never sending "auto" to the server
 * which does not accept it.
 */
export function nextSpeechMode(current: string | undefined): "manual" | "at-bottom" {
  return current === "at-bottom" ? "manual" : "at-bottom";
}

// Block-level elements the chat markdown renderer can emit. `Element.textContent`
// concatenates text nodes with no separator, so without an explicit break
// "<p>one</p><p>two</p>" is spoken as "onetwo", a heading runs into the paragraph
// after it, and table cells merge. Inserting a break at these boundaries is what
// makes the extracted text line up with what the user actually sees.
const SPEECH_BLOCK_TAGS = new Set([
  "ADDRESS", "ARTICLE", "ASIDE", "BLOCKQUOTE", "BR", "DD", "DIV", "DL", "DT",
  "FIELDSET", "FIGCAPTION", "FIGURE", "FOOTER", "FORM", "H1", "H2", "H3", "H4",
  "H5", "H6", "HEADER", "HR", "LI", "MAIN", "NAV", "OL", "P", "PRE",
  "SECTION", "TABLE", "TBODY", "TD", "TFOOT", "TH", "THEAD", "TR", "UL",
]);

// Nodes that never contribute speech: control affordances (the per-message Speak
// button opts out via `data-speech-exclude`), decorative icons, and non-visual
// tags. Numeric nodeType values are used so this works without the global Node
// constructor (jsdom test setups that don't expose it).
function isSpeechHidden(el: Element) {
  return (
    el.hasAttribute("data-speech-exclude") ||
    el.getAttribute("aria-hidden") === "true" ||
    el.tagName === "SCRIPT" ||
    el.tagName === "STYLE" ||
    el.tagName === "SVG" ||
    el.tagName === "NOSCRIPT" ||
    el.tagName === "TEMPLATE"
  );
}

/**
 * Extracts speech-ready plain text from a RENDERED markdown subtree.
 *
 * Speech must read what the user sees, not the raw markdown source: heading
 * hashes, emphasis markers, backticks and link targets are rendering details
 * that would otherwise be read aloud verbatim. Walking the rendered DOM yields
 * exactly the text the browser paints (markdown syntax is already gone) and
 * keeps working if the renderer's plugins change — a source-level markdown
 * stripper would silently drift from what is on screen.
 *
 * Returns "" for a null/absent root so callers can fall back or no-op.
 */
export function renderedSpeechText(root: Node | null | undefined): string {
  if (!root) return "";
  const parts: string[] = [];
  const walk = (node: Node) => {
    if (node.nodeType === 3 /* TEXT_NODE */) {
      parts.push(node.nodeValue ?? "");
      return;
    }
    if (node.nodeType !== 1 /* ELEMENT_NODE */) return;
    const el = node as Element;
    if (isSpeechHidden(el)) return;
    const block = SPEECH_BLOCK_TAGS.has(el.tagName);
    if (block) parts.push("\n");
    for (const child of Array.from(el.childNodes)) walk(child);
    if (block) parts.push("\n");
  };
  walk(root);
  return sanitizeSpeechText(
    parts
      .join("")
      .replace(/[^\S\n]+/g, " ")
      .replace(/\s*\n\s*/g, "\n")
      .replace(/\n{2,}/g, "\n"),
  );
}

/**
 * Speech text for every rendered markdown block under `root`, in visual order
 * (the transcript list's DOM order). Backs "Speak visible" — the transcript is
 * virtualized, so the mounted `[data-speech-content]` nodes are exactly the
 * rows on screen.
 */
export function renderedSpeechTexts(root: ParentNode | null | undefined, maxLength = 100_000): string {
  if (!root) return "";
  return Array.from(root.querySelectorAll("[data-speech-content]"))
    .map((node) => renderedSpeechText(node))
    .filter(Boolean)
    .join("\n")
    .slice(0, maxLength);
}

/**
 * Speech text for the LAST rendered markdown block under `root` — the
 * just-completed assistant message while the view is pinned to the bottom.
 * Backs the "at-bottom" auto-speak; "" when no rendered block is mounted, so
 * the caller can decide whether to fall back to the raw source.
 */
export function lastRenderedSpeechText(root: ParentNode | null | undefined): string {
  if (!root) return "";
  const nodes = root.querySelectorAll("[data-speech-content]");
  return nodes.length ? renderedSpeechText(nodes[nodes.length - 1]) : "";
}

export function chunkSpeechText(text: string, maxLength = 240) {
  const normalized = sanitizeSpeechText(text);
  if (!normalized || maxLength <= 0) return [];
  const chunks: string[] = [];
  let remaining = normalized;
  while (remaining.length > maxLength) {
    let cut = remaining.lastIndexOf(" ", maxLength);
    if (cut < Math.floor(maxLength / 2)) cut = maxLength;
    chunks.push(remaining.slice(0, cut).trim());
    remaining = remaining.slice(cut).trim();
  }
  if (remaining) chunks.push(remaining);
  return chunks;
}
