// Agent control for the in-document viewers: DOM scroll/select helpers, the
// find-command mapping shared by every find engine, and the find-state report.
// Reads and selects; never mutates rendered content.

import { useEffect } from "react";
import { HEADING_SELECTOR } from "../../lib/viewerDom";
import type { ViewerController, ViewerFindCommand, ViewerFindState } from "../../lib/viewerContext";
import { buildTextIndex, findMatches, rangeForMatch } from "./findDom";

/** Collapse whitespace and lowercase, so a needle matches however the source wraps it. */
export function normalizeMatchText(s: string): string {
  return s.replace(/\s+/g, " ").trim().toLowerCase();
}

/**
 * First heading under `root` whose text contains `heading` (case-insensitive,
 * whitespace-collapsed), or null. An empty heading throws: it would match the
 * first heading silently.
 */
export function findHeading(root: HTMLElement, heading: string): HTMLElement | null {
  const want = normalizeMatchText(heading);
  if (want === "") throw new Error("heading must not be empty");
  for (const h of Array.from(root.querySelectorAll<HTMLElement>(HEADING_SELECTOR))) {
    if (normalizeMatchText(h.textContent ?? "").includes(want)) return h;
  }
  return null;
}

/**
 * Range of the first case-insensitive occurrence of `query` in the text under
 * `root`, or null when absent. An empty query throws.
 */
export function firstMatchRange(root: HTMLElement, query: string): Range | null {
  if (query === "") throw new Error("text must not be empty");
  const index = buildTextIndex(root);
  const [first] = findMatches(index, query, { caseSensitive: false, wholeWord: false });
  if (!first) return null;
  return rangeForMatch(index, first);
}

/** Makes `range` the browser selection and scrolls its start into view. */
export function selectRange(range: Range): void {
  const sel = window.getSelection();
  if (!sel) throw new Error("window selection is unavailable");
  sel.removeAllRanges();
  sel.addRange(range);
  const node = range.startContainer;
  const el = node.nodeType === Node.ELEMENT_NODE ? (node as Element) : node.parentElement;
  if (!el) throw new Error("selected match has no element to scroll to");
  el.scrollIntoView({ block: "center" });
}

/** The engine surface every find bar exposes (DOM, PDF and iframe engines alike). */
export interface FindEngine {
  active: boolean;
  count: number;
  show(): void;
  hide(): void;
  setQuery(query: string): void;
  go(dir: 1 | -1): void;
}

/**
 * Maps an agent find command onto a find engine. `open` shows the bar and
 * sets the query; `next`/`prev` need an open bar with matches, and fail loudly
 * otherwise (a silent no-op would read as success); `close` is idempotent.
 */
export function runFindCommand(engine: FindEngine, cmd: ViewerFindCommand): boolean {
  switch (cmd.op) {
    case "open":
      if (cmd.query === "") throw new Error("find query must not be empty");
      engine.show();
      engine.setQuery(cmd.query);
      return true;
    case "next":
    case "prev":
      if (!engine.active) throw new Error("find is not open");
      if (engine.count === 0) throw new Error("no matches to step through");
      engine.go(cmd.op === "next" ? 1 : -1);
      return true;
    case "close":
      engine.hide();
      return true;
  }
}

/** Reported find state: null with the bar closed, else query, count and 1-based current (0 = none). */
export function findStateFor(f: { active: boolean; query: string; count: number; current: number }): ViewerFindState | null {
  if (!f.active) return null;
  return { query: f.query, count: f.count, current: f.count > 0 ? f.current + 1 : 0 };
}

/**
 * Reports find state whenever it changes. `searching` holds the report back
 * while an engine is still computing matches for a new query, so the report
 * never pairs the new query with the previous query's count.
 */
export function useFindStateReport(
  report: (patch: { find: ViewerFindState | null }) => void,
  f: { active: boolean; query: string; count: number; current: number; searching: boolean },
): void {
  const { active, query, count, current, searching } = f;
  useEffect(() => {
    if (searching) return;
    report({ find: findStateFor({ active, query, count, current }) });
  }, [report, active, query, count, current, searching]);
}

export interface DomViewerControlDeps {
  /** The scroll container (`ref` in the viewers). */
  scroller: { readonly current: HTMLElement | null };
  /** The element whose text is searchable (`columnRef` / `bodyRef`). */
  textRoot: { readonly current: HTMLElement | null };
  find: FindEngine;
  report: (patch: { selection: string }) => void;
}

/**
 * Controller for a DOM-rendered document. Scroll: heading or one-page-step
 * direction (page, slide and line are not this viewer's). Select: first text
 * match, shown as the browser selection. Find: the DOM engine. Throws when the
 * document is not rendered, a heading or text is absent, or a find command
 * cannot run.
 */
export function domViewerController(deps: DomViewerControlDeps): ViewerController {
  return {
    scroll(target) {
      const scroller = deps.scroller.current;
      if (!scroller) throw new Error("document is not rendered");
      if (target.heading !== undefined) {
        const root = deps.textRoot.current;
        if (!root) throw new Error("document is not rendered");
        const heading = findHeading(root, target.heading);
        if (!heading) throw new Error(`heading not found: ${target.heading}`);
        heading.scrollIntoView({ block: "start" });
        return true;
      }
      if (target.direction !== undefined) {
        const sign = target.direction === "down" ? 1 : -1;
        scroller.scrollBy({ top: sign * scroller.clientHeight * 0.8 });
        return true;
      }
      return false;
    },
    select(target) {
      if (target.text === undefined) return false;
      const root = deps.textRoot.current;
      if (!root) throw new Error("document is not rendered");
      const range = firstMatchRange(root, target.text);
      if (!range) throw new Error("text not found");
      selectRange(range);
      deps.report({ selection: range.toString() });
      return true;
    },
    find(cmd) {
      return runFindCommand(deps.find, cmd);
    },
  };
}
