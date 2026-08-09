import { createContext, useContext, useReducer, type ReactNode } from "react";
import type { Message, LivePart, TUIStatus, QuestionPrompt } from "../api/types";

export interface PermissionRequest {
  tool: string;
  command?: string;
  rule?: string;
  summary?: string;
  deny_reason?: string;
  model_unavailable?: string;
  request_id: string;
}

export interface QuestionRequest {
  request_id: string;
  questions: QuestionPrompt[];
}

export interface SessionContextMetrics {
  currentTokens: number;
  maxTokens: number;
  model: string;
}

/** Per-session chat state — one entry per open tab, keyed by session id (or
 *  the temporary `new-<ts>` tab id before the first message creates a real
 *  session). Kept in `ChatState.sessions` so every open tab can render and
 *  stream independently instead of sharing one global "current session". */
export interface SessionSlice {
  messages: Message[];
  // In-progress turn, streamed live until the turn_done snapshot commits it.
  live: LivePart[];
  isStreaming: boolean;
  error: string | null;
  pendingPermission: PermissionRequest | null;
  pendingQuestion: QuestionRequest | null;
  totalMessages: number; // total messages on server
  hasMore: boolean; // whether older messages exist
  loadingMore: boolean; // currently fetching older messages
  // True once this session's first page has been fetched at least once.
  // Lets ChatPanel skip re-fetching on remount and lets OpenSessionBar know
  // when a tab's "loading" spinner should clear.
  initialized: boolean;
  // Agent-run rows the user collapsed in the agent preview rail / Agents
  // panel, keyed by run id. Kept per-session so expand/collapse survives
  // switching session tabs or projects — RunNode remounts (new run tree,
  // new session) would otherwise reset every row to expanded.
  collapsedRunIds: string[];
}

export const emptySessionSlice: SessionSlice = {
  messages: [],
  live: [],
  isStreaming: false,
  error: null,
  pendingPermission: null,
  pendingQuestion: null,
  totalMessages: 0,
  hasMore: false,
  loadingMore: false,
  initialized: false,
  collapsedRunIds: [],
};

/** Reads one session's slice, falling back to the shared empty default for a
 *  session that hasn't been touched yet (or a null/missing id). Never
 *  mutated — always spread when producing an updated slice. */
export function getSessionSlice(
  state: ChatState,
  sessionId: string | null | undefined,
): SessionSlice {
  if (!sessionId) return emptySessionSlice;
  return state.sessions[sessionId] ?? emptySessionSlice;
}

export interface ChatState {
  sessions: Record<string, SessionSlice>;
  // Global fields: these reflect the single backend TUI/process, not any one
  // tab, so they stay flat on the top-level state.
  model: string | null;
  smallModel: string | null;
  smallModelEnabled: boolean;
  advisorModel: string | null;
  advisorEnabled: boolean;
  ocrModel: string | null;
  ocrEnabled: boolean;
  ocrBackend: string | null;
  // Live TUI status (model, advisor, IDE, session, cwd, context, spending,
  // modified files, LSP servers, extra paths). Updated by the SSE "status"
  // event so the bar tracks the TUI without polling. Null until the first
  // event arrives or the initial fetch resolves.
  tuiStatus: TUIStatus | null;
  sessionContext: SessionContextMetrics | null;
  spendingUSD: number | null;
  // True once the very first /api/tui-status fetch has resolved. Lets the UI
  // show "loading…" vs. "not connected" while waiting for the first frame.
  tuiStatusReady: boolean;
}

