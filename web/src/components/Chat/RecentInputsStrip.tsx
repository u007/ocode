/**
 * RecentInputsStrip — a quiet, read-only reminder of the last couple of things
 * the user typed, rendered above the chat composer (web + desktop, which share
 * this UI).
 *
 * It answers exactly one situation: the transcript is scrolled away from its
 * tail, so your own prompts are no longer on screen. Long turns and
 * post-compaction transcripts make that easy to lose, and re-deriving what you
 * asked for is expensive.
 *
 * Deliberately NOT interactive — no click-to-restore, no hover actions. The
 * transcript's per-message "Restore to input" button and the composer's ↑/↓
 * input history are the existing restore paths; a third one here would be a
 * competing affordance for the same job. The full text is on the `title`
 * tooltip, and the line is truncated to one row so the band cannot grow.
 *
 * Purely presentational: the parent (ChatInput) decides WHICH inputs and
 * WHETHER to show, using `recentUserInputs` (lib/recentInputs.ts) and the
 * per-session `transcriptScrolledUp` signal from ChatPanel's scroll handler.
 */

interface Props {
  /** Oldest → newest, already filtered to real typed input. */
  inputs: string[];
}

/** Newlines would make one input render as several rows and grow the band. */
function oneLine(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}

export default function RecentInputsStrip({ inputs }: Props) {
  const lines = inputs.map(oneLine).filter((text) => text.length > 0);
  if (lines.length === 0) return null;

  return (
    <ul
      data-testid="recent-inputs"
      aria-label="Recent inputs"
      className="mb-1 space-y-0.5"
    >
      {lines.map((text, i) => (
        <li
          key={`${i}:${text.slice(0, 24)}`}
          title={text}
          className="flex gap-1.5 text-xs text-muted-foreground/70"
        >
          <span aria-hidden="true" className="shrink-0 select-none">
            ›
          </span>
          {/* truncate (not line-clamp) keeps the band exactly one row per input
              no matter how long the prompt was. */}
          <span className="truncate">{text}</span>
        </li>
      ))}
    </ul>
  );
}
