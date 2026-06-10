import { useCallback } from "react";
import { useChatState, useChatDispatch } from "../stores/chatStore";
import { api, authHeaders } from "../api/client";

export function useChat() {
  const state = useChatState();
  const dispatch = useChatDispatch();

  // Submit is fire-and-forget: the message is forwarded to the TUI's agent and
  // ALL rendering (the user echo, live thinking/text tokens, tool activity, and
  // the final answer) arrives over the persistent mirror stream in SessionPage.
  // This keeps a single source of truth and makes the view identical whether the
  // turn was started here or in the TUI.
  const sendMessage = useCallback(
    (content: string) => {
      const sid = state.sessionId;
      if (!sid) {
        dispatch({ type: "SET_ERROR", error: "no active session" });
        return;
      }
      dispatch({ type: "SET_STREAMING", isStreaming: true });
      dispatch({ type: "SET_ERROR", error: null });

      // HandleSendMessage blocks until the turn completes; the mirror's turn_done
      // is the primary completion signal. The .then is a safety net in case that
      // frame is missed; the .catch surfaces a failed submit.
      api
        .sendMessage(sid, content)
        .then(() => dispatch({ type: "SET_STREAMING", isStreaming: false }))
        .catch((err) => {
          dispatch({ type: "SET_ERROR", error: err.message || "send failed" });
          dispatch({ type: "SET_STREAMING", isStreaming: false });
        });
    },
    [state.sessionId, dispatch],
  );

  // Local stop: the browser can't cancel the TUI's agent, so this only releases
  // the input. The turn continues in the TUI and the mirror will still commit it.
  const stop = useCallback(() => {
    dispatch({ type: "SET_STREAMING", isStreaming: false });
  }, [dispatch]);

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
