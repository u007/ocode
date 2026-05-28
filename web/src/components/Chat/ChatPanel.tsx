import { useEffect, useRef } from "react";
import { useChatState } from "../../stores/chatStore";
import MessageBubble from "./MessageBubble";

export default function ChatPanel() {
  const { messages } = useChatState();
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  return (
    <div className="flex-1 overflow-y-auto p-4">
      {messages.length === 0 && (
        <div className="flex h-full items-center justify-center text-zinc-500">
          Start a conversation
        </div>
      )}
      {messages.map((msg, i) => (
        <MessageBubble key={i} message={msg} />
      ))}
      <div ref={bottomRef} />
    </div>
  );
}
