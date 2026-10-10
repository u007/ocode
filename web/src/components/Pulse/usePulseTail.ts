import { useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import { eventBus } from "../../lib/eventBus";
import type { Message, PulseStatus } from "../../api/types";

/**
 * usePulseTail — the "what is this session doing right now" activity feed a
 * Pulse card shows on its face (live rows) or in its hover overlay (others).
 *
 * The feed is a list of ENTRIES, not a text tail: prose lines interleaved with
 * one line per tool call. A turn that is mostly tool calls used to show one
 * stale paragraph and nothing else, because only `text` frames were read.
 *
 * Two sources, and which one is used is not a preference:
 *
 *  - RUNNING: the server buffers this turn's streaming frames on the session
 *    manager entry, so `GET /api/sessions/:id/state` returns everything
 *    streamed so far in `live_frames` and the `text` / `tool_start` /
 *    `tool_result` SSE events carry the rest. Seed once, then append from the
 *    bus. The seed and the bus go through the SAME reducer so they cannot
 *    produce different entries for the same frames. `thinking` and
 *    `tool_output` are ignored: too noisy for a card.
 *  - ANY OTHER STATUS (idle, error, or a needs-you row paused on an ask): there
 *    is no live output to read. `appendLiveFrame` only buffers while
 *    `turnActive`, and the turn-end path drops the buffer, so `live_frames` is
 *    empty for a finished turn and subscribing to `text` would just wait for
 *    events that belong to the NEXT turn. The last turn on disk
 *    is the only honest preview there: the messages after the
 *    last user message, replayed through the same reducer so a settled card
 *    shows prose and tool lines just like a running one.
 *
 * The hook is enabled by the card's own gating: a LIVE card (running, or
 * paused on an ask) enables it unconditionally because it streams on the card
 * face, while every other status enables it only on hover/focus. It is
 * therefore both subscribed and unsubscribed constantly. Two consequences are
 * handled explicitly rather than left to chance: the subscription is torn down
 * in the effect cleanup (a leak here keeps appending to a card that is no
 * longer on screen), and every response is checked against a generation counter
 * so a fetch that resolves after the session changed — or after the hook was
 * disabled — cannot write into the new card's state.
 */

/** Entries kept. The on-card stream region scrolls, so this is no longer the
 *  visible height — it only bounds the DOM and the buffer. A prose entry is one
 *  newline-delimited line of the stream text; a tool entry is one tool call. */
export const PULSE_TAIL_ENTRIES = 60;

/** Longest tool command shown on a tool entry. Matches the server's
 *  `current_task` truncation (`derivePulseTask`, 80 runes). */
const TOOL_COMMAND_MAX = 80;

/**
 * Messages requested from the END of the transcript for a finished turn. The
 * endpoint's `limit`/`offset` count back from the newest message, so one
 * request gets the tail without first learning the transcript's length.
 */
const IDLE_FETCH_LIMIT = 200;

/**
 * Cap on the retained streaming text, summed over the text entries. The server
 * caps its own frame buffer (liveFramesByteCap); matching that here keeps a very
 * long turn from growing this buffer without bound.
 */
const TEXT_BUFFER_CAP = 8_000;

export interface PulseTailEntry {
  kind: "text" | "tool";
  text: string;
}

export interface PulseTail {
  entries: PulseTailEntry[];
  error: string | null;
  /**
   * True while the seed fetch is in flight.
   *
   * The card's overlay needs this to tell "not answered yet" apart from
   * "nothing to show": without it every hover flashes the empty state for the
   * duration of the request and then swaps it for real content. It tracks the
   * seed only — the running path's subscriptions never settle, so live
   * events arriving after the seed must not keep it spinning.
   */
  loading: boolean;
}

/**
 * The `text` SSE payload is server.TextDelta — a single `delta` string
 * (internal/server/handler_sse.go). Anything else on the envelope (a tool
 * frame, a discovery notice) yields no text.
 */
function textDelta(data: unknown): string {
  if (data === null || typeof data !== "object") return "";
  const delta = (data as { delta?: unknown }).delta;
  return typeof delta === "string" ? delta : "";
}

function stringField(data: unknown, key: string): string | undefined {
  if (data === null || typeof data !== "object") return undefined;
  const value = (data as Record<string, unknown>)[key];
  return typeof value === "string" ? value : undefined;
}

/** Argument keys that name what a tool call is acting on, in preference order. */
const ARG_SUMMARY_KEYS = ["command", "file_path", "path", "pattern", "query", "url"] as const;

/**
 * The one-line subject of a tool call, from its raw JSON `function.arguments`.
 *
 * Both the live `tool_start` SSE payload (its `command` field is the RAW
 * arguments string, internal/server/handler.go) and the persisted transcript
 * carry arguments JSON, so every path funnels through here. No usable string
 * key, or arguments that are not a JSON object, yields "" and the entry shows
 * the tool name only.
 */
function summarizeArgs(args: string): string {
  let parsed: unknown;
  try {
    parsed = JSON.parse(args);
  } catch {
    // intentionally not logged: some providers send arguments that are not
    // strict JSON, and the entry simply shows the tool name without a subject.
    return "";
  }
  for (const key of ARG_SUMMARY_KEYS) {
    const value = stringField(parsed, key);
    if (value !== undefined && value !== "") return value;
  }
  return "";
}

/** One tool line: a single-width glyph (never a wide emoji, which shifts the
 *  rest of the row in VS Code's renderer), the tool name, the clipped command. */
function toolLine(tool: string, args: string | undefined): string {
  const flat = summarizeArgs(args ?? "").replace(/\s+/g, " ").trim();
  if (flat === "") return `▸ ${tool}`;
  const chars = Array.from(flat);
  const shown = chars.length > TOOL_COMMAND_MAX ? `${chars.slice(0, TOOL_COMMAND_MAX).join("")}…` : flat;
  return `▸ ${tool} ${shown}`;
}

/** Internal entry: mutable so a `tool_result` can mark it done in place. The
 *  published copies are fresh objects, so React still sees new identities. */
interface FeedEntry extends PulseTailEntry {
  done?: boolean;
}

/**
 * The feed reducer shared by the seed frames and the live subscription.
 *
 * Tool entries are tracked by identity under their `call_id` rather than by
 * array index: the front of the array is trimmed as the feed grows, which would
 * shift every stored index. A result for a call whose entry was trimmed away, or
 * one never seen, marks nothing.
 */
class Feed {
  private items: FeedEntry[] = [];
  private byCallId = new Map<string, FeedEntry>();

  reset(): void {
    this.items = [];
    this.byCallId.clear();
  }

  /** Drop trailing blank text entries (an open line break) from the view. */
  snapshot(): PulseTailEntry[] {
    let end = this.items.length;
    while (end > 0 && this.items[end - 1].kind === "text" && this.items[end - 1].text === "") end -= 1;
    return this.items.slice(0, end).map(({ kind, text }) => ({ kind, text }));
  }

  apply(event: string, data: unknown): void {
    if (event === "text") this.appendText(textDelta(data));
    else if (event === "tool_start") this.startTool(data);
    else if (event === "tool_result") this.finishTool(data);
    else return;
    this.trim();
  }

  private appendText(delta: string): void {
    if (delta === "") return;
    const parts = delta.split("\n");
    parts.forEach((part, i) => {
      const last = this.items[this.items.length - 1];
      if (i === 0 && last?.kind === "text") {
        last.text += part;
        return;
      }
      // A newline straight after a tool line has no open prose line to end.
      if (i === 0 && part === "") return;
      this.items.push({ kind: "text", text: part });
    });
  }

  private startTool(data: unknown): void {
    const tool = stringField(data, "tool");
    if (tool === undefined) return;
    // An open blank line before a tool line is just the newline that ended the
    // prose; keeping it would render an empty row above every tool call.
    const last = this.items[this.items.length - 1];
    if (last?.kind === "text" && last.text === "") this.items.pop();
    const entry: FeedEntry = { kind: "tool", text: toolLine(tool, stringField(data, "command")) };
    this.items.push(entry);
    const callId = stringField(data, "call_id");
    if (callId !== undefined && callId !== "") this.byCallId.set(callId, entry);
  }

  private finishTool(data: unknown): void {
    const callId = stringField(data, "call_id");
    if (callId === undefined) return;
    const entry = this.byCallId.get(callId);
    if (entry === undefined || entry.done) return;
    this.byCallId.delete(callId);
    entry.done = true;
    const output = (stringField(data, "output") ?? "").trimStart();
    entry.text += /^(Error|error:)/.test(output) ? " ✗" : " ✓";
  }

  private trim(): void {
    if (this.items.length > PULSE_TAIL_ENTRIES) {
      this.items = this.items.slice(-PULSE_TAIL_ENTRIES);
    }
    let textChars = 0;
    for (const e of this.items) if (e.kind === "text") textChars += e.text.length;
    while (textChars > TEXT_BUFFER_CAP && this.items.length > 1) {
      const dropped = this.items.shift()!;
      if (dropped.kind === "text") textChars -= dropped.text.length;
    }
    if (textChars > TEXT_BUFFER_CAP) {
      const only = this.items[0];
      only.text = only.text.slice(-TEXT_BUFFER_CAP);
    }
  }
}

/**
 * The feed for a FINISHED turn: every message after the last real user message,
 * replayed through the same reducer the live path uses, so a settled card shows
 * the same shape as a running one. Injected `[ocode:` notices are user-role in
 * the transcript but are not the user's turn, so they are not a boundary. With
 * no user message at all the whole fetched slice is the turn.
 */
function feedFromTranscript(messages: Message[] | undefined): PulseTailEntry[] {
  const all = messages ?? [];
  let start = 0;
  for (let i = all.length - 1; i >= 0; i -= 1) {
    if (all[i].role === "user" && !all[i].content.startsWith("[ocode:")) {
      start = i + 1;
      break;
    }
  }
  const feed = new Feed();
  // Whether the feed currently ends in prose: a following assistant message
  // must then start a new line, but after a tool entry it already does.
  let proseOpen = false;
  for (const m of all.slice(start)) {
    if (m.role === "assistant") {
      if (m.content.trim() !== "") {
        if (proseOpen) feed.apply("text", { delta: "\n" });
        feed.apply("text", { delta: m.content });
        proseOpen = true;
      }
      for (const call of m.tool_calls ?? []) {
        proseOpen = false;
        feed.apply("tool_start", {
          tool: call.function.name,
          command: call.function.arguments,
          call_id: call.id,
        });
      }
    } else if (m.role === "tool" && m.tool_call_id) {
      feed.apply("tool_result", { call_id: m.tool_call_id, output: m.content });
    }
  }
  return feed.snapshot();
}

const LIVE_EVENTS = ["text", "tool_start", "tool_result"] as const;

export function usePulseTail(
  sessionId: string,
  enabled: boolean,
  status: PulseStatus,
): PulseTail {
  const [entries, setEntries] = useState<PulseTailEntry[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  /** Feed accumulated across the seed fetch and the live events. A ref, not
   *  state: the bus handlers must read and extend it without re-subscribing on
   *  every event, and only snapshots belong in state. */
  const feedRef = useRef(new Feed());
  /** Bumped on entry to the effect and again in its cleanup, so a response
   *  belonging to a superseded run can be recognised. */
  const generationRef = useRef(0);

  useEffect(() => {
    const generation = ++generationRef.current;
    const stale = () => generation !== generationRef.current;
    const unsubscribes: (() => void)[] = [];

    const feed = feedRef.current;
    feed.reset();
    setEntries([]);
    setError(null);
    setLoading(false);
    if (!enabled) return;

    /** Seed has answered (or failed) — stop reporting in-flight. Guarded by the
     *  same generation check as every other write, so a superseded run cannot
     *  clear the spinner belonging to the card that replaced it. */
    const settle = () => {
      if (!stale()) setLoading(false);
    };

    const publish = () => {
      if (stale()) return;
      setEntries(feed.snapshot());
    };
    const fail = (what: string, err: unknown) => {
      const detail = err instanceof Error ? err.message : String(err);
      // Logged even when the response is stale: a swallowed failure is how a
      // card shows nothing forever with no trace of why.
      console.error(`usePulseTail: ${what} failed for session ${sessionId}: ${detail}`);
      // The session id is in the surfaced text too, not just the log: the
      // overlay shows this to the user, who has no other way to tell which
      // card is the broken one.
      if (!stale()) setError(`${what} failed for ${sessionId}: ${detail}`);
    };

    if (status === "running") {
      setLoading(true);
      // The subscriptions below are installed synchronously, so events arrive
      // while the seed request is still in flight. Applying them straight to the
      // feed put them BEFORE the seed's own live_frames — out of order, and
      // showing twice any frame present in both. Hold them until the seed
      // lands, then replay seed-then-held in that order.
      let seeding = true;
      const held: { event: string; data: unknown }[] = [];
      const flushHeld = () => {
        for (const h of held) feed.apply(h.event, h.data);
        held.length = 0;
      };

      api
        .getSessionState(sessionId)
        .then((state) => {
          settle();
          if (stale()) return;
          if (!seeding) return; // the failure path already released the feed
          seeding = false;
          for (const frame of state.live_frames ?? []) feed.apply(frame.event, frame.data);
          flushHeld();
          publish();
        })
        .catch((err) => {
          // Stop holding events back: the overlay now carries the error, and a
          // subscriber that never flushes would show an empty card forever.
          seeding = false;
          if (held.length > 0) {
            flushHeld();
            publish();
          }
          settle();
          fail("session state", err);
        });

      for (const event of LIVE_EVENTS) {
        unsubscribes.push(
          eventBus.on(event, (env) => {
            if (env.session_id !== sessionId || stale()) return;
            if (seeding) {
              held.push({ event, data: env.data });
              return;
            }
            feed.apply(event, env.data);
            publish();
          }),
        );
      }
    } else {
      setLoading(true);
      api
        .getSession(sessionId, {
          limit: IDLE_FETCH_LIMIT,
          // Opt out of the revision baseline: this is a speculative read for a
          // card's preview, not an open tab's transcript load.
          // Recording one would stamp a FRESH revision over an open tab that is
          // still showing older content, and the revalidation poll would then
          // see "no change" and never repair it.
          noteRevision: false,
        })
        .then((detail) => {
          settle();
          if (stale()) return;
          setEntries(feedFromTranscript(detail.messages));
        })
        .catch((err) => {
          settle();
          fail("session transcript", err);
        });
    }

    return () => {
      generationRef.current += 1;
      for (const unsubscribe of unsubscribes) unsubscribe();
    };
  }, [sessionId, enabled, status]);

  return { entries, error, loading };
}
