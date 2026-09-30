/**
 * Pure helpers for the alt+up / alt+down jump between the user's own messages
 * in the transcript (web + desktop, which share this UI).
 *
 * This is NOT the composer's plain up/down input-history walk in ChatInput —
 * that one walks what you *typed*, this one walks what you *sent*.
 *
 * The authoritative index list comes from the server
 * (GET /api/sessions/{id}/user-messages, handler_session_user_messages.go) for
 * two reasons: the client only holds a tail window of the transcript, so a
 * client-side count would disagree with the TUI on the same session; and the
 * server and TUI share one definition of what counts as a user message, so
 * "msg 3/17" means the same thing on both surfaces.
 */

/** Cursor sentinel meaning "no walk in progress". */
export const NO_USER_JUMP = -1;

/**
 * Move the jump cursor by `dir` (-1 older, +1 newer).
 *
 * Mirrors the TUI's userJumpStep exactly, including the two decisions that are
 * easy to get wrong:
 *
 * - The seed. `cursor === -1` means "not in the list yet", and the first press
 *   in EITHER direction enters at the NEWEST message. The user is at the
 *   bottom of the conversation, so the newest is the nearest entry point; this
 *   keeps the two keys symmetric and means neither is a silent no-op.
 * - No wrap-around. The ctrl+f find bar wraps, but wrapping is disorienting
 *   when walking a document in one direction: alt+up past the oldest
 *   teleports you to the newest and the next alt+up appears dead. Clamping
 *   makes the ends feel like ends.
 */
export function nextUserJumpCursor(
  cursor: number,
  total: number,
  dir: -1 | 1,
): number {
  if (total <= 0) return NO_USER_JUMP;
  if (cursor < 0) return total - 1;
  const stepped = cursor + dir;
  if (stepped < 0) return 0;
  if (stepped > total - 1) return total - 1;
  return stepped;
}

/**
 * The status readout, e.g. "msg 3/17". Empty when no walk is in progress so
 * the caller can render nothing at all rather than "msg 0/0".
 */
export function userJumpLabel(cursor: number, total: number): string {
  if (cursor < 0 || total <= 0) return "";
  return `msg ${cursor + 1}/${total}`;
}

/**
 * Client-side mirror of the server's isCountableUserMessage, used only as the
 * fallback when the server index list is unavailable (older server, offline,
 * a failed request) — in which case the jump degrades to the messages already
 * in the loaded window rather than becoming dead.
 *
 * Kept deliberately identical to the Go predicate: role "user", not a
 * slash-command echo. Divergence here would make the web readout count
 * differently from the TUI's on the same session, which is exactly the
 * divergence the endpoint exists to remove.
 */
export function isCountableUserMessage(msg: { role?: string; content?: string }): boolean {
  if (msg.role !== "user") return false;
  return !(msg.content ?? "").trimStart().startsWith("/");
}

/**
 * Server indices of the countable user messages inside an already-loaded
 * message array, ascending. Only used for the no-endpoint fallback described
 * on isCountableUserMessage; the primary path is the server list.
 */
export function loadedUserMessageIndices(
  messages: ReadonlyArray<{ role?: string; content?: string }>,
): number[] {
  const out: number[] = [];
  for (let i = 0; i < messages.length; i++) {
    if (isCountableUserMessage(messages[i])) out.push(i);
  }
  return out;
}
