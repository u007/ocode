import { createContext, useCallback, useContext, useEffect, useRef, type ReactNode } from "react";
import { Store, useSelector } from "@tanstack/react-store";
import type { Message, LivePart, TUIStatus, AgentActivityEvent, QuestionPrompt, QuestionAnswerPayload } from "../api/types";

// ── Rehydrate pending asks from persisted transcript ──────────────
// The server persists a permission/question pause as a sentinel in the
// transcript (tool.SENTINEL_PERMISSION_ASK / QUESTION_PROMPT) but omits
// `permission`/`question` from liveFrames (session_manager.go). A reload
// therefore loses the dialog unless we reconstruct it from the messages
// snapshot. This mirrors the server's tailIsPermissionAsk / tailIsQuestionAsk
// (trailing tool-role run only).
export const SENTINEL_PERMISSION_ASK = "PERMISSION_ASK:";
export const SENTINEL_QUESTION_PROMPT = "QUESTION_PROMPT:";
export const SENTINEL_WAITING = "WAITING_FOR_USER_RESPONSE";

// Must match tool.QuestionDismissedResult (internal/tool/misc.go): the tool
// result the server writes in place of an unanswered question prompt when the
// user dismisses it. Mirrored locally so the optimistic rewrite matches the
// snapshot byte-for-byte and a reconcile does not reopen the dialog.
export const QUESTION_DISMISSED_RESULT =
  "The user dismissed the question prompt without answering.";

function trailingToolRunStart(messages: Message[]): number {
  let i = messages.length;
  while (i > 0 && messages[i - 1].role === "tool") i--;
  return i;
}

function parsePermissionFromMessage(msg: Message): PermissionRequest | null {
  if (msg.role !== "tool" || !msg.content.startsWith(SENTINEL_PERMISSION_ASK)) return null;
  const payload = msg.content.slice(SENTINEL_PERMISSION_ASK.length).trim();
  if (!payload) return null;
  try {
    const req = JSON.parse(payload) as Record<string, unknown>;
    const toolName = (req["tool_name"] as string) || "";
    if (!toolName) return null;
    const commandRaw = (req["command"] as string) || "";
    const argsRaw = req["args"];
    let command = commandRaw;
    if (!command && argsRaw != null) {
      command = typeof argsRaw === "string" ? (argsRaw as string) : JSON.stringify(argsRaw);
    }
    const toolCallId = (msg.tool_call_id as string | undefined) ?? "";
    return {
      tool: toolName,
      command: command || undefined,
      args: argsRaw,
      rule: (req["rule"] as string) || undefined,
      summary: (req["summary"] as string) || undefined,
      deny_reason: (req["deny_reason"] as string) || undefined,
      model_unavailable: (req["model_unavailable"] as string) || undefined,
      request_id: toolCallId || (req["request_id"] as string) || "",
      scope: (req["scope"] as string) || undefined,
      prefix: (req["prefix"] as string) || undefined,
      out_of_scope_path: (req["out_of_scope_path"] as string) || undefined,
    };
  } catch {
    return null;
  }
}

export function parseQuestionFromMessage(msg: Message): QuestionRequest | null {
  if (msg.role !== "tool") return null;
  const idx = msg.content.indexOf(SENTINEL_QUESTION_PROMPT);
  if (idx === -1) return null;
  if (!msg.content.includes(SENTINEL_WAITING)) return null;
  let payload = msg.content.slice(idx + SENTINEL_QUESTION_PROMPT.length);
  const waitIdx = payload.indexOf(SENTINEL_WAITING);
  if (waitIdx !== -1) payload = payload.slice(0, waitIdx);
  payload = payload.trim();
  if (!payload) return null;
  try {
    const prompts = JSON.parse(payload) as QuestionPrompt[];
    if (!Array.isArray(prompts) || prompts.length === 0) return null;
    const toolCallId = (msg.tool_call_id as string | undefined) ?? "";
    return { request_id: toolCallId || "", questions: prompts };
  } catch {
    return null;
  }
}

