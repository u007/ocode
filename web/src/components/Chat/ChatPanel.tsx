import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useChatState, useChatDispatch } from "../../stores/chatStore";
import { api } from "../../api/client";
import MessageBubble, { AssistantText } from "./MessageBubble";
import { ThinkingBlock, ToolBlock } from "./TurnParts";
import ChatSearchBar, { messageMatchesQuery } from "./ChatSearchBar";

const PAGE_SIZE = 50;

export default function ChatPanel() {
  const { messages, live, hasMore, loadingMore } = useChatState();
  const dispatch = useChatDispatch();
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const topRef = useRef<HTMLDivElement>(null);
  const [initialized, setInitialized] = useState(false);
  const [reachedTop, setReachedTop] = useState(false);
  const [sessionId, setSessionId] = useState<string | null>(null);
  // Whether the viewport is pinned to the bottom. Driven by handleScroll and
  // consulted by the auto-scroll effect so we only follow the tail when the
  // user is already at the bottom (and resume reliably after they return).
  const atBottomRef = useRef(true);
  const [showJumpToBottom, setShowJumpToBottom] = useState(false);

  // In-chat find bar (Ctrl/Cmd+F). Client-side, searches only loaded messages.
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [matchCursor, setMatchCursor] = useState(-1);
  // Per-message DOM refs so the current match can be scrolled into view.
  const messageRefs = useRef<(HTMLDivElement | null)[]>([]);
  // Set true while a search jump is scrolling so handleScroll doesn't fire the
  // scroll-up pagination loader (which would shift every message index and
  // land the highlight on the wrong bubble).
  const searchJumpRef = useRef(false);

  // Track the session ID from the store
  const storeSessionId = useChatState().sessionId;

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

  // Initial load: fetch last 50 messages and scroll to bottom.
  // When no session is active (storeSessionId is null), mark as initialized
  // so the empty-state "Start a conversation" placeholder is shown instead of
  // a blank panel.
  useEffect(() => {
    if (!storeSessionId) {
      setInitialized(true);
      return;
    }
    setSessionId(storeSessionId);

    api
      .getSession(storeSessionId, { limit: PAGE_SIZE })
      .then((detail) => {
        // Set messages and pagination state
        dispatch({
          type: "MERGE_SNAPSHOT",
          messages: detail.messages,
          total: detail.total,
        });
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
        console.error("Failed to load session:", err);
        setInitialized(true);
      });
  }, [storeSessionId, dispatch]);

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

  // Toggle the find bar with Ctrl/Cmd+F. Local to the chat tab: ChatPanel is
  // only mounted while the chat tab is active (App.tsx), so this window
  // listener is naturally scoped without touching useKeyboard.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === "f" && (e.metaKey || e.ctrlKey)) {
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
  // pagination loader while the smooth scroll settles.
  useEffect(() => {
    if (currentMatchMsgIndex < 0) return;
    const el = messageRefs.current[currentMatchMsgIndex];
    if (!el) return;
    atBottomRef.current = false;
    setShowJumpToBottom(true);
    searchJumpRef.current = true;
    el.scrollIntoView({ behavior: "smooth", block: "center" });
    const t = setTimeout(() => {
      searchJumpRef.current = false;
    }, 600);
    return () => clearTimeout(t);
  }, [currentMatchMsgIndex]);

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
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;

    // Track whether the user is pinned to the bottom (within 120px). This is
    // the authoritative source for the auto-scroll decision.
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distanceFromBottom < 120;
    atBottomRef.current = atBottom;
    setShowJumpToBottom(!atBottom);

    // Track whether user has scrolled to the very top (always, regardless of hasMore)
    setReachedTop(el.scrollTop < 5);

    // Load more when near top. Suppressed during a search jump so prepending
    // older messages can't shift indices out from under the active match.
    if (!hasMore || loadingMore || !sessionId || searchJumpRef.current) return;
    if (el.scrollTop < 100) {
      const currentCount = messages.length;
      dispatch({ type: "SET_LOADING_MORE", loading: true });

      api
        .getSession(sessionId, { limit: PAGE_SIZE, offset: currentCount })
        .then((detail) => {
          if (detail.messages.length > 0) {
            // Remember scroll position to maintain it after prepending
            const scrollHeightBefore = el.scrollHeight;
            dispatch({
              type: "PREPEND_MESSAGES",
              messages: detail.messages,
              total: detail.total,
            });
            // Maintain scroll position after prepend
            requestAnimationFrame(() => {
              const scrollHeightAfter = el.scrollHeight;
              el.scrollTop = scrollHeightAfter - scrollHeightBefore;
            });
          } else {
            dispatch({ type: "SET_LOADING_MORE", loading: false });
          }
        })
        .catch(() => {
          dispatch({ type: "SET_LOADING_MORE", loading: false });
        });
    }
  }, [hasMore, loadingMore, messages.length, sessionId, dispatch]);

  return (
    <div className="relative flex-1 min-h-0">
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
        {/* Top sentinel: "at the beginning" indicator */}
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

      {messages.map((msg, i) => {
        // A tool result still carrying a QUESTION_PROMPT: / PERMISSION_ASK:
        // sentinel is an unanswered prompt rendered by its dialog, not the
        // transcript. Once resolved, the content is replaced in place, so a live
        // sentinel only ever exists pre-resolution — skip it permanently.
        if (
          msg.role === "tool" &&
          (msg.content?.startsWith("QUESTION_PROMPT:") ||
            msg.content?.startsWith("PERMISSION_ASK:"))
        ) {
          return null;
        }
        return (
        <div
          key={`${msg.role}-${i}-${msg.content?.slice(0, 20)}`}
          ref={(el) => {
            messageRefs.current[i] = el;
          }}
          className={
            i === currentMatchMsgIndex
              ? "scroll-mt-16 rounded-lg ring-2 ring-yellow-400/70 ring-offset-2 ring-offset-zinc-950"
              : "scroll-mt-16"
          }
        >
          <MessageBubble message={msg} highlight={searchOpen ? searchQuery : ""} />
        </div>
        );
      })}

      {/* In-progress turn, streamed live until the turn_done snapshot commits it. */}
      {live.map((part, i) => {
        if (part.kind === "thinking")
          return <ThinkingBlock key={`live-${i}`} text={part.text} />;
        if (part.kind === "text")
          return <AssistantText key={`live-${i}`} content={part.text} />;
        return (
          <ToolBlock
            key={`live-${i}`}
            tool={part.tool}
            command={part.command}
            output={part.output}
          />
        );
      })}

      {/* Loading more indicator at bottom */}
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
