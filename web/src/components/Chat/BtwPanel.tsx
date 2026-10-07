import { X } from "lucide-react";
import { useCallback, useEffect, useRef } from "react";
import { api } from "../../api/client";
import { closeBtw, useBtwState } from "../../lib/btwStore";
import { Button } from "../ui/button";
import { ScrollArea } from "../ui/scroll-area";

/**
 * Docked, non-blocking panel for a `/btw` side query.
 *
 * It is deliberately NOT a dialog: the aside is a quick side question, so the
 * composer must stay usable while the answer streams. State comes from
 * btwStore (fed by the `btw` bus event); this component renders only.
 */
export function BtwPanel({ sessionId, host }: { sessionId: string; host?: string }) {
  const state = useBtwState(sessionId, host);
  const rootRef = useRef<HTMLDivElement>(null);

  const dismiss = useCallback(() => {
    // Cancel the server-side side query (independent of the main turn), then
    // drop the local panel. A failed cancel still closes the panel — the run's
    // frames are ignored once its state is gone — but log it so a broken
    // cancel is diagnosable.
    api.cancelBtw(sessionId, host).catch((err) => {
      console.warn("btw: cancel side query failed", err);
    });
    closeBtw(sessionId, host);
  }, [sessionId, host]);

  useEffect(() => {
    if (!state?.open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      // Only when focus is inside the panel, so Esc keeps its composer meaning
      // (Stop) everywhere else.
      if (!rootRef.current?.contains(document.activeElement)) return;
      e.preventDefault();
      dismiss();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [state?.open, dismiss]);

  if (!state || !state.open) return null;

  return (
    <div
      ref={rootRef}
      role="region"
      aria-label="By the way side query"
      data-testid="btw-panel"
      className="mx-2 mb-1 rounded-lg border border-border bg-card text-card-foreground shadow-sm"
    >
      <div className="flex items-center gap-2 border-b border-border/60 px-3 py-1.5">
        <span className="shrink-0 text-xs font-medium text-muted-foreground">↳ By The Way</span>
        <span className="min-w-0 flex-1 truncate text-xs text-foreground" title={state.question}>
          {state.question}
        </span>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6 shrink-0"
          aria-label="Close side query"
          onClick={dismiss}
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>
      <ScrollArea className="max-h-52">
        <div className="space-y-1 px-3 py-2 text-sm">
          {state.activity.map((line, i) => (
            <div
              key={i}
              className="whitespace-pre-wrap font-mono text-xs text-muted-foreground"
            >
              {line}
            </div>
          ))}
          {state.answer ? (
            <div className="whitespace-pre-wrap">{state.answer}</div>
          ) : state.loading ? (
            <div className="italic text-muted-foreground">Thinking…</div>
          ) : null}
          {state.error ? <div className="text-xs text-destructive">{state.error}</div> : null}
        </div>
      </ScrollArea>
    </div>
  );
}
