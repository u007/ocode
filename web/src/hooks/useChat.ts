import { useCallback } from "react";
import { useChatState, useChatDispatch } from "../stores/chatStore";
import { useSSE } from "./useSSE";
import type { SSETextEvent, SSESessionEvent, SSEDoneEvent } from "../api/types";

export function useChat() {
  const state = useChatState();
  const dispatch = useChatDispatch();
  const { send, close } = useSSE();

  const sendMessage = useCallback(
    (content: string) => {
      dispatch({ type: "ADD_MESSAGE", message: { role: "user", content } });
      dispatch({ type: "SET_STREAMING", isStreaming: true });
      dispatch({ type: "SET_ERROR", error: null });

      send(content, state.sessionId ?? undefined, (event, data) => {
        switch (event) {
          case "session":
            dispatch({ type: "SET_SESSION", sessionId: (data as SSESessionEvent).session_id });
            break;
          case "text":
            dispatch({ type: "APPEND_DELTA", delta: (data as SSETextEvent).delta });
            break;
          case "done":
            dispatch({ type: "SET_MODEL", model: (data as SSEDoneEvent).model });
            dispatch({ type: "SET_STREAMING", isStreaming: false });
            break;
          case "error":
            dispatch({ type: "SET_ERROR", error: (data as { error: string }).error });
            dispatch({ type: "SET_STREAMING", isStreaming: false });
            break;
        }
      });
    },
    [state.sessionId, send, dispatch],
  );

  const stop = useCallback(() => {
    close();
    dispatch({ type: "SET_STREAMING", isStreaming: false });
  }, [close, dispatch]);

  return { sendMessage, stop, isStreaming: state.isStreaming };
}
