import { useCallback } from "react";
import { useChatState, useChatDispatch } from "../stores/chatStore";
import { api, authHeaders } from "../api/client";

interface UseChatOptions {
  /** Called when a new session is created (first message from Home page). */
  onNewSession?: (sessionId: string) => void;
}

export function useChat(options?: UseChatOptions) {
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
      dispatch({ type: "SET_STREAMING", isStreaming: true });
      dispatch({ type: "SET_ERROR", error: null });

      // If no session exists yet (Home page after /new or first message),
      // use api.chat() which creates a new session and returns the session ID.
      const submitPromise = sid
        ? api.sendMessage(sid, content)
        : api.chat(content).then((res) => {
            // Store the new session ID so subsequent messages go to the same session
            dispatch({ type: "SET_SESSION", sessionId: res.sessionId });
            // Notify caller so it can navigate to the new session page
            options?.onNewSession?.(res.sessionId);
            return res;
          });

      // HandleSendMessage blocks until the turn completes; the mirror's turn_done
      // is the primary completion signal. The .then is a safety net in case that
      // frame is missed; the .catch surfaces a failed submit.
      submitPromise
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

  // Execute a shell command directly (for ! prefix commands)
  const executeShell = useCallback(
    async (command: string): Promise<{ output: string; exitCode: number; error: string }> => {
      try {
        return await api.shellCommand(command);
      } catch (err) {
        return {
          output: "",
          exitCode: 1,
          error: err instanceof Error ? err.message : "Failed to execute command",
        };
      }
    },
    [],
  );

  return {
    sendMessage,
    executeShell,
    stop,
    resolvePermission,
    isStreaming: state.isStreaming,
    pendingPermission: state.pendingPermission,
  };
}
