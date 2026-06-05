import { useChatState } from "../../stores/chatStore";
import { useTheme } from "../../hooks/useTheme";
import { Button } from "@/components/ui/button";
import { PanelRight } from "lucide-react";

interface Props {
  onCoworkToggle?: () => void;
  onModelClick?: () => void;
}

export default function StatusBar({ onCoworkToggle, onModelClick }: Props) {
  const { model, smallModel, advisorModel, advisorEnabled, isStreaming, error } =
    useChatState();
  const { theme, toggle } = useTheme();

  return (
    <div className="flex items-center justify-between border-t border-zinc-700 px-4 py-1.5 text-xs text-zinc-500">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={onModelClick}
          className="hover:text-zinc-300 transition-colors"
          title="Change model"
        >
          {model || "no model"}
        </button>
        {smallModel && (
          <span className="text-zinc-600">
            · small: {smallModel}
          </span>
        )}
        {advisorModel && (
          <span className="text-zinc-600">
            · advisor: {advisorModel}
          </span>
        )}
        <span className={advisorEnabled ? "text-emerald-500" : "text-zinc-600"}>
          · advisor {advisorEnabled ? "on" : "off"}
        </span>
        {isStreaming && (
          <span className="flex items-center gap-1">
            <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-blue-500" />
            streaming
          </span>
        )}
      </div>
      {error && <span className="text-red-400">{error}</span>}
      <div className="flex items-center gap-3">
        {onCoworkToggle && (
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={onCoworkToggle}
            title="Toggle cowork panel"
          >
            <PanelRight className="h-4 w-4" />
          </Button>
        )}
        <Button
          type="button"
          variant="ghost"
          size="icon"
          onClick={toggle}
          title={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
        >
          {theme === "dark" ? "☀️" : "🌙"}
        </Button>
        <span>ocode web</span>
      </div>
    </div>
  );
}
