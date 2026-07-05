import { useCallback, useEffect, useRef, useState } from "react";
import { useChatState, useChatDispatch } from "../../stores/chatStore";
import { api } from "../../api/client";
import MessageBubble, { AssistantText } from "./MessageBubble";
import { ThinkingBlock, ToolBlock } from "./TurnParts";

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

  // Track the session ID from the store
  const storeSessionId = useChatState().sessionId;

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
          bottomRef.current?.scrollIntoView({ behavior: "instant" });
        });
      })
      .catch((err) => {
        console.error("Failed to load session:", err);
        setInitialized(true);
      });
  }, [storeSessionId, dispatch]);

  // Auto-scroll to bottom on new messages/live content (only if already at bottom)
  useEffect(() => {
    if (!initialized) return;
    const el = scrollRef.current;
    if (!el) return;
    // Only auto-scroll if user is near the bottom (within 150px)
    const nearBottom =
      el.scrollHeight - el.scrollTop - el.clientHeight < 150;
    if (nearBottom) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [messages, live, initialized]);

  // Scroll-up handler: load older messages when near top
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;

    // Track whether user has scrolled to the very top (always, regardless of hasMore)
    setReachedTop(el.scrollTop < 5);

    // Load more when near top
    if (!hasMore || loadingMore || !sessionId) return;
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
    <div
      ref={scrollRef}
      className="flex-1 overflow-y-auto p-4"
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

      {messages.map((msg, i) => (
        <MessageBubble key={`${msg.role}-${i}-${msg.content?.slice(0, 20)}`} message={msg} />
      ))}

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
  );
}
