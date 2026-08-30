import { api } from "../api/client";
import { getSessionSlice, extractPendingFromMessages, type ChatAction, type ChatState } from "../stores/chatStore";
import type { ProjectAction } from "../stores/projectStore";
import type { Message, SSEPermissionEvent, TUIStatus } from "../api/types";
import type { BusEnvelope } from "./eventBus";
import { rekeyDraft } from "./tabDrafts";
import { rekeyQueue } from "./tabQueue";

/**
 * sessionEvents — pure routing of bus envelopes into chatStore/projectStore.
 *
 * Extracted from SessionTabSync so the routing logic (rekey, status patching,
 * per-session message/turn dispatch, unknown-session warnings, reconcile) is
 * unit-testable without mounting React. SessionTabSync is now a thin adapter
 * that feeds live envelopes from the event bus into `routeBusEnvelope`, and
 * fires `reconcileOpenSessions` on reconnect.
 *
 * Session-scoped events route into chatStore only for sessions this client
 * tracks: one with an open tab (any project), or one that already has a
 * slice (kept so late turn-tail events still land after a mid-turn close).
 * Unconditionally creating slices streamed every headless/cron/other-window
 * transcript into memory forever — unbounded growth in long-lived desktop
 * sessions. A tab opened later fetches its own history (ChatPanel initial
 * load) and turn state (SessionTabSync activation sync). Only a missing
 * session id (can't be routed at all) is a loud `console.warn`.
 */

export interface SessionEventRouter {
  /** Live set of open tab ids (any project). Mutated by the session_started
   *  rekey so the first `user_message` echo isn't dropped at the gate. */
  openSessionIds: Set<string>;
  dispatch: (action: ChatAction) => void;
  projectDispatch: (action: ProjectAction) => void;
  /** Read the current chat state (the router itself never holds stale
   *  state — the caller feeds it through a ref). */
  getState: () => ChatState;
}

// Coalesces high-frequency "thinking"/"text" deltas into fixed-interval
// flushes instead of dispatching (and re-rendering the live stream) once per
// SSE frame. Reasoning streams in particular can emit many small deltas per
// second; without this, each one forces a full React render + reflow of the
// growing live block. 90ms mirrors the TUI's own fix for the identical
// problem (see TODO.md "TUI streaming render: residual O(N) viewport cost" —
// coalescing its streaming render cadence to 90ms halved in-flight CPU with
// no perceptible animation loss).
export const LIVE_DELTA_FLUSH_MS = 90;
interface PendingDelta {
  sessionId: string;
  kind: "thinking" | "text";
  text: string;
  timer: ReturnType<typeof setTimeout>;
}
const pendingDeltas = new Map<string, PendingDelta>();

function bufferLiveDelta(
  sessionId: string,
  kind: "thinking" | "text",
  delta: string,
  dispatch: (action: ChatAction) => void,
): void {
  const key = `${sessionId}:${kind}`;
  const existing = pendingDeltas.get(key);
  if (existing) {
    existing.text += delta;
    return;
  }
  const buf: PendingDelta = {
    sessionId,
    kind,
    text: delta,
    timer: setTimeout(() => {
      pendingDeltas.delete(key);
      dispatch({ type: "LIVE_DELTA", sessionId, kind, delta: buf.text });
    }, LIVE_DELTA_FLUSH_MS),
  };
  pendingDeltas.set(key, buf);
}

/** Cancel any buffered thinking/text deltas for a session without flushing
 *  them. Call when a session's slice is torn down (tab closed) — an
 *  unflushed buffer's timer would otherwise fire after RESET and recreate a
 *  phantom slice for a session nothing else references anymore. */
export function cancelLiveDeltas(sessionId: string): void {
  for (const [key, buf] of pendingDeltas) {
    if (buf.sessionId !== sessionId) continue;
    clearTimeout(buf.timer);
    pendingDeltas.delete(key);
  }
}

