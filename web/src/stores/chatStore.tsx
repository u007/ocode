import { createContext, useCallback, useContext, useEffect, useRef, type ReactNode } from "react";
import { Store, useSelector } from "@tanstack/react-store";
import type { Message, LivePart, TUIStatus, QuestionPrompt } from "../api/types";

export interface PermissionRequest {
  tool: string;
  command?: string;
  rule?: string;
  summary?: string;
  deny_reason?: string;
  model_unavailable?: string;
  request_id: string;
  /** "tool" | "bash_prefix" — drives always-allow button availability. */
  scope?: string;
  /** Bash prefix for bash_prefix-scope asks (e.g. "rm"). */
  prefix?: string;
  /** Out-of-workspace target path; "always" persists this root to extra_allowed_paths. */
  out_of_scope_path?: string;
}

export interface QuestionRequest {
  request_id: string;
  questions: QuestionPrompt[];
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
  // Permission asks superseded by a newer one before being answered — a
  // single agent round can pause on more than one at once when it dispatches
  // several tool calls that each need approval. Kept as a stack so answering
  // the currently-shown dialog resurfaces the next-most-recent one instead of
  // silently dropping it (which left the turn stuck paused forever).
  permissionQueue: PermissionRequest[];
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
  // Live TUI status for this session (model, IDE, cwd, context, spending,
  // modified files, LSP servers, extra paths). Updated by the SSE "status"
  // event for this session id, so each tab tracks its own session instead of
  // whichever session most recently emitted a status event.
  tuiStatus: TUIStatus | null;
  // Part 05: per-session status/turn state. `turnActive` is the authoritative
  // streaming flag — set by turn_started, cleared by turn_done/turn_error —
  // and `isStreaming` (the legacy promise-derived flag) mirrors it. The
  // watchdog uses `lastHeartbeatAt` for stall detection and sets
  // `turnStalled`; `bootstrapStage` tracks the async session bootstrap. All
  // populated from bus events or the fetch-on-activation status fetch.
  turnActive: boolean;
  lastHeartbeatAt: number | null;
  bootstrapStage: string | null;
  turnStalled: boolean;
  statusLoading: boolean;
  // True after the user pressed Stop to interrupt a turn. While set, the
  // auto-drain of queued messages is suppressed (mirrors the TUI's
  // streamWasInterrupted which prevents drainQueuedItems on cancel). Cleared
  // when the user resumes or starts a new turn.
  wasInterrupted: boolean;
}

