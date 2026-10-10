import { useEffect, useState } from "react";
import { Loader2, X } from "lucide-react";
import { dismissCompaction, isCompactCommand, useCompactionState } from "../../lib/compactionState";
import { useSessionActivity } from "../../lib/commandActivity";
import CompactionCancelButton from "./CompactionCancelButton";

/** Composer feedback for a compaction this client is not already reporting as
 *  running work, plus the queued and failed states. Completion is reported by
 *  the persisted compaction-summary notice in the transcript, so nothing is
 *  retained once compaction finishes.
 *
 *  The running row is suppressed while CommandActivityBar is already drawing
 *  this client's own in-flight `/compact`: both render the same spinner and
 *  elapsed counter, so painting both stacks two identical rows above the
 *  composer. The suppression is deliberately narrow — a compaction this client
 *  did not start (another tab or browser, or a server-side automatic pass) has
 *  no command activity standing in for it, so the bar still carries those. */
export default function CompactionStatus({ sessionId, host, queued }: { sessionId?: string | null; host?: string; queued: boolean }) {
  const state = useCompactionState(sessionId);
  const activity = useSessionActivity(sessionId);
  const [now, setNow] = useState(Date.now);
  const reportedByCommandBar =
    state?.status === "active" && activity?.kind === "command" && isCompactCommand(activity.label);
  const active = state?.status === "active" && !reportedByCommandBar;
  const startedAt = active ? state.startedAt : undefined;
  useEffect(() => {
    if (startedAt === undefined) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [startedAt]);

  const failed = state?.status === "error";
  if (!active && !failed && !queued) return null;
  return (
    <div className="mb-2 rounded-md border border-border bg-muted px-3 py-2 text-xs flex items-start gap-2" role={failed ? "alert" : "status"}>
      {active && <Loader2 aria-hidden="true" className="h-4 w-4 shrink-0 animate-spin" />}
      <div className="min-w-0 flex-1 break-words">
        {active && <div>Compacting conversation… {Math.max(0, Math.floor((now - state.startedAt) / 1000))}s elapsed</div>}
        {failed && <div>Compaction failed: {state.error}</div>}
        {queued && <div>Compaction queued — waiting for the current work to finish. The running turn will not be interrupted.</div>}
      </div>
      {active && <CompactionCancelButton sessionId={sessionId} host={host} />}
      {/* Dismissal is an error-state affordance only. An active operation cannot be hidden (dismissCompaction refuses), so gating on `active` would render a dead button for a compaction the command bar is already covering. */}
      {failed && sessionId && <button type="button" aria-label="Dismiss compaction status" onClick={() => dismissCompaction(sessionId)} className="shrink-0 text-muted-foreground hover:text-foreground"><X className="h-3 w-3" /></button>}
    </div>
  );
}