/** Flush any buffered thinking/text deltas for a session immediately. Must be
 *  called before any dispatch that replaces or clears `live` (the "messages"
 *  turn-boundary snapshot, turn_done, turn_error) — otherwise a buffered
 *  delta's delayed flush would land after `live` was already reset, briefly
 *  reintroducing a stray live block after the turn visually completed.
 *
 *  Also required before any dispatch that APPENDS a new part to `live`
 *  (LIVE_TOOL_START, and the active variants of LIVE_PERMISSION_CHECK /
 *  LIVE_ADVISOR_CHECKPOINT): the reducer only continues the last part when
 *  kinds match, so a still-buffered text tail flushed after a tool block was
 *  appended renders as a separate bubble *below* the tools — splitting the
 *  sentence mid-word ("…and d" [tools] "esktop."). */
function flushLiveDeltas(sessionId: string, dispatch: (action: ChatAction) => void): void {
  for (const [key, buf] of pendingDeltas) {
    if (buf.sessionId !== sessionId) continue;
    clearTimeout(buf.timer);
    pendingDeltas.delete(key);
    dispatch({ type: "LIVE_DELTA", sessionId, kind: buf.kind, delta: buf.text });
  }
}

/** Session-scoped events that must carry a session id to be routed. */
const SESSION_SCOPED_EVENTS = new Set([
  "user_message",
  "thinking",
  "text",
  "tool_start",
  "tool_output",
  "tool_result",
  "turn_started",
  "turn_heartbeat",
  "turn_done",
  "turn_error",
  "messages",
  "question",
  "question_resolved",
  "permission",
  "permission_resolved",
  "permission_check",
  "advisor_checkpoint",
  "error",
  "session_bootstrap",
]);

/** Every event name `routeBusEnvelope` handles — the two process-global ones
 *  (`session_started`, `status`) plus every session-scoped event above. The
 *  bus dispatches per-event-type (see eventBus.ts), so a consumer must
 *  subscribe to each of these individually; there's no wildcard. */
export const ROUTABLE_EVENTS = ["session_started", "status", ...SESSION_SCOPED_EVENTS];

/** Highest bus seq already applied per session, via a live envelope or a
 *  reconcile replay (see reconcileOpenSessions). Lets a mid-turn reload's
 *  live_frames replay skip anything the live EventSource already delivered
 *  during the reconnect race — see the MERGE_SNAPSHOT comment in
 *  chatStore.tsx for why that race exists. */
const lastAppliedSeq = new Map<string, number>();

function markSeqApplied(sessionId: string, seq: number): void {
  if (!sessionId || seq == null) return;
  const prev = lastAppliedSeq.get(sessionId) ?? 0;
  if (seq > prev) lastAppliedSeq.set(sessionId, seq);
}

/** Test-only: clear the seq-dedup watermark. lastAppliedSeq is module-level
 *  (real usage is one page load), so replay tests need to reset it between
 *  cases that reuse the same session id. */
export function __resetLastAppliedSeqForTests(): void {
  lastAppliedSeq.clear();
}

