import { useEffect, useRef } from "react";
import { useChatDispatch, useChatState } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { api, connectSessionMirror } from "../../api/client";
import type { Message, SSEPermissionEvent, TUIStatus } from "../../api/types";

/**
 * SessionTabSync owns the cross-cutting behaviors of the multi-session tab
 * bar on the Home view:
 *
 * 1. Active-tab loading — whenever the active tab changes, select that session
 *    in the chat store. ChatPanel owns paginated message loading.
 * 2. Live mirror subscription — one persistent, unfiltered stream for status
 *    and chat events. Events are routed to the active session so tab switches
 *    do not create an EventSource gap.
 * 3. Tab title replacement — live status and session detail keep tab labels
 *    current.
 */
export default function SessionTabSync() {
  const chatState = useChatState();
  const chatDispatch = useChatDispatch();
  const { tabs, activeTabId, dispatch: projectDispatch } = useProjectState();

  const loadedTabRef = useRef<string | null>(null);
  const activeSessionRef = useRef<string | null>(null);
  const pendingNewSessionRef = useRef<{ tabId: string; sessionId: string } | null>(null);
  const activeTabRef = useRef(activeTabId);
  const tabsRef = useRef(tabs);
  activeTabRef.current = activeTabId;
  tabsRef.current = tabs;
  const stateRef = useRef(chatState);
  stateRef.current = chatState;

  // During a tab switch the store briefly still contains the old session. Do
  // not accept events in that window; the session-switch effect will set the
  // new ID after the tab becomes active.
  if (
    activeTabId &&
    !activeTabId.startsWith("new-") &&
    chatState.sessionId === activeTabId
  ) {
    activeSessionRef.current = activeTabId;
  } else if (
    !activeTabId ||
    !pendingNewSessionRef.current ||
    pendingNewSessionRef.current.tabId !== activeTabId
  ) {
    activeSessionRef.current = null;
  }

  // 1. Select the session represented by the active tab. ChatPanel watches
  // sessionId and fetches the paginated message snapshot.
  useEffect(() => {
    const tabId = activeTabId;
    if (!tabId) {
      loadedTabRef.current = null;
      chatDispatch({ type: "RESET" });
      return;
    }
    if (tabId.startsWith("new-")) {
      loadedTabRef.current = tabId;
      chatDispatch({ type: "RESET" });
      return;
    }
    if (loadedTabRef.current === tabId) {
      return;
    }
    loadedTabRef.current = tabId;
    // Clear the previous session before selecting the new one. This prevents
    // an in-flight history request from another tab from being mistaken for
    // the active session's already-loaded messages.
    chatDispatch({ type: "RESET" });
    chatDispatch({ type: "SET_SESSION", sessionId: tabId });

    api
      .getSession(tabId, { limit: 1 })
      .then((detail) => {
        if (detail.title && detail.title !== tabId) {
          projectDispatch({ type: "UPDATE_TAB_TITLE", id: tabId, title: detail.title });
        }
      })
      .catch((err) => {
        console.error("Failed to load session tab title:", err);
        loadedTabRef.current = null;
      });
  }, [activeTabId, chatDispatch, projectDispatch]);

  // 2. One persistent mirror for status and chat events. The server places the
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
        if (
          activeTabRef.current?.startsWith("new-") &&
          eventSessionId &&
          started.request_id === activeTabRef.current &&
          !tabsRef.current.some((tab) => tab.id === eventSessionId)
        ) {
          pendingNewSessionRef.current = {
            tabId: activeTabRef.current,
            sessionId: eventSessionId,
          };
          activeSessionRef.current = eventSessionId;
          // The tab-id transition below must not run the normal session-switch
          // reset: the first turn is already streaming into this state.
          loadedTabRef.current = eventSessionId;
          chatDispatch({ type: "SET_SESSION", sessionId: eventSessionId });
          projectDispatch({
            type: "UPDATE_TAB_ID",
            oldId: activeTabRef.current,
            newId: eventSessionId,
            newTitle: "New session",
          });
        }
        return;
      }

      if (event === "status") {
        const status = data as TUIStatus;
        chatDispatch({ type: "SET_TUI_STATUS", status });
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

      const activeSessionId = activeSessionRef.current;
      if (!activeSessionId || !eventSessionId || eventSessionId !== activeSessionId) {
        return;
      }

      switch (event) {
        case "messages": {
          const snapshot = Array.isArray(data)
            ? (data as Message[])
            : ((data as { messages?: Message[] }).messages ?? []);
          const current = stateRef.current;
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
                chatDispatch({ type: "ADD_MESSAGE", message }),
              );
              chatDispatch({ type: "SET_TOTAL", total: snapshot.length });
              break;
            }
          }
          chatDispatch({ type: "SET_MESSAGES", messages: snapshot });
          chatDispatch({ type: "SET_TOTAL", total: snapshot.length });
          break;
        }
        case "user_message":
          chatDispatch({
            type: "ADD_MESSAGE",
            message: { role: "user", content: (data as { content: string }).content },
          });
          chatDispatch({ type: "SET_STREAMING", isStreaming: true });
          break;
        case "thinking":
          chatDispatch({
            type: "LIVE_DELTA",
            kind: "thinking",
            delta: (data as { delta: string }).delta,
          });
          chatDispatch({ type: "SET_STREAMING", isStreaming: true });
          break;
        case "text":
          chatDispatch({
            type: "LIVE_DELTA",
            kind: "text",
            delta: (data as { delta: string }).delta,
          });
          chatDispatch({ type: "SET_STREAMING", isStreaming: true });
          break;
        case "tool_start":
          chatDispatch({
            type: "LIVE_TOOL_START",
            tool: (data as { tool: string }).tool,
            command: (data as { command?: string }).command,
          });
          chatDispatch({ type: "SET_STREAMING", isStreaming: true });
          break;
        case "tool_result":
          chatDispatch({
            type: "LIVE_TOOL_RESULT",
            output: (data as { output: string }).output,
          });
          break;
        case "turn_done":
          chatDispatch({ type: "SET_STREAMING", isStreaming: false });
          break;
        case "question": {
          const question = data as {
            request_id: string;
            questions: import("../../api/types").QuestionPrompt[];
          };
          chatDispatch({
            type: "QUESTION_REQUEST",
            question: {
              request_id: question.request_id,
              questions: question.questions,
            },
          });
          break;
        }
        case "question_resolved":
          chatDispatch({ type: "QUESTION_RESOLVED" });
          break;
        case "permission":
          chatDispatch({
            type: "PERMISSION_REQUEST",
            permission: data as SSEPermissionEvent,
          });
          break;
        case "permission_resolved":
          chatDispatch({ type: "PERMISSION_RESOLVED" });
          break;
        case "error":
          chatDispatch({ type: "SET_ERROR", error: (data as { error: string }).error });
          chatDispatch({ type: "SET_STREAMING", isStreaming: false });
          break;
      }
    });
  }, [chatDispatch]);

  // 3. When a live status event carries a session title for one of our open
  // tabs, replace the tab label. Empty titles are ignored.
  useEffect(() => {
    const status = chatState.tuiStatus;
    if (!status?.session_title || !status.session_id) return;
    const tab = tabs.find((t) => t.id === status.session_id);
    if (tab && tab.title !== status.session_title) {
      projectDispatch({ type: "UPDATE_TAB_TITLE", id: tab.id, title: status.session_title });
    }
  }, [chatState.tuiStatus, tabs, projectDispatch]);

  return null;
}
