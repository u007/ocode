import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useChatSelector, useChatDispatch, getSessionSlice, parseQuestionFromMessage, type QuestionRequest } from "../../stores/chatStore";
import { useProjectDispatch } from "../../stores/projectStore";
import { api } from "../../api/client";
import MessageBubble, { AssistantText, hasRenderableText } from "./MessageBubble";
import { StatusBlock, ThinkingBlock, ToolBlock, NoticeBlock } from "./TurnParts";
import ChatSearchBar, { messageMatchesQuery } from "./ChatSearchBar";
import ModelPromptRow from "./ModelPromptRow";
import { RESTORE_EVENT } from "../../lib/inputRestore";
import { SESSION_PREFETCH_LIMIT, takePrefetchedSession } from "../../lib/sessionPrefetch";
import {
  buildJumpTargets,
  inWindowMatchCount,
  olderPrefixFetch,
  serverIndexToLocal,
  type ServerSearchResult,
} from "../../lib/sessionSearch";
import { requestSpeech } from "../Speech/SpeechProvider";
import { lastRenderedSpeechText, renderedSpeechTexts } from "../Speech/speechUtils";
import { ArrowDown, ArrowUp, Volume2 } from "lucide-react";

const PAGE_SIZE = 50;
/** Debounce for the full-transcript search query. Long enough to avoid a
 *  request per keystroke, short enough that the out-of-window count settles
 *  while the user is still reading the result. */
const SEARCH_DEBOUNCE_MS = 200;
/** Scroll a container to an offset. Falls back to the `scrollTop` property
 *  when `Element.prototype.scrollTo` is unavailable (jsdom), so the scroll
 *  affordances degrade instead of throwing under test. */
function scrollElementTo(el: HTMLElement, top: number, behavior: ScrollBehavior) {
  if (typeof el.scrollTo === "function") {
    el.scrollTo({ top, behavior });
  } else {
    el.scrollTop = top;
  }
}

interface ChatPanelProps {
  /** The tab this instance renders — a real session id or a temporary
   *  `new-<ts>` tab id. One ChatPanel is mounted per open tab (App.tsx),
   *  so this never changes across this instance's lifetime. */
  sessionId: string;
  /** SSH/WSL host of the tab's project (undefined for local). Routes the
   *  transcript fetch — and the prefetch hand-off — through /api/remote/{host}
   *  so a remote session's messages load instead of 404ing locally. */
  host?: string;
  /** Continue the session's settled-but-unfinished turn (the server-reported
   *  `interrupted` flag). App owns the send path (ChatPanel has none), so it
   *  passes this callback down; must be a STABLE reference or the memo on this
   *  component is defeated for every mounted tab. */
  onContinueInterrupted?: (sessionId: string) => void;
}

