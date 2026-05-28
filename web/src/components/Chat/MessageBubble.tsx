import type { Message } from "../../api/types";

interface Props {
  message: Message;
}

export default function MessageBubble({ message }: Props) {
  const isUser = message.role === "user";

  return (
    <div className={`flex ${isUser ? "justify-end" : "justify-start"} mb-3`}>
      <div className={`max-w-[80%] rounded-lg px-4 py-2 ${
        isUser ? "bg-blue-600 text-white" : "bg-zinc-800 text-zinc-100"
      }`}>
        <pre className="whitespace-pre-wrap font-sans text-sm">
          {message.content}
        </pre>
      </div>
    </div>
  );
}
