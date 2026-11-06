// DOM helpers shared by the viewers that report what the reader sees
// (markdown, docx, pdf, pptx). Reads geometry only; never mutates the DOM.

export interface VisibleTextResult {
  text: string;
  heading: string | null;
}

const BLOCK_SELECTOR = "h1,h2,h3,h4,h5,h6,p,li,pre,td,th,blockquote,dt,dd,figcaption";
export const HEADING_SELECTOR = "h1,h2,h3,h4,h5,h6";
const HEADING_MAX = 120;
/** A heading whose bottom edge is within this many px of the viewport top counts as above it. */
const ABOVE_SLACK = 8;

/** Vertical overlap only: the viewers scroll on the Y axis. */
function overlapsViewport(rect: DOMRect, view: DOMRect): boolean {
  return rect.bottom > view.top && rect.top < view.bottom;
}

function headingFor(scroller: HTMLElement, view: DOMRect): string | null {
  let above: HTMLElement | null = null;
  let firstInside: HTMLElement | null = null;
  for (const h of scroller.querySelectorAll<HTMLElement>(HEADING_SELECTOR)) {
    const rect = h.getBoundingClientRect();
    if (rect.bottom <= view.top + ABOVE_SLACK) above = h;
    else if (!firstInside && overlapsViewport(rect, view)) firstInside = h;
  }
  const heading = above ?? firstInside;
  if (!heading) return null;
  // One line: a setext "heading" made of a frontmatter block otherwise carries newlines.
  return (heading.textContent ?? "").replace(/\s+/g, " ").trim().slice(0, HEADING_MAX) || null;
}

/**
 * Text of the block elements intersecting the scroller's viewport, in document
 * order, plus the nearest heading at or above the viewport top (or the first
 * visible heading). A block nested in an already-included block is skipped,
 * because the ancestor's text already carries it.
 */
export function collectVisibleText(scroller: HTMLElement, maxChars: number): VisibleTextResult {
  const view = scroller.getBoundingClientRect();
  let text = "";
  let last: HTMLElement | null = null;
  for (const el of scroller.querySelectorAll<HTMLElement>(BLOCK_SELECTOR)) {
    if (last?.contains(el)) continue;
    if (!overlapsViewport(el.getBoundingClientRect(), view)) continue;
    last = el;
    const block = (el.textContent ?? "").trim();
    if (!block) continue;
    text = text ? `${text}\n${block}` : block;
    if (text.length > maxChars) break;
  }
  return { text: text.slice(0, maxChars), heading: headingFor(scroller, view) };
}

/**
 * Trailing throttle: the latest call's arguments run once, `ms` after the first
 * call of a burst. `cancel` drops a pending call (use it on unmount).
 */
export function throttleTrailing<T extends (...a: never[]) => void>(fn: T, ms: number): T & { cancel(): void } {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let pending: Parameters<T> | null = null;
  const run = () => {
    timer = null;
    const args = pending;
    pending = null;
    if (args) fn(...args);
  };
  const throttled = (...args: Parameters<T>) => {
    pending = args;
    if (timer === null) timer = setTimeout(run, ms);
  };
  throttled.cancel = () => {
    if (timer !== null) clearTimeout(timer);
    timer = null;
    pending = null;
  };
  return throttled as unknown as T & { cancel(): void };
}