export function routeBusEnvelope(env: BusEnvelope, r: SessionEventRouter): void {
  const { event, session_id: envSessionId, data } = env;
  // The envelope's session_id is authoritative (the server tags at source);
  // fall back to a payload-embedded session_id for legacy-shaped payloads
  // (e.g. status snapshots carrying session_id inside data).
  const payloadSessionId =
    data && typeof data === "object" && "session_id" in data
      ? String((data as { session_id?: unknown }).session_id || "")
      : "";
  const eventSessionId = envSessionId || payloadSessionId || null;

  if (event === "session_started") {
    const started = data as { session_id?: string; request_id?: string };
    // A `new-*` tab's first message correlates via request_id. Rekey it to the
    // real session id — idempotent, so it's safe even if the direct
    // api.chat() response (App.tsx's handleSessionCreated) wins this race.
    if (
      started.request_id &&
      started.request_id.startsWith("new-") &&
      eventSessionId &&
      r.openSessionIds.has(started.request_id) &&
      !r.openSessionIds.has(eventSessionId)
    ) {
      r.dispatch({ type: "REKEY_SESSION", oldId: started.request_id, newId: eventSessionId });
      rekeyDraft(started.request_id, eventSessionId);
      rekeyQueue(started.request_id, eventSessionId);
      r.projectDispatch({
        type: "UPDATE_TAB_ID",
        oldId: started.request_id,
        newId: eventSessionId,
        newTitle: "New session",
      });
      // Keep the routing set in sync immediately. The server broadcasts
      // `session_started` and the `user_message` echo back-to-back; the rekey
      // above is an async React state update, so the set still holds only the
      // temp `new-*` id when the echo arrives. Without this, the very first
      // message of a session is dropped at the routing gate below.
      r.openSessionIds.delete(started.request_id);
      r.openSessionIds.add(eventSessionId);
    }
    return;
  }

  if (event === "status") {
    const status = data as TUIStatus;
    if (eventSessionId && sessionIsTracked(r, eventSessionId)) {
      r.dispatch({ type: "SET_TUI_STATUS", sessionId: eventSessionId, status });
      if (status.session_title) {
        r.projectDispatch({
          type: "UPDATE_TAB_TITLE",
          id: eventSessionId,
          title: status.session_title,
        });
      }
    }
    if (status.advisor_enabled !== undefined) {
      r.dispatch({ type: "SET_ADVISOR_ENABLED", enabled: !!status.advisor_enabled });
    }
    if (status.advisor_model !== undefined) {
      r.dispatch({ type: "SET_ADVISOR_MODEL", model: status.advisor_model });
    }
    if (status.small_model !== undefined) {
      r.dispatch({ type: "SET_SMALL_MODEL", model: status.small_model });
    }
    if (status.ocr_backend !== undefined) {
      r.dispatch({ type: "SET_OCR_BACKEND", backend: status.ocr_backend || "openai-compat" });
    }
    // NOTE: deliberately NOT dispatching the global SET_MODEL here. A status
    // snapshot's main_model is the *effective model of that one session*
    // (which may be a per-session override); seeding the process-global model
    // from it is what made one tab's model show up on every other tab. The
    // global fallback is seeded once from /api/config/model at startup.
    if (status.ocr_enabled !== undefined) {
      r.dispatch({ type: "SET_OCR_ENABLED", enabled: !!status.ocr_enabled });
    }
    if (status.ocr_model !== undefined) {
      r.dispatch({ type: "SET_OCR_MODEL", model: status.ocr_model || "" });
    }
    return;
  }

  // Turn lifecycle and bootstrap are per-session too — route them into that
  // session's slice (they set the streaming/turn state on the session).
  if (event === "turn_started") {
    return routeSessionScoped(r, env, eventSessionId, (sessionId) => {
      r.dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: true });
      r.dispatch({ type: "SET_ERROR", sessionId, error: null });
    });
  }
  if (event === "turn_heartbeat") {
    return routeSessionScoped(r, env, eventSessionId, (sessionId) => {
      r.dispatch({ type: "SET_TURN_HEARTBEAT", sessionId });
    });
  }
  if (event === "turn_done") {
    return routeSessionScoped(r, env, eventSessionId, (sessionId) => {
      flushLiveDeltas(sessionId, r.dispatch);
      r.dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: false });
      r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
    });
  }
  if (event === "turn_error") {
    return routeSessionScoped(r, env, eventSessionId, (sessionId) => {
      flushLiveDeltas(sessionId, r.dispatch);
      r.dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: false });
      r.dispatch({
        type: "SET_ERROR",
        sessionId,
        error: (data as { error?: string }).error || "turn failed",
      });
    });
  }
  if (event === "session_bootstrap") {
    return routeSessionScoped(r, env, eventSessionId, (sessionId) => {
      const stage = (data as { stage?: string }).stage ?? null;
      r.dispatch({ type: "SET_BOOTSTRAP_STAGE", sessionId, stage });
      // A bootstrap with no open tab may still need the tab created — handled
      // by session_started, which follows bootstrap in turn order.
    });
  }

  // Every other event is per-session chat content — route it to that
  // session's slice.
  routeSessionScoped(r, env, eventSessionId, (sessionId) => {
    switch (event) {
      case "messages": {
        flushLiveDeltas(sessionId, r.dispatch);
        const snapshot = Array.isArray(data)
          ? (data as Message[])
          : ((data as { messages?: Message[] }).messages ?? []);
        const current = r.getState().sessions[sessionId];
        // When older history is paginated in (hasMore), a live snapshot only
        // ever carries the tail the server currently holds in memory — if it
        // extends what's already loaded, append just the new tail instead of
        // replacing, so the scrolled-back prefix isn't dropped.
        if (
          current?.hasMore &&
          current.messages.length > 0 &&
          snapshot.length > current.messages.length
        ) {
          let isPrefix = true;
          for (let i = 0; i < current.messages.length; i++) {
            if (
              current.messages[i].role !== snapshot[i]?.role ||
              current.messages[i].content !== snapshot[i]?.content
            ) {
              isPrefix = false;
              break;
            }
          }
          if (isPrefix) {
            snapshot.slice(current.messages.length).forEach((message) =>
              r.dispatch({ type: "ADD_MESSAGE", sessionId, message }),
            );
            r.dispatch({ type: "SET_TOTAL", sessionId, total: snapshot.length });
            return;
          }
        }
        r.dispatch({ type: "SET_MESSAGES", sessionId, messages: snapshot });
        r.dispatch({ type: "SET_TOTAL", sessionId, total: snapshot.length });
        return;
      }
      case "user_message":
        r.dispatch({
          type: "ADD_MESSAGE",
          sessionId,
          message: { role: "user", content: (data as { content: string }).content },
        });
        r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
        return;
      case "thinking":
        bufferLiveDelta(sessionId, "thinking", (data as { delta: string }).delta, r.dispatch);
        if (!r.getState().sessions[sessionId]?.isStreaming) {
          r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
        }
        return;
      case "text":
        bufferLiveDelta(sessionId, "text", (data as { delta: string }).delta, r.dispatch);
        if (!r.getState().sessions[sessionId]?.isStreaming) {
          r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
        }
        return;
      case "tool_start":
        // Flush first: LIVE_TOOL_START appends a part, and a buffered text
        // tail (the end of the sentence that introduced the call) must land
        // in the text bubble above it, not in a new one below.
        flushLiveDeltas(sessionId, r.dispatch);
        r.dispatch({
          type: "LIVE_TOOL_START",
          sessionId,
          tool: (data as { tool: string }).tool,
          callId: (data as { call_id?: string }).call_id,
          command: (data as { command?: string }).command,
        });
        r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
        return;
      case "tool_output":
        r.dispatch({
          type: "LIVE_TOOL_OUTPUT",
          sessionId,
          callId: (data as { call_id?: string }).call_id,
          chunk: (data as { chunk: string }).chunk,
        });
        return;
      case "tool_result":
        r.dispatch({
          type: "LIVE_TOOL_RESULT",
          sessionId,
          callId: (data as { call_id?: string }).call_id,
          output: (data as { output: string }).output,
        });
        return;
      case "question": {
        const question = data as {
          request_id: string;
          questions: import("../api/types").QuestionPrompt[];
        };
        r.dispatch({
          type: "QUESTION_REQUEST",
          sessionId,
          question: { request_id: question.request_id, questions: question.questions },
        });
        return;
      }
      case "question_resolved":
        r.dispatch({ type: "QUESTION_RESOLVED", sessionId });
        return;
      case "permission":
        r.dispatch({
          type: "PERMISSION_REQUEST",
          sessionId,
          permission: data as SSEPermissionEvent,
        });
        return;
      case "permission_resolved": {
        const evData = data as { request_id?: string };
        r.dispatch({ type: "PERMISSION_RESOLVED", sessionId, requestId: evData.request_id });
        return;
      }
      case "permission_check": {
        const evData = data as { tool: string; model: string; active: boolean };
        // active=true appends a status part — flush buffered deltas first so
        // pending text isn't orphaned below it (same race as tool_start).
        if (evData.active) flushLiveDeltas(sessionId, r.dispatch);
        r.dispatch({
          type: "LIVE_PERMISSION_CHECK",
          sessionId,
          tool: evData.tool,
          model: evData.model,
          active: evData.active,
        });
        return;
      }
      case "advisor_checkpoint": {
        const evData = data as { kind: string; active: boolean };
        if (evData.active) flushLiveDeltas(sessionId, r.dispatch);
        r.dispatch({
          type: "LIVE_ADVISOR_CHECKPOINT",
          sessionId,
          kind: evData.kind,
          active: evData.active,
        });
        return;
      }
      case "error":
        r.dispatch({ type: "SET_ERROR", sessionId, error: (data as { error: string }).error });
        r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
        r.dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: false });
        return;
      default:
        return; // unrecognized event type — no routing, no warn
    }
  });
}

