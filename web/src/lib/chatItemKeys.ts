/**
 * Virtual-item keys for the chat transcript.
 *
 * `Message` carries no durable id, so the virtualizer needs a synthetic key
 * that is (a) unique within the rendered window and (b) STABLE across store
 * updates that replace the message objects wholesale.
 *
 * Object identity is not stable enough. The turn-boundary snapshot is a
 * `SET_MESSAGES` (the turn boundary) and `MERGE_SNAPSHOT` (initial load and
 * the turn watchdog), each of which replaces the whole `messages` array with
 * parsed JSON and clears `live` in the same reducer update. Keying on object
 * identity therefore hands every message a brand-new key at the end of every
 * turn, which empties the virtualizer's `itemSizeCache`. The list container's
 * height IS `getTotalSize()`, so it collapses from real measured heights back
 * to `count * estimateSize`; the pin's `el.scrollTop = el.scrollHeight` is then
 * clamped against the shrunken container and the viewport jumps upward. That is
 * the "chat bounces up when the loop finishes" report.
 *
 * The global transcript position (window start + local index) is stable across
 * both of the two operations that rewrite the array:
 *   - `SET_MESSAGES` / `MERGE_SNAPSHOT` — a page of the same transcript, so the
 *     start and every local index land on the same global position even though
 *     every object is new;
 *   - `PREPEND_MESSAGES` — the store advances the window start backwards by
 *     exactly the number of prepended rows, so existing global positions do not
 *     move.
 *
 * A key only has to be unique and stable; it is never used to look a message up
 * in the server transcript, so it stays correct after a rewind or truncate even
 * though the underlying rows have genuinely moved.
 */

/** Sentinel for "the window start is not known yet" (a slice built purely from
 *  SSE deltas before its first page resolved). Mirrors the store's own use of
 *  `windowStartServerIndex < 0` to mean unknown. */
export const UNKNOWN_WINDOW_START = -1;

/**
 * Virtual-item key for a rendered transcript row.
 *
 * @param windowStartServerIndex Server index of `messages[0]`, or a negative
 *   value when unknown.
 * @param originalIndex Index of the row within the loaded `messages` array.
 */
export function transcriptItemKey(windowStartServerIndex: number, originalIndex: number): number {
  if (windowStartServerIndex < 0) {
    // No anchor yet: local position is the best available identity. This only
    // applies to a slice that has never received a page, so there is no
    // previous key to be inconsistent with.
    return originalIndex;
  }
  return windowStartServerIndex + originalIndex;
}