export const emptySessionSlice: SessionSlice = {
  messages: [],
  live: [],
  isStreaming: false,
  error: null,
  pendingPermission: null,
  permissionQueue: [],
  pendingQuestion: null,
  totalMessages: 0,
  hasMore: false,
  loadingMore: false,
  initialized: false,
  collapsedRunIds: [],
  tuiStatus: null,
  turnActive: false,
  lastHeartbeatAt: null,
  bootstrapStage: null,
  turnStalled: false,
  statusLoading: false,
  wasInterrupted: false,
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

/** Selector: the per-session status snapshot (populated by the
 *  fetch-on-activation status fetch and patched by bus `status` events). */
export function getSessionStatus(
  state: ChatState,
  sessionId: string | null | undefined,
): TUIStatus | null {
  return getSessionSlice(state, sessionId).tuiStatus;
}

/** Selector: the per-session turn-state fields consumed by the streaming
 *  spinner, the watchdog, and the bootstrap indicator. */
export function getTurnState(
  state: ChatState,
  sessionId: string | null | undefined,
): { turnActive: boolean; lastHeartbeatAt: number | null; turnStalled: boolean; bootstrapStage: string | null } {
  const s = getSessionSlice(state, sessionId);
  return {
    turnActive: s.turnActive,
    lastHeartbeatAt: s.lastHeartbeatAt,
    turnStalled: s.turnStalled,
    bootstrapStage: s.bootstrapStage,
  };
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
  spendingUSD: number | null;
  // True once the very first /api/tui-status fetch has resolved. Lets the UI
  // show "loading…" vs. "not connected" while waiting for the first frame.
  tuiStatusReady: boolean;
}

export type ChatAction =
  | { type: "ADD_MESSAGE"; sessionId: string; message: Message }
  | { type: "SET_MESSAGES"; sessionId: string; messages: Message[] }
  | { type: "MARK_INITIALIZED"; sessionId: string }
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
  | {
      type: "LIVE_TOOL_START";
      sessionId: string;
      tool: string;
      callId?: string;
      command?: string;
    }
  | { type: "LIVE_TOOL_OUTPUT"; sessionId: string; callId?: string; chunk: string }
  | { type: "LIVE_TOOL_RESULT"; sessionId: string; callId?: string; output: string }
  | { type: "LIVE_RESET"; sessionId: string }
  | { type: "LIVE_PERMISSION_CHECK"; sessionId: string; tool: string; model: string; active: boolean }
  | { type: "LIVE_ADVISOR_CHECKPOINT"; sessionId: string; kind: string; active: boolean }
  | { type: "PERMISSION_REQUEST"; sessionId: string; permission: PermissionRequest }
  | { type: "PERMISSION_RESOLVED"; sessionId: string; requestId?: string }
  | { type: "QUESTION_REQUEST"; sessionId: string; question: QuestionRequest }
  | { type: "QUESTION_RESOLVED"; sessionId: string }
  | { type: "PREPEND_MESSAGES"; sessionId: string; messages: Message[]; total: number }
  | { type: "SET_LOADING_MORE"; sessionId: string; loading: boolean }
  | { type: "MERGE_SNAPSHOT"; sessionId: string; messages: Message[]; total: number }
  | { type: "SET_TOTAL"; sessionId: string; total: number }
  | { type: "SET_SPENDING"; spendingUSD: number | null }
  | { type: "SET_TUI_STATUS"; sessionId: string; status: TUIStatus }
  | { type: "SET_STATUS_LOADING"; sessionId: string; loading: boolean }
  | { type: "SET_TURN_STATE"; sessionId: string; turnActive: boolean }
  | { type: "SET_TURN_HEARTBEAT"; sessionId: string }
  | { type: "SET_TURN_STALLED"; sessionId: string; stalled: boolean }
  | { type: "SET_BOOTSTRAP_STAGE"; sessionId: string; stage: string | null }
  | { type: "SET_TUI_STATUS_READY"; ready: boolean }
  | { type: "SET_WAS_INTERRUPTED"; sessionId: string; wasInterrupted: boolean }
  | { type: "INTERRUPT"; sessionId: string }
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

/** Locate the live tool part a streamed chunk or result belongs to.
 *
 *  With a callId the match is exact, which is what keeps concurrent tool calls
 *  from writing into each other's bubbles. Without one (legacy events that
 *  predate call-id threading) it falls back to the most recent tool still
 *  awaiting output — correct for sequential calls, a guess for parallel ones.
 *  Returns -1 when nothing matches. */
function findPendingToolIndex(live: LivePart[], callId?: string): number {
  if (callId) {
    return live.findIndex(
      (p) => p.kind === "tool" && p.callId === callId && p.output === undefined,
    );
  }
  for (let i = live.length - 1; i >= 0; i--) {
    const part = live[i];
    if (part.kind === "tool" && part.output === undefined) return i;
  }
  return -1;
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
      // the live buffer it supersedes. It also marks the slice initialized:
      // the mirror's snapshot is authoritative history, so a tab whose slice
      // was populated by the mirror must not keep its "loading" spinner
      // waiting on a history fetch that is now redundant.
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        messages: action.messages,
        live: [],
        initialized: true,
      }));
    case "MARK_INITIALIZED":
      // Marks a slice as initialized without replacing its content — used when
      // the mirror already populated the slice (messages/live) before the
      // initial history fetch resolved, so the fetch result is not allowed to
      // clobber newer live state but the tab spinner still clears.
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        initialized: true,
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
        live: [
          ...s.live,
          {
            kind: "tool",
            tool: action.tool,
            callId: action.callId,
            command: action.command,
          },
        ],
      }));
    case "LIVE_TOOL_OUTPUT":
      return updateSession(state, action.sessionId, (s) => {
        const idx = findPendingToolIndex(s.live, action.callId);
        if (idx < 0) return s;
        const part = s.live[idx];
        if (part.kind !== "tool") return s;
        const live = [...s.live];
        live[idx] = { ...part, stream: (part.stream ?? "") + action.chunk };
        return { ...s, live };
      });
    case "LIVE_TOOL_RESULT":
      return updateSession(state, action.sessionId, (s) => {
        const idx = findPendingToolIndex(s.live, action.callId);
        if (idx < 0) return s;
        const part = s.live[idx];
        if (part.kind !== "tool") return s;
        const live = [...s.live];
        live[idx] = { ...part, output: action.output };
        return { ...s, live };
      });
    case "LIVE_RESET":
      return updateSession(state, action.sessionId, (s) => ({ ...s, live: [] }));
    case "LIVE_PERMISSION_CHECK": {
      const text = `Checking permission for ${action.tool} (${action.model})…`;
      return updateSession(state, action.sessionId, (s) => {
        if (action.active) {
          return { ...s, live: [...s.live, { kind: "status", text }] };
        }
        const live = [...s.live];
        for (let i = live.length - 1; i >= 0; i--) {
          const part = live[i];
          if (part.kind === "status" && part.text === text) {
            live.splice(i, 1);
            break;
          }
        }
        return { ...s, live };
      });
    }
    case "LIVE_ADVISOR_CHECKPOINT": {
      const text = `Advisor ${action.kind} checkpoint — reviewing…`;
      return updateSession(state, action.sessionId, (s) => {
        if (action.active) {
          return { ...s, live: [...s.live, { kind: "status", text }] };
        }
        const live = [...s.live];
        for (let i = live.length - 1; i >= 0; i--) {
          const part = live[i];
          if (part.kind === "status" && part.text === text) {
            live.splice(i, 1);
            break;
          }
        }
        return { ...s, live };
      });
    }
    case "PERMISSION_REQUEST":
      return updateSession(state, action.sessionId, (s) => {
        // A round that dispatched multiple tool calls needing approval can
        // raise more than one ask before any is answered. The newest still
        // wins the dialog (matching prior behavior), but the superseded one
        // is queued instead of dropped — see PERMISSION_RESOLVED, which
        // resurfaces it once the current dialog is answered.
        if (!s.pendingPermission || s.pendingPermission.request_id === action.permission.request_id) {
          return { ...s, pendingPermission: action.permission };
        }
        return {
          ...s,
          pendingPermission: action.permission,
          permissionQueue: [...s.permissionQueue, s.pendingPermission],
        };
      });
    case "PERMISSION_RESOLVED":
      return updateSession(state, action.sessionId, (s) => {
        // A resolve for an ask that isn't the one currently shown (a stale
        // dismissal for an older/queued ask) must not close the newer dialog
        // — just drop it from the queue so it isn't resurfaced later.
        if (
          action.requestId &&
          s.pendingPermission &&
          s.pendingPermission.request_id !== action.requestId
        ) {
          return {
            ...s,
            permissionQueue: s.permissionQueue.filter((p) => p.request_id !== action.requestId),
          };
        }
        const queue = [...s.permissionQueue];
        const next = queue.pop();
        return { ...s, pendingPermission: next ?? null, permissionQueue: queue };
      });
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
    case "SET_SPENDING":
      return { ...state, spendingUSD: action.spendingUSD };
    case "SET_TUI_STATUS":
      return {
        ...updateSession(state, action.sessionId, (s) => ({ ...s, tuiStatus: action.status })),
        tuiStatusReady: true,
      };
    case "SET_STATUS_LOADING":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        statusLoading: action.loading,
      }));
    case "SET_TURN_STATE":
      // turnActive is the authoritative streaming flag. Clearing it also
      // clears the watchdog stall and heartbeat so a later activation of the
      // same session starts from a clean state.
      return updateSession(state, action.sessionId, (s) =>
        action.turnActive
          ? { ...s, turnActive: true, lastHeartbeatAt: Date.now(), turnStalled: false }
          : {
              ...s,
              turnActive: false,
              lastHeartbeatAt: null,
              turnStalled: false,
              isStreaming: false,
            },
      );
    case "SET_TURN_HEARTBEAT":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        lastHeartbeatAt: Date.now(),
        turnStalled: false,
      }));
    case "SET_TURN_STALLED":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        turnStalled: action.stalled,
      }));
    case "SET_BOOTSTRAP_STAGE":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        bootstrapStage: action.stage,
      }));
    case "SET_WAS_INTERRUPTED":
      return updateSession(state, action.sessionId, (s) => ({ ...s, wasInterrupted: action.wasInterrupted }));
    case "INTERRUPT":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        wasInterrupted: true,
        isStreaming: false,
        turnActive: false,
        lastHeartbeatAt: null,
        turnStalled: false,
        pendingPermission: null,
        permissionQueue: [],
        pendingQuestion: null,
        live: [],
      }));
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
      //
      // Mid-turn guard: the server persists a transcript only after the
      // turn's Step returns (runTurn saves post-Step), so a snapshot fetched
      // mid-turn is staler than memory — but only where memory actually
      // holds newer state, and only `messages` counts as that state: a
      // slice with committed messages must not regress to the pre-turn
      // page (the "chat stuck after some messages" bug). Live parts alone
      // do NOT suppress the merge — after a mid-stream page reload the
      // mirror reconnects before the history fetch resolves, so deltas land
      // on a virgin slice first; an empty `messages` array holds nothing
      // newer than disk, and dropping the snapshot left the reloaded chat
      // blank until turn_done. The merge instead applies the disk page and
      // preserves the live buffer while the turn is active. The
      // turn-boundary `messages` broadcast commits the full transcript
      // (and clears live) when the turn ends. reconcileOpenSessions
      // dispatches SET_TURN_STATE before this merge, so a genuinely
      // finished turn whose turn_done was missed flips turnActive first
      // and still gets the recovery merge below.
      return updateSession(state, action.sessionId, (s) => {
        if (s.turnActive && s.messages.length > 0) {
          return {
            ...s,
            totalMessages: action.total,
            hasMore: s.messages.length < action.total,
            initialized: true,
          };
        }
        return {
          ...s,
          messages: action.messages,
          totalMessages: action.total,
          hasMore: action.messages.length < action.total,
          live: s.turnActive ? s.live : [],
          initialized: true,
        };
      });
    default:
      return state;
  }
}