function routeSessionScoped(
  r: SessionEventRouter,
  env: BusEnvelope,
  eventSessionId: string | null,
  apply: (sessionId: string) => void,
): void {
  const { event } = env;
  // Not session-scoped by our table (e.g. process-global events like
  // git_status/spending/logs) — nothing to route here.
  if (!SESSION_SCOPED_EVENTS.has(event)) return;
  if (!eventSessionId) {
    console.warn(`eventBus: '${event}' event arrived without a session id — cannot route`);
    return;
  }
  // Tracked-session gate (see the module docstring): maintain slices only
  // for open tabs and already-known sessions — never create one for a
  // never-opened session.
  if (!sessionIsTracked(r, eventSessionId)) return;
  // Only advance the replay watermark for envelopes actually applied here —
  // marking it before this gate would let a dropped envelope (tab closed,
  // e.g.) silently suppress a reconcile replay of a frame the client never
  // applied. See reconcileOpenSessions.
  markSeqApplied(eventSessionId, env.seq);
  apply(eventSessionId);
}

/** True when routing may touch this session's chatStore slice: it has an
 *  open tab, or a slice already exists (late tail of a just-closed turn). */
function sessionIsTracked(r: SessionEventRouter, sessionId: string): boolean {
  return r.openSessionIds.has(sessionId) || r.getState().sessions[sessionId] !== undefined;
}

