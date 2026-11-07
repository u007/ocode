// Viewer context: what the user is looking at in a preview, so the chat can
// attach it to the next message.
//
// One record per chat session (keyed by the session tab id). The sidebar /
// reader PreviewHost owns the DOCUMENT part (`setViewerDoc`): path, kind and
// project. Each viewer owns the VIEW part (`patchViewerState`): where the user
// is (page, slide, sheet range, nearest heading, line range), the last
// non-empty highlight and the text currently in the viewport. The composer
// reads the record at send time (`readViewerState` / `formatViewerContext`)
// and prepends it to the message, exactly like the existing editor-context and
// Ask-LLM refs. Nothing here talks to the server: the context rides in the
// user message, so it stays in the volatile tail and never busts the cached
// prompt prefix.
//
// Selection is STICKY: clicking the composer collapses the browser selection,
// so a viewer reports `selection: null` only for a deselect made INSIDE the
// viewer (a collapsed mouseup there), never for one made elsewhere. A new doc
// resets everything.

import { createContext, useCallback, useContext, useEffect, useRef, useSyncExternalStore } from "react";
import type { PreviewKind } from "./previewKind";

export interface ViewerLocation {
  /** 1-based page and page count (PDF). */
  page?: number;
  pageCount?: number;
  /** 1-based slide and slide count (PPTX). */
  slide?: number;
  slideCount?: number;
  /** Active sheet name and the active range in A1 notation (Excel). */
  sheet?: string;
  range?: string;
  /** Nearest heading at or above the viewport top (markdown, docx, html). */
  heading?: string;
  /** 1-based inclusive line range (code/text viewers). */
  lines?: { start: number; end: number };
  /** 1-based inclusive line range currently in the editor viewport. */
  visibleLines?: { start: number; end: number };
}

export interface ViewerDoc {
  path: string;
  kind: PreviewKind;
  projectRoot?: string;
  projectHost?: string;
}

/** The in-viewer find bar: query, match count and 1-based current match (0 = none). */
export interface ViewerFindState {
  query: string;
  count: number;
  current: number;
}

/** Result of the last agent control directive applied to this session's viewer. */
export interface ViewerControlOutcome {
  id: string;
  status: "applied" | "failed";
  error?: string;
}

export interface ViewerState extends ViewerDoc {
  location: ViewerLocation;
  /** Last non-empty highlight inside the viewer, or null after an in-viewer deselect. */
  selection: string | null;
  /** Text in the viewport at the last report, capped at VIEWER_VISIBLE_MAX. */
  visibleText: string | null;
  find: ViewerFindState | null;
  control: ViewerControlOutcome | null;
  updatedAt: number;
}

/** A view update from a viewer. `path` must match the current doc or it is dropped. */
export interface ViewerPatch {
  path: string;
  location?: ViewerLocation;
  selection?: string | null;
  visibleText?: string | null;
  find?: ViewerFindState | null;
  control?: ViewerControlOutcome | null;
}

/** Agent-driven scroll. Each viewer uses the fields it understands. */
export type ViewerScrollTarget = {
  page?: number;
  slide?: number;
  line?: number;
  heading?: string;
  direction?: "up" | "down";
};

/** Agent-driven selection: a text needle, a line range, or an A1-style range. */
export type ViewerSelectTarget = {
  text?: string;
  lines?: { start: number; end: number };
  range?: string;
};

export type ViewerFindCommand =
  | { op: "open"; query: string }
  | { op: "next" }
  | { op: "prev" }
  | { op: "close" };

/**
 * What a viewer exposes to agent control. Each method returns true when it
 * handled the target, false when this viewer does not support it, and throws
 * an Error(message) for a failure (no match, bad range).
 */
export interface ViewerController {
  scroll?(target: ViewerScrollTarget): boolean;
  select?(target: ViewerSelectTarget): boolean;
  find?(cmd: ViewerFindCommand): boolean;
}

export const VIEWER_SELECTION_MAX = 1500;
export const VIEWER_VISIBLE_MAX = 1200;

const states = new Map<string, ViewerState>();
const listeners = new Map<string, Set<() => void>>();

function notify(key: string): void {
  listeners.get(key)?.forEach((cb) => cb());
}

/**
 * Collapse space runs and drop blank lines; cap the result. Tabs are kept:
 * a spreadsheet selection arrives as TSV and the tabs ARE its column
 * boundaries (only spaces around a line are trimmed, so a leading empty cell
 * survives too).
 */
