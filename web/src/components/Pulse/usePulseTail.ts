import { useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import { eventBus } from "../../lib/eventBus";
import type { Message, PulseStatus } from "../../api/types";

/**
 * usePulseTail — the short "what is this session saying right now" preview a
 * Pulse card shows in its hover overlay.
 *
 * Two sources, and which one is used is not a preference:
 *
 *  - RUNNING: the server buffers this turn's streaming frames on the session
 *    manager entry, so `GET /api/sessions/:id/state` returns everything
 *    streamed so far in `live_frames` and the `text` SSE events carry the
 *    rest. Seed once, then append from the bus.
 *  - ANY OTHER STATUS (idle, error, or a needs-you row paused on an ask): there
 *    is no live output to read. `appendLiveFrame` only buffers while
 *    `turnActive`, and the turn-end path drops the buffer, so `live_frames` is
 *    empty for a finished turn and subscribing to `text` would just wait for
 *    events that belong to the NEXT turn. The tail of the last assistant
 *    message on disk is the only honest preview there.
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

/** Logical lines kept. Enough to see the shape of the answer, few enough to stay a
 *  preview rather than a transcript.
 *
 *  These are lines of the STREAM TEXT (newline-delimited), not lines of screen.
 *  On the card they soft-wrap, so one of these can fill several screen lines and
 *  PULSE_TAIL_LINES no longer has to equal the reserved height — anything past
 *  what the box holds overflows out of the top and is clipped. The cap still
 *  bounds the buffer and the DOM, which is what it is for; the height budget is
 *  PulseCard's `CARD_MIN_H`. In the overlay each of these is exactly one
 *  truncated screen line, so there the count still is the visible height. */
export const PULSE_TAIL_LINES = 7;

/**
 * Messages requested from the END of the transcript for a finished turn. The
 * endpoint's `limit`/`offset` count back from the newest message, so one
 * request gets the tail without first learning the transcript's length.
 */
const IDLE_FETCH_LIMIT = 200;

/**
 * Cap on the retained streaming text. The server caps its own frame buffer
 * (liveFramesByteCap); matching that here keeps a very long turn from growing
 * this buffer without bound. Only the derived last PULSE_TAIL_LINES lines ever
 * reach state, so the cap cannot change what is displayed.
 */
const TEXT_BUFFER_CAP = 8_000;

export interface PulseTail {
  lines: string[];
  error: string | null;
  /**
   * True while the seed fetch is in flight.
   *
   * The card's overlay needs this to tell "not answered yet" apart from
   * "nothing to show": without it every hover flashes the empty state for the
   * duration of the request and then swaps it for real content. It tracks the
   * seed only — the running path's `text` subscription never settles, so live
   * deltas arriving after the seed must not keep it spinning.
   */
  loading: boolean;
}

/** Last `max` non-trailing lines of `text`, oldest first. */
function lastLines(text: string, max: number): string[] {
  const trimmed = text.replace(/\n+$/, "");
  if (trimmed === "") return [];
  return trimmed.split("\n").slice(-max);
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

/** Newest assistant message with real content, or "" when there is none. */
function lastAssistantContent(messages: Message[] | undefined): string {
  for (let i = (messages?.length ?? 0) - 1; i >= 0; i -= 1) {
    const m = messages![i];
    if (m.role === "assistant" && m.content.trim() !== "") return m.content;
  }
  return "";
}

export function usePulseTail(
  sessionId: string,
  enabled: boolean,
  status: PulseStatus,
): PulseTail {
  const [lines, setLines] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  /** Text accumulated across the seed fetch and the live deltas. A ref, not
   *  state: only its last few lines belong in state, and the buffer must stay
   *  readable from the bus handler without re-subscribing on every delta. */
  const bufferRef = useRef("");
  /** Bumped on entry to the effect and again in its cleanup, so a response
   *  belonging to a superseded run can be recognised. */
  const generationRef = useRef(0);

  useEffect(() => {
    const generation = ++generationRef.current;
    const stale = () => generation !== generationRef.current;
    let unsubscribe: (() => void) | null = null;

    bufferRef.current = "";
    setLines([]);
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
      setLines(lastLines(bufferRef.current, PULSE_TAIL_LINES));
    };
    const append = (chunk: string) => {
      if (stale() || chunk === "") return;
      bufferRef.current += chunk;
      if (bufferRef.current.length > TEXT_BUFFER_CAP) {
        bufferRef.current = bufferRef.current.slice(-TEXT_BUFFER_CAP);
      }
      publish();
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
      // The "text" subscription below is installed synchronously, so chunks
      // arrive while the seed request is still in flight. Appending them
      // straight to the buffer put them BEFORE the seed's own live_frames —
      // out of order, and showing twice any frame present in both. Hold them
      // until the seed lands, then replay seed-then-held in that order.
      let seeding = true;
      let heldLive = "";
      const cap = (text: string) =>
        text.length > TEXT_BUFFER_CAP ? text.slice(-TEXT_BUFFER_CAP) : text;

      api
        .getSessionState(sessionId)
        .then((state) => {
          settle();
          if (stale()) return;
          let seedText = "";
          for (const frame of state.live_frames ?? []) {
            if (frame.event !== "text") continue;
            seedText += textDelta(frame.data);
          }
          if (!seeding) return; // the failure path already released the buffer
          seeding = false;
          bufferRef.current = cap(seedText + heldLive);
          heldLive = "";
          publish();
        })
        .catch((err) => {
          // Stop holding chunks back: the overlay now carries the error, and a
          // subscriber that never flushes would show an empty card forever.
          seeding = false;
          if (heldLive) {
            bufferRef.current = cap(heldLive);
            heldLive = "";
            publish();
          }
          settle();
          fail("session state", err);
        });

      unsubscribe = eventBus.on("text", (env) => {
        if (env.session_id !== sessionId) return;
        const chunk = textDelta(env.data);
        if (seeding) {
          heldLive += chunk;
          return;
        }
        append(chunk);
      });
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
          setLines(lastLines(lastAssistantContent(detail.messages), PULSE_TAIL_LINES));
        })
        .catch((err) => {
          settle();
          fail("session transcript", err);
        });
    }

    return () => {
      generationRef.current += 1;
      unsubscribe?.();
    };
  }, [sessionId, enabled, status]);

  return { lines, error, loading };
}
