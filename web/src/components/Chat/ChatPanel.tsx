import { useEffect, useRef } from "react";
import { useChatState } from "../../stores/chatStore";
import MessageBubble, { AssistantText } from "./MessageBubble";
import { ThinkingBlock, ToolBlock } from "./TurnParts";

export default function ChatPanel() {
  const { messages, live } = useChatState();
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, live]);

  return (
    <div className="flex-1 overflow-y-auto p-4">
      {messages.length === 0 && live.length === 0 && (
        <div className="flex h-full items-center justify-center text-zinc-500">
          Start a conversation
        </div>
      )}
      {messages.map((msg, i) => (
        <MessageBubble key={i} message={msg} />
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
      <div ref={bottomRef} />
    </div>
  );
}