// Backed by a @tanstack/store Store instance rather than useReducer, so the
// action-dispatch shape (chatReducer + ChatAction) is preserved 1:1 for the
// ~20 existing consumers of useChatState/useChatDispatch — only the storage
// engine underneath changed.
const ChatStoreContext = createContext<Store<ChatState> | null>(null);

export function ChatProvider({ children }: { children: ReactNode }) {
  const storeRef = useRef<Store<ChatState> | null>(null);
  if (!storeRef.current) storeRef.current = new Store(initialState);
  return (
    <ChatStoreContext.Provider value={storeRef.current}>{children}</ChatStoreContext.Provider>
  );
}

function useChatStore(): Store<ChatState> {
  const store = useContext(ChatStoreContext);
  if (!store) throw new Error("useChatState/useChatDispatch must be used within ChatProvider");
  return store;
}

export function useChatState(): ChatState {
  return useSelector(useChatStore());
}

// Subscribes only to the projection `selector` returns, re-rendering when
// that specific value changes reference (default Object.is compare) rather
// than on every dispatch anywhere in the store. Safe to select a session
// slice via getSessionSlice: updateSession only replaces the touched
// session's object, so unrelated sessions' dispatches leave the selected
// reference unchanged and this correctly skips the re-render.
export function useChatSelector<T>(
  selector: (state: ChatState) => T,
  isEqual?: (a: T, b: T) => boolean,
): T {
  return useSelector(useChatStore(), selector, isEqual ? { compare: isEqual } : undefined);
}

// For consumers that only need the latest state for an imperative read
// (inside a callback, effect, or event handler) and never use it to drive
// JSX — subscribes to the store without ever triggering a re-render of the
// calling component. Use this instead of useChatState() whenever the
// component's own render output doesn't depend on chat state; every
// dispatch (including one per streamed token) otherwise forces a full
// re-render of the caller and its entire subtree for no visible reason.
export function useChatStateRef(): { readonly current: ChatState } {
  const store = useChatStore();
  const ref = useRef(store.state);
  ref.current = store.state;
  useEffect(() => {
    const sub = store.subscribe(() => {
      ref.current = store.state;
    });
    return () => sub.unsubscribe();
  }, [store]);
  return ref;
}

export function useChatDispatch(): (action: ChatAction) => void {
  const store = useChatStore();
  return useCallback((action: ChatAction) => store.setState((prev) => chatReducer(prev, action)), [store]);
}