export type ChatAction =
  | { type: "ADD_MESSAGE"; sessionId: string; message: Message }
  | { type: "SET_MESSAGES"; sessionId: string; messages: Message[] }
  | { type: "SET_MODEL"; model: string }
  | { type: "SET_SMALL_MODEL"; model: string }
  | { type: "SET_SMALL_MODEL_ENABLED"; enabled: boolean }
  | { type: "SET_ADVISOR_MODEL"; model: string }
  | { type: "SET_ADVISOR_ENABLED"; enabled: boolean }
  | { type: "SET_OCR_MODEL"; model: string }
  | { type: "SET_OCR_ENABLED"; enabled: boolean }
  | { type: "SET_OCR_BACKEND"; backend: string }
  | { type: "SET_STREAMING"; sessionId: string; isStreaming: boolean }
  | { type: "SET_ERROR"; sessionId: string; error: string | null }
  | { type: "APPEND_DELTA"; sessionId: string; delta: string }
  | { type: "LIVE_DELTA"; sessionId: string; kind: "thinking" | "text"; delta: string }
  | { type: "LIVE_TOOL_START"; sessionId: string; tool: string; command?: string }
  | { type: "LIVE_TOOL_RESULT"; sessionId: string; output: string }
  | { type: "LIVE_RESET"; sessionId: string }
  | { type: "PERMISSION_REQUEST"; sessionId: string; permission: PermissionRequest }
  | { type: "PERMISSION_RESOLVED"; sessionId: string }
  | { type: "QUESTION_REQUEST"; sessionId: string; question: QuestionRequest }
  | { type: "QUESTION_RESOLVED"; sessionId: string }
  | { type: "PREPEND_MESSAGES"; sessionId: string; messages: Message[]; total: number }
  | { type: "SET_LOADING_MORE"; sessionId: string; loading: boolean }
  | { type: "MERGE_SNAPSHOT"; sessionId: string; messages: Message[]; total: number }
  | { type: "SET_TOTAL"; sessionId: string; total: number }
  | { type: "SET_SESSION_CONTEXT"; context: SessionContextMetrics | null }
  | { type: "SET_SPENDING"; spendingUSD: number | null }
  | { type: "SET_TUI_STATUS"; status: TUIStatus }
  | { type: "SET_TUI_STATUS_READY"; ready: boolean }
  | { type: "REKEY_SESSION"; oldId: string; newId: string }
  | { type: "TOGGLE_RUN_COLLAPSED"; sessionId: string; runId: string }
  | { type: "RESET"; sessionId: string };

export const initialState: ChatState = {
  sessions: {},
  model: null,
  smallModel: null,
  smallModelEnabled: false,
  advisorModel: null,
  advisorEnabled: true,
  ocrModel: null,
  ocrEnabled: false,
  ocrBackend: "openai-compat",
  tuiStatus: null,
  sessionContext: null,
  spendingUSD: null,
  tuiStatusReady: false,
};

