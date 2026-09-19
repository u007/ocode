import { useEffect, useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { AskContext } from "@/stores/chatStore";

interface Props {
  context?: AskContext | null;
}

/**
 * Shows the LLM's most recent message (and its reasoning) inside the
 * permission/question ask dialogs, so the user can see what the agent was
 * doing instead of judging a bare command. Renders nothing when no assistant
 * message is available.
 */
export default function AskContextPreview({ context }: Props) {
  const [thinkingOpen, setThinkingOpen] = useState(true);
  const thinking = context?.thinking ?? "";
  // Re-expand when a new ask replaces the previous one while the dialog stays
  // mounted (the permission queue resurfaces the next ask without unmounting).
  useEffect(() => {
    setThinkingOpen(true);
  }, [thinking]);
  if (!context || (!context.text && !context.thinking)) return null;
  return (
    <section
      aria-label="Last model message"
      className="max-w-full rounded-lg border border-border bg-muted/40 p-3"
    >
      <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
        Last model message
      </div>
      {context.text && (
        <p className="mt-1 whitespace-pre-wrap break-words text-sm text-foreground [overflow-wrap:anywhere]">
          {context.text}
        </p>
      )}
      {context.thinking && (
        <div className="mt-2">
          <button
            type="button"
            onClick={() => setThinkingOpen((v) => !v)}
            aria-expanded={thinkingOpen}
            className="flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
          >
            {thinkingOpen ? (
              <ChevronDown aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
            ) : (
              <ChevronRight aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
            )}
            <span>🧠 Thinking</span>
          </button>
          {thinkingOpen && (
            <pre className="mt-1 max-h-40 overflow-y-auto whitespace-pre-wrap break-words rounded border border-border/60 bg-card/60 p-2 font-mono text-xs text-muted-foreground [overflow-wrap:anywhere]">
              {context.thinking}
            </pre>
          )}
        </div>
      )}
    </section>
  );
}
