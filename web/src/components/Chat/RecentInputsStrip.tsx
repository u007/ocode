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
 * ── Why the "Sent — not queued" label ──────────────────────────────────────
 * `ChatInput` renders the queued-message list in this same stack (below the
 * attach pills), and a queued row is also one line of muted message text. Two
 * bare lines here were therefore read as pending messages. The strip now names
 * itself and every row starts with a send glyph, which the queue rows never do
 * (they are numbered, clickable buttons under an "N queued — …" hint). Do not
 * drop the label: without it these lines carry no positive statement that they
 * were already sent. Keep the strip non-interactive — the label must never
 * imply "click to send".
 *
 * Purely presentational: the parent (ChatInput) decides WHICH inputs and
 * WHETHER to show, using `recentUserInputs` (lib/recentInputs.ts) and the
 * per-session `transcriptScrolledUp` signal from ChatPanel's scroll handler.
 */

import { Send } from "lucide-react";
import { useId } from "react";

interface Props {
  /** Oldest → newest, already filtered to real typed input. */
  inputs: string[];
}

/** Newlines would make one input render as several rows and grow the band. */
function oneLine(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}

export default function RecentInputsStrip({ inputs }: Props) {
  // All chat tabs stay mounted, so a fixed id would be duplicated in the DOM.
  const hintId = useId();
  const lines = inputs.map(oneLine).filter((text) => text.length > 0);
  if (lines.length === 0) return null;

  return (
    <div data-testid="recent-inputs" className="mb-1">
      <div
        id={hintId}
        className="mb-0.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground/60"
      >
        Sent — not queued
      </div>
      <ul
        aria-label="Recent inputs"
        aria-describedby={hintId}
        className="space-y-0.5"
      >
        {lines.map((text, i) => (
          <li
            key={`${i}:${text.slice(0, 24)}`}
            title={text}
            className="flex items-center gap-1.5 text-xs text-muted-foreground/70"
          >
            {/* A send glyph, never a number or a chevron: the queued list below
                is numbered and clickable, and this line is already in the
                transcript. */}
            <Send
              className="h-3 w-3 shrink-0 text-muted-foreground/50"
              aria-hidden="true"
            />
            {/* truncate (not line-clamp) keeps the band exactly one row per input
                no matter how long the prompt was. */}
            <span className="truncate">{text}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
