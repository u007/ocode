import { useCallback } from "react";
import { useChatState, useChatDispatch } from "../stores/chatStore";
import { useSSE } from "./useSSE";
import { authHeaders } from "../api/client";
import type {
  SSETextEvent,
  SSESessionEvent,
  SSEDoneEvent,
  SSEPermissionEvent,
} from "../api/types";

export function useChat() {
  const state = useChatState();
  const dispatch = useChatDispatch();
  const { send, close } = useSSE();

  const sendMessage = useCallback(
    (content: string) => {
      dispatch({ type: "ADD_MESSAGE", message: { role: "user", content } });
      dispatch({ type: "SET_STREAMING", isStreaming: true });
      dispatch({ type: "SET_ERROR", error: null });

      send(content, state.sessionId ?? undefined, state.model ?? undefined, (event, data) => {
        switch (event) {
          case "session": {
            const sessionId = (data as SSESessionEvent).session_id;
            dispatch({ type: "SET_SESSION", sessionId });
            break;
          }
          case "text":
            dispatch({ type: "APPEND_DELTA", delta: (data as SSETextEvent).delta });
            break;
          case "done":
            dispatch({ type: "SET_MODEL", model: (data as SSEDoneEvent).model });
            dispatch({ type: "SET_STREAMING", isStreaming: false });
            break;
          case "error":
            dispatch({
              type: "SET_ERROR",
              error: (data as { error: string }).error,
            });
            dispatch({ type: "SET_STREAMING", isStreaming: false });
            break;
          case "permission_required": {
            const perm = data as SSEPermissionEvent;
            dispatch({
              type: "PERMISSION_REQUEST",
              permission: {
                tool: perm.tool,
                command: perm.command,
                request_id: perm.request_id,
              },
            });
            break;
          }
        }
      });
    },
    [state.sessionId, send, dispatch],
  );

  const stop = useCallback(() => {
    close();
    dispatch({ type: "SET_STREAMING", isStreaming: false });
  }, [close, dispatch]);

  const resolvePermission = useCallback(
    async (requestId: string, approved: boolean) => {
      try {
        await fetch("/api/permissions", {
          method: "POST",
          headers: { "Content-Type": "application/json", ...authHeaders() },
          body: JSON.stringify({ request_id: requestId, approved }),
        });
        dispatch({ type: "PERMISSION_RESOLVED" });
      } catch (err) {
        console.error("Failed to send permission response:", err);
      }
    },
    [dispatch],
  );

  return {
    sendMessage,
    stop,
    resolvePermission,
    isStreaming: state.isStreaming,
    pendingPermission: state.pendingPermission,
  };
}
