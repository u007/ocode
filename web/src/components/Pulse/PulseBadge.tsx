import { usePulse } from "@/stores/pulseStore";

/**
 * PulseBadge — the always-visible "how much is happening" pill in the app
 * header. Clicking it opens the dashboard.
 *
 * It renders NOTHING when both counts are zero. A permanent "● 0 · ◆ 0" would
 * train the user to ignore the one surface whose entire purpose is telling them
 * something needs them, which is the opposite of what a badge is for.
 */
export function PulseBadge({
  onClick,
  countsOverride,
}: {
  onClick: () => void;
  /** Test seam: supply counts instead of reading the store. */
  countsOverride?: { running: number; needsYou: number };
}) {
  const { counts } = usePulse();
  const c = countsOverride ?? counts;
  if (c.running === 0 && c.needsYou === 0) return null;

  const parts: string[] = [];
  if (c.running > 0) parts.push(`● ${c.running}`);
  if (c.needsYou > 0) parts.push(`◆ ${c.needsYou}`);

  const needPhrase = c.needsYou === 1 ? "1 session needs you" : `${c.needsYou} sessions need you`;
  const runPhrase = c.running === 1 ? "1 running" : `${c.running} running`;

  return (
    <button
      type="button"
      onClick={onClick}
      data-testid="pulse-badge"
      // The glyphs mean nothing to a screen reader, so the label carries the
      // counts as words.
      aria-label={`Open Pulse: ${needPhrase}, ${runPhrase}`}
      className="shrink-0 h-7 px-2 rounded-md text-xs tabular-nums text-muted-foreground hover:text-foreground hover:bg-accent transition-colors whitespace-nowrap"
    >
      {parts.join(" · ")}
    </button>
  );
}
