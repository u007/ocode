import { useCallback } from "react";
import { useChatState, useChatDispatch, getSessionSlice } from "../stores/chatStore";
import { api } from "../api/client";
import type { QuestionAnswerPayload } from "../api/types";

interface UseChatOptions {
  /** Called when a new session is created (first message from an empty tab). */
  onNewSession?: (sessionId: string) => void;
}

// sessionId is the tab this hook is scoped to — a real session id, a
// temporary `new-<ts>` tab id (before the first message creates a session),
// or null when no tab is active.
export function useChat(sessionId: string | null, options?: UseChatOptions) {
  const state = useChatState();
  const dispatch = useChatDispatch();
  const slice = getSessionSlice(state, sessionId);

  // Submit is fire-and-forget: the message is forwarded to the TUI's agent and
  // ALL rendering (the user echo, live thinking/text tokens, tool activity, and
  // the final answer) arrives over the persistent mirror stream in
  // SessionTabSync. This keeps a single source of truth and makes the view
  // identical whether the turn was started here or in the TUI.
  const sendMessage = useCallback(
    (content: string) => {
      if (!sessionId) return;
      const isRealSession = !sessionId.startsWith("new-");
      dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
      dispatch({ type: "SET_ERROR", sessionId, error: null });

      // A `new-*` tab has no session yet — api.chat() creates one and the
      // request_id (this tab's id) lets SessionTabSync rekey the tab once the
      // "session_started" event (or this response, whichever wins the race)
      // reports the real session id.
      const submitPromise = isRealSession
        ? api.sendMessage(sessionId, content)
        : api.chat(content, undefined, undefined, sessionId).then((res) => {
            options?.onNewSession?.(res.sessionId);
            return res;
          });

      // HandleSendMessage blocks until the turn completes; the mirror's turn_done
      // is the primary completion signal. The .then is a safety net in case that
      // frame is missed; the .catch surfaces a failed submit.
      submitPromise
        .then(() => dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false }))
        .catch((err) => {
          dispatch({ type: "SET_ERROR", sessionId, error: err.message || "send failed" });
          dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
        });
    },
    [sessionId, dispatch, options?.onNewSession],
  );

  // Local stop: the browser can't cancel the TUI's agent, so this only releases
  // the input. The turn continues in the TUI and the mirror will still commit it.
  const stop = useCallback(() => {
    if (!sessionId) return;
    dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
  }, [dispatch, sessionId]);

  // Resolve a pending agent permission ask via the dedicated resolve endpoint
  // (NOT the config POST /api/permissions, which sets a tool rule). Only a
  // confirmed success dismisses the dialog; failures keep it open so the user
  // can retry.
  const resolvePermission = useCallback(
    async (requestId: string, approved: boolean) => {
      if (!sessionId) return false;
      try {
        await api.resolvePermission(requestId, sessionId, approved);
        dispatch({ type: "PERMISSION_RESOLVED", sessionId });
        return true;
      } catch (err) {
        console.error("Failed to resolve permission:", err);
        dispatch({
          type: "SET_ERROR",
          sessionId,
          error: err instanceof Error ? err.message : "permission resolve failed",
        });
        return false;
      }
    },
    [dispatch, sessionId],
  );

  // Submit answers to a pending agent question prompt. Mirrors the TUI's
  // submitQuestionAnswers: all answers go in one POST, and only a confirmed
  // success dismisses the dialog. Failures keep it open and surface an error.
  const submitQuestionAnswers = useCallback(
    async (requestId: string, answers: QuestionAnswerPayload[]) => {
      if (!sessionId) return false;
      try {
        await api.answerQuestion(requestId, sessionId, answers);
        dispatch({ type: "QUESTION_RESOLVED", sessionId });
        return true;
      } catch (err) {
        console.error("Failed to answer question:", err);
        dispatch({
          type: "SET_ERROR",
          sessionId,
          error: err instanceof Error ? err.message : "question answer failed",
        });
        return false;
      }
    },
    [dispatch, sessionId],
  );

  // Execute a shell command directly (for ! prefix commands)
  const executeShell = useCallback(
    async (
      command: string,
    ): Promise<{ output: string; exitCode: number; error: string }> => {
      try {
        return await api.shellCommand(command);
      } catch (err) {
        return {
          output: "",
          exitCode: 1,
          error:
            err instanceof Error ? err.message : "Failed to execute command",
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
    submitQuestionAnswers,
    isStreaming: slice.isStreaming,
    pendingPermission: slice.pendingPermission,
    pendingQuestion: slice.pendingQuestion,
  };
}
