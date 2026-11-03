/**
 * Navigation keys from a PHONE SOFT KEYBOARD, encoded for the pty.
 *
 * WHY this exists: ghostty-web encodes a navigation key from `event.code` only
 * (`mapKeyCode(ev.code)` → escape sequence), and its keydown handler returns
 * immediately for `keyCode === 229` — the marker Android puts on EVERY
 * soft-keyboard event. Phone keyboards that expose an arrow row (SwiftKey
 * delegates it through `InputConnection.sendKeyEvent`, Gboard's cursor keys
 * likewise) therefore produce a keydown that ghostty-web drops: no `code` it
 * recognises, or the 229 bail-out, so nothing reaches the encoder and no
 * `preventDefault` happens either. Ordinary text still works because it rides
 * `beforeinput`, not `keydown` — which is why typing looked fine while arrows
 * were dead.
 *
 * The same drop applies to Backspace: its keydown is ignored on the 229 marker,
 * so `softKeyboardBackspace` covers it.
 *
 * The panel owns those leftovers in a capture-phase listener and feeds the
 * sequence back through `term.input(..., true)` (the typed-input path), so the
 * pty sees exactly what a hardware key would send.
 */

interface NavKeySequence {
  /** Normal cursor-key form (`ESC [ A`). */
  dec: string;
  /** Application cursor keys (DECCKM / DEC mode 1), used by vim, less, tmux. */
  app: string;
}

/**
 * Keys that produce no text (so `beforeinput` never sees them) and are safe to
 * synthesise. Enter/Tab are deliberately absent: phone keyboards deliver those
 * through the text path, and claiming them here would double-send. Backspace is
 * handled by `softKeyboardBackspace` below.
 */
const SEQUENCES: Record<string, NavKeySequence> = {
  ArrowUp: { dec: "\x1b[A", app: "\x1bOA" },
  ArrowDown: { dec: "\x1b[B", app: "\x1bOB" },
  ArrowRight: { dec: "\x1b[C", app: "\x1bOC" },
  ArrowLeft: { dec: "\x1b[D", app: "\x1bOD" },
  Home: { dec: "\x1b[H", app: "\x1bOH" },
  End: { dec: "\x1b[F", app: "\x1bOF" },
  PageUp: { dec: "\x1b[5~", app: "\x1b[5~" },
  PageDown: { dec: "\x1b[6~", app: "\x1b[6~" },
  Insert: { dec: "\x1b[2~", app: "\x1b[2~" },
  Delete: { dec: "\x1b[3~", app: "\x1b[3~" },
};

/** Some Android soft keyboards send `key: "Unidentified"` but keep the code. */
const NAME_BY_KEYCODE: Record<number, string> = {
  38: "ArrowUp",
  40: "ArrowDown",
  39: "ArrowRight",
  37: "ArrowLeft",
  36: "Home",
  35: "End",
  33: "PageUp",
  34: "PageDown",
  45: "Insert",
  46: "Delete",
};

/** The IME marker Android soft keyboards stamp on every synthesized key. */
const IME_KEYCODE = 229;

export interface SoftKeyboardEventShape {
  key: string;
  code: string;
  keyCode: number;
  ctrlKey: boolean;
  altKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
}

/**
 * True when ghostty-web must NOT own the event: it is IME-marked, or carries no
 * usable physical `code`. A hardware key with a real `code` stays with
 * ghostty-web's encoder, so intercepting it would double-send.
 */
function looksLikeSoftKeyboard(ev: SoftKeyboardEventShape): boolean {
  return ev.keyCode === IME_KEYCODE || !ev.code || ev.code === "Unidentified";
}

/**
 * Returns the bytes to send for a soft-keyboard navigation key, or null when
 * the event should be left to ghostty-web (or to the browser).
 *
 * @param applicationCursor `term.getMode(1, false)` — DECCKM.
 */
export function softKeyboardNavigationSequence(
  ev: SoftKeyboardEventShape,
  applicationCursor: boolean,
): string | null {
  // A modified nav key is a different escape sequence (Shift+Arrow selects,
  // Ctrl+Arrow moves by word) and ghostty-web's encoder has to own it: we can
  // only synthesise the unmodified forms, and sending a plain arrow for a
  // modified one is worse than sending nothing. Phone keyboards produce no
  // modifiers, so this only protects the hardware/IME path.
  if (ev.ctrlKey || ev.altKey || ev.metaKey || ev.shiftKey) return null;

  const byKey = SEQUENCES[ev.key];
  const byCode = ev.code ? SEQUENCES[ev.code] : undefined;
  const name = byKey ? ev.key : byCode ? ev.code : NAME_BY_KEYCODE[ev.keyCode];
  const sequence = name ? SEQUENCES[name] : undefined;
  if (!sequence) return null;
  // ghostty-web handles the event itself whenever it gets a physical `code`
  // and the event is not IME-marked — INCLUDING codes this table does not name
  // (keypad arrows, F-keys, Kitty encodings). Intercepting those would bypass
  // its encoder and could double-send, so only soft-keyboard-shaped events
  // are ours (see `looksLikeSoftKeyboard`).
  if (!looksLikeSoftKeyboard(ev)) return null;

  return applicationCursor ? sequence.app : sequence.dec;
}

/**
 * Returns DEL (`\x7f`) for a soft-keyboard Backspace, or null when the event
 * should be left to ghostty-web (a hardware Backspace) or to the browser.
 *
 * Same drop as the nav keys: ghostty-web ignores a Backspace keydown on the
 * 229 marker, so nothing reaches the pty. A hardware Backspace (real `code`,
 * no marker) is ghostty-web's to send — returning DEL here would double it.
 */
export function softKeyboardBackspace(ev: SoftKeyboardEventShape): string | null {
  // Modified Backspace (Ctrl/Alt/Meta/Shift) is a different byte sequence
  // (word delete) that ghostty-web's encoder owns; phone keyboards send none.
  if (ev.ctrlKey || ev.altKey || ev.metaKey || ev.shiftKey) return null;
  if (ev.key !== "Backspace" && ev.keyCode !== 8) return null;
  if (!looksLikeSoftKeyboard(ev)) return null;
  return "\x7f";
}
