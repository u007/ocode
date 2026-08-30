import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useChatSelector, useChatDispatch, getSessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { api } from "../../api/client";
import MessageBubble, { AssistantText } from "./MessageBubble";
import { StatusBlock, ThinkingBlock, ToolBlock } from "./TurnParts";
import ChatSearchBar, { messageMatchesQuery } from "./ChatSearchBar";
import { RESTORE_EVENT } from "../../lib/inputRestore";

const PAGE_SIZE = 50;

interface ChatPanelProps {
  /** The tab this instance renders — a real session id or a temporary
   *  `new-<ts>` tab id. One ChatPanel is mounted per open tab (App.tsx),
   *  so this never changes across this instance's lifetime. */
  sessionId: string;
}

export default function ChatPanel({ sessionId }: ChatPanelProps) {
  // Scoped to this tab's own session: getSessionSlice returns the exact same
  // object reference across dispatches that don't touch this session (see
  // updateSession's immutable per-key update), so other tabs' streamed
  // tokens don't re-render this ChatPanel instance.
  const slice = useChatSelector((s) => getSessionSlice(s, sessionId));
  const dispatch = useChatDispatch();
  const { dispatch: projectDispatch } = useProjectState();
  const { messages, live, hasMore, loadingMore } = slice;
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const topRef = useRef<HTMLDivElement>(null);
  const [initialized, setInitialized] = useState(false);
  const loadGenerationRef = useRef(0);
  const stateRef = useRef(slice);
  stateRef.current = slice;
  const [reachedTop, setReachedTop] = useState(false);
  // Whether the viewport is pinned to the bottom. Driven by handleScroll and
  // consulted by the auto-scroll effect so we only follow the tail when the
  // user is already at the bottom (and resume reliably after they return).
  const atBottomRef = useRef(true);
  const [showJumpToBottom, setShowJumpToBottom] = useState(false);

  // Display-only feedback for /compact (and any auto-compaction completion).
  // This banner is ephemeral UI only — it does not mutate chatStore messages,
  // so it survives the SSE "messages" SET_MESSAGES that replaces the transcript
  // immediately after compaction and would otherwise wipe a synthetic ADD_MESSAGE.
  const [compactNotice, setCompactNotice] = useState<string | null>(null);
  useEffect(() => {
    const handler = (e: Event) => {
      const ce = e as CustomEvent<{ sessionId: string; originalLen: number; compactedLen: number }>;
      if (!ce.detail || ce.detail.sessionId !== sessionId) return;
      setCompactNotice(`Compacted: ${ce.detail.originalLen} → ${ce.detail.compactedLen} messages`);
    };
    window.addEventListener("ocode:compact", handler as EventListener);
    return () => window.removeEventListener("ocode:compact", handler as EventListener);
  }, [sessionId]);
  useEffect(() => {
    if (!compactNotice) return;
    const t = setTimeout(() => setCompactNotice(null), 8000);
    return () => clearTimeout(t);
  }, [compactNotice]);

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

  // In-chat find bar (Ctrl/Cmd+F). Client-side, searches only loaded messages.
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [matchCursor, setMatchCursor] = useState(-1);
  // Set true while a search jump is scrolling so handleScroll doesn't fire the
  // scroll-up pagination loader (which would shift every message index and
  // land the highlight on the wrong bubble).
  const searchJumpRef = useRef(false);

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
    messages.forEach((m, idx) => {
      if (m.role === "tool" && m.tool_call_id && !isSentinelToolContent(m.content)) {
        resultById.set(m.tool_call_id, { content: m.content, idx });
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
          return { tc, resultContent: undefined, resultIdx: undefined };
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

  const currentMatchEntryPos =
    matchCursor >= 0 && matchCursor < matchEntryPositions.length
      ? matchEntryPositions[matchCursor]
      : -1;

  // Initial load: fetch this session's last 50 messages once (skipped for a
  // `new-*` tab, which has no session yet, and for a session whose slice is
  // already initialized — e.g. this ChatPanel remounted, or a live SSE event
  // populated the slice before this fetch resolved).
  useEffect(() => {
    const generation = ++loadGenerationRef.current;
    let cancelled = false;

    if (!sessionId || sessionId.startsWith("new-")) {
      setInitialized(true);
      return () => {
        cancelled = true;
      };
    }
    if (stateRef.current.initialized) {
      setInitialized(true);
      return () => {
        cancelled = true;
      };
    }
    setInitialized(false);

    api
      .getSession(sessionId, { limit: PAGE_SIZE * 2 })
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
            atBottomRef.current = true;
            setShowJumpToBottom(false);
          }
        });
      })
      .catch((err) => {
        if (cancelled || generation !== loadGenerationRef.current) return;
        console.error("Failed to load session:", err);
        setInitialized(true);
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, dispatch, projectDispatch]);

  // Auto-scroll to bottom on new committed messages, but ONLY when the user
  // is already pinned to the bottom. We scroll instantly (not smooth) so a
  // burst of streaming tokens can't start a competing smooth animation — that
  // competition is what caused the down/up bounce and eventual lockout. The
  // explicit "jump to bottom" button uses smooth scrolling instead.
  useEffect(() => {
    if (!initialized) return;
    const el = scrollRef.current;
    if (!el) return;
    if (atBottomRef.current) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages, live, initialized]);

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

  const closeSearch = useCallback(() => {
    setSearchOpen(false);
    setSearchQuery("");
    setMatchCursor(-1);
  }, []);

  // Reset the cursor to the first match whenever the match set changes (new
  // query, or the loaded message set shifted). -1 when there is nothing to jump
  // to so the counter reads "No matches" instead of "1/0".
  useEffect(() => {
    setMatchCursor(matchEntryPositions.length > 0 ? 0 : -1);
  }, [matchEntryPositions]);

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

  const gotoNextMatch = useCallback(() => {
    setMatchCursor((c) =>
      matchEntryPositions.length === 0 ? -1 : (c + 1) % matchEntryPositions.length,
    );
  }, [matchEntryPositions.length]);

  const gotoPrevMatch = useCallback(() => {
    setMatchCursor((c) =>
      matchEntryPositions.length === 0
        ? -1
        : (c - 1 + matchEntryPositions.length) % matchEntryPositions.length,
    );
  }, [matchEntryPositions.length]);

  // Pin to bottom immediately (used by the "jump to bottom" affordance).
  const scrollToBottom = useCallback((smooth = false) => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth ? "smooth" : "auto" });
    requestAnimationFrame(() => {
      atBottomRef.current = true;
      setShowJumpToBottom(false);
    });
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

    cancelAnimationFrame(rafRef.current);
    rafRef.current = requestAnimationFrame(() => {
      const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      const atBottom = distanceFromBottom < 200;
      atBottomRef.current = atBottom;
      setShowJumpToBottom(!atBottom);
    });

    setReachedTop(el.scrollTop < 5);

    if (!hasMore || loadingMore || sessionId.startsWith("new-") || searchJumpRef.current) return;
    if (el.scrollTop < 100) {
      const currentCount = messages.length;
      dispatch({ type: "SET_LOADING_MORE", sessionId, loading: true });

      api
        .getSession(sessionId, { limit: PAGE_SIZE, offset: currentCount })
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
  }, [hasMore, loadingMore, messages.length, sessionId, dispatch]);

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
            matchCount={matchEntryPositions.length}
            current={matchCursor}
            onNext={gotoNextMatch}
            onPrev={gotoPrevMatch}
            onClose={closeSearch}
          />
        </div>
      )}
      <div
        ref={scrollRef}
        className="relative flex-1 min-h-0 overflow-y-auto p-4"
        onScroll={handleScroll}
      >
        {compactNotice && (
          <div className="mb-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100 flex items-center justify-between gap-2">
            <span>{compactNotice}</span>
            <button type="button" aria-label="Dismiss" onClick={() => setCompactNotice(null)} className="text-amber-700 dark:text-amber-200 hover:opacity-70 text-xs px-1">✕</button>
          </div>
        )}
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
          <div className="flex h-full items-center justify-center text-muted-foreground">
            Start a conversation
          </div>
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
                          <ThinkingBlock text={entry.assistant.reasoning_content} highlight={highlight} />
                        ) : null}
                        {entry.calls.map(({ tc, resultContent }) => (
                          <ToolBlock
                            key={tc.id}
                            tool={tc.function.name}
                            command={tc.function.arguments}
                            output={resultContent}
                            highlight={highlight}
                          />
                        ))}
                        {entry.assistant.content ? (
                          <AssistantText content={entry.assistant.content} />
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
                return <ThinkingBlock key={`live-${i}`} text={part.text} />;
              if (part.kind === "text")
                return <AssistantText key={`live-${i}`} content={part.text} />;
              if (part.kind === "status")
                return <StatusBlock key={`live-${i}`} text={part.text} />;
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

        <div ref={bottomRef} />
      </div>

      {showJumpToBottom && (
        <button
          type="button"
          onClick={() => scrollToBottom(true)}
          className="absolute bottom-4 right-4 z-10 flex h-9 w-9 items-center justify-center rounded-full bg-accent text-accent-foreground shadow-lg transition-colors hover:bg-accent"
          title="Scroll to bottom"
          aria-label="Scroll to bottom"
        >
          ↓
        </button>
      )}
    </div>
  );
}
