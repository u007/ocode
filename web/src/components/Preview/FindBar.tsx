import type { RefObject } from "react";
import type { FindOptions } from "./findDom";

/**
 * Shared find bar for every previewable document surface (PDF, Markdown,
 * Word, HTML). Same markup/aria labels everywhere so the toolbar, the
 * keyboard shortcuts and the tests stay uniform across viewers.
 */
export function FindBar({
  query,
  onQueryChange,
  current,
  count,
  searching,
  opts,
  onToggleOption,
  onNext,
  onPrev,
  onClose,
  inputRef,
  placeholder = "Find in document",
}: {
  query: string;
  onQueryChange: (value: string) => void;
  current: number;
  count: number;
  searching?: boolean;
  opts: FindOptions;
  onToggleOption: (key: keyof FindOptions) => void;
  onNext: () => void;
  onPrev: () => void;
  onClose: () => void;
  inputRef?: RefObject<HTMLInputElement | null>;
  placeholder?: string;
}) {
  const idleBtn = "rounded px-1.5 py-0.5 hover:bg-muted disabled:opacity-40";
  const toggleOn = "rounded px-1.5 py-0.5 bg-muted text-foreground";
  const toggleOff = "rounded px-1.5 py-0.5 text-muted-foreground hover:bg-muted";
  return (
    <div role="search" className="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1 text-xs">
      <input
        ref={inputRef}
        autoFocus
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
        placeholder={placeholder}
        aria-label="Find text"
        className="h-6 w-44 rounded border border-border bg-transparent px-1.5 outline-none focus:ring-1 focus:ring-ring"
      />
      <span aria-live="polite" className="min-w-12 text-center text-muted-foreground">
        {searching ? "…" : `${count ? current + 1 : 0}/${count}`}
      </span>
      <button type="button" disabled={!count} onClick={onPrev} className={idleBtn} aria-label="Previous match" title="Previous match (Shift+F3)">
        ↑
      </button>
      <button type="button" disabled={!count} onClick={onNext} className={idleBtn} aria-label="Next match" title="Next match (Enter, F3)">
        ↓
      </button>
      <button
        type="button"
        aria-label="Match case"
        title="Match case"
        aria-pressed={opts.caseSensitive}
        className={opts.caseSensitive ? toggleOn : toggleOff}
        onClick={() => onToggleOption("caseSensitive")}
      >
        Aa
      </button>
      <button
        type="button"
        aria-label="Whole word"
        title="Whole word"
        aria-pressed={opts.wholeWord}
        className={opts.wholeWord ? toggleOn : toggleOff}
        onClick={() => onToggleOption("wholeWord")}
      >
        ab
      </button>
      <button type="button" onClick={onClose} className={idleBtn} aria-label="Close find" title="Close (Esc)">
        ✕
      </button>
    </div>
  );
}
