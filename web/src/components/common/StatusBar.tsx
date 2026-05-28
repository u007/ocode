import { useChatState } from "../../stores/chatStore";
import { useTheme } from "../../hooks/useTheme";

export default function StatusBar() {
  const { model, isStreaming, error } = useChatState();
  const { theme, toggle } = useTheme();

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
      <div className="flex items-center gap-3">
        <button
          onClick={toggle}
          className="hover:text-zinc-300 transition-colors"
          title={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
        >
          {theme === "dark" ? "☀️" : "🌙"}
        </button>
        <span>ocode web</span>
      </div>
    </div>
  );
}