function ChatPanel({ sessionId, host, onContinueInterrupted }: ChatPanelProps) {
  // Scoped to this tab's own session: getSessionSlice returns the exact same
  // object reference across dispatches that don't touch this session (see
  // updateSession's immutable per-key update), so other tabs' streamed
  // tokens don't re-render this ChatPanel instance.
  const slice = useChatSelector((s) => getSessionSlice(s, sessionId));
  const dispatch = useChatDispatch();
  // Stable dispatch-only subscription: this must NOT read the project state, or
  // every tab/project switch would re-render every mounted (hidden) ChatPanel.
  // Combined with `memo` below, a hidden tab only re-renders for its own chat
  // slice — never because a sibling tab became active.
  const projectDispatch = useProjectDispatch();
  const { messages, live, hasMore, loadingMore, windowStartServerIndex, totalMessages } = slice;
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const topRef = useRef<HTMLDivElement>(null);
  const [initialized, setInitialized] = useState(false);
  // Initial-load failure state. A transcript that could not be fetched must
  // never render as the empty "Start a conversation" placeholder — that reads
  // as "the whole conversation is gone" (reported after a desktop reload left
  // a tab showing only the composer while the transcript was intact
  // server-side). Surface the failure and offer a Retry instead.
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loadRetry, setLoadRetry] = useState(0);
  const forceReloadRef = useRef(false);
  const loadGenerationRef = useRef(0);
  const stateRef = useRef(slice);
  stateRef.current = slice;
  const [reachedTop, setReachedTop] = useState(false);
  // Whether the viewport is pinned to the bottom. Driven by handleScroll and
  // consulted by the auto-scroll effect so we only follow the tail when the
  // user is already at the bottom (and resume reliably after they return).
  // This IS the scroll lock: false means the user scrolled up and auto-scroll
  // must not follow until they scroll back down or the lock is reset.
  const atBottomRef = useRef(true);
  // Last observed scrollTop. Used to detect an UPWARD move synchronously in
  // handleScroll: a scroll event whose offset decreased can only come from the
  // user (our own pins always increase it). Unpinning in the same tick as the
  // event — instead of waiting for the deferred recompute — is what stops a
  // streamed token that lands in the same frame from re-pinning and swallowing
  // the user's scroll-up.
  const lastScrollTopRef = useRef(0);
  // Previous committed message-list size, so the auto-scroll effect can tell a
  // transcript RESET (truncate/clear/replace, which shrinks the list) from an
  // append or prepend (which only grow it).
  const prevMessagesRef = useRef<{ count: number } | null>(null);
  const [showJumpToBottom, setShowJumpToBottom] = useState(false);
  // Mirror of showJumpToBottom for the other edge: shown whenever there is more
  // than a viewport-threshold of transcript above the current position. Set by
  // handleScroll (the scroll listener) and by the pin-to-bottom effects, which
  // move the offset without a user-initiated scroll.
  const [showJumpToTop, setShowJumpToTop] = useState(false);
  const [selectedText, setSelectedText] = useState("");
  const lastCompletedAssistantRef = useRef<string | null>(null);
  const wasTurnActiveRef = useRef(false);

  useEffect(() => {
    const wasTurnActive = wasTurnActiveRef.current;
    wasTurnActiveRef.current = slice.turnActive;
    if (!initialized || !wasTurnActive || slice.isStreaming || slice.turnActive) return;
    const assistant = [...messages].reverse().find((message) => message.role === "assistant" && message.content.trim());
    if (!assistant) return;
    // Speak what is RENDERED, not the markdown source (heading hashes, `**`,
    // backticks and link targets must not be read aloud). The transcript is
    // virtualized, so the completed message's DOM node is only guaranteed while
    // the view is pinned to the bottom — which is exactly when "at-bottom" mode
    // speaks. Fall back to the source if the node is somehow not mounted.
    const rendered = lastRenderedSpeechText(listContainerRef.current);
    const text = (rendered || assistant.content).trim();
    if (!text) return;
    const key = `${messages.length}:${text}`;
    if (lastCompletedAssistantRef.current === key) return;
    lastCompletedAssistantRef.current = key;
    window.dispatchEvent(new CustomEvent("ocode:assistant-complete", { detail: { text, atBottom: atBottomRef.current } }));
  }, [initialized, messages, slice.isStreaming, slice.turnActive]);

  // Restore-to-input truncation: TUI truncates messages[:index] when restoring a
  // user message. This listener owns history mutation; ChatInput only handles
  // the draft. Uses entry.originalIndex (absolute messages index), not the
  // virtualized row index.
  useEffect(() => {
    const handler = (e: Event) => {
      const ce = e as CustomEvent<{ sessionId: string; text: string; index?: number }>;
      if (!ce.detail || ce.detail.sessionId !== sessionId) return;
      const idx = ce.detail.index;
      if (typeof idx !== "number" || !Number.isFinite(idx)) return;
      // Don't truncate while a turn is active — it could append after truncation.
      if (slice.isStreaming || slice.turnActive || slice.live.length > 0) return;
      // If older messages are not fully loaded, indices are not absolute.
      if (slice.hasMore) return;
      if (idx < 0 || idx > slice.messages.length) return;
      // Only allow restoring a user message; if not, just let ChatInput handle draft.
      const msg = slice.messages[idx];
      if (!msg || msg.role !== "user") return;
      dispatch({ type: "TRUNCATE_MESSAGES", sessionId, keepUntil: idx });
      // Persist truncation server-side; failure is non-fatal (client already truncated).
      if (!sessionId.startsWith("new-")) {
        api.truncateSession(sessionId, idx).catch((err) => {
          console.warn("truncateSession failed", err);
        });
      }
    };
    window.addEventListener(RESTORE_EVENT, handler as EventListener);
    return () => window.removeEventListener(RESTORE_EVENT, handler as EventListener);
  }, [sessionId, slice.messages, slice.hasMore, slice.isStreaming, slice.turnActive, slice.live, dispatch]);

  // In-chat find bar (Ctrl/Cmd+F). Match computation happens in TWO layers:
  //   1. Instant, client-side, over the loaded window (below) — drives
  //      highlighting and keeps typing responsive with no network.
  //   2. A debounced server query over the WHOLE transcript, so hits outside
  //      the loaded window (the store caps it at MAX_SLICE_MESSAGES) are not
  //      silently missed. See lib/sessionSearch.ts and
  //      internal/server/handler_session_search.go.
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [matchCursor, setMatchCursor] = useState(-1);
  // Server-side result: total hits in the full transcript + their server
  // indices. `null` until the debounced query resolves (or for a draft tab).
  const [serverMatches, setServerMatches] = useState<ServerSearchResult | null>(null);
  // Set true while a search jump is scrolling so handleScroll doesn't fire the
  // scroll-up pagination loader (which would shift every message index and
  // land the highlight on the wrong bubble).
  const searchJumpRef = useRef(false);

  // Debounced full-transcript search. The local matcher above keeps typing
  // instant; this fills in the hits the loaded window does not contain. A
  // stale response must not overwrite a newer query, so the effect is keyed on
  // the query and guarded by `cancelled` (the same pattern the transcript
  // fetch uses). Skipped for a draft tab (no server session yet) and while the
  // bar is closed.
  useEffect(() => {
    const q = searchQuery.trim();
    // Any query/open change invalidates the previous result SYNCHRONOUSLY.
    // Otherwise the old indices stay live through the debounce + round-trip,
    // so buildJumpTargets pairs the previous query's server hits with the new
    // query's local highlight (wrong count, dead "next") until it resolves.
    setServerMatches(null);
    if (!searchOpen || !q || !sessionId || sessionId.startsWith("new-")) {
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      api.searchSession(sessionId, q, {}, host)
        .then((res) => {
          if (cancelled) return;
          setServerMatches(res);
        })
        .catch((err) => {
          // A search failure must not break the find bar: fall back to the
          // in-window matches rather than surfacing a dialog-less error.
          if (cancelled) return;
          console.warn("session search failed", err);
          setServerMatches(null);
        });
    }, SEARCH_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [searchOpen, searchQuery, sessionId, host]);


  // --- Virtualizer coordinate fix (Finding 2) ---------------------------------
  // `scrollMargin` is the offset (scroll-surface padding + the variable-height
  // status/loading header) between the top of the scroll surface and the first
  // virtualized message. Without it the virtualizer assumes the list starts at
  // scrollTop 0 and windows the wrong range once the header is present.
  const listContainerRef = useRef<HTMLDivElement>(null);
  const [listMargin, setListMargin] = useState(0);
  // `Message` has no stable id, so key virtual items by message-object identity
  // (referentially stable across prepend pagination) rather than array index,
  // which would mis-associate measurements after a prepend.
  function isSentinelToolContent(content: string | undefined): boolean {
    return !!content && (content.startsWith("QUESTION_PROMPT:") || content.startsWith("PERMISSION_ASK:"));
  }

  type RenderEntry =
    | { kind: "single"; msg: import("../../api/types").Message; originalIndex: number }
    | {
        kind: "tool-group";
        assistant: import("../../api/types").Message;
        originalIndex: number;
        calls: Array<{
          tc: import("../../api/types").ToolCall;
          resultContent?: string;
          resultIdx?: number;
          /** The unanswered `question` prompt this call is paused on, so the
           *  card can re-open its dialog on demand. */
          pendingQuestion?: QuestionRequest;
        }>;
      };

  const msgKeyMap = useRef(new WeakMap<object, number>());
  const msgKeyCounter = useRef(0);
  const renderEntriesRef = useRef<RenderEntry[]>([]);
  const getItemKey = useCallback((index: number): number => {
    const entry = renderEntriesRef.current[index] as RenderEntry | undefined;
    if (!entry) return index;
    const keyObj: object =
      entry.kind === "single" ? (entry.msg as object) : (entry.assistant as object);
    let id = msgKeyMap.current.get(keyObj);
    if (id === undefined) {
      id = msgKeyCounter.current++;
      msgKeyMap.current.set(keyObj, id);
    }
    return id;
  }, []);

  // Group tool results into their parent assistant turn so the web transcript
  // matches the TUI and the user can tell which result belongs to which
  // request (e.g. two consecutive `read` calls). A `tool-group` entry renders
  // every tool call with its result inside a single bubble; orphan `tool`
  // messages (parent not in the loaded window due to pagination, or unmatched
  // id) remain as ordinary singles so nothing is lost.
  // Sentinel tool messages (QUESTION_PROMPT / PERMISSION_ASK) are already
  // rendered by dedicated dialogs and are excluded from both grouping and
  // virtualization so they never claim a slot.
  const renderEntries: RenderEntry[] = useMemo(() => {
    const resultById = new Map<string, { content: string; idx: number }>();
    const pendingQuestionById = new Map<string, QuestionRequest>();
    messages.forEach((m, idx) => {
      if (m.role === "tool" && m.tool_call_id && !isSentinelToolContent(m.content)) {
        resultById.set(m.tool_call_id, { content: m.content, idx });
      }
      if (m.role === "tool" && m.tool_call_id) {
        const q = parseQuestionFromMessage(m);
        if (q) pendingQuestionById.set(m.tool_call_id, q);
      }
    });
    const entries: RenderEntry[] = [];
    // Pre-mark ids that will be consumed by upcoming assistant groups so that
    // when we later encounter the tool message we can skip it. Building the
    // set in a first pass keeps the second pass strictly forward and avoids
    // order-dependence. A consumed guard ensures one-to-one attachment if a
    // malformed transcript reuses the same tool_call_id.
    const willConsume = new Set<string>();
    for (const m of messages) {
      if (m.role === "assistant" && m.tool_calls?.length) {
        for (const tc of m.tool_calls) {
          if (resultById.has(tc.id)) willConsume.add(tc.id);
        }
      }
    }
    const consumed = new Set<string>();
    for (let i = 0; i < messages.length; i++) {
      const msg = messages[i];
      if (msg.role === "tool" && isSentinelToolContent(msg.content)) continue;
      if (msg.role === "assistant" && msg.tool_calls?.length) {
        const calls = msg.tool_calls.map((tc) => {
          const hit = resultById.get(tc.id);
          // Enforce one-to-one: a result already attached to an earlier call
          // is not re-attached to a later duplicate id.
          if (hit && !consumed.has(tc.id)) {
            consumed.add(tc.id);
            return { tc, resultContent: hit.content, resultIdx: hit.idx };
          }
          if (hit) {
            // Duplicate id — show the call without a second copy of the same result.
            return { tc, resultContent: undefined, resultIdx: undefined };
          }
          return { tc, resultContent: undefined, resultIdx: undefined, pendingQuestion: pendingQuestionById.get(tc.id) };
        });
        entries.push({ kind: "tool-group", assistant: msg, originalIndex: i, calls });
        continue;
      }
      if (msg.role === "tool") {
        const id = msg.tool_call_id ?? "";
        if (id && willConsume.has(id) && resultById.has(id)) {
          // This result is already attached to its parent group; skip the
          // detached bubble. The sentinel case was already continued above.
          continue;
        }
        entries.push({ kind: "single", msg, originalIndex: i });
        continue;
      }
      entries.push({ kind: "single", msg, originalIndex: i });
    }
    return entries;
  }, [messages]);
  renderEntriesRef.current = renderEntries;

  // Only committed messages are virtualized — a long session's history is
  // what was growing the DOM (and retained JS heap: fiber nodes, markdown/
  // syntax-highlighter output) unboundedly, since nothing ever unmounted as
  // the user scrolled past it. `live` (the in-progress turn) is rendered as a
  // normal (non-virtualized) tail inside the same scroll container: it's
  // always visible, short-lived, and its own size churns too fast for
  // virtualization to help.
  // estimateSize is deliberately rough (real heights vary a lot — code
  // blocks vs. one-line replies); measureElement (wired via the ref callback
  // below) corrects it per item after first paint.
  const virtualizer = useVirtualizer({
    count: renderEntries.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 96,
    overscan: 8,
    // Coordinate space = scroll surface top + this margin. Measured live (see
    // effect below) so the header's variable height is always accounted for.
    scrollMargin: listMargin,
    getItemKey,
  });

  // Keep `scrollMargin` in sync with the real offset of the virtualized list
  // (the variable-height status/loading header at topRef). Only the header's
  // height affects offsetTop; observing the virtualized list itself or the
  // scroll surface caused thrash (totalSize -> ResizeObserver -> setListMargin
  // -> re-render -> new totalSize). Frame-batched and value-guarded so no
  // stale margin causes the split-then-merge flicker.
  const blockRendered = initialized && messages.length > 0;

  // Server-derived interrupted-turn flag: the session settled on a turn that
  // never got a reply. Render an inline row at the END of the transcript (not a
  // transcript entry — it must not enter the message list or the search index)
  // offering a one-click Continue. Suppressed whenever the reply might still be
  // coming or another affordance owns the state:
  //   - `wasInterrupted` is the live user-Stop signal (ChatInput treats it as
  //     "sending blocked"), so the two must never both show;
  //   - a running/streaming turn or live parts mean work is in flight;
  //   - a pending question/permission means the dialog owns it;
  //   - an empty transcript, a draft (`new-*`) tab, or a failed load has
  //     nothing to continue.
  const showInterrupted =
    initialized &&
    slice.interrupted &&
    !slice.wasInterrupted &&
    !slice.turnActive &&
    !slice.isStreaming &&
    live.length === 0 &&
    !slice.pendingPermission &&
    !slice.pendingQuestion &&
    !loadError &&
    messages.length > 0 &&
    !sessionId.startsWith("new-");

  // Continue hides the row immediately (optimistic) and hands the session to
  // App's send path; the server's next `turn_started`/state keeps it hidden.
  const handleContinueInterrupted = () => {
    dispatch({ type: "SET_INTERRUPTED", sessionId, interrupted: false });
    onContinueInterrupted?.(sessionId);
  };

  useLayoutEffect(() => {
    let raf = 0;
    const measure = () => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        const el = listContainerRef.current;
        const top = el ? el.offsetTop : 0;
        setListMargin((prev) => (prev === top ? prev : top));
      });
    };
    measure();
    const ro = new ResizeObserver(measure);
    if (topRef.current) ro.observe(topRef.current);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, [blockRendered]);

  // Match entry positions: which virtualized render entries contain the
  // query. For tool-groups the search covers the parent assistant's
  // reasoning/text, every tool name/args, and each attached result's
  // content so a hit on a result correctly highlights its parent group.
  // Sentinel tool messages are never rendered and are excluded from search,
  // mirroring the virtualization filter.
  const matchEntryPositions = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    if (!q) return [] as number[];
    const out: number[] = [];
    renderEntries.forEach((entry, pos) => {
      if (entry.kind === "single") {
        // messageMatchesQuery covers user/assistant content, reasoning, and
        // tool_calls. For orphan tool results we also match the resolved
        // tool name (not stored on the tool message itself).
        if (messageMatchesQuery(entry.msg, q)) {
          out.push(pos);
          return;
        }
        if (entry.msg.role === "tool" && entry.msg.tool_call_id) {
          // Fallback: search the tool name resolved from its parent call.
          // Build a tiny ad-hoc lookup for this entry only; the global
          // toolNameById map is built below and available via closure.
          // We check it here via a scan of messages to avoid TDZ, and rely
          // on the main toolNameById memo for the normal path — but since
          // that memo is defined after this block we cannot reference it
          // directly. Instead, just scan the already-known result's parent
          // by checking the raw messages array for a matching tool_call id.
          // This is O(n) per orphan tool entry, but orphan entries are rare
          // and n is bounded by loaded history.
          for (const m of messages) {
            for (const tc of m.tool_calls ?? []) {
              if (tc.id === entry.msg.tool_call_id && tc.function.name.toLowerCase().includes(q)) {
                out.push(pos);
                return;
              }
            }
          }
        }
        return;
      }
      // tool-group
      const a = entry.assistant;
      if (a.reasoning_content?.toLowerCase().includes(q) || a.content?.toLowerCase().includes(q)) {
        out.push(pos);
        return;
      }
      for (const c of entry.calls) {
        if (
          c.tc.function.name.toLowerCase().includes(q) ||
          c.tc.function.arguments.toLowerCase().includes(q) ||
          (c.resultContent && c.resultContent.toLowerCase().includes(q))
        ) {
          out.push(pos);
          return;
        }
      }
    });
    return out;
  }, [renderEntries, searchQuery, messages]);

  // Map a server transcript index to its render-entry position. Server indices
  // live in the same post-load message array the virtualizer's `messages` holds
  // (see handler_session_search.go), so the only translation needed is
  // windowStartServerIndex. Several messages fold into one bubble (a tool call
  // and its result), hence the collapse-to-one-entry in buildJumpTargets.
  const entryPosByServerIndex = useMemo(() => {
    const map = new Map<number, number>();
    if (serverMatches === null || windowStartServerIndex < 0) return map;
    const localPos = new Map<number, number>();
    renderEntries.forEach((entry, pos) => {
      localPos.set(entry.originalIndex, pos);
      // A tool result folded into its parent group is not a top-level entry,
      // so its server index is absent from LocalPos and a search hit on the
      // result would map to entryPos -1 (a dead "next"). Register each result
      // against its parent bubble; buildJumpTargets then collapses it into the
      // same target as the call that produced it.
      if (entry.kind === "tool-group") {
        for (const c of entry.calls) {
          if (c.resultIdx !== undefined) localPos.set(c.resultIdx, pos);
        }
      }
    });
    for (const si of serverMatches.indices) {
      const local = serverIndexToLocal(si, messages, windowStartServerIndex);
      if (local < 0) continue;
      const pos = localPos.get(local);
      if (pos !== undefined) map.set(si, pos);
    }
    return map;
  }, [renderEntries, serverMatches, windowStartServerIndex, messages]);

  // Ordered navigation targets: full-transcript hits when the server query has
  // resolved, otherwise the instant local matches. See lib/sessionSearch.ts.
  const jumpTargets = useMemo(
    () =>
      buildJumpTargets({
        serverIndices: serverMatches?.indices ?? null,
        entryPosByServerIndex,
        localEntryPositions: matchEntryPositions,
        windowStartServerIndex,
      }),
    [serverMatches, entryPosByServerIndex, matchEntryPositions, windowStartServerIndex],
  );

  // Secondary find-bar text. Only meaningful when the full-transcript search
  // knows about more hits than the loaded window shows — otherwise it would
  // just repeat the counter.
  const searchNote = useMemo(() => {
    if (serverMatches === null) return undefined;
    const inView = inWindowMatchCount(serverMatches.indices, entryPosByServerIndex);
    if (serverMatches.total <= inView) return undefined;
    const totalLabel = serverMatches.truncated ? `${serverMatches.total}+` : `${serverMatches.total}`;
    return `${totalLabel} total, ${inView} in view`;
  }, [serverMatches, entryPosByServerIndex]);

  const currentMatchEntryPos =
    matchCursor >= 0 && matchCursor < jumpTargets.length
      ? jumpTargets[matchCursor].entryPos
      : -1;

  // Initial load: fetch the tail of this session's transcript once
  // (SESSION_PREFETCH_LIMIT messages). Skipped for a `new-*` tab, which has no
  // session yet, and for a session whose slice is already initialized — e.g.
  // this ChatPanel remounted, or a live SSE event populated the slice before
  // this fetch resolved.
  useEffect(() => {
    const generation = ++loadGenerationRef.current;
    let cancelled = false;

    if (!sessionId || sessionId.startsWith("new-")) {
      setLoadError(null);
      setInitialized(true);
      return () => {
        cancelled = true;
      };
    }
    if (stateRef.current.initialized && !forceReloadRef.current) {
      setLoadError(null);
      setInitialized(true);
      return () => {
        cancelled = true;
      };
    }
    forceReloadRef.current = false;
    setInitialized(false);
    setLoadError(null);

    // A hover prefetch (lib/sessionPrefetch) may already have this request in
    // flight or resolved — use it so the tab paints straight from warm data
    // instead of starting a cold round-trip. Falls back to a fresh fetch when
    // nothing is warm or the warm entry is stale.
    (takePrefetchedSession(sessionId, host) ?? api.getSession(sessionId, { limit: SESSION_PREFETCH_LIMIT }, host))
      .then((detail) => {
        if (cancelled || generation !== loadGenerationRef.current) return;
        // Mirrors MERGE_SNAPSHOT guard in chatStore.tsx — only committed
        // messages suppress the merge; live alone does not. The reducer
        // preserves the live buffer mid-turn.
        const current = stateRef.current;
        if (current.messages.length > 0) {
          // The mirror already populated the slice while the fetch was in
          // flight — its state is newer than disk. Do not wipe it with this
          // older snapshot, but do mark the slice initialized so the tab's
          // "loading" spinner clears.
          dispatch({ type: "MARK_INITIALIZED", sessionId });
          setInitialized(true);
          return;
        }
        dispatch({
          type: "MERGE_SNAPSHOT",
          sessionId,
          messages: detail.messages,
          total: detail.total,
        });
        if (detail.title && detail.title !== sessionId) {
          projectDispatch({ type: "UPDATE_TAB_TITLE", id: sessionId, title: detail.title });
        }
        setInitialized(true);
        // Scroll to bottom after initial render
        requestAnimationFrame(() => {
          const el = scrollRef.current;
          if (el) {
            el.scrollTop = el.scrollHeight;
            lastScrollTopRef.current = el.scrollTop;
            atBottomRef.current = true;
            setShowJumpToBottom(false);
            // Pinned to the bottom of a long transcript: the top affordance is
            // available even though no user scroll has fired yet.
            setShowJumpToTop(el.scrollHeight - el.clientHeight > 200);
          }
        });
      })
      .catch((err) => {
        if (cancelled || generation !== loadGenerationRef.current) return;
        console.error("Failed to load session:", err);
        // Never fall through to the empty-conversation render on a failed
        // fetch: that looks like the transcript was wiped when the request
        // merely failed (a remote proxy error, a restart-time reconnect).
        setLoadError(err instanceof Error ? err.message : "the transcript could not be fetched");
        setInitialized(true);
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, host, dispatch, projectDispatch, loadRetry]);

  // A new session (or any session-id change) starts with a clean tail lock:
  // there is no prior reader scroll intent to preserve. ChatPanel is keyed by
  // tab id in App.tsx, so this usually coincides with a remount; making it
  // explicit keeps the invariant testable and guards a future non-keyed reuse.
  useEffect(() => {
    atBottomRef.current = true;
    lastScrollTopRef.current = 0;
    prevMessagesRef.current = null;
    setShowJumpToBottom(false);
    setShowJumpToTop(false);
  }, [sessionId]);

  // Auto-scroll to bottom on new committed messages, but ONLY when the user
  // is already pinned to the bottom. We scroll instantly (not smooth) so a
  // burst of streaming tokens can't start a competing smooth animation — that
  // competition is what caused the down/up bounce and eventual lockout. The
  // explicit "jump to bottom" button uses smooth scrolling instead.
  //
  // The lock RESETS (auto-scroll re-arms) when carrying it would be meaningless:
  //   - the transcript was replaced by a shorter one (truncate / clear / reset),
  //     so the reader's old position no longer refers to this content;
  //   - the content no longer overflows the viewport (no scrollbar), so there is
  //     nothing to be scrolled away from and later growth should track the tail.
  useEffect(() => {
    if (!initialized) return;
    const el = scrollRef.current;
    if (!el) return;
    // Appends and prepends only ever grow the committed list; a shrink means
    // the transcript was reset/replaced under the reader.
    const prev = prevMessagesRef.current;
    prevMessagesRef.current = { count: messages.length };
    if (prev && messages.length < prev.count) {
      atBottomRef.current = true;
    }
    // Only meaningful while the panel has a layout box: a hidden (display:none)
    // tab reports scrollHeight/clientHeight 0 and must NOT be mistaken for
    // "no scrollbar" — that would silently re-arm a reader's lock every time a
    // stream event lands while the tab is in the background.
    const hasScrollbar = el.scrollHeight - el.clientHeight > 1;
    if (el.clientHeight > 0 && !hasScrollbar) {
      // No scrollbar: there is no position to be locked away from, so re-arm
      // the follow and clear both affordances. Fall through to the pin below so
      // a programmatic pin still happens (a no-op in a real browser where the
      // content fits, but it keeps the pinned invariant true for tests/layout).
      atBottomRef.current = true;
      setShowJumpToBottom(false);
      setShowJumpToTop(false);
    }
    if (atBottomRef.current) {
      el.scrollTop = el.scrollHeight;
      lastScrollTopRef.current = el.scrollTop;
    }
  }, [messages, live, initialized]);

  // Re-pin when this panel transitions from hidden back to visible. Inactive
  // tabs (and the Chat→Files/Git view switch, and terminal focus in App.tsx)
  // are CSS-hidden with display:none rather than unmounted, so the scroll
  // container loses its layout box: while hidden scrollHeight reads 0 (the
  // auto-scroll effect above can't follow the tail, and the browser discards
  // the scroll offset), and on re-show scrollTop is 0. If no new store event
  // arrives after returning — e.g. the stream finished while the tab was in
  // the background — the [messages, live] effect never re-runs and a user who
  // was at the bottom lands at the top instead. A ResizeObserver on the scroll
  // element catches the 0 → >0 height transition regardless of which wrapper
  // hid the panel; the follow-up rAF re-pin rides out the virtualizer
  // re-windowing and measurement corrections after the jump.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    let prevHeight = -1; // unknown until the first observation callback
    let raf = 0;
    const ro = new ResizeObserver((entries) => {
      const cr = entries[0]?.contentRect;
      const height = cr ? cr.height : el.clientHeight;
      // prevHeight === -1 covers a panel whose very first observation already
      // arrives while visible (e.g. the effect re-created when `initialized`
      // flipped); pinning then matches the initial-load-to-bottom behavior.
      if (height > 0 && initialized && el.scrollHeight - el.clientHeight <= 1) {
        // No scrollbar (e.g. the window grew to fit the transcript): the lock
        // cannot apply, so re-arm the follow for when it overflows again.
        atBottomRef.current = true;
        lastScrollTopRef.current = el.scrollTop;
        setShowJumpToBottom(false);
        setShowJumpToTop(false);
      } else if (height > 0 && (prevHeight === 0 || prevHeight === -1) && atBottomRef.current && initialized) {
        el.scrollTop = el.scrollHeight;
        lastScrollTopRef.current = el.scrollTop;
        setShowJumpToBottom(false);
        setShowJumpToTop(el.scrollHeight - el.clientHeight > 200);
        cancelAnimationFrame(raf);
        raf = requestAnimationFrame(() => {
          if (atBottomRef.current) {
            el.scrollTop = el.scrollHeight;
            lastScrollTopRef.current = el.scrollTop;
          }
        });
      }
      prevHeight = height;
    });
    ro.observe(el);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, [initialized]);

  // Follow the virtualizer's estimate→measure corrections at the end of a turn.
  // The [messages, live] effect above is one-shot per store update, but the
  // turn-end `messages` broadcast swaps the streamed tail from the live block
  // (already laid out at its real height, and pinned) into the virtualized list,
  // where each freshly committed entry first carries only `estimateSize` (96px).
  // The virtualizer then corrects every one of those items to its measured
  // height over the following frames, growing the list container AFTER the pin
  // ran — and the scroll-ELEMENT observer above deliberately ignores non-zero→
  // non-zero box changes, so nothing follows the growth and the viewport is left
  // above the bottom (the "chat scrolls back up when the loop finishes" report).
  // Observe the virtualized CONTENT instead: while the panel is pinned, re-pin
  // after every size correction. The user-scrolled-up case is guarded by
  // `atBottomRef`, exactly like the [messages, live] effect — a reader who broke
  // the pin is never yanked back.
  const hasList = renderEntries.length > 0;
  useEffect(() => {
    const el = scrollRef.current;
    const list = listContainerRef.current;
    if (!el || !list) return;
    let raf = 0;
    const pin = () => {
      if (!atBottomRef.current) return;
      // The write lands after layout but before paint, so the correction never
      // shows as a visible frame at the old (short) offset. The follow-up frame
      // rides out the virtualizer's re-windowing, mirroring the hidden→visible
      // re-pin above.
      el.scrollTop = el.scrollHeight;
      lastScrollTopRef.current = el.scrollTop;
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        if (atBottomRef.current) {
          el.scrollTop = el.scrollHeight;
          lastScrollTopRef.current = el.scrollTop;
        }
      });
    };
    const ro = new ResizeObserver(pin);
    ro.observe(list);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, [hasList]);

  // Toggle the find bar with Ctrl/Cmd+F. Local to this tab: each ChatPanel
  // instance is only visible while its tab is active (App.tsx CSS-hides the
  // rest), so this window listener would fire for every open tab — guard on
  // visibility via the DOM (offsetParent is null while `hidden`).
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "f" && (e.metaKey || e.ctrlKey)) {
        if (scrollRef.current?.offsetParent === null) return;
        e.preventDefault();
        setSearchOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // `/search [query]` / `/find [query]` from the composer dispatches this event
  // so the command route (App → commands.ts) doesn't need a direct handle on
  // this tab's find-bar state. Same visibility guard as Ctrl/Cmd+F: only the
  // visible tab reacts (hidden tabs have offsetParent === null).
  useEffect(() => {
    const onOpen = (e: Event) => {
      if (scrollRef.current?.offsetParent === null) return;
      const query = (e as CustomEvent<{ query?: string }>).detail?.query ?? "";
      setSearchQuery(query);
      setMatchCursor(-1);
      setSearchOpen(true);
    };
    window.addEventListener("ocode:open-chat-search", onOpen);
    return () => window.removeEventListener("ocode:open-chat-search", onOpen);
  }, []);

  const closeSearch = useCallback(() => {
    setSearchOpen(false);
    setSearchQuery("");
    setMatchCursor(-1);
  }, []);

  // Reset the cursor to the first match whenever the match set changes (new
  // query, or the loaded message set shifted). -1 when there is nothing to jump
  // to so the counter reads "No matches" instead of "1/0".
  useEffect(() => {
    setMatchCursor(jumpTargets.length > 0 ? 0 : -1);
  }, [jumpTargets]);

  // Scroll the current match into view. Flag the jump so handleScroll skips the
  // pagination loader while the smooth scroll settles. Unlike a plain DOM
  // scrollIntoView, this works even when the match isn't currently rendered
  // (virtualizer.scrollToIndex handles jumping to an unmeasured item and
  // correcting position once it's measured).
  useEffect(() => {
    if (currentMatchEntryPos < 0) return;
    atBottomRef.current = false;
    setShowJumpToBottom(true);
    searchJumpRef.current = true;
    virtualizer.scrollToIndex(currentMatchEntryPos, { align: "center", behavior: "smooth" });
    const t = setTimeout(() => {
      searchJumpRef.current = false;
    }, 600);
    return () => clearTimeout(t);
  }, [currentMatchEntryPos, virtualizer]);

  // When the selected match lives OUTSIDE the loaded window its entryPos is -1
  // (see buildJumpTargets), so the scroll effect above has nothing to do. Load
  // the contiguous prefix from the match up to the window start; PREPEND_MESSAGES
  // then places it at local 0, the target recomputes with a resolved entryPos,
  // and the scroll effect runs. Keyed by the server index via a ref so
  // re-selecting the same off-window match does not refetch.
  const pendingJumpRef = useRef<number | null>(null);
  // Server indices we have already tried to fetch for this query. A match that
  // is excluded from renderEntries (a sentinel QUESTION_PROMPT/PERMISSION_ASK
  // tool message) can never resolve to an entry position, so without this the
  // effect would refetch it on every re-render forever.
  const attemptedJumpsRef = useRef<Set<number>>(new Set());
  useEffect(() => {
    attemptedJumpsRef.current = new Set();
  }, [searchQuery, sessionId]);
  useEffect(() => {
    if (matchCursor < 0 || matchCursor >= jumpTargets.length) return;
    const target = jumpTargets[matchCursor];
    if (target.entryPos >= 0) return; // already loaded — scroll effect handles it
    // Bail on ANY in-flight prefix fetch, not just one for this target: a
    // second overlapping fetch would race the first and both would prepend,
    // duplicating ranges and (without the store clamp) driving the anchor
    // negative. The user can press "next" again once this one settles.
    if (pendingJumpRef.current !== null) return;
    if (attemptedJumpsRef.current.has(target.serverIndex)) return; // already tried
    const fetch = olderPrefixFetch(target.serverIndex, windowStartServerIndex, totalMessages);
    if (!fetch) return;
    pendingJumpRef.current = target.serverIndex;
    attemptedJumpsRef.current.add(target.serverIndex);
    dispatch({ type: "SET_LOADING_MORE", sessionId, loading: true });
    api
      .getSession(sessionId, fetch, host)
      .then((detail) => {
        if (detail.messages.length === 0) return;
        dispatch({
          type: "PREPEND_MESSAGES",
          sessionId,
          messages: detail.messages,
          total: detail.total,
        });
      })
      .catch((err) => {
        // A failed jump must not break the find bar; the counter still shows
        // the hit, it just cannot scroll to it. Clear the attempted marker so
        // a transient network error does not permanently dead-end the target.
        console.warn("search jump fetch failed", err);
        attemptedJumpsRef.current.delete(target.serverIndex);
      })
      .finally(() => {
        pendingJumpRef.current = null;
        dispatch({ type: "SET_LOADING_MORE", sessionId, loading: false });
      });
  }, [matchCursor, jumpTargets, messages, windowStartServerIndex, totalMessages, sessionId, host, dispatch]);

  const gotoNextMatch = useCallback(() => {
    setMatchCursor((c) =>
      jumpTargets.length === 0 ? -1 : (c + 1) % jumpTargets.length,
    );
  }, [jumpTargets.length]);

  const gotoPrevMatch = useCallback(() => {
    setMatchCursor((c) =>
      jumpTargets.length === 0
        ? -1
        : (c - 1 + jumpTargets.length) % jumpTargets.length,
    );
  }, [jumpTargets.length]);

  // Pin to bottom immediately (used by the "jump to bottom" affordance).
  const scrollToBottom = useCallback((smooth = false) => {
    const el = scrollRef.current;
    if (!el) return;
    scrollElementTo(el, el.scrollHeight, smooth ? "smooth" : "auto");
    requestAnimationFrame(() => {
      atBottomRef.current = true;
      lastScrollTopRef.current = el.scrollTop;
      setShowJumpToBottom(false);
    });
  }, []);

  // Jump back to the start of the transcript (the "scroll to top" affordance).
  // The scroll listener takes over from here: handleScroll flips
  // showJumpToTop off (we are at the top) and showJumpToBottom on, and drops
  // the at-bottom pin so streaming no longer yanks the view back down.
  const scrollToTop = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    scrollElementTo(el, 0, "smooth");
    atBottomRef.current = false;
    setShowJumpToTop(false);
    setShowJumpToBottom(true);
  }, []);

  // Scroll-up handler: load older messages when near top, and track whether we
  // are pinned to the bottom so the auto-scroll effect can decide to follow.
  // Uses requestAnimationFrame to defer the scroll position check, giving the
  // auto-scroll useEffect a chance to scroll first. This prevents a race where
  // content growth fires a scroll event before the effect runs, which would
  // incorrectly disable auto-scroll during lengthy tool call results.
  const rafRef = useRef<number>(0);
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;

    // Synchronous upward-move check. A scroll event whose offset DECREASED can
    // only come from the user (or an explicit jump up) — our own pins always
    // increase it. Unpinning here, in the same tick as the event, is what stops
    // a streamed token that lands before the deferred recompute below from
    // re-pinning and swallowing the scroll-up (the "can't stay scrolled up
    // while the LLM streams" report). The deferred pass still owns the
    // near-bottom re-pin and the content-growth race it was written for.
    const top = el.scrollTop;
    const prevTop = lastScrollTopRef.current;
    lastScrollTopRef.current = top;
    if (top < prevTop - 1) {
      atBottomRef.current = false;
    }

    cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(() => {
      // A pin may have moved the offset since the event; record it so the next
      // event doesn't misread our own downward write as an upward move.
      lastScrollTopRef.current = el.scrollTop;
      // With no scrollbar the distance below is always negative, so the general
      // path already re-arms the follow (and clears both affordances).
      const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      const atBottom = distanceFromBottom < 200;
      atBottomRef.current = atBottom;
      setShowJumpToBottom(!atBottom);
      setShowJumpToTop(el.scrollTop > 200);
    });

    setReachedTop(el.scrollTop < 5);

    if (!hasMore || loadingMore || sessionId.startsWith("new-") || searchJumpRef.current) return;
    if (el.scrollTop < 100) {
      // Skip the SERVER rows already loaded (total - windowStart), not the
      // local array length: client-only ADD_MESSAGE injections inflate the
      // latter and would skip past the window start, leaving a gap. Fall back
      // to the array length only when the anchor is unknown.
      const loadedServerCount = windowStartServerIndex >= 0
        ? Math.max(0, totalMessages - windowStartServerIndex)
        : messages.length;
      dispatch({ type: "SET_LOADING_MORE", sessionId, loading: true });

      api
        .getSession(sessionId, { limit: PAGE_SIZE, offset: loadedServerCount }, host)
        .then((detail) => {
          if (detail.messages.length > 0) {
            const scrollHeightBefore = el.scrollHeight;
            dispatch({
              type: "PREPEND_MESSAGES",
              sessionId,
              messages: detail.messages,
              total: detail.total,
            });
            requestAnimationFrame(() => {
              const scrollHeightAfter = el.scrollHeight;
              el.scrollTop = scrollHeightAfter - scrollHeightBefore;
            });
          } else {
            dispatch({ type: "SET_LOADING_MORE", sessionId, loading: false });
          }
        })
        .catch(() => {
          dispatch({ type: "SET_LOADING_MORE", sessionId, loading: false });
        });
    }
  }, [hasMore, loadingMore, messages.length, windowStartServerIndex, totalMessages, sessionId, host, dispatch]);

  // Role "tool" messages carry only tool_call_id, not the tool's name — resolve
  // it here from the assistant message that issued the call, so replayed
  // history can syntax-highlight tool output the same as the live stream does.
  const toolNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const msg of messages) {
      for (const tc of msg.tool_calls ?? []) {
        map.set(tc.id, tc.function.name);
      }
    }
    return map;
  }, [messages]);

  return (
    <div className="relative h-full min-h-0 flex flex-col">
      {searchOpen && (
        <div className="absolute inset-x-0 top-0 z-20">
          <ChatSearchBar
            query={searchQuery}
            onQueryChange={setSearchQuery}
            matchCount={jumpTargets.length}
            current={matchCursor}
            onNext={gotoNextMatch}
            onPrev={gotoPrevMatch}
            onClose={closeSearch}
            note={searchNote}
          />
        </div>
      )}
      <div className="relative flex shrink-0 items-center justify-end gap-2 border-b border-border px-3 py-1">
        {messages.length > 0 && (
          <>
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-40"
              disabled={!selectedText}
              title="Speak selected chat text"
              onClick={() => requestSpeech(selectedText)}
            >
              <Volume2 className="h-3.5 w-3.5" /> Speak selection
            </button>
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
              title="Speak visible chat text"
              onClick={() => requestSpeech(renderedSpeechTexts(scrollRef.current))}
            >
              <Volume2 className="h-3.5 w-3.5" /> Speak visible
            </button>
          </>
        )}
        {/* Anchored to the header's bottom edge (top-full) so the "scroll to
            top" affordance floats at the TOP-right of the transcript rather
            than stacked above the scroll-to-bottom button at the bottom. */}
        {showJumpToTop && (
          <button
            type="button"
            onClick={scrollToTop}
            className="absolute right-4 top-full z-10 mt-2 flex h-9 w-9 items-center justify-center rounded-full bg-accent text-accent-foreground shadow-lg transition-colors hover:bg-accent"
            title="Scroll to top"
            aria-label="Scroll to top"
          >
            <ArrowUp className="h-4 w-4" />
          </button>
        )}
      </div>
      <div
        ref={scrollRef}
        className="relative flex-1 min-h-0 overflow-y-auto p-4"
        onScroll={handleScroll}
        onMouseUp={() => setSelectedText(window.getSelection()?.toString().trim() ?? "")}
      >
        {initialized && messages.length > 0 && (
          <div ref={topRef} className="py-4">
            {loadingMore && (
              <div className="text-center text-muted-foreground text-sm py-2">
                Loading older messages…
              </div>
            )}
            {!loadingMore && !hasMore && reachedTop && (
              <div className="text-center text-foreground text-xs py-2 border-b border-border mb-4">
                Beginning of conversation
              </div>
            )}
            {!loadingMore && hasMore && !reachedTop && (
              <div className="text-center text-foreground text-xs py-2">
                ↑ Scroll up for older messages
              </div>
            )}
            {!loadingMore && hasMore && reachedTop && (
              <div className="text-center text-muted-foreground text-sm py-2">
                Loading older messages…
              </div>
            )}
          </div>
        )}

        {messages.length === 0 && live.length === 0 && initialized && (
          loadError ? (
            <div
              className="flex h-full flex-col items-center justify-center gap-2 px-4 text-center"
              role="alert"
            >
              <div className="text-sm text-foreground">Couldn't load this conversation</div>
              <div className="max-w-md break-words text-xs text-muted-foreground">{loadError}</div>
              <button
                type="button"
                className="mt-1 rounded-md border border-border bg-card px-3 py-1 text-xs text-foreground transition-colors hover:bg-accent"
                onClick={() => {
                  forceReloadRef.current = true;
                  setLoadRetry((n) => n + 1);
                }}
              >
                Retry
              </button>
            </div>
          ) : (
            <div className="flex h-full items-center justify-center text-muted-foreground">
              Start a conversation
            </div>
          )
        )}

        {renderEntries.length > 0 && (
          <div
            ref={listContainerRef}
            style={{ height: virtualizer.getTotalSize(), width: "100%", position: "relative" }}
          >
            {virtualizer.getVirtualItems().map((virtualItem) => {
              const entry = renderEntries[virtualItem.index];
              const isCurrentMatch = virtualItem.index === currentMatchEntryPos;
              const highlight = searchOpen ? searchQuery : "";
              return (
                <div
                  key={virtualItem.key}
                  data-index={virtualItem.index}
                  ref={virtualizer.measureElement}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    // `start` includes scrollMargin; subtract it so the item is
                    // positioned relative to the list container (which already
                    // sits at that offset inside the scroll surface).
                    transform: `translateY(${virtualItem.start - listMargin}px)`,
                  }}
                >
                  <div
                    className={
                      isCurrentMatch
                        ? "scroll-mt-16 rounded-lg ring-2 ring-yellow-400/70 ring-offset-2 ring-offset-background"
                        : "scroll-mt-16"
                    }
                  >
                    {entry.kind === "single" ? (
                      <MessageBubble
                        message={entry.msg}
                        highlight={highlight}
                        toolName={
                          entry.msg.tool_call_id ? toolNameById.get(entry.msg.tool_call_id) : undefined
                        }
                        sessionId={sessionId}
                        messageIndex={entry.originalIndex}
                      />
                    ) : (
                      <>
                        {entry.assistant.reasoning_content ? (
                          <ThinkingBlock text={entry.assistant.reasoning_content} highlight={highlight} onSpeak={() => requestSpeech(entry.assistant.reasoning_content || "")} />
                        ) : null}
                        {entry.calls.map(({ tc, resultContent, pendingQuestion }) => (
                          <ToolBlock
                            key={tc.id}
                            tool={tc.function.name}
                            command={tc.function.arguments}
                            output={resultContent}
                            highlight={highlight}
                            onOpenQuestion={
                              pendingQuestion
                                ? () => dispatch({ type: "QUESTION_REQUEST", sessionId, question: pendingQuestion })
                                : undefined
                            }
                          />
                        ))}
                        {hasRenderableText(entry.assistant.content) ? (
                          <AssistantText content={entry.assistant.content} onSpeak={requestSpeech} />
                        ) : null}
                      </>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {loadingMore && messages.length > 0 && (
          <div className="text-center text-muted-foreground text-sm py-2">
            Loading…
          </div>
        )}

        {live.length > 0 && (
          <div>
            {live.map((part, i) => {
              if (part.kind === "thinking")
                return <ThinkingBlock key={`live-${i}`} text={part.text} onSpeak={() => requestSpeech(part.text || "")} />;
              if (part.kind === "text")
                return hasRenderableText(part.text) ? (
                  <AssistantText key={`live-${i}`} content={part.text} onSpeak={requestSpeech} />
                ) : null;
              if (part.kind === "status")
                return <StatusBlock key={`live-${i}`} text={part.text} />;
              if (part.kind === "notice")
                return <NoticeBlock key={`live-${i}`} text={part.text} />;
              return (
                <ToolBlock
                  key={`live-${i}`}
                  tool={part.tool}
                  command={part.command}
                  stream={part.stream}
                  output={part.output}
                />
              );
            })}
          </div>
        )}

        {/* The session-wide "working" indicator lives in the bottom StatusBar
            (components/common/StatusBar.tsx), driven by the same
            isStreaming || turnActive signal — kept here only as a comment so
            nobody re-adds a duplicate label in the transcript. */}

        {showInterrupted && (
          <div
            role="status"
            className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-md border border-border bg-card px-3 py-2 text-xs text-muted-foreground"
          >
            <span>The previous reply was interrupted</span>
            <button
              type="button"
              onClick={handleContinueInterrupted}
              className="inline-flex items-center gap-1 rounded-full border border-border px-2.5 py-0.5 text-xs text-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              Continue
            </button>
          </div>
        )}

        <div ref={bottomRef} />
      </div>

      {/* Mirror of the TUI's "◆ Model prompt" bottom chrome row: shows the
          active model's custom prompt source + force-injected Kaizen
          directives. Sits between the transcript and ChatInput like the TUI's
          chrome; renders nothing for untuned models. */}
      <ModelPromptRow prompt={slice.tuiStatus?.model_prompt} model={slice.tuiStatus?.main_model} />

      {showJumpToBottom && (
        <div className="absolute bottom-4 right-4 z-10">
          <button
            type="button"
            onClick={() => scrollToBottom(true)}
            className="flex h-9 w-9 items-center justify-center rounded-full bg-accent text-accent-foreground shadow-lg transition-colors hover:bg-accent"
            title="Scroll to bottom"
            aria-label="Scroll to bottom"
          >
            <ArrowDown className="h-4 w-4" />
          </button>
        </div>
      )}
    </div>
  );
}

/** `sessionId` is the only prop and never changes across an instance's
 *  lifetime (App mounts one ChatPanel per open tab), so memo is fully
 *  effective: a parent re-render — e.g. another tab becoming active — is a
 *  no-op here. Background tabs keep streaming via the chat store, untouched. */
export default memo(ChatPanel);