export function tidyViewerText(text: string, max: number): string {
  const lines = text
    .split(/\r?\n/)
    .map((l) => l.replace(/ +/g, " ").replace(/^ +| +$/g, ""))
    .filter((l) => l.trim().length > 0);
  const joined = lines.join("\n");
  return joined.length > max ? `${joined.slice(0, max)}…` : joined;
}

/**
 * Set (or clear with null) the document a session is previewing. A different
 * path resets location, selection and visible text; the same path keeps them.
 */
export function setViewerDoc(key: string, doc: ViewerDoc | null): void {
  const prev = states.get(key);
  if (doc === null) {
    if (!prev) return;
    states.delete(key);
    notify(key);
    return;
  }
  if (prev && prev.path === doc.path && prev.kind === doc.kind && prev.projectRoot === doc.projectRoot && prev.projectHost === doc.projectHost) {
    return;
  }
  states.set(key, {
    ...doc,
    location: {},
    selection: null,
    visibleText: null,
    find: null,
    control: null,
    updatedAt: Date.now(),
  });
  notify(key);
}

/** Apply a view update. Ignored when the session has no doc or the path differs. */
export function patchViewerState(key: string, patch: ViewerPatch): void {
  const prev = states.get(key);
  if (!prev || prev.path !== patch.path) return;
  const next: ViewerState = { ...prev, updatedAt: Date.now() };
  if (patch.location !== undefined) next.location = { ...prev.location, ...patch.location };
  if (patch.selection !== undefined) {
    next.selection = patch.selection === null ? null : tidyViewerText(patch.selection, VIEWER_SELECTION_MAX) || null;
  }
  if (patch.visibleText !== undefined) {
    next.visibleText = patch.visibleText === null ? null : tidyViewerText(patch.visibleText, VIEWER_VISIBLE_MAX) || null;
  }
  if (patch.find !== undefined) next.find = patch.find;
  if (patch.control !== undefined) next.control = patch.control;
  states.set(key, next);
  notify(key);
}

export function readViewerState(key: string): ViewerState | null {
  return states.get(key) ?? null;
}

export function subscribeViewerState(key: string, cb: () => void): () => void {
  let set = listeners.get(key);
  if (!set) {
    set = new Set();
    listeners.set(key, set);
  }
  set.add(cb);
  return () => {
    // Search every key: a rekey may have moved this callback to another key.
    listeners.forEach((s, k) => {
      if (s.delete(cb) && s.size === 0) listeners.delete(k);
    });
  };
}

/**
 * Move a session's record and listeners to a new key (a draft tab becoming a
 * real session). No-op when the old key has no record or the keys are equal.
 */
export function rekeyViewerState(oldKey: string, newKey: string): void {
  if (oldKey === newKey) return;
  const state = states.get(oldKey);
  if (!state) return;
  states.delete(oldKey);
  states.set(newKey, state);
  const moved = listeners.get(oldKey);
  if (moved) {
    listeners.delete(oldKey);
    const target = listeners.get(newKey) ?? new Set<() => void>();
    moved.forEach((cb) => target.add(cb));
    listeners.set(newKey, target);
  }
  notify(oldKey);
  notify(newKey);
}

/** Test helper: forget every session's record. */
export function resetViewerStates(): void {
  states.clear();
  listeners.forEach((_, key) => notify(key));
}

/** Live record for a session, or null without a key or doc. */
export function useViewerState(key: string | null | undefined): ViewerState | null {
  const subscribe = useCallback((cb: () => void) => (key ? subscribeViewerState(key, cb) : () => {}), [key]);
  const get = useCallback(() => (key ? readViewerState(key) : null), [key]);
  return useSyncExternalStore(subscribe, get, get);
}

/**
 * The session key the enclosing PreviewHost reports under. Null (the default)
 * means the viewer is not accompanying a chat (Files tab, Preview sub-tab),
 * so reports are dropped.
 */
export const ViewerReportContext = createContext<string | null>(null);

/**
 * Reporter for one viewer: `report({ location, selection, visibleText })`.
 * A stable no-op outside a ViewerReportContext provider. Viewers call it on
 * load, on (throttled) scroll and on selection changes; the store drops a
 * report whose path is no longer the previewed doc.
 */
