import { useChatState } from "../../stores/chatStore";

export default function StatusBar() {
  const { model, isStreaming, error } = useChatState();

  return (
    <div className="flex items-center justify-between border-t border-zinc-700 px-4 py-1.5 text-xs text-zinc-500">
      <div className="flex items-center gap-3">
        <span>{model || "no model"}</span>
        {isStreaming && (
          <span className="flex items-center gap-1">
            <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-blue-500" />
            streaming
          </span>
        )}
      </div>
      {error && <span className="text-red-400">{error}</span>}
      <span>ocode web</span>
    </div>
  );
}
