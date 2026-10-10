import { useState } from "react";
import { api } from "../../api/client";
import { reportActionError } from "../../lib/actionErrors";

/**
 * Cancel affordance for an in-flight compaction (a manual /compact or an
 * automatic pass). The server interrupts every pass for the session, and its
 * terminal compaction_done frame clears the bars on every connected client, so
 * this button never clears compaction state itself on success — it only
 * disables while the request is in flight and surfaces a failure as an action
 * error. A second click while in flight is a no-op.
 */
export default function CompactionCancelButton({
  sessionId,
  host,
  className,
}: {
  sessionId?: string | null;
  host?: string;
  className?: string;
}) {
  const [cancelling, setCancelling] = useState(false);
  if (!sessionId) return null;

  const onCancel = async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancelCompaction(sessionId, host);
    } catch (err) {
      setCancelling(false);
      reportActionError(err, "Cancel compaction");
    }
  };

  return (
    <button
      type="button"
      aria-label="Cancel compaction"
      onClick={onCancel}
      disabled={cancelling}
      className={
        className ??
        "shrink-0 rounded border border-border px-2 py-0.5 text-muted-foreground hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
      }
    >
      {cancelling ? "Cancelling…" : "Cancel"}
    </button>
  );
}