export function extractPendingFromMessages(messages: Message[]): {
  pendingPermission: PermissionRequest | null;
  permissionQueue: PermissionRequest[];
  pendingQuestion: QuestionRequest | null;
} {
  const start = trailingToolRunStart(messages);
  const trailing = messages.slice(start);
  const asks: PermissionRequest[] = [];
  let question: QuestionRequest | null = null;
  for (const msg of trailing) {
    const perm = parsePermissionFromMessage(msg);
    if (perm) asks.push(perm);
    const q = parseQuestionFromMessage(msg);
    if (q) question = q; // last question wins (at most one pending round)
  }
  if (asks.length === 0) {
    return { pendingPermission: null, permissionQueue: [], pendingQuestion: question };
  }
  const pendingPermission = asks[asks.length - 1];
  const permissionQueue = asks.slice(0, -1);
  return { pendingPermission, permissionQueue, pendingQuestion: question };
}

// ── LLM context for a pending ask ─────────────────────────────────
// The permission/question ask events carry only the tool and its arguments, so
// the dialog cannot show *why* the agent is asking. Derive the assistant
// message that immediately preceded the ask from the session state instead:
// the in-progress live buffer while the turn is paused, or the persisted
// transcript on a reload/rehydrate.
export interface AskContext {
  /** Assistant reasoning ("thinking") for the message behind the ask. */
  thinking?: string;
  /** Assistant prose that introduced the tool call / question. */
  text?: string;
}

// Live parts that do not delimit an assistant message: transient status
// ("Checking permission for …") and discovery/indexing notices can be appended
// mid-turn between an assistant message's prose and its tool bubbles, so they
// must not break the walk-back.
function isTransientLivePart(part: LivePart): boolean {
  return part.kind === "status" || part.kind === "notice";
}

// Pull the assistant message the pending ask belongs to out of the live
// buffer. Tool parts delimit messages (one assistant message can carry several
// tool calls); trailing tool/status/notice parts are skipped so the walk-back
// starts on the message's prose.
function liveAskContext(live: LivePart[]): AskContext | null {
  let end = live.length;
  while (end > 0 && (live[end - 1].kind === "tool" || isTransientLivePart(live[end - 1]))) {
    end--;
  }
  if (end === 0) return null;
  let start = end;
  while (start > 0) {
    const part = live[start - 1];
    if (part.kind === "thinking" || part.kind === "text" || isTransientLivePart(part)) {
      start--;
      continue;
    }
    break; // tool part — previous assistant message boundary
  }
  const parts = live.slice(start, end);
  const thinking = parts
    .filter((p): p is { kind: "thinking"; text: string } => p.kind === "thinking")
    .map((p) => p.text)
    .join("")
    .trim();
  const text = parts
    .filter((p): p is { kind: "text"; text: string } => p.kind === "text")
    .map((p) => p.text)
    .join("")
    .trim();
  if (!thinking && !text) return null;
  return { thinking: thinking || undefined, text: text || undefined };
}

/** Assistant message (prose + reasoning) that led to the currently-pending
 *  permission/question ask, for display inside the ask dialog. Prefers the
 *  live buffer (mid-turn pause); falls back to the last assistant message in
 *  the transcript (reload/rehydrate). Returns null when neither holds an
 *  assistant message, so callers render nothing rather than an empty panel. */
export function extractAskContext(
  messages: Message[],
  live: LivePart[],
): AskContext | null {
  const fromLive = liveAskContext(live);
  if (fromLive) return fromLive;
  // The transcript only carries the pending ask once the server has written
  // its sentinel as a trailing tool message (extractPendingFromMessages). With
  // no trailing tool run the live buffer is mid-turn and the last committed
  // assistant message belongs to an earlier turn — showing it would
  // misattribute the ask, so return nothing instead.
  if (trailingToolRunStart(messages) === messages.length) return null;
  for (let i = messages.length - 1; i >= 0; i--) {
    const msg = messages[i];
    if (msg.role !== "assistant") continue;
    const thinking = (msg.reasoning_content ?? "").trim();
    const text = (msg.content ?? "").trim();
    if (!thinking && !text) return null;
    return { thinking: thinking || undefined, text: text || undefined };
  }
  return null;
}

