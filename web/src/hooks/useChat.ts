import { useCallback } from "react";
import {
  useChatSelector,
  useChatDispatch,
  useChatStateRef,
  getSessionSlice,
  extractAskContext,
} from "../stores/chatStore";
import { useProjectState, findProjectPathForTab } from "../stores/projectStore";
import { api, ApiError } from "../api/client";
import { resolveSessionHost } from "./useSessionHost";
import type { PermissionDecision, QuestionAnswerPayload } from "../api/types";
import type { PermissionDecideResult } from "../components/Chat/PermissionDialog";

interface UseChatOptions {
  /** Called when a new session is created (first message from an empty tab). */
  onNewSession?: (sessionId: string) => void;
}

// sessionId is the tab this hook is scoped to — a real session id, a
// temporary `new-<ts>` tab id (before the first message creates a session),
// or null when no tab is active.
export function useChat(sessionId: string | null, options?: UseChatOptions) {
  const dispatch = useChatDispatch();
  const { state: projectState } = useProjectState();
  // Narrow, field-level subscriptions. Subscribing to the whole session slice
  // (`getSessionSlice(s, sessionId)`) used to re-render every consumer — HomeApp
  // and each mounted ChatInput — on every streamed token: `updateSession`
  // replaces the slice object on each LIVE_DELTA, which flushes every 90ms
  // (see LIVE_DELTA_FLUSH_MS in lib/sessionEvents.ts). Only the fields this
  // hook actually RETURNS should drive re-renders, so each gets its own
  // selector with the store's default Object.is comparison. Per-turn fields
  // that callbacks need (e.g. `model` for a draft tab's first send) are read
  // imperatively through stateRef at call time instead.
  const stateRef = useChatStateRef();
  const wasInterrupted = useChatSelector(
    (s) => getSessionSlice(s, sessionId).wasInterrupted,
  );
  // True only for an LLM-loop failure (SSE turn_error/error), never a submit
  // failure — so the composer's Retry is offered only when the server actually
  // has a turn to re-run. See SessionSlice.turnError.
  const turnError = useChatSelector(
    (s) => getSessionSlice(s, sessionId).turnError,
  );
  const isStreaming = useChatSelector((s) => {
    const slice = getSessionSlice(s, sessionId);
    return slice.isStreaming || slice.turnActive;
  });
  // Whether this session has any conversation content yet — committed messages
  // or an in-progress turn streaming into the live buffer. Drives the
  // composer's quick-action strip (Compact / Continue / Recap), which is
  // hidden on a brand-new (`new-*`) tab or an empty session: there is nothing
  // to compact, nothing to continue, and nothing to recap. A narrow boolean
  // selector so streamed deltas never re-render consumers.
  const hasConversation = useChatSelector((s) => {
    const slice = getSessionSlice(s, sessionId);
    return slice.messages.length > 0 || slice.live.length > 0;
  });
  const pendingPermission = useChatSelector(
    (s) => getSessionSlice(s, sessionId).pendingPermission,
  );
  const pendingQuestion = useChatSelector(
    (s) => getSessionSlice(s, sessionId).pendingQuestion,
  );
  // The assistant message (prose + reasoning) behind the pending ask, shown
  // inside the permission/question dialogs. Shallow-compared so streamed
  // deltas elsewhere in the store do not re-render App; null whenever nothing
  // is pending, so the common case is an O(1) selector return.
  const askContext = useChatSelector(
    (s) => {
      const slice = getSessionSlice(s, sessionId);
      if (!slice.pendingPermission && !slice.pendingQuestion) return null;
      return extractAskContext(slice.messages, slice.live);
    },
    (a, b) =>
      (a?.thinking ?? "") === (b?.thinking ?? "") &&
      (a?.text ?? "") === (b?.text ?? ""),
  );
  // The SSH/WSL host for that path (undefined for a local project). A `!`
  // command must run on the machine that owns the project — sending it to a
  // remote host is what keeps the shell resolved there instead of the local
  // one producing `fork/exec /bin/zsh: no such file or directory`.
  // Resolved with the same single-match trust rule as the terminal: a path
  // registered for both a local and a remote project is ambiguous, and
  // picking whichever entry sorts first would run the command on the wrong
  // machine, so an ambiguous path sends no host (the server runs it locally).
  // useChat falls back to the active project for brand-new draft tabs.
  const projectHost = resolveSessionHost(projectState, sessionId ?? undefined, { fallbackToActive: true });
  const projectPath = sessionId
    ? findProjectPathForTab(projectState, sessionId) ?? projectState.activeProject?.path
    : projectState.activeProject?.path;

  // Recover the pending ask when a send is refused because the session is
  // already paused on one (HTTP 409 ErrPermissionPending). The live
  // `permission`/`question` event may have been missed while the tab was
  // backgrounded, and the sentinel may be absent from the persisted
  // transcript, so the server's live session state is the only place the ask
  // can be recovered from. Hydrating it opens the PermissionDialog (or the
  // question prompt) so the user can approve/reject instead of being stuck
  // with "resolve it before sending a new message".
  const hydratePendingAsks = useCallback(async () => {
    if (!sessionId || sessionId.startsWith("new-")) return;
    try {
      const state = await api.getSessionState(sessionId, projectHost);
      for (const permission of state.pending_asks?.permissions ?? []) {
        dispatch({ type: "PERMISSION_REQUEST", sessionId, permission });
      }
      for (const question of state.pending_asks?.questions ?? []) {
        dispatch({ type: "QUESTION_REQUEST", sessionId, question });
      }
    } catch (err) {
      console.warn("failed to recover pending permission ask", err);
    }
  }, [sessionId, dispatch, projectHost]);

  // Submit is fire-and-forget: the message is forwarded to the TUI's agent and
  // ALL rendering (the user echo, live thinking/text tokens, tool activity, and
  // the final answer) arrives over the persistent mirror stream in
  // SessionTabSync. This keeps a single source of truth and makes the view
  // identical whether the turn was started here or in the TUI.
  const sendMessage = useCallback(
    (content: string): Promise<boolean> => {
      if (!sessionId) return Promise.resolve(false);
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
      if (!isRealSession && !projectPath) {
        dispatch({
          type: "SET_ERROR",
          sessionId,
          error: "Select a project before starting a chat.",
        });
        dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
        return Promise.resolve(false);
      }
      // A draft tab's locally-picked model and/or permission mode (sidebar
      // pickers) ride along with the first message; the server persists them
      // as the new session's overrides. Undefined when the tab never changed
      // them (server falls back to the global defaults). Read imperatively at
      // send time so they do not have to be reactive render dependencies.
      const draftSlice = getSessionSlice(stateRef.current, sessionId);
      const model = draftSlice.model;
      const permissionMode = draftSlice.permissionMode;
      const submitPromise = isRealSession
        ? api.sendMessage(sessionId, content, projectHost)
        : api
            .chat(content, undefined, model, sessionId, projectPath, projectHost, permissionMode)
            .then((res) => {
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
      // Resolve true once the server has *dispatched* the turn (202) — this is
      // the success/acceptance signal. A rejected submit (network/validation)
      // resolves false so the caller can roll back any queue bookkeeping.
      return submitPromise
        .then(() => true)
        .catch((err) => {
          // 409 = the session is paused on a permission/question ask the
          // server refused to step past. Recover the dialog from live state
          // so the user can resolve it (see hydratePendingAsks).
          if (err instanceof ApiError && err.status === 409) {
            void hydratePendingAsks();
          }
          dispatch({ type: "SET_ERROR", sessionId, error: err?.message || "send failed" });
          dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
          return false;
        });
    },
    [sessionId, dispatch, projectPath, projectHost, options?.onNewSession, stateRef, hydratePendingAsks],
  );

  // Stop: optimistically clears local streaming state and queues, then asks
  // the server to cancel the in-flight turn (Agent.Cancel). Queued messages
  // are preserved and auto-drain is suppressed until Resume, mirroring the
  // TUI's streamWasInterrupted behavior. The server's eventual turn_done or
  // turn_error becomes a no-op for UI state (already cleared) but still
  // syncs the persisted transcript.
  const stop = useCallback(() => {
    if (!sessionId) return;
    dispatch({ type: "INTERRUPT", sessionId });
    // Don't block UI on the cancel RPC; fire and forget. If the session is
    // a temp `new-*` id with no server session yet, skip the call.
    if (!sessionId.startsWith("new-")) {
      api.cancelSession(sessionId, projectHost).catch((err) => {
        console.warn("cancel session failed", err);
      });
    }
  }, [dispatch, sessionId, projectHost]);

  const resume = useCallback(() => {
    if (!sessionId) return;
    dispatch({ type: "SET_WAS_INTERRUPTED", sessionId, wasInterrupted: false });
  }, [dispatch, sessionId]);

  // Retry the last turn after a user Stop or an LLM-loop error: clear the stop
  // gate / error state, then ask the server to re-run the existing transcript
  // tail IN PLACE (POST /api/sessions/:id/retry). The server never appends a new
  // user row, so the user's message is not duplicated — see HandleRetrySession.
  // Mirrors the TUI's Ctrl+Y retry (model.retryLastLLMError). A draft (`new-*`)
  // tab has no server session and nothing to retry.
  const retryLastTurn = useCallback(async (): Promise<boolean> => {
    if (!sessionId || sessionId.startsWith("new-")) return false;
    dispatch({ type: "SET_WAS_INTERRUPTED", sessionId, wasInterrupted: false });
    // SET_ERROR(null) also clears the retryable `turnError` flag (see chatStore).
    dispatch({ type: "SET_ERROR", sessionId, error: null });
    dispatch({ type: "SET_STREAMING", sessionId, isStreaming: true });
    try {
      await api.retrySession(sessionId, projectHost);
      return true;
    } catch (err) {
      dispatch({
        type: "SET_ERROR",
        sessionId,
        error: err instanceof Error ? err.message : "retry failed",
      });
      dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
      return false;
    }
  }, [sessionId, dispatch, projectHost]);

  // Resolve a pending agent permission ask via the dedicated resolve endpoint
  // (NOT the config POST /api/permissions, which sets a tool rule). A confirmed
  // success dismisses the dialog; a retryable failure (network, 5xx) keeps it
  // open with the error shown so the user can retry. A 404/409 means the
  // server no longer holds this ask (the agent was released or the server
  // restarted — the persisted transcript drops the sentinel on reload — or
  // the ask was already answered elsewhere): retrying can never succeed, so
  // the dialog is dismissed instead of staying stuck open. Note: the server
  // also broadcasts a permission_resolved SSE frame as soon as the decision is
  // applied (before the continuation round), so the dialog closes promptly
  // even while this request is still in flight.
  const resolvePermission = useCallback(
    async (requestId: string, decision: PermissionDecision): Promise<PermissionDecideResult> => {
      if (!sessionId) return { ok: false, error: "no active session" };
      try {
        await api.resolvePermission(requestId, sessionId, decision, projectHost);
        dispatch({ type: "PERMISSION_RESOLVED", sessionId, requestId });
        return { ok: true };
      } catch (err) {
        console.error("Failed to resolve permission:", err);
        const message = err instanceof Error ? err.message : "permission resolve failed";
        const stale = err instanceof ApiError && (err.status === 404 || err.status === 409);
        if (stale) {
          dispatch({ type: "PERMISSION_RESOLVED", sessionId, requestId });
        }
        dispatch({ type: "SET_ERROR", sessionId, error: message });
        return { ok: false, error: message };
      }
    },
    [dispatch, sessionId, projectHost],
  );

  // Submit answers to a pending agent question prompt. Mirrors the TUI's
  // submitQuestionAnswers: all answers go in one POST, and only a confirmed
  // success dismisses the dialog. Failures keep it open and surface an error.
  const submitQuestionAnswers = useCallback(
    async (requestId: string, answers: QuestionAnswerPayload[]) => {
      if (!sessionId) return false;
      try {
        await api.answerQuestion(requestId, sessionId, answers, projectHost);
        // Echo the answers locally before dismissing the dialog so the chat
        // shows the questions + the selections that were sent to the LLM
        // immediately, without waiting for the continuation turn's snapshot
        // (see QUESTION_ANSWERED in chatStore).
        dispatch({ type: "QUESTION_ANSWERED", sessionId, requestId, answers });
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
    [dispatch, sessionId, projectHost],
  );

  // Cancel a pending agent question prompt without answering it (the web
  // equivalent of the TUI's Esc on the dialog). Mirrors submitQuestionAnswers:
  // only a confirmed success dismisses the dialog. A 404/409 means the server
  // no longer holds this ask (already answered/dismissed elsewhere, or the
  // agent was released) — retrying can never succeed, so the dialog is
  // dismissed locally instead of staying stuck open.
  const cancelQuestion = useCallback(
    async (requestId: string): Promise<boolean> => {
      if (!sessionId) return false;
      try {
        await api.cancelQuestion(requestId, sessionId, projectHost);
        dispatch({ type: "QUESTION_DISMISSED", sessionId, requestId });
        return true;
      } catch (err) {
        console.error("Failed to cancel question:", err);
        const stale = err instanceof ApiError && (err.status === 404 || err.status === 409);
        if (stale) {
          dispatch({ type: "QUESTION_RESOLVED", sessionId, requestId });
          return true;
        }
        dispatch({
          type: "SET_ERROR",
          sessionId,
          error: err instanceof Error ? err.message : "question cancel failed",
        });
        return false;
      }
    },
    [dispatch, sessionId, projectHost],
  );

  // Execute a shell command directly (for ! prefix commands). A remote
  // project's command runs on its host through the host's own login shell;
  // `host` is omitted for local projects so the server keeps its local path.
  // The tab id is passed as `session` so a local command runs in the tab's
  // persistent shell, carrying env/aliases and cwd between commands; `cwd` in
  // the result is the directory the command actually ran in.
  const executeShell = useCallback(
    async (
      command: string,
    ): Promise<{ output: string; exitCode: number; error: string; cwd: string }> => {
      try {
        return await api.shellCommand(command, projectPath, projectHost, sessionId ?? undefined);
      } catch (err) {
        return {
          output: "",
          exitCode: 1,
          error:
            err instanceof Error ? err.message : "Failed to execute command",
          // No server response: report the directory the command would have run
          // in, so the client always has a cwd to show.
          cwd: projectPath ?? "",
        };
      }
    },
    [projectPath, projectHost, sessionId],
  );

  return {
    sendMessage,
    executeShell,
    stop,
    resume,
    retryLastTurn,
    wasInterrupted,
    turnError,
    resolvePermission,
    submitQuestionAnswers,
    cancelQuestion,
    // isStreaming derives from the per-session turn state (Part 05): set
    // optimistically on 202 (SET_STREAMING), confirmed by turn_started
    // (turnActive), cleared by turn_done/turn_error or a rejected submit.
    isStreaming,
    hasConversation,
    pendingPermission,
    pendingQuestion,
    askContext,
  };
}
