import { useState, type KeyboardEvent } from "react";
import { useChat } from "../../hooks/useChat";

export default function ChatInput() {
  const [input, setInput] = useState("");
  const { sendMessage, stop, isStreaming } = useChat();

  const handleSend = () => {
    const trimmed = input.trim();
    if (!trimmed || isStreaming) return;
    setInput("");
    sendMessage(trimmed);
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="border-t border-zinc-700 p-4">
      <div className="flex gap-2">
        <textarea
          className="flex-1 resize-none rounded-lg border border-zinc-600 bg-zinc-800 p-3 text-sm text-zinc-100 placeholder-zinc-500 focus:border-blue-500 focus:outline-none"
          rows={2}
          placeholder="Type a message... (Enter to send, Shift+Enter for newline)"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
        />
        {isStreaming ? (
          <button onClick={stop} className="self-end rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700">
            Stop
          </button>
        ) : (
          <button onClick={handleSend} disabled={!input.trim()} className="self-end rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:opacity-50">
            Send
          </button>
        )}
      </div>
    </div>
  );
}
