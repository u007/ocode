interface EditorContextChipProps {
  path: string;
  selection?: { startLine: number; endLine: number } | null;
  /** When provided, an X button is shown that removes the chip (sticky for the session). */
  onRemove?: () => void;
}

/**
 * A small chip shown above ChatInput when an editor tab is active.
 * Displays the active file path and selected line range (if any).
 * Read-only indicator of the live editor context. When `onRemove` is provided,
 * an X button lets the user drop it from the chat for the rest of the session
 * (it re-injects only if a genuinely new file tab is opened).
 */
export default function EditorContextChip({ path, selection, onRemove }: EditorContextChipProps) {
  const label = selection
    ? `${path}:${selection.startLine}-${selection.endLine}`
    : path;

  return (
    <span className="inline-flex items-center gap-1 text-xs bg-blue-900/50 text-blue-300 rounded px-2 py-0.5 font-mono max-w-[180px]">
      <span className="w-1.5 h-1.5 rounded-full bg-blue-400 shrink-0" />
      <span className="truncate">{label}</span>
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          className="text-blue-300/70 hover:text-blue-100 shrink-0"
          aria-label={`Remove ${path} from this message`}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
            <path d="M2 2L8 8M8 2L2 8" />
          </svg>
        </button>
      )}
    </span>
  );
}
