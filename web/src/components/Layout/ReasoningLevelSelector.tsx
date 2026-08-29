import { useState, useEffect } from "react";
import { api } from "../../api/client";
import { Brain } from "lucide-react";

// Reasoning levels matching the backend ThinkingBudgetLabels
const REASONING_LEVELS = ["off", "low", "med", "high", "xhigh", "max"] as const;
type ReasoningLevel = (typeof REASONING_LEVELS)[number];

interface Props {
  /** Current thinking budget from TUI status (0 = off) */
  thinkingBudget?: number;
  /** Disabled state when model selection is not available */
  disabled?: boolean;
}

/**
 * Compact reasoning level selector for the sidebar.
 * Shows the current level as a clickable dropdown to cycle through
 * reasoning effort levels (off → low → med → high → xhigh → max).
 */
export default function ReasoningLevelSelector({ thinkingBudget, disabled }: Props) {
  const [isOpen, setIsOpen] = useState(false);
  const [currentLevel, setCurrentLevel] = useState<ReasoningLevel>("off");

  // Derive level from thinking budget (0 = off, 1024 = low, etc.)
  useEffect(() => {
    if (thinkingBudget === undefined) return;

    // Map budget to level name
    if (thinkingBudget === 0) {
      setCurrentLevel("off");
    } else if (thinkingBudget <= 1024) {
      setCurrentLevel("low");
    } else if (thinkingBudget <= 8000) {
      setCurrentLevel("med");
    } else if (thinkingBudget <= 16000) {
      setCurrentLevel("high");
    } else if (thinkingBudget <= 32000) {
      setCurrentLevel("xhigh");
    } else {
      setCurrentLevel("max");
    }
  }, [thinkingBudget]);

  const handleLevelChange = async (level: ReasoningLevel) => {
    // Optimistically update UI so the dropdown reflects the selection
    // immediately, even if the API call is slow or fails.
    const prevLevel = currentLevel;
    setCurrentLevel(level);
    setIsOpen(false);
    try {
      await api.setThinkingBudget(level);
    } catch (err) {
      console.error("Failed to set reasoning level:", err);
      // Revert on failure so the user sees the actual state.
      setCurrentLevel(prevLevel);
    }
  };

  // Get color for level display
  const getLevelColor = (level: ReasoningLevel): string => {
    switch (level) {
      case "off":
        return "text-muted-foreground";
      case "low":
        return "text-muted-foreground";
      case "med":
        return "text-blue-400";
      case "high":
        return "text-purple-400";
      case "xhigh":
        return "text-pink-400";
      case "max":
        return "text-amber-400";
      default:
        return "text-muted-foreground";
    }
  };

  return (
    <div className="relative mt-2">
      <button
        type="button"
        onClick={() => !disabled && setIsOpen(!isOpen)}
        disabled={disabled}
        className="w-full flex items-center gap-2 px-1 py-1 text-left text-xs transition-colors hover:bg-muted disabled:cursor-default disabled:hover:bg-transparent rounded"
      >
        <Brain className={`w-3 h-3 ${getLevelColor(currentLevel)}`} />
        <span className="text-muted-foreground">Reason:</span>
        <span className={`font-medium ${getLevelColor(currentLevel)}`}>
          {currentLevel.toUpperCase()}
        </span>
      </button>

      {isOpen && !disabled && (
        <div className="absolute left-0 top-full mt-1 w-32 bg-muted border border-border rounded-md shadow-lg z-50">
          {REASONING_LEVELS.map((level) => (
            <button
              key={level}
              type="button"
              onClick={() => handleLevelChange(level)}
              className={`w-full flex items-center gap-2 px-3 py-1.5 text-xs text-left hover:bg-accent ${
                level === currentLevel ? "bg-accent" : ""
              }`}
            >
              <Brain className={`w-3 h-3 ${getLevelColor(level)}`} />
              <span className={level === currentLevel ? "font-medium text-foreground" : "text-muted-foreground"}>
                {level.toUpperCase()}
              </span>
              {level === currentLevel && (
                <span className="ml-auto text-muted-foreground">✓</span>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