export function useViewerReport(path: string): (patch: Omit<ViewerPatch, "path">) => void {
  const key = useContext(ViewerReportContext);
  return useCallback(
    (patch: Omit<ViewerPatch, "path">) => {
      if (!key) return;
      patchViewerState(key, { path, ...patch });
    },
    [key, path],
  );
}

interface ControllerEntry {
  c: ViewerController;
}

/** Per key, a stack of registrations: the top one is the active controller. */
const controllers = new Map<string, ControllerEntry[]>();

/**
 * Register an agent-control controller for a session key. Registrations stack:
 * the last one is active, and its unregister removes exactly that registration
 * wherever it sits, so a viewer mounted over another one restores it on exit.
 * Unregistering twice is a no-op.
 */
export function registerViewerController(key: string, c: ViewerController): () => void {
  const entry: ControllerEntry = { c };
  controllers.set(key, [...(controllers.get(key) ?? []), entry]);
  return () => {
    const stack = controllers.get(key);
    if (!stack) return;
    const rest = stack.filter((e) => e !== entry);
    if (rest.length === stack.length) return;
    if (rest.length === 0) controllers.delete(key);
    else controllers.set(key, rest);
  };
}

/**
 * Register this viewer's controller under the enclosing ViewerReportContext.
 * The controller object may change every render, so it is held in a ref and
 * the registered proxy always calls the latest one. No-op outside a provider.
 */
export function useViewerController(path: string, controller: ViewerController): void {
  const key = useContext(ViewerReportContext);
  const latest = useRef(controller);
  latest.current = controller;
  useEffect(() => {
    if (!key) return;
    return registerViewerController(key, {
      scroll: (target) => {
        const c = latest.current;
        return c.scroll ? c.scroll(target) : false;
      },
      select: (target) => {
        const c = latest.current;
        return c.select ? c.select(target) : false;
      },
      find: (cmd) => {
        const c = latest.current;
        return c.find ? c.find(cmd) : false;
      },
    });
  }, [key, path]);
}

/** The controller for a session key, or null when no viewer registered one. */
export function readViewerController(key: string): ViewerController | null {
  const stack = controllers.get(key);
  if (!stack || stack.length === 0) return null;
  return stack[stack.length - 1].c;
}

/** Short human location: "p.3/12", "slide 2/8", "Sheet1!B2:D9", "§ Install", "L10-L24". */
export function describeViewerLocation(loc: ViewerLocation): string {
  const parts: string[] = [];
  if (loc.page !== undefined) parts.push(loc.pageCount ? `p.${loc.page}/${loc.pageCount}` : `p.${loc.page}`);
  if (loc.slide !== undefined) parts.push(loc.slideCount ? `slide ${loc.slide}/${loc.slideCount}` : `slide ${loc.slide}`);
  if (loc.sheet !== undefined || loc.range !== undefined) {
    parts.push(loc.sheet && loc.range ? `${loc.sheet}!${loc.range}` : (loc.sheet ?? loc.range ?? ""));
  }
  if (loc.heading) parts.push(`§ ${loc.heading}`);
  if (loc.lines) parts.push(formatLineRange(loc.lines));
  else if (loc.visibleLines) parts.push(`view ${formatLineRange(loc.visibleLines)}`);
  return parts.filter(Boolean).join(", ");
}

function formatLineRange(r: { start: number; end: number }): string {
  return r.start === r.end ? `L${r.start}` : `L${r.start}-L${r.end}`;
}

/**
 * The text the composer prepends to the user's message. Same shape as the
 * Ask-LLM ref (`@path (label): "excerpt"`) so the model reads it the same way:
 *
 *   @docs/guide.md (markdown, § Install) selected: "…"
 *   @report.pdf (pdf, p.3/12) visible: "…"
 *
 * `fallbackSelection` is the Ask-LLM chip excerpt, used when the viewer holds
 * no live highlight (the user clicked Ask LLM, then deselected in the viewer).
 */
export function formatViewerContext(state: ViewerState, fallbackSelection?: string | null): string {
  const loc = describeViewerLocation(state.location);
  const head = `@${state.path} (${state.kind}${loc ? `, ${loc}` : ""})`;
  const selection = state.selection ?? (fallbackSelection ? tidyViewerText(fallbackSelection, VIEWER_SELECTION_MAX) : null);
  if (selection) return `${head} selected: "${selection}"`;
  if (state.visibleText) return `${head} visible: "${state.visibleText}"`;
  return head;
}
