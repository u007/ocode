import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { useSessionActivity } from "../../lib/commandActivity";
import { isCompactCommand } from "../../lib/compactionState";
import CompactionCancelButton from "./CompactionCancelButton";

/**
 * A fast command never paints, so instant handlers (/yolo, /effort, /clear)
 * produce no visible bar at all while a genuinely slow one (/recap, /share,
 * /mask) always surfaces. This is a RENDER-side delay only: activity is always
 * recorded in the store, so nothing is excluded from tracking — the bar is
 * simply not mounted for work that finished before a human could notice.
 */
const MOUNT_DELAY_MS = 400;

/**
 * Composer bar reporting the one thing currently running for this session: a
 * blocking client-side slash command, or the skill the model loaded this turn.
 *
 * Deliberately informational — it does NOT feed ChatInput's `busy` flag, so it
 * never changes send/queue behaviour. Sibling of CompactionStatus and styled to
 * match, so the two read as one system above the composer. When both could
 * apply — this client running its own `/compact` — CompactionStatus stands down
 * so the composer never shows two identical spinner rows.
 */
export default function CommandActivityBar({ sessionId, host }: { sessionId?: string | null; host?: string }) {
  const activity = useSessionActivity(sessionId);
  const [revealed, setRevealed] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  // Hold the bar back until the work has lasted long enough to be worth
  // reporting. The timer is keyed on the identity of the activity entry, so a
  // new activity restarts the wait and a cleared one cancels it — this is also
  // what keeps a timer from a previous session from firing after a tab switch.
  const identity = activity ? `${activity.kind}:${activity.startedAt}` : null;
  useEffect(() => {
    if (!identity) {
      setRevealed(false);
      return;
    }
    setRevealed(false);
    const timer = window.setTimeout(() => setRevealed(true), MOUNT_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [identity]);

  // Elapsed ticker, active only while the bar is actually on screen.
  useEffect(() => {
    if (!activity || !revealed) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [activity, revealed]);

  if (!activity || !revealed) return null;

  // The label already reads `Skill "name"`, so one "Running" prefix serves both
  // shapes without repeating the word.
  const label = activity.kind === "command" ? activity.label : `Skill "${activity.name}"`;
  const elapsed = Math.max(0, Math.floor((now - activity.startedAt) / 1000));

  return (
    <div
      className="mb-2 flex items-center gap-2 rounded-md border border-border bg-muted px-3 py-2 text-xs"
      role="status"
    >
      <Loader2 aria-hidden="true" className="h-4 w-4 shrink-0 animate-spin" />
      {/* truncate + min-w-0: a long command or skill name must never grow the
          composer's bottom chrome past one row. */}
      <div className="min-w-0 flex-1 truncate" title={label}>
        <span className="text-muted-foreground">Running</span>{" "}
        <span className="font-medium text-foreground">{label}</span>{" "}
        <span className="text-muted-foreground">· {elapsed}s elapsed</span>
      </div>
      {/* A manual /compact can be slow enough to cancel; other commands and
          skill loads have no cancel affordance. */}
      {activity.kind === "command" && isCompactCommand(activity.label) && (
        <CompactionCancelButton sessionId={sessionId} host={host} />
      )}
    </div>
  );
}

export { MOUNT_DELAY_MS };