/**
 * Reconcile on reconnect: for every open tab with a real session id, fetch
 * the session's turn state (GET /api/sessions/:id/state) and refetch its
 * transcript. Placeholder `new-*` tabs are skipped. Recovery for the
 * persisted transcript is state fetch + transcript refetch; the one
 * deliberate exception is `live_frames` — whatever streaming text/thinking/
 * tool activity the server still has buffered from the session's current
 * turn — which is replayed through routeBusEnvelope (the same reducer path a
 * live envelope takes) so a mid-turn reload doesn't lose the in-progress
 * reply while waiting for turn_done. Frames already applied by a live
 * envelope during the reconnect race are skipped via lastAppliedSeq.
 */
export async function reconcileOpenSessions(
  openSessionIds: ReadonlySet<string>,
  router: SessionEventRouter,
): Promise<void> {
  const { dispatch } = router;
  const realIds = [...openSessionIds].filter((id) => !id.startsWith("new-"));
  await Promise.all(
    realIds.map(async (sessionId) => {
      try {
        const [state, detail] = await Promise.all([
          api.getSessionState(sessionId),
          api.getSession(sessionId, { limit: RECONCILE_PAGE_SIZE }),
        ]);
        // Turn state from the authoritative server snapshot. Preserve the
        // client's running state when the turn is merely paused on a pending
        // permission/question (server reports turn_active=false then).
        const slice = getSessionSlice(router.getState(), sessionId);
        // A turn paused on a pending permission/question reports
        // turn_active=false server-side. The sentinel lives in the fetched
        // transcript, which may be the only signal after a reload/reconnect
        // that missed the ask event — derive the pending-ask status from the
        // transcript, not just the current (possibly empty) client slice.
        const transcriptPending = extractPendingFromMessages(detail.messages);
        const hasPendingAsk = !!(
          slice.pendingPermission ||
          slice.pendingQuestion ||
          transcriptPending.pendingPermission ||
          transcriptPending.pendingQuestion
        );
        const wasActive = slice.turnActive;
        applyReconcileState(dispatch, sessionId, state, hasPendingAsk, wasActive);
        dispatch({ type: "MERGE_SNAPSHOT", sessionId, messages: detail.messages, total: detail.total });
        const watermark = lastAppliedSeq.get(sessionId) ?? 0;
        for (const frame of state.live_frames ?? []) {
          if (frame.seq <= watermark) continue;
          routeBusEnvelope({ event: frame.event, session_id: sessionId, seq: frame.seq, data: frame.data }, router);
        }
      } catch (err) {
        console.warn(`eventBus: reconcile failed for session ${sessionId}`, err);
      }
    }),
  );
}

