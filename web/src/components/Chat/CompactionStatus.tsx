import { useEffect, useState } from "react";
import { Loader2, X } from "lucide-react";
import { dismissCompaction, useCompactionState } from "../../lib/compactionState";

/** Composer feedback deliberately lives outside the replaceable transcript. */
export default function CompactionStatus({ sessionId, queued }: { sessionId?: string | null; queued: boolean }) {
  const state = useCompactionState(sessionId);
  const [now, setNow] = useState(Date.now);
  const startedAt = state?.status === "active" ? state.startedAt : undefined;
  useEffect(() => {
    if (startedAt === undefined) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [startedAt]);

  if (!state && !queued) return null;
  const active = state?.status === "active";
  return (
    <div className="mb-2 rounded-md border border-border bg-muted px-3 py-2 text-xs flex items-start gap-2" role={state?.status === "error" ? "alert" : "status"}>
      {active && <Loader2 aria-hidden="true" className="h-4 w-4 shrink-0 animate-spin" />}
      <div className="min-w-0 flex-1 break-words">
        {active && <div>Compacting conversation… {Math.max(0, Math.floor((now - state.startedAt) / 1000))}s elapsed</div>}
        {state?.status === "complete" && <div>Compacted: {state.originalLen} → {state.compactedLen} messages</div>}
        {state?.status === "error" && <div>Compaction failed: {state.error}</div>}
        {queued && <div>Compaction queued — waiting for the current work to finish. The running turn will not be interrupted.</div>}
      </div>
      {state && !active && sessionId && <button type="button" aria-label="Dismiss compaction status" onClick={() => dismissCompaction(sessionId)} className="shrink-0 text-muted-foreground hover:text-foreground"><X className="h-3 w-3" /></button>}
    </div>
  );
}
