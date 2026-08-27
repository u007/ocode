import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useChatSelector, useChatDispatch, getSessionSlice } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { api } from "../../api/client";
import MessageBubble, { AssistantText } from "./MessageBubble";
import { StatusBlock, ThinkingBlock, ToolBlock } from "./TurnParts";
import ChatSearchBar, { messageMatchesQuery } from "./ChatSearchBar";

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

  // In-chat find bar (Ctrl/Cmd+F). Client-side, searches only loaded messages.
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [matchCursor, setMatchCursor] = useState(-1);
  // Set true while a search jump is scrolling so handleScroll doesn't fire the
  // scroll-up pagination loader (which would shift every message index and
  // land the highlight on the wrong bubble).
  const searchJumpRef = useRef(false);

  // A few structural message kinds (question/permission prompts already
  // rendered by dedicated UI elsewhere) are never shown in the transcript.
  // Filtered out here, before virtualization, so they never claim a virtual
  // slot — filtering post-render (returning null per item) would leave a
  // blank gap sized by estimateSize instead. `i` is the index into the raw
  // `messages` array — matchIndices/currentMatchMsgIndex/toolNameById all key
  // on that original index, since search and tool-name resolution reasonably
  // still consider the full history.
  const visibleMessages = useMemo(
    () =>
      messages
        .map((msg, i) => ({ msg, i }))
        .filter(
          ({ msg }) =>
            !(
              msg.role === "tool" &&
              (msg.content?.startsWith("QUESTION_PROMPT:") || msg.content?.startsWith("PERMISSION_ASK:"))
            ),
        ),
    [messages],
  );
  // original message index -> position within visibleMessages, for scrolling
  // the virtualizer to a search match (which is indexed by original index).
  const visiblePositionByIndex = useMemo(() => {
    const map = new Map<number, number>();
    visibleMessages.forEach(({ i }, pos) => map.set(i, pos));
    return map;
  }, [visibleMessages]);

  // Only committed messages are virtualized — a long session's history is
  // what was growing the DOM (and retained JS heap: fiber nodes, markdown/
  // syntax-highlighter output) unboundedly, since nothing ever unmounted as
  // the user scrolled past it. `live` (the in-progress turn) stays rendered
  // as a normal tail below the virtualized window: it's always visible,
  // short-lived, and its own size churns too fast for virtualization to help.
  // estimateSize is deliberately rough (real heights vary a lot — code
  // blocks vs. one-line replies); measureElement (wired via the ref callback
  // below) corrects it per item after first paint.
  const virtualizer = useVirtualizer({
    count: visibleMessages.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 96,
    overscan: 8,
  });

  // Match indices: message positions containing the query (case-insensitive).
  // Only loaded messages are searched — the "searching loaded messages" hint
  // in the bar sets that expectation.
  const matchIndices = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    if (!q) return [] as number[];
    const out: number[] = [];
    messages.forEach((msg, i) => {
      if (messageMatchesQuery(msg, q)) out.push(i);
    });
    return out;
  }, [messages, searchQuery]);

  const currentMatchMsgIndex =
    matchCursor >= 0 && matchCursor < matchIndices.length
      ? matchIndices[matchCursor]
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
      .getSession(sessionId, { limit: PAGE_SIZE })
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

  // Auto-scroll to bottom on new messages/live content, but ONLY when the user
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
    setMatchCursor(matchIndices.length > 0 ? 0 : -1);
  }, [matchIndices]);

  // Scroll the current match into view. Flag the jump so handleScroll skips the
  // pagination loader while the smooth scroll settles. Unlike a plain DOM
  // scrollIntoView, this works even when the match isn't currently rendered
  // (virtualizer.scrollToIndex handles jumping to an unmeasured item and
  // correcting position once it's measured).
  useEffect(() => {
    if (currentMatchMsgIndex < 0) return;
    const pos = visiblePositionByIndex.get(currentMatchMsgIndex);
    if (pos === undefined) return;
    atBottomRef.current = false;
    setShowJumpToBottom(true);
    searchJumpRef.current = true;
    virtualizer.scrollToIndex(pos, { align: "center", behavior: "smooth" });
    const t = setTimeout(() => {
      searchJumpRef.current = false;
    }, 600);
    return () => clearTimeout(t);
  }, [currentMatchMsgIndex, visiblePositionByIndex, virtualizer]);

  const gotoNextMatch = useCallback(() => {
    setMatchCursor((c) =>
      matchIndices.length === 0 ? -1 : (c + 1) % matchIndices.length,
    );
  }, [matchIndices.length]);

  const gotoPrevMatch = useCallback(() => {
    setMatchCursor((c) =>
      matchIndices.length === 0
        ? -1
        : (c - 1 + matchIndices.length) % matchIndices.length,
    );
  }, [matchIndices.length]);

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
    <div className="relative h-full min-h-0">
      {searchOpen && (
        <div className="absolute inset-x-0 top-0 z-20">
          <ChatSearchBar
            query={searchQuery}
            onQueryChange={setSearchQuery}
            matchCount={matchIndices.length}
            current={matchCursor}
            onNext={gotoNextMatch}
            onPrev={gotoPrevMatch}
            onClose={closeSearch}
          />
        </div>
      )}
      <div
        ref={scrollRef}
        className="absolute inset-0 overflow-y-auto p-4"
        onScroll={handleScroll}
      >
        {initialized && messages.length > 0 && (
          <div ref={topRef} className="py-4">
            {loadingMore && (
              <div className="text-center text-zinc-500 text-sm py-2">
                Loading older messages…
              </div>
            )}
            {!loadingMore && !hasMore && reachedTop && (
              <div className="text-center text-zinc-600 text-xs py-2 border-b border-zinc-800 mb-4">
                Beginning of conversation
              </div>
            )}
            {!loadingMore && hasMore && !reachedTop && (
              <div className="text-center text-zinc-600 text-xs py-2">
                ↑ Scroll up for older messages
              </div>
            )}
            {!loadingMore && hasMore && reachedTop && (
              <div className="text-center text-zinc-500 text-sm py-2">
                Loading older messages…
              </div>
            )}
          </div>
        )}

        {messages.length === 0 && live.length === 0 && initialized && (
          <div className="flex h-full items-center justify-center text-zinc-500">
            Start a conversation
          </div>
        )}

        {visibleMessages.length > 0 && (
          <div style={{ height: virtualizer.getTotalSize(), width: "100%", position: "relative" }}>
            {virtualizer.getVirtualItems().map((virtualItem) => {
              const { msg, i } = visibleMessages[virtualItem.index];
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
                    transform: `translateY(${virtualItem.start}px)`,
                  }}
                >
                  <div
                    className={
                      i === currentMatchMsgIndex
                        ? "scroll-mt-16 rounded-lg ring-2 ring-yellow-400/70 ring-offset-2 ring-offset-zinc-950"
                        : "scroll-mt-16"
                    }
                  >
                    <MessageBubble
                      message={msg}
                      highlight={searchOpen ? searchQuery : ""}
                      toolName={msg.tool_call_id ? toolNameById.get(msg.tool_call_id) : undefined}
                    />
                  </div>
                </div>
              );
            })}
          </div>
        )}

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

        {loadingMore && messages.length > 0 && (
          <div className="text-center text-zinc-500 text-sm py-2">
            Loading…
          </div>
        )}

        <div ref={bottomRef} />
      </div>
      {showJumpToBottom && (
        <button
          type="button"
          onClick={() => scrollToBottom(true)}
          className="absolute bottom-4 right-4 z-10 flex h-9 w-9 items-center justify-center rounded-full bg-zinc-700 text-zinc-100 shadow-lg transition-colors hover:bg-zinc-600"
          title="Scroll to bottom"
          aria-label="Scroll to bottom"
        >
          ↓
        </button>
      )}
    </div>
  );
}