export const RECONCILE_PAGE_SIZE = 100;

/**
 * applyReconcileState — pure application of a GET /api/sessions/:id/state
 * snapshot to the store. Exported for unit tests and reused by the
 * activation/load/reconnect reconcile (reconcileOpenSessions) so every
 * reconcile path shares one turn-state policy.
 *
 * `hasPendingAsk` preserves the client's running state when the turn is merely
 * paused on a permission/question ask: the server reports `turn_active: false`
 * then (runTurn has returned and is awaiting the answer), but the turn is NOT
 * finished. Clearing the client's turn state would make the running indicator
 * flicker to "stopped" during the pause — the pending dialog is still up, so we
 * keep the turn alive and only drop the stall marker.
 *
 * `wasActive` is the client's current turn-active flag. When the server reports
 * active but the client was inactive (fresh activation / missed turn_started),
 * we arm SET_TURN_STATE(true) so MERGE_SNAPSHOT preserves the in-flight live
 * buffer. When the client is already active we must NOT re-dispatch it — that
 * reducer action resets lastHeartbeatAt and clears turnStalled, which would
 * mask a genuine stall the watchdog is still waiting to confirm with a real
 * heartbeat.
 */
export function applyReconcileState(
  dispatch: (a: ChatAction) => void,
  sessionId: string,
  state: { bootstrap_stage: string; turn_active: boolean; last_seq: number },
  hasPendingAsk = false,
  wasActive = false,
): void {
  if (!state.turn_active) {
    if (hasPendingAsk) {
      // Paused on a pending ask — OR a reload/reconnect that missed the ask
      // event but whose persisted transcript carries the sentinel. The turn is
      // still running, so arm the indicator (never let it look stopped) and
      // keep the stall cleared; MERGE_SNAPSHOT will surface the dialog.
      dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: true });
      dispatch({ type: "SET_TURN_STALLED", sessionId, stalled: false });
      return;
    }
    // Server-side turn is done — clear streaming + turn state.
    dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: false });
    dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
    dispatch({ type: "SET_TURN_STALLED", sessionId, stalled: false });
  } else {
    // Turn active server-side. Only arm the running state when the client was
    // inactive (fresh activation / missed turn_started); when already active,
    // leave turn state alone so a pending stall survives until a real
    // heartbeat clears it.
    if (!wasActive) {
      dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: true });
    }
    if (state.bootstrap_stage) {
      dispatch({ type: "SET_BOOTSTRAP_STAGE", sessionId, stage: state.bootstrap_stage });
    }
  }
}
