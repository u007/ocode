interface EditorContextChipProps {
  path: string;
  selection?: { startLine: number; endLine: number } | null;
}

/**
 * A small chip shown above ChatInput when an editor tab is active.
 * Displays the active file path and selected line range (if any).
 * Read-only indicator of the live editor context — there is no per-message
 * dismiss affordance (YAGNI unless requested later).
 */
export default function EditorContextChip({ path, selection }: EditorContextChipProps) {
  const label = selection
    ? `${path}:${selection.startLine}-${selection.endLine}`
    : path;

  return (
    <span className="inline-flex items-center gap-1 text-xs bg-blue-900/50 text-blue-300 rounded px-2 py-0.5 font-mono">
      <span className="w-1.5 h-1.5 rounded-full bg-blue-400 shrink-0" />
      {label}
    </span>
  );
}
