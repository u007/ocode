import { X } from "lucide-react";
import { basename } from "../../lib/utils";
import { describeViewerLocation, formatViewerContext, type ViewerState } from "../../lib/viewerContext";

interface ViewerContextChipProps {
  state: ViewerState;
  /** Drops the viewer context from this message only; it returns on the next one. */
  onRemove: () => void;
}

/**
 * Chip above ChatInput showing what the preview viewer will attach to the
 * outgoing message: file, location and whether a selection or viewport text
 * is included. The tooltip shows the exact text that will be sent.
 */
export default function ViewerContextChip({ state, onRemove }: ViewerContextChipProps) {
  const location = describeViewerLocation(state.location);
  const marker = state.selection ? " · selection" : state.visibleText ? " · visible" : "";
  const label = `${basename(state.path)}${location ? ` · ${location}` : ""}${marker}`;

  return (
    <span
      className="inline-flex items-center gap-1 text-xs bg-violet-900/50 text-violet-300 rounded px-2 py-0.5 font-mono max-w-[260px]"
      title={formatViewerContext(state).slice(0, 300)}
    >
      <span className="w-1.5 h-1.5 rounded-full bg-violet-400 shrink-0" />
      <span className="truncate">{label}</span>
      <button
        type="button"
        onClick={onRemove}
        className="text-violet-300/70 hover:text-violet-100 shrink-0"
        aria-label="Remove viewer context from this message"
      >
        <X className="w-3 h-3" />
      </button>
    </span>
  );
}