export interface PermissionRequest {
  tool: string;
  command?: string;
  /** Preserved for both live events and recovered transcript asks. */
  args?: unknown;
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
  // True when the LAST turn ended on an LLM-loop failure (SSE turn_error /
  // error frame), as opposed to a submit/validation failure — which also lands
  // in `error` but has nothing to retry server-side. Drives the composer's
  // retry action after a Stop or a failed turn. Cleared when a new turn starts
  // (SET_ERROR null) or the user retries.
  turnError: boolean;
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
  // Server index of `messages[0]`: how many transcript messages precede the
  // loaded window. Lets a server search index (`GET /api/sessions/{id}/search`)
  // be translated into a local position as `serverIndex - windowStartServerIndex`.
  //
  // Why an explicit anchor instead of deriving it: `totalMessages` is the
  // server's count, but ADD_MESSAGE (App.tsx, for command replies like /help or
  // /recap status) appends a CLIENT-ONLY message that is never persisted —
  // so `totalMessages - messages.length` drifts negative and blind arithmetic
  // would jump to the wrong bubble. Injections only ever append at the tail, so
  // they never move this anchor. Maintained by MERGE_SNAPSHOT / PREPEND_MESSAGES
  // (see updateSession); -1 means "not yet known", and callers must fall back
  // to a tail reload rather than guess.
  windowStartServerIndex: number;
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
  // Server-derived "settled on an unfinished turn" flag (GET /state). Distinct
  // from `wasInterrupted`, which is the live user-Stop signal that BLOCKS
  // sending — reusing it would disable the Continue action this flag drives.
  // The server is authoritative: the client sets it from each state payload
  // and clears it optimistically only when the user clicks Continue.
  interrupted: boolean;
  // Main model picked for a draft ("new-*") tab before the session exists
  // server-side. The sidebar shows it optimistically and the first message
  // sends it so the session starts with the chosen model; once the real
  // session id is known it is persisted as a per-session override and this
  // local value is no longer consulted (tuiStatus.main_model wins for real
  // sessions). Undefined = no pick yet.
  model?: string;
  // Permission mode picked for a draft ("new-*") tab before the session exists
  // server-side (normal|yolo|locked|sandbox). Same lifecycle as `model`: the
  // first message sends it and the server persists it as the new session's
  // per-session override; for a real session tuiStatus.permission_mode wins.
  // Undefined = never picked (follows the config default).
  permissionMode?: string;
  // Optimistic per-session advisor on/off, written the instant the sidebar's
  // checkbox is clicked so the flip is visible during the PUT round trip. It
  // shadows tuiStatus.advisor_enabled until an authoritative snapshot confirms
  // the same value (see SET_TUI_STATUS), then clears to undefined. Kept (not
  // cleared) while a snapshot disagrees, so a background status poll that raced
  // the in-flight PUT cannot clobber the just-clicked state. Undefined = no
  // pending flip; fall back to the session's snapshot / the process default.
  advisorEnabled?: boolean;
}

