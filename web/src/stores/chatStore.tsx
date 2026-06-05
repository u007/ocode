import { createContext, useContext, useReducer, type ReactNode } from "react";
import type { Message } from "../api/types";

export interface PermissionRequest {
  tool: string;
  command?: string;
  request_id: string;
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
  | { type: "PERMISSION_REQUEST"; permission: PermissionRequest }
  | { type: "PERMISSION_RESOLVED" }
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
};

function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "ADD_MESSAGE":
      return { ...state, messages: [...state.messages, action.message] };
    case "SET_MESSAGES":
      return { ...state, messages: action.messages };
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
    case "PERMISSION_REQUEST":
      return { ...state, pendingPermission: action.permission };
    case "PERMISSION_RESOLVED":
      return { ...state, pendingPermission: null };
    case "RESET":
      // Preserve the advisor on/off toggle across new sessions — the server
      // keeps it for the handler's lifetime, so the status must not snap back.
      return { ...initialState, advisorEnabled: state.advisorEnabled };
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
