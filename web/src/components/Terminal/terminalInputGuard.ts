/**
 * Suppresses a soft-keyboard (IME) commit that arrives TWICE.
 *
 * WHY this exists. ghostty-web de-duplicates a repeated commit only inside its
 * own 100 ms window (`BEFORE_INPUT_IGNORE_MS` in the vendored build). Measured
 * on the real terminal on 2026-11-02 (Chromium + CDP `Input.imeSetComposition`
 * then `Input.insertText`, the Android soft-keyboard event shape):
 *
 *   - re-commit 60 ms after the first  -> frames to the pty: ["ls"]
 *   - re-commit 250 ms after the first -> frames to the pty: ["ls", "ls"]
 *
 * The event sequence is identical in both cases; only the timing differs. A
 * phone's main thread is busy enough that the second delivery is routine, so the
 * word reaches the pty twice and the shell runs the command twice.
 *
 * SCOPE. Deliberately narrow, because swallowing real input is worse than
 * showing a duplicate:
 *   - only a payload equal to a recorded `compositionend` data can ever be
 *     suppressed. Keydown-derived text (one frame per keystroke, and every
 *     key-repeat repeat) and pastes never match a composition commit, so they
 *     are never inspected;
 *   - it must ALSO equal the previous accepted payload, i.e. nothing was typed
 *     between the two (an Enter, space or any other frame clears the record);
 *   - only payloads of 2+ characters are considered: a single character is the
 *     normal unit of deliberate repetition (and of key repeat), so it is never
 *     suppressed, while a repeated WORD with nothing in between is not
 *     something a human types twice;
 *   - frames carrying C0 control characters (Enter, arrows, bracketed paste)
 *     are always accepted;
 *   - past the window an identical word is accepted again, so deliberately
 *     repeating a word still works.
 */

/** How long after an accepted commit an identical re-commit is still treated as
 *  the same word. A phone re-commit measured at 250 ms. 600 ms is the narrowest
 *  window the duplicate tests allow (a re-commit at 600 ms must still drop), so
 *  a repeated word typed a moment later is not swallowed. No event-level signal
 *  separates a re-delivery from a real repeat (both are a `compositionend` with
 *  the same text), so the window is the only discriminator. */
export const IME_DUPLICATE_WINDOW_MS = 600;

const C0_CONTROLS = /[\u0000-\u001f\u007f]/;

function nowMs(): number {
  return typeof performance !== "undefined" ? performance.now() : Date.now();
}

export interface TerminalInputGuard {
  /** Record the text of a `compositionend` seen on the terminal's input element. */
  noteComposition(text: string): void;
  /** True when `data` should be sent to the pty / browser socket. */
  accept(data: string, now?: number): boolean;
  /** A non-composition input (keystroke, paste) was sent: the pending commit is over. */
  noteInput(): void;
  /** Forget everything (a new socket or a new terminal). */
  reset(): void;
}

export function createTerminalInputGuard(windowMs: number = IME_DUPLICATE_WINDOW_MS): TerminalInputGuard {
  // The last composed text we were told about, and the last frame we let
  // through. Both are needed: the first says "this text came from an IME", the
  // second says "nothing has been typed since it went out".
  let committed: string | null = null;
  let lastAccepted: string | null = null;
  let lastAcceptedAt = Number.NEGATIVE_INFINITY;

  // Clearing `committed` alone is not enough: a later commit re-arms it, and a
  // stale `lastAccepted` would still match, so both must go.
  const endCommit = () => {
    committed = null;
    lastAccepted = null;
    lastAcceptedAt = Number.NEGATIVE_INFINITY;
  };

  return {
    noteComposition(text) {
      // An empty commit ("composition cancelled") arms nothing.
      committed = text === "" ? null : text;
    },

    accept(data, now = nowMs()) {
      if (data === "") return true;
      const isDuplicate =
        committed !== null &&
        data === committed &&
        data === lastAccepted &&
        data.length >= 2 &&
        now - lastAcceptedAt <= windowMs &&
        !C0_CONTROLS.test(data);
      if (isDuplicate) return false;

      lastAccepted = data;
      lastAcceptedAt = now;
      // Any other frame means the commit is over; a later identical word is a
      // new word, not a re-delivery of this one.
      if (data !== committed) committed = null;
      return true;
    },

    noteInput: endCommit,
    reset: endCommit,
  };
}

/**
 * Frames the TERMINAL writes back on its own, through the same `onData` path a
 * keystroke takes: focus-in/out reports (`ESC [ I` / `ESC [ O`), primary and
 * secondary device-attribute replies (`CSI ? … c` / `CSI > … c`), cursor-position
 * reports (`CSI row ; col R`) and mouse reports (SGR `CSI < … M|m`, X10 `CSI M`
 * followed by three bytes). None of them is typing, so none may raise the
 * "input is paused" notice. A frame counts only when it is made ENTIRELY of these.
 */
const TERMINAL_REPLY =
  /^(?:\x1b\[[IO]|\x1b\[[?>][0-9;]*c|\x1b\[\??[0-9]+;[0-9]+R|\x1b\[<[0-9;]*[Mm]|\x1b\[M[\s\S]{3})+$/;

export function isTerminalGeneratedReply(data: string): boolean {
  return TERMINAL_REPLY.test(data);
}