export const emptySessionSlice: SessionSlice = {
  messages: [],
  live: [],
  isStreaming: false,
  error: null,
  turnError: false,
  pendingPermission: null,
  permissionQueue: [],
  pendingQuestion: null,
  totalMessages: 0,
  hasMore: false,
  loadingMore: false,
  windowStartServerIndex: -1,
  initialized: false,
  collapsedRunIds: [],
  tuiStatus: null,
  turnActive: false,
  lastHeartbeatAt: null,
  bootstrapStage: null,
  turnStalled: false,
  statusLoading: false,
  wasInterrupted: false,
  interrupted: false,
  model: undefined,
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
  // Per-session main model pick for a draft ("new-*") tab — see
  // SessionSlice.model. Does NOT touch the global s.model.
  | { type: "SET_SESSION_MODEL"; sessionId: string; model: string | undefined }
  // Per-session permission-mode pick for a draft ("new-*") tab — see
  // SessionSlice.permissionMode. Does NOT touch any other tab.
  | { type: "SET_SESSION_PERMISSION_MODE"; sessionId: string; mode: string | undefined }
  | { type: "SET_SMALL_MODEL"; model: string }
  | { type: "SET_SMALL_MODEL_ENABLED"; enabled: boolean }
  | { type: "SET_ADVISOR_MODEL"; model: string }
  | { type: "SET_ADVISOR_ENABLED"; enabled: boolean }
  // Optimistic per-session advisor gate (see SessionSlice.advisorEnabled).
  // Session-scoped: never touches the global s.advisorEnabled. `enabled:
  // undefined` clears the pending flip so the authoritative snapshot wins.
  | { type: "SET_SESSION_ADVISOR_ENABLED"; sessionId: string; enabled?: boolean }
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
  /** A transient informational line appended to the live buffer (discovery
   *  notices mirrored from the TUI). Append-only: unlike a permission/advisor
   *  status part it is never removed on completion. */
  | { type: "LIVE_NOTICE"; sessionId: string; text: string }
  | { type: "PERMISSION_REQUEST"; sessionId: string; permission: PermissionRequest }
  | { type: "PERMISSION_RESOLVED"; sessionId: string; requestId?: string }
  | { type: "QUESTION_REQUEST"; sessionId: string; question: QuestionRequest }
  | { type: "QUESTION_RESOLVED"; sessionId: string; requestId?: string }
  | {
      type: "QUESTION_ANSWERED";
      sessionId: string;
      requestId: string;
      answers: QuestionAnswerPayload[];
    }
  | { type: "QUESTION_DISMISSED"; sessionId: string; requestId: string }
  | { type: "PREPEND_MESSAGES"; sessionId: string; messages: Message[]; total: number }
  | { type: "SET_LOADING_MORE"; sessionId: string; loading: boolean }
  | { type: "MERGE_SNAPSHOT"; sessionId: string; messages: Message[]; total: number }
  | { type: "SET_TOTAL"; sessionId: string; total: number }
  | { type: "SET_SPENDING"; spendingUSD: number | null }
  | { type: "SET_TUI_STATUS"; sessionId: string; status: TUIStatus }
  | { type: "SET_AGENT_ACTIVITY"; sessionId: string; activity: AgentActivityEvent }
  | { type: "SET_STATUS_LOADING"; sessionId: string; loading: boolean }
  | { type: "SET_TURN_STATE"; sessionId: string; turnActive: boolean }
  | { type: "SET_TURN_ERROR"; sessionId: string; turnError: boolean }
  | { type: "SET_TURN_HEARTBEAT"; sessionId: string }
  | { type: "SET_TURN_STALLED"; sessionId: string; stalled: boolean }
  | { type: "SET_BOOTSTRAP_STAGE"; sessionId: string; stage: string | null }
  | { type: "SET_TUI_STATUS_READY"; ready: boolean }
  | { type: "SET_WAS_INTERRUPTED"; sessionId: string; wasInterrupted: boolean }
  | { type: "SET_INTERRUPTED"; sessionId: string; interrupted: boolean }
  | { type: "INTERRUPT"; sessionId: string }
  | { type: "REKEY_SESSION"; oldId: string; newId: string }
  | { type: "TOGGLE_RUN_COLLAPSED"; sessionId: string; runId: string }
  | { type: "RESET"; sessionId: string }
  | { type: "TRUNCATE_MESSAGES"; sessionId: string; keepUntil: number };

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

/**
 * MAX_SLICE_MESSAGES caps the in-memory `messages` window per open tab.
 * Long-lived tabs grow unbounded today: every turn's full-transcript
 * broadcast, every streamed delta append, and every paginated prepend land
 * in the same array, so a session left open for days accumulates the entire
 * transcript (megabytes of tool output per message) in the reducer state —
 * the same desktop-memory pressure the Go side's bounded reads address.
 *
 * The window stays a contiguous TAIL of the server transcript, which is the
 * invariant the rest of the app relies on:
 *  - ChatPanel scroll-up fetches with `offset: currentCount` ("skip this
 *    many from the end") — correct as long as `messages` is the transcript's
 *    newest N.
 *  - hasMore is derived from totalMessages, so trimming the head flips
 *    hasMore naturally and the user can page the trimmed prefix back in.
 *  - TRUNCATE_MESSAGES indices are relative to the loaded window; the cap
 *    only ever drops from the head after a fresh append, never reorders.
 *
 * When the cap trims, totalMessages is unchanged (it tracks the server
 * total), so pagination math stays exact. Exemptions: PREPEND_MESSAGES and
 * TRUNCATE_MESSAGES are user-driven window operations that preserve
 * contiguity themselves.
 */
export const MAX_SLICE_MESSAGES = 400;

/**
 * capMessages keeps the newest MAX_SLICE_MESSAGES of a messages window.
 * Returns the input array unchanged when within the cap (no allocation).
 * `total` is the authoritative server total (defaults to messages.length);
 * hasMore must reflect "older messages exist server-side", so a trimmed
 * window (or a window shorter than total) reports true.
 */
export function capMessages(
  messages: Message[],
  total = messages.length,
): { messages: Message[]; hasMore: boolean } {
  if (messages.length <= MAX_SLICE_MESSAGES) {
    return { messages, hasMore: total > messages.length };
  }
  return {
    messages: messages.slice(messages.length - MAX_SLICE_MESSAGES),
    hasMore: true, // total >= messages.length > cap, so older always exist
  };
}

/** True when `next` is provably older than the snapshot already displayed.
 *
 *  Status snapshots reach a session slice from four independent writers whose
 *  responses can resolve out of order — a 15s poll, the visibilitychange
 *  refresh, the server's session-tagged SSE push, and a mutation's own
 *  refetch. Without an ordering guard a slow GET issued *before* a user
 *  mutation lands *after* the fresher push and reverts the UI to the old
 *  value until the next poll ("it works out of the blue later").
 *
 *  `updated_at` is stamped server-side when the snapshot is built
 *  (buildStatusSnapshot / HandleSessionStatus / pushSessionStatusSnapshot),
 *  so it orders snapshots correctly across all four transports.
 *
 *  Comparisons are deliberately conservative: two snapshots with no
 *  parseable `updated_at` are never treated as stale, and equal timestamps
 *  are accepted (a same-instant writer must still be able to update fields
 *  the other one omitted). */
export function isStaleStatus(
  prev: TUIStatus | null | undefined,
  next: TUIStatus | null | undefined,
): boolean {
  if (!prev || !next) return false;
  const prevAt = Date.parse(prev.updated_at ?? "");
  const nextAt = Date.parse(next.updated_at ?? "");
  if (Number.isNaN(prevAt) || Number.isNaN(nextAt)) return false;
  return nextAt < prevAt;
}

/** Drop the three live agent-loop activity fields from a status snapshot.
 *
 *  Returns the input unchanged when there is nothing to clear, so the common
 *  "turn ended, no activity was ever recorded" path keeps the existing object
 *  identity and doesn't re-render every subscriber for no reason. */
function clearedActivity(status: TUIStatus | null): TUIStatus | null {
  if (!status) return status;
  if (
    status.llm_running === undefined &&
    status.active_tools === undefined &&
    status.active_agents === undefined
  ) {
    return status;
  }
  return { ...status, llm_running: undefined, active_tools: undefined, active_agents: undefined };
}

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
    case "ADD_MESSAGE": {
      return updateSession(state, action.sessionId, (s) => {
        const grown = [...s.messages, action.message];
        const capped = capMessages(grown, s.totalMessages || grown.length);
        // If the append pushed the window past the cap it trims the HEAD, which
        // moves the window start forward by exactly the number dropped. Without
        // this the anchor would go stale and a search jump would land short.
        const trimmed = grown.length - capped.messages.length;
        return {
          ...s,
          messages: capped.messages,
          windowStartServerIndex:
            s.windowStartServerIndex >= 0 ? s.windowStartServerIndex + trimmed : -1,
          hasMore: capped.hasMore || s.hasMore,
        };
      });
    }
    case "SET_MESSAGES":
      // Authoritative snapshot lands at a turn boundary — commit it and clear
      // the live buffer it supersedes. It also marks the slice initialized:
      // the mirror's snapshot is authoritative history, so a tab whose slice
      // was populated by the mirror must not keep its "loading" spinner
      // waiting on a history fetch that is now redundant.
      return updateSession(state, action.sessionId, (s) => {
        const pending = extractPendingFromMessages(action.messages);
        const capped = capMessages(action.messages);
        return {
          ...s,
          messages: capped.messages,
          live: [],
          initialized: true,
          hasMore: capped.hasMore,
          totalMessages: Math.max(s.totalMessages, action.messages.length),
          // The turn-boundary broadcast carries the WHOLE transcript, so the
          // window start is however many messages the cap trimmed off the head.
          windowStartServerIndex: Math.max(0, action.messages.length - capped.messages.length),
          pendingPermission: pending.pendingPermission,
          permissionQueue: pending.permissionQueue,
          pendingQuestion: pending.pendingQuestion,
        };
      });
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
    case "SET_SESSION_MODEL":
      return updateSession(state, action.sessionId, (s) => ({ ...s, model: action.model }));
    case "SET_SESSION_PERMISSION_MODE":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        permissionMode: action.mode,
      }));
    case "SET_SMALL_MODEL":
      return { ...state, smallModel: action.model };
    case "SET_SMALL_MODEL_ENABLED":
      return { ...state, smallModelEnabled: action.enabled };
    case "SET_ADVISOR_MODEL":
      return { ...state, advisorModel: action.model };
    case "SET_ADVISOR_ENABLED":
      return { ...state, advisorEnabled: action.enabled };
    case "SET_SESSION_ADVISOR_ENABLED":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        advisorEnabled: action.enabled,
      }));
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
      // A null error clears the whole error surface — including the retryable
      // turn-error flag set by turn_error/error SSE frames — because it means a
      // new turn is starting (send/retry) or the turn started cleanly. A
      // non-null error only updates `error`: the retry flag is owned by the SSE
      // handlers so a submit failure cannot masquerade as a retryable turn.
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        error: action.error,
        turnError: action.error === null ? false : s.turnError,
      }));
    case "SET_TURN_ERROR":
      return updateSession(state, action.sessionId, (s) =>
        s.turnError === action.turnError ? s : { ...s, turnError: action.turnError },
      );
    case "APPEND_DELTA":
      return updateSession(state, action.sessionId, (s) => {
        const msgs = [...s.messages];
        const last = msgs[msgs.length - 1];
        if (last && last.role === "assistant") {
          msgs[msgs.length - 1] = { ...last, content: last.content + action.delta };
        } else {
          msgs.push({ role: "assistant", content: action.delta });
        }
        const capped = capMessages(msgs, s.totalMessages || msgs.length);
        // Same head-trim accounting as ADD_MESSAGE: a streamed append that
        // exceeds the cap drops from the head and advances the window start.
        const trimmed = msgs.length - capped.messages.length;
        return {
          ...s,
          messages: capped.messages,
          windowStartServerIndex:
            s.windowStartServerIndex >= 0 ? s.windowStartServerIndex + trimmed : -1,
          hasMore: capped.hasMore || s.hasMore,
        };
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
    case "LIVE_NOTICE":
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        live: [...s.live, { kind: "notice", text: action.text }],
      }));
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
      return updateSession(state, action.sessionId, (s) => {
        // A resolve for a question that isn't the one on screen (a stale
        // dismissal for an older/superseded round) must not close the newer
        // dialog. Permission resolves already carry this guard; questions use
        // a single pending slot, so a mismatched request_id is ignored.
        if (
          action.requestId &&
          s.pendingQuestion &&
          s.pendingQuestion.request_id !== action.requestId
        ) {
          return s;
        }
        return { ...s, pendingQuestion: null };
      });
    case "QUESTION_DISMISSED":
      // Optimistic echo of a Cancel the browser just POSTed. Mirrors
      // QUESTION_ANSWERED: rewrite the local sentinel tool result in place with
      // the same dismissal notice the server persists, so the chat stops
      // showing the prompt and a later reconcile (which re-derives the pending
      // ask from the transcript sentinel) does not immediately reopen the
      // dialog. Idempotent and a no-op when the sentinel is not in the loaded
      // page — the server snapshot is then the only source.
      //
      // A dismissal is a deliberate STOP, not an interrupted turn: clear the
      // server-derived `interrupted` flag here so the "previous reply was
      // interrupted / Continue" notice cannot linger (the server now classifies
      // a dismissed-question tail as stopped, so the next reconcile agrees).
      return updateSession(state, action.sessionId, (s) => {
        let replaced = false;
        const messages = s.messages.map((m) => {
          if (
            replaced ||
            m.role !== "tool" ||
            m.tool_call_id !== action.requestId ||
            !m.content.includes(SENTINEL_QUESTION_PROMPT)
          ) {
            return m;
          }
          replaced = true;
          return { ...m, content: QUESTION_DISMISSED_RESULT };
        });
        return { ...s, messages, pendingQuestion: null, interrupted: false };
      });
    case "QUESTION_ANSWERED":
      // Optimistic echo of the answers the browser just POSTed. The server
      // rewrites the pending `question` tool result in place with the
      // shape-compatible payload parsed from the same answers
      // (handler_questions.go applyQuestionAnswer -> questionAnswerPayload,
      // whose optional fields are omitempty — so equal shape, not necessarily
      // byte-identical), so mirroring it here
      // makes the Q&A visible the instant the dialog is submitted instead of
      // only when the continuation turn's `messages` snapshot lands. The
      // snapshot later replaces the message with equivalent content, so this
      // stays idempotent; when the sentinel is not in the loaded page (deep
      // history / hydrated ask) the turn-end snapshot is the only source and
      // the local rewrite is a no-op.
      return updateSession(state, action.sessionId, (s) => {
        const answers = JSON.stringify(action.answers);
        let replaced = false;
        const messages = s.messages.map((m) => {
          if (
            replaced ||
            m.role !== "tool" ||
            m.tool_call_id !== action.requestId ||
            !m.content.includes(SENTINEL_QUESTION_PROMPT)
          ) {
            return m;
          }
          replaced = true;
          return { ...m, content: answers };
        });
        return { ...s, messages, pendingQuestion: null };
      });
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
    case "SET_AGENT_ACTIVITY": {
      // Live agent-loop reading from the headless server (web/desktop chat),
      // mirroring the TUI's own activity feed. MERGE the three activity fields
      // into the existing snapshot — this payload carries nothing else, and a
      // replace would blank the model / context gauge / spend the rest of the
      // status bar renders. Defaults to empty rather than undefined so a turn
      // that just finished its last tool always reports "nothing running".
      const activity = action.activity;
      return updateSession(state, action.sessionId, (s) => ({
        ...s,
        tuiStatus: {
          ...(s.tuiStatus ?? {}),
          llm_running: !!activity.llm_running,
          active_tools: activity.active_tools ?? [],
          active_agents: activity.active_agents ?? [],
        },
      }));
    }
    case "SET_TUI_STATUS": {
      // Reject a snapshot generated before the one already displayed. The four
      // writers (15s poll, visibility refresh, SSE push, mutation refetch)
      // resolve out of order, so without this a slow GET issued before a
      // mutation clobbers the fresher snapshot with the old value and only the
      // next poll repairs it ("works out of the blue later").
      if (isStaleStatus(getSessionSlice(state, action.sessionId).tuiStatus, action.status)) {
        return state;
      }
      return {
        ...updateSession(state, action.sessionId, (s) => {
          // Reconcile an optimistic advisor flip with the authoritative
          // snapshot: clear it once the snapshot carries the same value (the
          // truth now lives in tuiStatus). A snapshot that still disagrees is
          // a poll that raced the in-flight PUT, so keep the optimistic value
          // rather than flashing the checkbox back. A snapshot without the
          // field (older payload) leaves the pending flip alone.
          const authoritative = action.status?.advisor_enabled;
          const confirmed = s.advisorEnabled !== undefined && authoritative === s.advisorEnabled;
          return { ...s, tuiStatus: action.status, advisorEnabled: confirmed ? undefined : s.advisorEnabled };
        }),
        tuiStatusReady: true,
      };
    }
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
              // Turn boundary: the agent loop is no longer running, so the
              // live reading is stale. StatusBar gates its activity row on
              // isStreaming||turnActive regardless, but clearing here stops a
              // late agent_activity frame from parking a dead "⟳ llm" in the
              // snapshot for the next turn to inherit.
              tuiStatus: clearedActivity(s.tuiStatus),
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
    case "SET_INTERRUPTED":
      // Server-derived flag (see SessionSlice.interrupted). Assigned verbatim
      // from each /state payload; the client has no other clearing logic.
      return updateSession(state, action.sessionId, (s) =>
        s.interrupted === action.interrupted ? s : { ...s, interrupted: action.interrupted },
      );
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
      //
      // The anchor advances by exactly the number of messages prepended: the
      // new window starts `len(action.messages)` transcript positions earlier
      // than it did. Callers always request a contiguous older block ending at
      // the current window start, so this subtraction is exact. A slice with no
      // anchor yet (-1, e.g. a slice populated purely by live SSE before its
      // first page resolved) adopts the position implied by this page.
      return updateSession(state, action.sessionId, (s) => {
        const hasMore = action.messages.length > 0 && s.messages.length + action.messages.length < action.total;
        const nextStart = s.windowStartServerIndex >= 0
          // Clamp at 0: a well-formed contiguous prefix never underflows, but a
          // racing/duplicate prepend could, and a negative anchor is treated as
          // "unknown" by consumers (silently disabling server anchoring).
          ? Math.max(0, s.windowStartServerIndex - action.messages.length)
          : Math.max(0, action.total - s.messages.length - action.messages.length);
        return {
          ...s,
          messages: [...action.messages, ...s.messages],
          totalMessages: action.total,
          windowStartServerIndex: nextStart,
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
        const pending = extractPendingFromMessages(action.messages);
        if (s.turnActive && s.messages.length > 0) {
          // Mid-turn guard: the live buffer/committed messages are newer than
          // this disk snapshot, so they are preserved. Pending asks however
          // must still be hydrated from the snapshot when the live slice has
          // none — reconcileOpenSessions arms turnActive BEFORE dispatching
          // this merge (applyReconcileState), so a reload/reconnect that
          // missed the live `question`/`permission` event would otherwise
          // never surface the dialog (the mid-turn guard skipped the
          // re-derivation entirely and the sentinel was the only recovery
          // source). An already-live pending ask always wins over the
          // snapshot — the live event is newer.
          return {
            ...s,
            totalMessages: action.total,
            hasMore: s.messages.length < action.total,
            initialized: true,
            pendingPermission: s.pendingPermission ?? pending.pendingPermission,
            permissionQueue:
              s.pendingPermission != null
                ? s.permissionQueue
                : pending.permissionQueue,
            pendingQuestion: s.pendingQuestion ?? pending.pendingQuestion,
          };
        }
        const capped = capMessages(action.messages, action.total);
        return {
          ...s,
          messages: capped.messages,
          totalMessages: action.total,
          // A snapshot is always a TAIL page of the transcript (getSession
          // returns the newest N), so the window start is exactly the messages
          // the snapshot is short of the server total.
          windowStartServerIndex: Math.max(0, action.total - capped.messages.length),
          hasMore: capped.hasMore,
          live: s.turnActive ? s.live : [],
          initialized: true,
          pendingPermission: pending.pendingPermission,
          permissionQueue: pending.permissionQueue,
          pendingQuestion: pending.pendingQuestion,
        };
      });
    case "TRUNCATE_MESSAGES":
      return updateSession(state, action.sessionId, (s) => {
        const keep = Math.max(0, Math.min(action.keepUntil, s.messages.length));
        if (keep === s.messages.length) return s;
        return {
          ...s,
          messages: s.messages.slice(0, keep),
          totalMessages: keep,
          hasMore: false,
          live: [],
          pendingPermission: null,
          permissionQueue: [],
          pendingQuestion: null,
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
