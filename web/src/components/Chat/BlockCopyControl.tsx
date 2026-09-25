import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, Copy } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "../ui/popover";
import { copyTextToClipboard } from "../../lib/clipboard";
import { cn } from "@/lib/utils";

type CopyState = "idle" | "copied" | "failed";

/**
 * Split copy control for a chat block. The main button copies the rendered
 * text ("as it is"); the chevron opens a menu with a raw-source item. Both
 * paths use the shared clipboard helper so the desktop shell and insecure LAN
 * origins keep working.
 */
export function BlockCopyControl({
  rawText,
  getRenderedText,
  rawLabel = "Copy as raw Markdown",
  ariaLabel = "Copy message",
  className,
}: {
  /** Original source for the block. Also the fallback when DOM extraction is empty. */
  rawText: string;
  /** Extracts the visible text from the rendered block at click time. */
  getRenderedText?: () => string;
  /** Menu item label; callers adapt it when the source is not Markdown. */
  rawLabel?: string;
  /** Accessible name for the main Copy button. */
  ariaLabel?: string;
  className?: string;
}) {
  const [state, setState] = useState<CopyState>("idle");
  const [menuOpen, setMenuOpen] = useState(false);
  const resetRef = useRef<number | null>(null);

  useEffect(
    () => () => {
      if (resetRef.current !== null) window.clearTimeout(resetRef.current);
    },
    [],
  );

  const flash = (next: Exclude<CopyState, "idle">) => {
    setState(next);
    if (resetRef.current !== null) window.clearTimeout(resetRef.current);
    resetRef.current = window.setTimeout(
      () => setState("idle"),
      next === "copied" ? 1500 : 3000,
    );
  };

  const write = async (text: string) => {
    const ok = await copyTextToClipboard(text);
    flash(ok ? "copied" : "failed");
  };

  const copyRendered = () => {
    let rendered = "";
    try {
      rendered = getRenderedText?.() ?? "";
    } catch {
      // intentionally not logged: DOM extraction is best effort; falling back
      // to the raw source is the expected degradation for an unreadable block.
      rendered = "";
    }
    void write(rendered.trim() !== "" ? rendered : rawText);
  };

  const copyRaw = () => {
    setMenuOpen(false);
    void write(rawText);
  };

  const failureHint = "Copy blocked — select the text and press Ctrl/Cmd+C";
  return (
    <span
      data-testid="block-copy"
      data-speech-exclude=""
      className={cn(
        "inline-flex items-center overflow-hidden rounded border border-border/40 text-muted-foreground",
        className,
      )}
    >
      <button
        type="button"
        data-testid="block-copy-default"
        data-state={state}
        aria-label={ariaLabel}
        title={state === "failed" ? failureHint : ariaLabel}
        onClick={copyRendered}
        className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[11px] hover:bg-background hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
      >
        {state === "copied" ? (
          <Check className="h-3.5 w-3.5" aria-hidden="true" />
        ) : (
          <Copy className="h-3.5 w-3.5" aria-hidden="true" />
        )}
        <span>{state === "copied" ? "Copied" : state === "failed" ? "Copy failed" : "Copy"}</span>
      </button>
      <Popover open={menuOpen} onOpenChange={setMenuOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            data-testid="block-copy-menu"
            aria-label="More copy options"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            className="inline-flex items-center border-l border-border/40 px-1 py-0.5 hover:bg-background hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            <ChevronDown className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
        </PopoverTrigger>
        <PopoverContent role="menu" align="end" sideOffset={4} className="w-52 p-1">
          <button
            type="button"
            role="menuitem"
            data-testid="block-copy-raw"
            onClick={copyRaw}
            className="w-full rounded px-2 py-1.5 text-left text-xs text-popover-foreground hover:bg-accent hover:text-accent-foreground focus:bg-accent focus:text-accent-foreground focus:outline-none"
          >
            {rawLabel}
          </button>
        </PopoverContent>
      </Popover>
      <span role="status" aria-live="polite" className="sr-only">
        {state === "copied" ? "Copied" : state === "failed" ? "Copy failed" : ""}
      </span>
    </span>
  );
}
