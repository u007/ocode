import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Archive, ChevronDown, ChevronRight } from "lucide-react";

/**
 * Marker prefix for the synthetic system message the agent splices into the
 * transcript when a compaction replaces earlier turns
 * (internal/agent/compact.go: compactionSummaryMarker). The server persists and
 * broadcasts the message verbatim, so detecting it here is what makes the
 * compact notice part of the transcript: it survives reloads and the server's
 * post-compaction `messages` broadcast, unlike the ephemeral composer feedback.
 */
export const COMPACTION_SUMMARY_MARKER = "[ocode:compaction-summary]";

export function isCompactionSummary(content: string): boolean {
  return content.trimStart().startsWith(COMPACTION_SUMMARY_MARKER);
}

/**
 * Splits the persisted summary into its header line (e.g. "Compacted summary
 * covering 24 messages") and the markdown body produced by the summary model.
 */
export function parseCompactionSummary(content: string): { header: string; body: string } {
  const rest = content.trimStart().slice(COMPACTION_SUMMARY_MARKER.length).replace(/^\s*\n/, "");
  const newline = rest.indexOf("\n");
  if (newline === -1) {
    return { header: rest.trim(), body: "" };
  }
  return { header: rest.slice(0, newline).trim(), body: rest.slice(newline + 1).trim() };
}

/**
 * CompactionNotice renders the persisted compaction-summary message as an
 * inline transcript notice instead of showing the raw marker. The body is the
 * full summary the model produced; it is collapsed by default so the notice
 * stays compact in the transcript.
 */
export default function CompactionNotice({ content }: { content: string }) {
  const [expanded, setExpanded] = useState(false);
  const { header, body } = parseCompactionSummary(content);
  const label = header || "Compacted summary";
  return (
    <div className="flex justify-start mb-3" data-testid="compaction-notice">
      <div className="max-w-[95%] md:max-w-[80%] w-full rounded-lg border border-border bg-muted/60 px-3 py-2">
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
          className="flex items-center gap-2 w-full text-left text-xs font-medium text-muted-foreground hover:text-foreground"
        >
          {expanded ? (
            <ChevronDown aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
          ) : (
            <ChevronRight aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
          )}
          <Archive aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-cyan-400" />
          <span className="truncate">{label}</span>
        </button>
        {expanded && body && (
          <div className="mt-2 prose prose-invert prose-sm max-w-none text-sm text-foreground">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{body}</ReactMarkdown>
          </div>
        )}
      </div>
    </div>
  );
}
