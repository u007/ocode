import { useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "../ui/popover";
import { cn } from "@/lib/utils";
import {
  type PreviewWidthPreset,
  previewWidthMenuLabel,
  previewWidthShortLabel,
  resolvePreviewWidth,
  setPreviewWidthPreset,
  usePreviewWidthPreset,
} from "../../lib/previewWidth";

const PRESETS: PreviewWidthPreset[] = ["auto", "letter", "a4", "full"];

/**
 * Compact control for the markdown reading column (see lib/previewWidth).
 *
 * A four-way segmented control does not fit beside a long file path in the
 * ~400px sidebar header, so this is one compact button showing the CURRENT
 * column ("A4", "Letter", "Full") that opens a popover. It writes the GLOBAL
 * `previewWidth` preference, so the sidebar, the Preview tab and the Files-tab
 * preview always agree; nothing here is per file.
 *
 * The options are plain toggle buttons (`aria-pressed`), NOT an ARIA menu or
 * radiogroup: Radix Popover provides no roving focus or arrow-key navigation,
 * so claiming menu semantics would be a promise the widget does not keep.
 */
export function PreviewWidthMenu() {
  const [open, setOpen] = useState(false);
  const preset = usePreviewWidthPreset();
  const resolved = resolvePreviewWidth(preset);
  const short = previewWidthShortLabel(resolved);
  const title = preset === "auto" ? `Page width: Auto (${short})` : `Page width: ${short}`;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          data-testid="preview-width-trigger"
          aria-label={title}
          aria-haspopup="dialog"
          aria-expanded={open}
          title={title}
          className="inline-flex shrink-0 items-center gap-0.5 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
        >
          {short}
          <ChevronDown className="h-3 w-3" aria-hidden="true" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" sideOffset={4} className="w-56 p-1">
        <div className="px-2 py-1 text-[10px] uppercase tracking-wide text-muted-foreground">
          Page width
        </div>
        {PRESETS.map((p) => (
          <button
            key={p}
            type="button"
            aria-pressed={p === preset}
            data-testid={`preview-width-${p}`}
            onClick={() => {
              setPreviewWidthPreset(p);
              setOpen(false);
            }}
            className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs text-popover-foreground hover:bg-accent hover:text-accent-foreground focus:bg-accent focus:text-accent-foreground focus:outline-none"
          >
            <Check
              className={cn("h-3 w-3 shrink-0", p === preset ? "opacity-100" : "opacity-0")}
              aria-hidden="true"
            />
            <span className="min-w-0 flex-1 truncate">{previewWidthMenuLabel(p)}</span>
          </button>
        ))}
      </PopoverContent>
    </Popover>
  );
}
