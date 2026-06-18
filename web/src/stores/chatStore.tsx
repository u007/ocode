import { createContext, useContext, useReducer, type ReactNode } from "react";
import type { Message, LivePart, TUIStatus } from "../api/types";

export interface PermissionRequest {
  tool: string;
  command?: string;
  request_id: string;
}

export interface SessionContextMetrics {
  currentTokens: number;
  maxTokens: number;
  model: string;
}

interface ChatState {
  messages: Message[];
  sessionId: string | null;
  model: string | null;
  smallModel: string | null;
  advisorModel: string | null;
  advisorEnabled: boolean;
  isStreaming: boolean;
  error: string | null;
  pendingPermission: PermissionRequest | null;
  // In-progress turn, streamed live until the turn_done snapshot commits it.
  live: LivePart[];
  // Lazy loading state
  totalMessages: number; // total messages on server
  hasMore: boolean; // whether older messages exist
  loadingMore: boolean; // currently fetching older messages
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

type ChatAction =
  | { type: "ADD_MESSAGE"; message: Message }
  | { type: "SET_MESSAGES"; messages: Message[] }
  | { type: "SET_SESSION"; sessionId: string }
  | { type: "SET_MODEL"; model: string }
  | { type: "SET_SMALL_MODEL"; model: string }
  | { type: "SET_ADVISOR_MODEL"; model: string }
  | { type: "SET_ADVISOR_ENABLED"; enabled: boolean }
  | { type: "SET_STREAMING"; isStreaming: boolean }
  | { type: "SET_ERROR"; error: string | null }
  | { type: "APPEND_DELTA"; delta: string }
  | { type: "LIVE_DELTA"; kind: "thinking" | "text"; delta: string }
  | { type: "LIVE_TOOL_START"; tool: string; command?: string }
  | { type: "LIVE_TOOL_RESULT"; output: string }
  | { type: "LIVE_RESET" }
  | { type: "PERMISSION_REQUEST"; permission: PermissionRequest }
  | { type: "PERMISSION_RESOLVED" }
  | { type: "PREPEND_MESSAGES"; messages: Message[]; total: number }
  | { type: "SET_LOADING_MORE"; loading: boolean }
  | { type: "MERGE_SNAPSHOT"; messages: Message[]; total: number }
  | { type: "SET_TOTAL"; total: number }
  | { type: "SET_SESSION_CONTEXT"; context: SessionContextMetrics | null }
  | { type: "SET_SPENDING"; spendingUSD: number | null }
  | { type: "SET_TUI_STATUS"; status: TUIStatus }
  | { type: "SET_TUI_STATUS_READY"; ready: boolean }
  | { type: "RESET" };

const initialState: ChatState = {
  messages: [],
  sessionId: null,
  model: null,
  smallModel: null,
  advisorModel: null,
  advisorEnabled: true,
  isStreaming: false,
  error: null,
  pendingPermission: null,
  live: [],
  totalMessages: 0,
  hasMore: false,
  loadingMore: false,
  tuiStatus: null,
  sessionContext: null,
  spendingUSD: null,
  tuiStatusReady: false,
};

function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "ADD_MESSAGE":
      return { ...state, messages: [...state.messages, action.message] };
    case "SET_MESSAGES":
      // Authoritative snapshot lands at a turn boundary — commit it and clear
      // the live buffer it supersedes.
      return { ...state, messages: action.messages, live: [] };
    case "SET_SESSION":
      return { ...state, sessionId: action.sessionId };
    case "SET_MODEL":
      return { ...state, model: action.model };
    case "SET_SMALL_MODEL":
      return { ...state, smallModel: action.model };
    case "SET_ADVISOR_MODEL":
      return { ...state, advisorModel: action.model };
    case "SET_ADVISOR_ENABLED":
      return { ...state, advisorEnabled: action.enabled };
    case "SET_STREAMING":
      return { ...state, isStreaming: action.isStreaming };
    case "SET_ERROR":
      return { ...state, error: action.error };
    case "APPEND_DELTA": {
      const msgs = [...state.messages];
      const last = msgs[msgs.length - 1];
      if (last && last.role === "assistant") {
        msgs[msgs.length - 1] = { ...last, content: last.content + action.delta };
      } else {
        msgs.push({ role: "assistant", content: action.delta });
      }
      return { ...state, messages: msgs };
    }
    case "LIVE_DELTA": {
      const live = [...state.live];
      const last = live[live.length - 1];
      if (last && last.kind === action.kind) {
        live[live.length - 1] = { ...last, text: last.text + action.delta };
      } else {
        live.push({ kind: action.kind, text: action.delta });
      }
      return { ...state, live };
    }
    case "LIVE_TOOL_START":
      return {
        ...state,
        live: [
          ...state.live,
          { kind: "tool", tool: action.tool, command: action.command },
        ],
      };
    case "LIVE_TOOL_RESULT": {
      const live = [...state.live];
      // Attach to the most recent tool part still awaiting its result.
      for (let i = live.length - 1; i >= 0; i--) {
        const part = live[i];
        if (part.kind === "tool" && part.output === undefined) {
          live[i] = { ...part, output: action.output };
          return { ...state, live };
        }
      }
      return state;
    }
    case "LIVE_RESET":
      return { ...state, live: [] };
    case "PERMISSION_REQUEST":
      return { ...state, pendingPermission: action.permission };
    case "PERMISSION_RESOLVED":
      return { ...state, pendingPermission: null };
    case "RESET":
      // Preserve the advisor on/off toggle across new sessions — the server
      // keeps it for the handler's lifetime, so the status must not snap back.
      // The TUI status snapshot is also preserved so the bar doesn't blank out
      // during a /new; the next "status" SSE event will overwrite it anyway.
      return {
        ...initialState,
        advisorEnabled: state.advisorEnabled,
        tuiStatus: state.tuiStatus,
        spendingUSD: state.spendingUSD,
        tuiStatusReady: state.tuiStatusReady,
      };
    case "SET_SESSION_CONTEXT":
      return { ...state, sessionContext: action.context };
    case "SET_SPENDING":
      return { ...state, spendingUSD: action.spendingUSD };
    case "SET_TUI_STATUS":
      return { ...state, tuiStatus: action.status, tuiStatusReady: true };
    case "SET_TUI_STATUS_READY":
      return { ...state, tuiStatusReady: action.ready };
    case "PREPEND_MESSAGES": {
      // Older messages loaded via scroll-up. Prepend and update pagination state.
      const hasMore = action.messages.length > 0 && state.messages.length + action.messages.length < action.total;
      return {
        ...state,
        messages: [...action.messages, ...state.messages],
        totalMessages: action.total,
        hasMore,
        loadingMore: false,
      };
    }
    case "SET_TOTAL":
      return {
        ...state,
        totalMessages: action.total,
        hasMore: state.messages.length < action.total,
      };
    case "SET_LOADING_MORE":
      return { ...state, loadingMore: action.loading };
    case "MERGE_SNAPSHOT": {
      // Merge snapshot into current state.
      // If action.messages is a full snapshot (length == total), replace all.
      // If it's a paginated subset, set as initial/older messages.
      if (action.messages.length === action.total) {
        // Full snapshot — replace all messages
        return { ...state, messages: action.messages, totalMessages: action.total, hasMore: false, live: [] };
      }
      // Paginated subset — this is either initial load or older messages
      if (state.messages.length === 0) {
        // Initial load: set the paginated messages
        return {
          ...state,
          messages: action.messages,
          totalMessages: action.total,
          hasMore: action.messages.length < action.total,
          live: [],
        };
      }
      // We already have messages — this shouldn't happen with MERGE_SNAPSHOT
      // (use PREPEND_MESSAGES for older messages). Just replace.
      return { ...state, messages: action.messages, totalMessages: action.total, hasMore: action.messages.length < action.total, live: [] };
    }
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