function updateSession(
  state: ChatState,
  sessionId: string,
  updater: (slice: SessionSlice) => SessionSlice,
): ChatState {
  const current = state.sessions[sessionId] ?? emptySessionSlice;
  return { ...state, sessions: { ...state.sessions, [sessionId]: updater(current) } };
}

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "ADD_MESSAGE":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        messages: [...s.messages, action.message],
      }));
    case "SET_MESSAGES":
      // Authoritative snapshot lands at a turn boundary — commit it and clear
      // the live buffer it supersedes.
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        messages: action.messages,
        live: [],
      }));
    case "SET_MODEL":
      return { ...state, model: action.model };
    case "SET_SMALL_MODEL":
      return { ...state, smallModel: action.model };
    case "SET_SMALL_MODEL_ENABLED":
      return { ...state, smallModelEnabled: action.enabled };
    case "SET_ADVISOR_MODEL":
      return { ...state, advisorModel: action.model };
    case "SET_ADVISOR_ENABLED":
      return { ...state, advisorEnabled: action.enabled };
    case "SET_OCR_MODEL":
      return { ...state, ocrModel: action.model };
    case "SET_OCR_ENABLED":
      return { ...state, ocrEnabled: action.enabled };
    case "SET_OCR_BACKEND":
      return { ...state, ocrBackend: action.backend };
    case "SET_STREAMING":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        isStreaming: action.isStreaming,
      }));
    case "SET_ERROR":
      return updateSession(state, action.sessionId, (s) => ({ ...s, error: action.error }));
    case "APPEND_DELTA":
      return updateSession(state, action.sessionId, (s) => {
        const msgs = [...s.messages];
        const last = msgs[msgs.length - 1];
        if (last && last.role === "assistant") {
          msgs[msgs.length - 1] = { ...last, content: last.content + action.delta };
        } else {
          msgs.push({ role: "assistant", content: action.delta });
        }
        return { ...s, messages: msgs };
      });
    case "LIVE_DELTA":
      return updateSession(state, action.sessionId, (s) => {
        const live = [...s.live];
        const last = live[live.length - 1];
        if (last && last.kind === action.kind) {
          live[live.length - 1] = { ...last, text: last.text + action.delta };
        } else {
          live.push({ kind: action.kind, text: action.delta });
        }
        return { ...s, live };
      });
    case "LIVE_TOOL_START":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        live: [...s.live, { kind: "tool", tool: action.tool, command: action.command }],
      }));
    case "LIVE_TOOL_RESULT":
      return updateSession(state, action.sessionId, (s) => {
        const live = [...s.live];
        // Attach to the most recent tool part still awaiting its result.
        for (let i = live.length - 1; i >= 0; i--) {
          const part = live[i];
          if (part.kind === "tool" && part.output === undefined) {
            live[i] = { ...part, output: action.output };
            return { ...s, live };
          }
        }
        return s;
      });
    case "LIVE_RESET":
      return updateSession(state, action.sessionId, (s) => ({ ...s, live: [] }));
    case "PERMISSION_REQUEST":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        pendingPermission: action.permission,
      }));
    case "PERMISSION_RESOLVED":
      return updateSession(state, action.sessionId, (s) => ({ ...s, pendingPermission: null }));
    case "QUESTION_REQUEST":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        pendingQuestion: action.question,
      }));
    case "QUESTION_RESOLVED":
      return updateSession(state, action.sessionId, (s) => ({ ...s, pendingQuestion: null }));
    case "RESET": {
      const sessions = { ...state.sessions };
      delete sessions[action.sessionId];
      return { ...state, sessions };
    }
    case "REKEY_SESSION": {
      const slice = state.sessions[action.oldId];
      if (!slice) return state; // already rekeyed by a racing dispatch — no-op
      const sessions = { ...state.sessions };
      delete sessions[action.oldId];
      sessions[action.newId] = slice;
      return { ...state, sessions };
    }
    case "TOGGLE_RUN_COLLAPSED":
      return updateSession(state, action.sessionId, (s) => {
        const has = s.collapsedRunIds.includes(action.runId);
        return {
          ...s,
          collapsedRunIds: has
            ? s.collapsedRunIds.filter((id) => id !== action.runId)
            : [...s.collapsedRunIds, action.runId],
        };
      });
    case "SET_SESSION_CONTEXT":
      return { ...state, sessionContext: action.context };
    case "SET_SPENDING":
      return { ...state, spendingUSD: action.spendingUSD };
    case "SET_TUI_STATUS":
      return { ...state, tuiStatus: action.status, tuiStatusReady: true };
    case "SET_TUI_STATUS_READY":
      return { ...state, tuiStatusReady: action.ready };
    case "PREPEND_MESSAGES":
      // Older messages loaded via scroll-up. Prepend and update pagination state.
      return updateSession(state, action.sessionId, (s) => {
        const hasMore = action.messages.length > 0 && s.messages.length + action.messages.length < action.total;
        return {
          ...s,
          messages: [...action.messages, ...s.messages],
          totalMessages: action.total,
          hasMore,
          loadingMore: false,
        };
      });
    case "SET_TOTAL":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        totalMessages: action.total,
        hasMore: s.messages.length < action.total,
      }));
    case "SET_LOADING_MORE":
      return updateSession(state, action.sessionId, (s) => ({ ...s, loadingMore: action.loading }));
    case "MERGE_SNAPSHOT":
      // Merge snapshot into current state.
      // If action.messages is a full snapshot (length == total), replace all.
      // Otherwise it's a paginated subset — the initial page load.
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        messages: action.messages,
        totalMessages: action.total,
        hasMore: action.messages.length < action.total,
        live: [],
        initialized: true,
      }));
    default:
      return state;
  }
}

const ChatStateContext = createContext<ChatState>(initialState);
const ChatDispatchContext = createContext<React.Dispatch<ChatAction>>(() => {});

export function ChatProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(chatReducer, initialState);
  return (
    <ChatStateContext.Provider value={state}>
      <ChatDispatchContext.Provider value={dispatch}>
        {children}
      </ChatDispatchContext.Provider>
    </ChatStateContext.Provider>
  );
}

export function useChatState() {
  return useContext(ChatStateContext);
}

export function useChatDispatch() {
  return useContext(ChatDispatchContext);
}
