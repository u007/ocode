import { useEffect, useRef } from "react";
import { useChatDispatch, useChatState, getSessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { connectSessionMirror } from "../../api/client";
import type { Message, SSEPermissionEvent, TUIStatus } from "../../api/types";

/**
 * SessionTabSync owns the cross-cutting behaviors of the multi-session tab
 * bar on the Home view:
 *
 * 1. Live mirror subscription — one persistent, unfiltered stream for status
 *    and chat events. Every event is routed to its own session's slice in
 *    chatStore as long as that session has an open tab (in any project) —
 *    not just the currently active one, so background tabs keep streaming.
 * 2. Tab title replacement — live status keeps tab labels current.
 */
export default function SessionTabSync() {
  const chatState = useChatState();
  const chatDispatch = useChatDispatch();
  const { state: projectState, dispatch: projectDispatch } = useProjectState();

  // All open tab ids across every project, recomputed each render so the SSE
  // handler (a stable closure inside the effect below) always sees the
  // current set via this ref.
  const openSessionIdsRef = useRef<Set<string>>(new Set());
  openSessionIdsRef.current = new Set(
    Object.values(projectState.tabsByProject).flat().map((tab) => tab.id),
  );

  // The mirror handler below is a stable closure (effect deps don't include
  // chatState); read current slices through this ref so it never sees stale
  // per-session state from the render that installed the listener.
  const chatStateRef = useRef(chatState);
  chatStateRef.current = chatState;

  // One persistent mirror for status and chat events. The server places the
  // session ID in the SSE `id` field; existing event payloads remain unchanged.
  useEffect(() => {
    return connectSessionMirror(undefined, (event, data, transportSessionId) => {
      const payloadSessionId =
        data && typeof data === "object" && "session_id" in data
          ? String((data as { session_id?: unknown }).session_id || "")
          : "";
      const eventSessionId = transportSessionId || payloadSessionId || null;

      if (event === "session_started") {
        const started = data as { session_id?: string; request_id?: string };
        // A `new-*` tab's first message correlates via request_id. Rekey it
        // to the real session id — idempotent, so it's safe even if the
        // direct api.chat() response (App.tsx's handleSessionCreated) wins
        // this race instead.
        if (
          started.request_id &&
          started.request_id.startsWith("new-") &&
          eventSessionId &&
          openSessionIdsRef.current.has(started.request_id) &&
          !openSessionIdsRef.current.has(eventSessionId)
        ) {
          chatDispatch({
            type: "REKEY_SESSION",
            oldId: started.request_id,
            newId: eventSessionId,
          });
          projectDispatch({
            type: "UPDATE_TAB_ID",
            oldId: started.request_id,
            newId: eventSessionId,
            newTitle: "New session",
          });
          // Keep the routing ref in sync immediately. The server broadcasts
          // `session_started` and the `user_message` echo back-to-back; the
          // rekey above is an async React state update, so the ref still holds
          // only the temp `new-*` id when the echo arrives. Without this, the
          // very first message of a session is dropped at the routing gate
          // below and never shows in the transcript until the turn-end
          // `messages` snapshot (and is lost entirely if that snapshot races).
          openSessionIdsRef.current.delete(started.request_id);
          openSessionIdsRef.current.add(eventSessionId);
        }
        return;
      }

      if (event === "status") {
        const status = data as TUIStatus;
        if (eventSessionId && openSessionIdsRef.current.has(eventSessionId)) {
          chatDispatch({ type: "SET_TUI_STATUS", sessionId: eventSessionId, status });
          if (status.session_title) {
            projectDispatch({
              type: "UPDATE_TAB_TITLE",
              id: eventSessionId,
              title: status.session_title,
            });
          }
        }
        if (status.advisor_enabled !== undefined) {
          chatDispatch({ type: "SET_ADVISOR_ENABLED", enabled: !!status.advisor_enabled });
        }
        if (status.advisor_model !== undefined) {
          chatDispatch({ type: "SET_ADVISOR_MODEL", model: status.advisor_model });
        }
        if (status.small_model !== undefined) {
          chatDispatch({ type: "SET_SMALL_MODEL", model: status.small_model });
        }
        if (status.ocr_backend !== undefined) {
          chatDispatch({ type: "SET_OCR_BACKEND", backend: status.ocr_backend || "openai-compat" });
        }
        if (status.main_model) {
          chatDispatch({ type: "SET_MODEL", model: status.main_model });
        }
        if (status.ocr_enabled !== undefined) {
          chatDispatch({ type: "SET_OCR_ENABLED", enabled: !!status.ocr_enabled });
        }
        if (status.ocr_model !== undefined) {
          chatDispatch({ type: "SET_OCR_MODEL", model: status.ocr_model });
        }
        return;
      }

      // Every other event type is per-session — route it to that session's
      // slice as long as it has an open tab. A session with no open tab (e.g.
      // driven purely from the TUI, never opened here) is ignored, same as
      // today's effective behavior for sessions this UI has never seen.
      if (!eventSessionId || !openSessionIdsRef.current.has(eventSessionId)) {
        return;
      }
      const sessionId = eventSessionId;

      switch (event) {
        case "messages": {
          const snapshot = Array.isArray(data)
            ? (data as Message[])
            : ((data as { messages?: Message[] }).messages ?? []);
          const current = getSessionSlice(chatStateRef.current, sessionId);
          // When older history is paginated in (hasMore), a live snapshot only
          // ever carries the tail the server currently holds in memory — if it
          // extends what's already loaded, append just the new tail instead of
          // replacing, so the scrolled-back prefix isn't dropped.
          if (
            current.hasMore &&
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
                chatDispatch({ type: "ADD_MESSAGE", sessionId, message }),
              );
              chatDispatch({ type: "SET_TOTAL", sessionId, total: snapshot.length });
              break;
            }
          }
          chatDispatch({ type: "SET_MESSAGES", sessionId, messages: snapshot });
          chatDispatch({ type: "SET_TOTAL", sessionId, total: snapshot.length });
          break;
        }
        case "user_message":
          chatDispatch({
            type: "ADD_MESSAGE",
            sessionId,
            message: { role: "user", content: (data as { content: string }).content },
          });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
          break;
        case "thinking":
          chatDispatch({
            type: "LIVE_DELTA",
            sessionId,
            kind: "thinking",
            delta: (data as { delta: string }).delta,
          });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
          break;
        case "text":
          chatDispatch({
            type: "LIVE_DELTA",
            sessionId,
            kind: "text",
            delta: (data as { delta: string }).delta,
          });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
          break;
        case "tool_start":
          chatDispatch({
            type: "LIVE_TOOL_START",
            sessionId,
            tool: (data as { tool: string }).tool,
            command: (data as { command?: string }).command,
          });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
          break;
        case "tool_result":
          chatDispatch({
            type: "LIVE_TOOL_RESULT",
            sessionId,
            output: (data as { output: string }).output,
          });
          break;
        case "turn_done":
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
          break;
        case "question": {
          const question = data as {
            request_id: string;
            questions: import("../../api/types").QuestionPrompt[];
          };
          chatDispatch({
            type: "QUESTION_REQUEST",
            sessionId,
            question: {
              request_id: question.request_id,
              questions: question.questions,
            },
          });
          break;
        }
        case "question_resolved":
          chatDispatch({ type: "QUESTION_RESOLVED", sessionId });
          break;
        case "permission":
          chatDispatch({
            type: "PERMISSION_REQUEST",
            sessionId,
            permission: data as SSEPermissionEvent,
          });
          break;
        case "permission_resolved": {
          const evData = data as { request_id?: string };
          chatDispatch({ type: "PERMISSION_RESOLVED", sessionId, requestId: evData.request_id });
          break;
        }
        case "error":
          chatDispatch({ type: "SET_ERROR", sessionId, error: (data as { error: string }).error });
          chatDispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
          break;
      }
    });
  }, [chatDispatch, projectDispatch]);

  return null;
}
