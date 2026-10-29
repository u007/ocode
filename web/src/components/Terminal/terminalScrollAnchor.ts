import type { Terminal } from "ghostty-web";

/** The terminal surface this helper needs; a narrow type keeps it unit-testable. */
type ScrollableTerminal = Pick<
  Terminal,
  "write" | "getViewportY" | "getScrollbackLength" | "scrollToLine" | "buffer"
>;

/**
 * Write pty output without yanking a scrolled-back viewport to the bottom.
 *
 * ghostty-web 0.4.0's `writeInternal` ends every write with:
 *
 *     if (this.viewportY !== 0) this.scrollToBottom();
 *
 * so whatever the user is reading is discarded on the next chunk. A streaming
 * program (Claude Code, a build log, `tail -f`) writes continuously, so a wheel
 * scroll is undone almost immediately and the terminal appears to bounce back
 * to the bottom. xterm.js (and native Ghostty) only follow output while the
 * viewport is already at the bottom.
 *
 * This wraps the write: when the user is scrolled back, restore the viewport to
 * the same CONTENT afterwards. `viewportY` counts lines above the bottom, so
 * new scrollback pushes that content `grew` lines further from the bottom —
 * adding `grew` keeps the same text on screen, and a write that adds no
 * scrollback (a spinner redrawing one line) keeps the offset unchanged.
 *
 * Only the normal buffer has scrollback. The alternate screen (vim, less, a
 * full-screen TUI) is always at the tail, so it is left alone.
 *
 * Remove this once the vendored fork stops resetting the viewport in `write`
 * (rebuild it with `web/scripts/pack-ghostty-web.sh`).
 */
export function writePreservingScrollback(term: ScrollableTerminal, text: string): void {
  const viewportY = term.getViewportY();
  if (viewportY <= 0 || term.buffer.active.type !== "normal") {
    term.write(text);
    return;
  }
  const scrollbackBefore = term.getScrollbackLength();
  // The library resets viewportY to 0 inside write(); put it back below.
  term.write(text);
  const grew = term.getScrollbackLength() - scrollbackBefore;
  term.scrollToLine(viewportY + Math.max(0, grew));
}
