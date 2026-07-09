import { useEffect, useRef, type ReactNode } from "react";
import { ChevronDown, ChevronUp, X } from "lucide-react";
import type { Message } from "../../api/types";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

// messageMatchesQuery mirrors the TUI's chat-search scope (chat_search.go):
// case-insensitive substring match across the visible content, reasoning
// tokens, and every tool call's name + arguments. needle is pre-lowercased.
export function messageMatchesQuery(msg: Message, needle: string): boolean {
  if (msg.content && msg.content.toLowerCase().includes(needle)) return true;
  if (
    msg.reasoning_content &&
    msg.reasoning_content.toLowerCase().includes(needle)
  )
    return true;
  for (const tc of msg.tool_calls ?? []) {
    if (tc.function.name.toLowerCase().includes(needle)) return true;
    if (tc.function.arguments.toLowerCase().includes(needle)) return true;
  }
  return false;
}

// highlightMatches splits text on every case-insensitive occurrence of query
// and wraps the matches in <mark>. Returns the raw string when there is no
// query or no match so callers stay allocation-free in the common case.
export function highlightMatches(text: string, query: string): ReactNode {
  const q = query.trim();
  if (!q || !text) return text;
  const needle = q.toLowerCase();
  const hay = text.toLowerCase();
  if (!hay.includes(needle)) return text;

  const parts: ReactNode[] = [];
  let pos = 0;
  for (let idx = hay.indexOf(needle, pos); idx >= 0; idx = hay.indexOf(needle, pos)) {
    if (idx > pos) parts.push(text.slice(pos, idx));
    parts.push(
      <mark
        key={idx}
        className="rounded-sm bg-yellow-400/80 text-zinc-900"
      >
        {text.slice(idx, idx + needle.length)}
      </mark>,
    );
    pos = idx + needle.length;
  }
  if (pos < text.length) parts.push(text.slice(pos));
  return parts;
}

interface Props {
  query: string;
  onQueryChange: (q: string) => void;
  matchCount: number;
  // 0-based index of the current match, or -1 when nothing is selected.
  current: number;
  onNext: () => void;
  onPrev: () => void;
  onClose: () => void;
}

// ChatSearchBar is the in-chat find bar for the web SPA — the counterpart to
// the TUI's ctrl+f bar. It is presentational: match computation and navigation
// live in ChatPanel. Enter jumps to the next match, Shift+Enter to the
// previous (both wrap around), Esc closes.
export default function ChatSearchBar({
  query,
  onQueryChange,
  matchCount,
  current,
  onNext,
  onPrev,
  onClose,
}: Props) {
  const inputRef = useRef<HTMLInputElement>(null);

  // Focus and select the field whenever the bar mounts so re-opening starts a
  // fresh search without the user reaching for the mouse.
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const counter =
    query.trim() === ""
      ? "type to search"
      : matchCount === 0
        ? "No matches"
        : `${current + 1}/${matchCount}`;

  return (
    <div className="flex items-center gap-2 border-b border-zinc-800 bg-zinc-900 px-3 py-2">
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
        placeholder="Find in chat…"
        className="h-8 flex-1 bg-zinc-950 text-sm"
      />
      <span
        className={`min-w-[4.5rem] text-center text-xs tabular-nums ${
          query.trim() !== "" && matchCount === 0
            ? "text-red-400"
            : "text-zinc-400"
        }`}
      >
        {counter}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-7 w-7 text-zinc-400 hover:text-zinc-100"
        onClick={onPrev}
        disabled={matchCount === 0}
        title="Previous match (Shift+Enter)"
        aria-label="Previous match"
      >
        <ChevronUp className="h-4 w-4" />
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-7 w-7 text-zinc-400 hover:text-zinc-100"
        onClick={onNext}
        disabled={matchCount === 0}
        title="Next match (Enter)"
        aria-label="Next match"
      >
        <ChevronDown className="h-4 w-4" />
      </Button>
      <span className="hidden text-[11px] text-zinc-600 sm:inline">
        searching loaded messages
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="h-7 w-7 text-zinc-400 hover:text-zinc-100"
        onClick={onClose}
        title="Close (Esc)"
        aria-label="Close search"
      >
        <X className="h-4 w-4" />
      </Button>
    </div>
  );
}
