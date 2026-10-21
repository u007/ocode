import { cn } from "@/lib/utils";

interface Props {
  /** The options the agent proposed for the last reply. */
  suggestions: string[];
  /** Called with the chosen option. The parent fills the composer with it. */
  onSelect: (suggestion: string) => void;
}

/**
 * SuggestedResponses — the row of follow-up options the agent proposed at the
 * end of its last reply, rendered directly ABOVE the quick-action strip at the
 * bottom of the composer.
 *
 * Purely presentational, like `QuickActionsBar`: the parent owns what a click
 * does (the composer fills the text; it never auto-sends) and whether the row
 * shows at all.
 *
 * The options are model-authored, so they are rendered as plain text — never
 * as markdown and never as HTML. A suggestion that contained markup would
 * otherwise be able to inject formatting into the transcript.
 */
export default function SuggestedResponses({ suggestions, onSelect }: Props) {
  if (suggestions.length === 0) return null;

  return (
    <div
      role="toolbar"
      aria-label="Suggested responses"
      className="mt-2 flex flex-wrap items-center gap-1"
    >
      {suggestions.map((suggestion) => (
        <button
          key={suggestion}
          type="button"
          onClick={() => onSelect(suggestion)}
          title={suggestion}
          aria-label={suggestion}
          className={cn(
            "inline-flex max-w-[22rem] items-center gap-1 rounded-full border border-border px-2.5 py-0.5 text-xs text-muted-foreground transition-colors",
            "hover:bg-accent hover:text-accent-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
          )}
        >
          <span className="truncate">{suggestion}</span>
        </button>
      ))}
    </div>
  );
}
