import { useEffect, useRef } from "react";
import { ChevronDown, ChevronUp, X } from "lucide-react";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

interface Props {
  query: string;
  onQueryChange: (q: string) => void;
  resultCount: number;
  resultIndex: number;
  onNext: () => void;
  onPrev: () => void;
  onClose: () => void;
}

export default function TerminalFindBar({
  query,
  onQueryChange,
  resultCount,
  resultIndex,
  onNext,
  onPrev,
  onClose,
}: Props) {
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const counter =
    query.trim() === ""
      ? "type to search"
      : resultCount === 0
        ? "No matches"
        : `${resultIndex + 1}/${resultCount}`;

  return (
    <div
      className="absolute right-2 top-2 z-20 flex items-center gap-2 rounded-md border border-border bg-card px-2 py-1.5 shadow-lg"
      role="search"
      aria-label="Find in terminal"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onClose();
        }
      }}
    >
      <Input
        ref={inputRef}
        value={query}
        onChange={(e) => onQueryChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            if (e.shiftKey) onPrev();
            else onNext();
          } else if (e.key === "Escape") {
            e.preventDefault();
            onClose();
          }
        }}
        placeholder="Find…"
        className="h-7 w-44 bg-background text-sm"
        aria-label="Find in terminal"
      />
      <span
        className={`min-w-[4.5rem] text-center text-xs tabular-nums ${
          query.trim() !== "" && resultCount === 0 ? "text-red-400" : "text-muted-foreground"
        }`}
      >
        {counter}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-7 w-7 text-muted-foreground hover:text-foreground"
        onClick={onPrev}
        disabled={resultCount === 0}
        title="Previous match (Shift+Enter)"
        aria-label="Previous match"
      >
        <ChevronUp className="h-4 w-4" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-7 w-7 text-muted-foreground hover:text-foreground"
        onClick={onNext}
        disabled={resultCount === 0}
        title="Next match (Enter)"
        aria-label="Next match"
      >
        <ChevronDown className="h-4 w-4" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-7 w-7 text-muted-foreground hover:text-foreground"
        onClick={onClose}
        title="Close (Esc)"
        aria-label="Close find bar"
      >
        <X className="h-4 w-4" />
      </Button>
    </div>
  );
}
