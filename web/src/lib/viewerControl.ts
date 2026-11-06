// Agent control of the file viewer. A Go tool result carries a directive line
// `VIEWER_CONTROL:{"id":"vc_…","action":"scroll","page":3}` through the chat
// transcript. `useViewerControl` finds directives in the active session's
// messages and applies each one once to the viewer registered under that
// session key (`registerViewerController`). The outcome lands on the session
// record as `control`, so the server can read it back mid-turn.

import { useEffect, useMemo, useRef } from "react";
import { useChatSelector } from "../stores/chatStore";
import type { Message } from "../api/types";
import {
  patchViewerState,
  readViewerController,
  readViewerState,
  type ViewerControlOutcome,
  type ViewerController,
} from "./viewerContext";

export const VIEWER_CONTROL_SENTINEL = "VIEWER_CONTROL:";

export type ViewerControlAction =
  | { id: string; action: "scroll"; page?: number; slide?: number; line?: number; heading?: string; direction?: "up" | "down" }
  | { id: string; action: "select"; text?: string; lines?: { start: number; end: number }; range?: string }
  | { id: string; action: "find"; query: string }
  | { id: string; action: "find_next" }
  | { id: string; action: "find_prev" }
  | { id: string; action: "find_close" };

const ACTION_NAMES = new Set<string>(["scroll", "select", "find", "find_next", "find_prev", "find_close"]);
const DIRECTIVE_RE = /VIEWER_CONTROL:(\{[^\r\n]*\})/g;
const EMPTY_MESSAGES: Message[] = [];

/** Reads one directive's JSON, throwing when it is not a usable directive. */
function toAction(json: string): ViewerControlAction {
  const raw: unknown = JSON.parse(json);
  if (typeof raw !== "object" || raw === null) throw new Error("directive is not an object");
  const o = raw as Record<string, unknown>;
  if (typeof o.id !== "string" || o.id === "") throw new Error("directive has no id");
  if (typeof o.action !== "string" || !ACTION_NAMES.has(o.action)) throw new Error(`unknown action ${String(o.action)}`);
  if (o.action === "find" && typeof o.query !== "string") throw new Error("find directive has no query");
  // Each viewer validates the target values it actually uses.
  return raw as ViewerControlAction;
}

/**
 * Scans message contents for `VIEWER_CONTROL:{...}` (single-line JSON right
 * after the sentinel) and returns every directive in order. A repeated id keeps
 * its first occurrence. A directive that fails to parse is skipped and logged.
 */
export function parseViewerControls(contents: string[]): ViewerControlAction[] {
  const out: ViewerControlAction[] = [];
  const seen = new Set<string>();
  for (const text of contents) {
    if (!text.includes(VIEWER_CONTROL_SENTINEL)) continue;
    for (const match of text.matchAll(DIRECTIVE_RE)) {
      const json = match[1];
      let action: ViewerControlAction;
      try {
        action = toAction(json);
      } catch (err) {
        console.warn("viewerControl: skipped directive", { json, error: err instanceof Error ? err.message : String(err) });
        continue;
      }
      if (seen.has(action.id)) continue;
      seen.add(action.id);
      out.push(action);
    }
  }
  return out;
}

function failed(id: string, error: string): ViewerControlOutcome {
  return { id, status: "failed", error };
}

/** Runs one action on a controller. Returns false when the viewer does not support it. */
function dispatch(c: ViewerController, action: ViewerControlAction): boolean {
  switch (action.action) {
    case "scroll":
      return c.scroll
        ? c.scroll({ page: action.page, slide: action.slide, line: action.line, heading: action.heading, direction: action.direction })
        : false;
    case "select":
      return c.select ? c.select({ text: action.text, lines: action.lines, range: action.range }) : false;
    case "find":
      return c.find ? c.find({ op: "open", query: action.query }) : false;
    case "find_next":
      return c.find ? c.find({ op: "next" }) : false;
    case "find_prev":
      return c.find ? c.find({ op: "prev" }) : false;
    case "find_close":
      return c.find ? c.find({ op: "close" }) : false;
  }
}

/**
 * Applies one directive to the controller registered for `key`. Never throws:
 * every failure becomes a `failed` outcome. The outcome is written to the
 * session record's `control` field when a record exists.
 */
export function applyViewerControl(key: string, action: ViewerControlAction): ViewerControlOutcome {
  const controller = readViewerController(key);
  let outcome: ViewerControlOutcome;
  if (!controller) {
    outcome = failed(action.id, "no preview open");
  } else {
    try {
      outcome = dispatch(controller, action)
        ? { id: action.id, status: "applied" }
        : failed(action.id, "not supported by this viewer");
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      console.warn("viewerControl: directive failed", { key, id: action.id, action: action.action, error: message });
      outcome = failed(action.id, message);
    }
  }
  const current = readViewerState(key);
  if (current) patchViewerState(key, { path: current.path, control: outcome });
  return outcome;
}

/**
 * Applies the session's control directives to its viewer. Directives already
 * present on the first run for a session are recorded as applied without
 * running, so a reload or a session switch never replays old directives. The
 * applied set resets when the session changes.
 */
export function useViewerControl(sessionId: string | null): void {
  const messages = useChatSelector((s) => (sessionId ? (s.sessions[sessionId]?.messages ?? EMPTY_MESSAGES) : EMPTY_MESSAGES));
  const contents = useMemo(() => messages.map((m) => m.content), [messages]);
  const seen = useRef<{ key: string; ids: Set<string> } | null>(null);

  useEffect(() => {
    if (!sessionId) {
      seen.current = null;
      return;
    }
    const directives = parseViewerControls(contents);
    const prev = seen.current;
    if (!prev || prev.key !== sessionId) {
      seen.current = { key: sessionId, ids: new Set(directives.map((d) => d.id)) };
      return;
    }
    for (const directive of directives) {
      if (prev.ids.has(directive.id)) continue;
      prev.ids.add(directive.id);
      applyViewerControl(sessionId, directive);
    }
  }, [sessionId, contents]);
}
