import { api } from "../api/client";
import type { ChatAction, ChatState } from "../stores/chatStore";
import type { ProjectAction } from "../stores/projectStore";
import type { Message, SSEPermissionEvent, TUIStatus } from "../api/types";
import type { BusEnvelope } from "./eventBus";

/**
 * sessionEvents — pure routing of bus envelopes into chatStore/projectStore.
 *
 * Extracted from SessionTabSync so the routing logic (rekey, status patching,
 * per-session message/turn dispatch, unknown-session warnings, reconcile) is
 * unit-testable without mounting React. SessionTabSync is now a thin adapter
 * that feeds live envelopes from the event bus into `routeBusEnvelope`, and
 * fires `reconcileOpenSessions` on reconnect.
 *
 * Unknown-session events are a loud `console.warn`, never a silent drop —
 * a session-scoped event that no open tab can receive indicates either a
 * missed `session_started` rekey or a session driven elsewhere (TUI) that the
 * user should know about.
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

/** Session-scoped events that must carry a session id; a session-scoped event
 *  whose session has no open tab is a routing miss (warn, never silent). */
const SESSION_SCOPED_EVENTS = new Set([
  "user_message",
  "thinking",
  "text",
  "tool_start",
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
  "error",
  "session_bootstrap",
]);

/** Every event name `routeBusEnvelope` handles — the two process-global ones
 *  (`session_started`, `status`) plus every session-scoped event above. The
 *  bus dispatches per-event-type (see eventBus.ts), so a consumer must
 *  subscribe to each of these individually; there's no wildcard. */
export const ROUTABLE_EVENTS = ["session_started", "status", ...SESSION_SCOPED_EVENTS];

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
    if (eventSessionId && r.openSessionIds.has(eventSessionId)) {
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
    if (status.main_model) {
      r.dispatch({ type: "SET_MODEL", model: status.main_model });
    }
    if (status.ocr_enabled !== undefined) {
      r.dispatch({ type: "SET_OCR_ENABLED", enabled: !!status.ocr_enabled });
    }
    if (status.ocr_model !== undefined) {
      r.dispatch({ type: "SET_OCR_MODEL", model: status.ocr_model || "" });
    }
    return;
  }

  // Turn lifecycle and bootstrap are per-session too — route them through the
  // open-tab gate below (they set the streaming/turn state on the session).
  if (event === "turn_started") {
    return routeSessionScoped(env, eventSessionId, r, (sessionId) => {
      r.dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: true });
      r.dispatch({ type: "SET_ERROR", sessionId, error: null });
    });
  }
  if (event === "turn_heartbeat") {
    return routeSessionScoped(env, eventSessionId, r, (sessionId) => {
      r.dispatch({ type: "SET_TURN_HEARTBEAT", sessionId });
    });
  }
  if (event === "turn_done") {
    return routeSessionScoped(env, eventSessionId, r, (sessionId) => {
      r.dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: false });
      r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
    });
  }
  if (event === "turn_error") {
    return routeSessionScoped(env, eventSessionId, r, (sessionId) => {
      r.dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: false });
      r.dispatch({
        type: "SET_ERROR",
        sessionId,
        error: (data as { error?: string }).error || "turn failed",
      });
    });
  }
  if (event === "session_bootstrap") {
    return routeSessionScoped(env, eventSessionId, r, (sessionId) => {
      const stage = (data as { stage?: string }).stage ?? null;
      r.dispatch({ type: "SET_BOOTSTRAP_STAGE", sessionId, stage });
      // A bootstrap with no open tab may still need the tab created — handled
      // by session_started, which follows bootstrap in turn order.
    });
  }

  // Every other event is per-session chat content — route it to that
  // session's slice as long as it has an open tab.
  routeSessionScoped(env, eventSessionId, r, (sessionId) => {
    switch (event) {
      case "messages": {
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
        r.dispatch({
          type: "LIVE_DELTA",
          sessionId,
          kind: "thinking",
          delta: (data as { delta: string }).delta,
        });
        r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
        return;
      case "text":
        r.dispatch({
          type: "LIVE_DELTA",
          sessionId,
          kind: "text",
          delta: (data as { delta: string }).delta,
        });
        r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
        return;
      case "tool_start":
        r.dispatch({
          type: "LIVE_TOOL_START",
          sessionId,
          tool: (data as { tool: string }).tool,
          command: (data as { command?: string }).command,
        });
        r.dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
        return;
      case "tool_result":
        r.dispatch({
          type: "LIVE_TOOL_RESULT",
          sessionId,
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
  env: BusEnvelope,
  eventSessionId: string | null,
  r: SessionEventRouter,
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
  if (!r.openSessionIds.has(eventSessionId)) {
    console.warn(
      `eventBus: '${event}' event for session ${eventSessionId} (project ${env.project ?? "?"}) has no open tab — dropping`,
    );
    return;
  }
  apply(eventSessionId);
}

/**
 * Reconcile on reconnect: for every open tab with a real session id, fetch
 * the session's turn state (GET /api/sessions/:id/state) and refetch its
 * transcript. Placeholder `new-*` tabs are skipped. Recovery after a
 * disconnect is state fetch + transcript refetch, never event replay.
 */
export async function reconcileOpenSessions(
  openSessionIds: ReadonlySet<string>,
  dispatch: (action: ChatAction) => void,
): Promise<void> {
  const realIds = [...openSessionIds].filter((id) => !id.startsWith("new-"));
  await Promise.all(
    realIds.map(async (sessionId) => {
      try {
        const [state, detail] = await Promise.all([
          api.getSessionState(sessionId),
          api.getSession(sessionId, { limit: RECONCILE_PAGE_SIZE }),
        ]);
        // Turn state from the authoritative server snapshot.
        dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: state.turn_active });
        if (state.bootstrap_stage) {
          dispatch({ type: "SET_BOOTSTRAP_STAGE", sessionId, stage: state.bootstrap_stage });
        }
        dispatch({ type: "MERGE_SNAPSHOT", sessionId, messages: detail.messages, total: detail.total });
      } catch (err) {
        console.warn(`eventBus: reconcile failed for session ${sessionId}`, err);
      }
    }),
  );
}

export const RECONCILE_PAGE_SIZE = 50;
