import { useCallback } from "react";
import { useChatState, useChatDispatch, getSessionSlice } from "../stores/chatStore";
import { useProjectState, findProjectPathForTab } from "../stores/projectStore";
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
  const { state: projectState } = useProjectState();
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
      // Bind the new session to the project that owns this tab (not just the
      // currently active project — the send may come from a background tab).
      // Without an explicit project_path the server would fall back to its
      // own cwd, which for the desktop app is $HOME.
      const projectPath =
        findProjectPathForTab(projectState, sessionId) ??
        projectState.activeProject?.path;
      if (!isRealSession && !projectPath) {
        dispatch({
          type: "SET_ERROR",
          sessionId,
          error: "Select a project before starting a chat.",
        });
        dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
        return;
      }
      const submitPromise = isRealSession
        ? api.sendMessage(sessionId, content)
        : api.chat(content, undefined, undefined, sessionId, projectPath).then((res) => {
            options?.onNewSession?.(res.sessionId);
            return res;
          });

      // The send endpoints resolve as soon as the server has *dispatched* the
      // turn (202), not when it finishes — they no longer hold a connection
      // open for the whole turn, which is what starved other sessions of the
      // browser's six-per-origin connection budget. So a resolved promise says
      // nothing about completion and must NOT clear the streaming flag: the
      // mirror's `turn_done` (or `error`) frame is the completion signal, and
      // both are handled in SessionTabSync. Only a failed submit is handled
      // here.
      submitPromise.catch((err) => {
        dispatch({ type: "SET_ERROR", sessionId, error: err.message || "send failed" });
        dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
      });
    },
    [sessionId, dispatch, projectState, options?.onNewSession],
  );

  // Local stop: the browser can't cancel the TUI's agent, so this only releases
  // the input and clears the UI streaming state. The turn continues server-side;
  // a later turn_done/turn_error becomes a no-op (turn state already cleared).
  const stop = useCallback(() => {
    if (!sessionId) return;
    dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: false });
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
        dispatch({ type: "PERMISSION_RESOLVED", sessionId, requestId });
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
    // isStreaming derives from the per-session turn state (Part 05): set
    // optimistically on 202 (SET_STREAMING), confirmed by turn_started
    // (turnActive), cleared by turn_done/turn_error or a rejected submit.
    isStreaming: slice.isStreaming || slice.turnActive,
    pendingPermission: slice.pendingPermission,
    pendingQuestion: slice.pendingQuestion,
  };
}
