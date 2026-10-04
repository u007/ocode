/**
 * Commands the composer dispatches IMMEDIATELY even while a turn is running,
 * instead of parking them in the per-tab queue (`lib/tabQueue`).
 *
 * TUI parity: the TUI has the same concept as its `isInstantCmd` chain
 * (`internal/tui/model.go`), where `/btw` has been instant since 0.8.55.
 *
 * Unlike the TUI, the web queued EVERY slash command while busy — so an aside
 * typed during a long turn sat invisible until the turn ended.
 *
 * Membership is a persistence-safety decision, not a convenience one. A
 * command belongs here only if the server can act on it without writing to the
 * session transcript underneath a live turn. Writing mid-turn is what breaks
 * persistence: the stored transcript stops being a prefix of the in-memory
 * snapshot, so every later live snapshot is dropped
 * (`session.liveAppendStart` → `samePrefix` bails when the snapshot is
 * shorter) and the turn-end sync save reports `ErrTranscriptConflict` — and
 * both failures are only logged, silently losing the rest of the turn.
 *
 * So: only add a command here once you have confirmed its server handler has a
 * mid-turn path that keeps the message inside `as.messages` (the shape
 * `Handler.tryEnqueueInjection` provides). A command that starts or mutates a
 * turn must stay queued.
 */
const INSTANT_COMMANDS = new Set(["/btw", "/by-the-way"]);

/**
 * True when `text` is a slash command that must run now rather than queue.
 * Compares case-insensitively on the command word only, so `/btw hello` and
 * `/BTW hello` both match and `/btwx` does not.
 */
export function isInstantCommand(text: string): boolean {
  return INSTANT_COMMANDS.has(text.trim().split(/\s+/, 1)[0].toLowerCase());
}
