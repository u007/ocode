// Agent control of the file viewer. A Go tool result carries a directive line
// `VIEWER_CONTROL:{"id":"vc_…","action":"scroll","page":3}` through the chat
// transcript. `useViewerControl` finds directives in the active session and
// applies each one once to the viewer registered under that session key
// (`registerViewerController`). The outcome lands on the session record as
// `control`, so the server can read it back mid-turn.

import { useEffect, useRef, useState } from "react";
import { useChatSelector } from "../stores/chatStore";
import type { LivePart, Message } from "../api/types";
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
const EMPTY_LIVE: LivePart[] = [];

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

/** Per-session bookkeeping for `useViewerControl`. */
interface ControlTrack {
  key: string;
  /** Directive ids already applied or seeded. */
  handled: Set<string>;
  /**
   * Server index of the last committed message scanned. Messages past it are
   * new. Null while the window's position is unknown. A server index survives a
   * turn-end commit, which replaces every message object, and a prepend, which
   * shifts local positions but not server ones.
   */
  anchor: number | null;
  /** True once the session's history has been seeded. */
  seeded: boolean;
}

/**
 * Marks every directive in `messages` as seen, without applying it, and moves
 * the anchor to the tail. Runs for the first seed and whenever the window
 * position is unknown (`windowStart` -1, e.g. a slice populated only by live
 * SSE before its first page resolved). There a committed directive cannot be
 * told apart from history, so it is deliberately not applied. Live tool results
 * still apply.
 */
function adoptTranscript(t: ControlTrack, messages: Message[], windowStart: number): void {
  for (const directive of parseViewerControls(messages.map((m) => m.content))) t.handled.add(directive.id);
  t.anchor = windowStart >= 0 ? windowStart + messages.length - 1 : null;
}

function liveOutputs(live: LivePart[]): string[] {
  const out: string[] = [];
  for (const part of live) {
    if (part.kind === "tool" && part.output) out.push(part.output);
  }
  return out;
}

/**
 * Applies the session's control directives to its viewer, each id at most once.
 *
 * - Live tool results (the turn in flight) apply as they arrive, so the agent
 *   sees the outcome mid-turn.
 * - Committed history is seeded once the store reports the session initialized
 *   (the history fetch or the turn-end snapshot has landed). Those directives
 *   were already acted on, so a reload or session switch never replays them.
 * - Committed messages that arrive later apply only when their server index is
 *   past the anchor, the last server index scanned. Older messages prepended by
 *   scrolling back stay silent, and so do messages loaded while the window's
 *   position is unknown (see `adoptTranscript`).
 *
 * The bookkeeping resets when the session changes.
 */
export function useViewerControl(sessionId: string | null): void {
  const messages = useChatSelector((s) => (sessionId ? (s.sessions[sessionId]?.messages ?? EMPTY_MESSAGES) : EMPTY_MESSAGES));
  const live = useChatSelector((s) => (sessionId ? (s.sessions[sessionId]?.live ?? EMPTY_LIVE) : EMPTY_LIVE));
  const initialized = useChatSelector((s) => (sessionId ? (s.sessions[sessionId]?.initialized ?? false) : false));
  // -1 is the store's own "position unknown" value, which is also what a missing slice has.
  const windowStart = useChatSelector((s) => (sessionId ? (s.sessions[sessionId]?.windowStartServerIndex ?? -1) : -1));
  const track = useRef<ControlTrack | null>(null);
  // One directive per macrotask. A find must render before the find_next
  // behind it in the same batch can run, so the next one waits for a tick.
  const ready = useRef(true);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (!sessionId) {
      track.current = null;
      return;
    }
    if (!track.current || track.current.key !== sessionId) {
      track.current = { key: sessionId, handled: new Set(), anchor: null, seeded: false };
    }
    const t = track.current;
    // Wait for the history to land: seeding an empty transcript would make
    // every directive loaded later look new.
    if (!t.seeded && initialized) t.seeded = true;
    // No anchor yet (first seed, or the window position is still unknown): take
    // the loaded transcript as seen. This is also the path when a prepend first
    // reveals the position, so the rows it brings in stay silent.
    if (t.seeded && t.anchor === null) adoptTranscript(t, messages, windowStart);

    // Transcript order: committed messages past the anchor, then the live turn.
    const pending: ViewerControlAction[] = [];
    if (t.seeded && t.anchor !== null) {
      // Local position of the anchor. A truncation that cut below it clamps to
      // the tail, so only rows appended after the surviving tail are fresh. A
      // head trim past it leaves every loaded row fresh.
      const at = Math.min(t.anchor - windowStart, messages.length - 1);
      pending.push(...parseViewerControls(messages.slice(Math.max(at + 1, 0)).map((m) => m.content)));
    }
    pending.push(...parseViewerControls(liveOutputs(live)));

    const next = pending.find((d) => !t.handled.has(d.id));
    if (next && ready.current) {
      t.handled.add(next.id);
      applyViewerControl(sessionId, next);
      ready.current = false;
      setTimeout(() => {
        ready.current = true;
        setTick((n) => n + 1);
      }, 0);
    }
    // The anchor moves only once everything scanned has applied, so the
    // directives still waiting for a tick are found again on the next pass.
    // Known limit: a client-only row (ADD_MESSAGE, e.g. a /help reply) counts
    // toward `messages.length` but has no server index, so the tail index can
    // run ahead of the server by that many rows. The next turn-end commit drops
    // those rows and the first new server rows at or below the anchor go
    // unscanned. Those are usually the user's own prompt, and live tool results
    // still apply.
    if (t.anchor !== null && !pending.some((d) => !t.handled.has(d.id))) {
      t.anchor = windowStart + messages.length - 1;
    }
  }, [sessionId, messages, live, initialized, windowStart, tick]);
}
