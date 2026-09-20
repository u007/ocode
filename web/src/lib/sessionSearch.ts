import type { Message } from "../api/types";

/**
 * Full-transcript search helpers for the in-chat find bar.
 *
 * The find bar matches over the LOADED window client-side (instant, no
 * network), but that window is capped (`MAX_SLICE_MESSAGES`) — so on a long
 * session earlier hits were invisible. `GET /api/sessions/{id}/search` returns
 * indices across the WHOLE transcript; this module turns those server indices
 * into a navigable target list and the fetch needed to reach an off-window hit.
 *
 * Kept separate from ChatPanel so the index arithmetic (the part that is easy
 * to get subtly wrong) is unit-testable without rendering.
 */

/** Shape of GET /api/sessions/{id}/search. */
export interface ServerSearchResult {
  /** Total matches in the full transcript (may exceed `indices.length`). */
  total: number;
  /** Ascending message positions in the server's post-load array. */
  indices: number[];
  /** The scan hit its cap; more matches exist beyond `indices`. */
  truncated: boolean;
  /** Transcript length the indices refer to. */
  scanned: number;
}

/** One navigable search hit. */
export interface SearchJumpTarget {
  /** Server transcript index, or -1 for a purely-local (unanchored) match. */
  serverIndex: number;
  /** Render-entry position when loaded, or -1 when the message must be fetched. */
  entryPos: number;
}

/**
 * Translate a server transcript index into a position in the loaded window, or
 * -1 when that message is not currently loaded.
 *
 * `windowStartServerIndex` is the server index of `messages[0]` (maintained by
 * the store). It is -1 when the anchor is unknown — e.g. a slice populated
 * purely by live SSE before its first page resolved — in which case we refuse
 * to guess rather than risk highlighting the wrong bubble.
 */
export function serverIndexToLocal(
  serverIndex: number,
  messages: Message[],
  windowStartServerIndex: number,
): number {
  if (windowStartServerIndex < 0) return -1;
  const local = serverIndex - windowStartServerIndex;
  if (local < 0 || local >= messages.length) return -1;
  return local;
}

/**
 * The fetch needed to bring an out-of-window match into the loaded window.
 *
 * The window is a contiguous TAIL block, so the only way to include an older
 * match without leaving a gap is to load the contiguous prefix from the match
 * up to the window start. `getSession` paginates with `offset` = "skip this
 * many from the END", so:
 *
 *   - `offset = totalMessages - windowStartServerIndex` is the number of
 *     SERVER messages currently loaded (the server's transcript is what
 *     `totalMessages` and `windowStartServerIndex` both describe), so the
 *     server serves the block ending exactly at `windowStart`. It is NOT
 *     `messages.length`: client-only `ADD_MESSAGE` injections (/help, /recap
 *     status, command notices — see the `windowStartServerIndex` comment in
 *     chatStore.tsx) inflate the local array without being persisted, so using
 *     it would skip too far and leave a gap.
 *   - `limit = windowStart - matchIndex` then walks the start back to the
 *     match, returning exactly `[matchIndex, windowStart)`.
 *
 * The caller prepends those via `PREPEND_MESSAGES` (cap-exempt), leaving a
 * contiguous transcript. Returns null when no fetch is needed or possible: the
 * match is already loaded, the anchor is unknown, or the match is NEWER than
 * the window (a non-tail/pinned window cannot be fixed by prepending).
 *
 * NOTE (bounded-window tradeoff): `limit` is deliberately uncapped. A hit far
 * back in a very long transcript therefore materialises the whole prefix in one
 * jump, bypassing `MAX_SLICE_MESSAGES`. That is intentional — the point of the
 * full-transcript search is to reach old hits, and `PREPEND_MESSAGES` is
 * cap-exempt for that reason; the alternative (capping the block) would need
 * multi-round-trip iteration and per-target bookkeeping. The server cost is
 * unchanged either way (`PaginatedLoad` already parses the whole file per
 * page), so only the client array size differs.
 */
export function olderPrefixFetch(
  matchIndex: number,
  windowStartServerIndex: number,
  totalMessages: number,
): { limit: number; offset: number } | null {
  if (matchIndex < 0 || windowStartServerIndex < 0) return null;
  // In the loaded window (or newer than it) — nothing to prepend.
  if (matchIndex >= windowStartServerIndex) return null;
  // An anchor beyond the server total means the two counts disagree (a
  // mid-turn/live slice); refusing to guess beats prepending a shifted block.
  if (totalMessages < windowStartServerIndex) return null;
  const limit = windowStartServerIndex - matchIndex;
  if (limit <= 0) return null;
  return { limit, offset: totalMessages - windowStartServerIndex };
}

/**
 * Count how many server-reported matches are already inside the loaded,
 * NAVIGABLE window. Uses the render-entry map rather than raw before/after
 * bounds: a sentinel (QUESTION_PROMPT/PERMISSION_ASK) or a tool result folded
 * into its parent group occupies a server index but has no navigable target,
 * so counting it as "in view" would overstate coverage and suppress the note
 * while real hits are still off-window.
 */
export function inWindowMatchCount(
  indices: number[],
  entryPosByServerIndex: Map<number, number>,
): number {
  let n = 0;
  for (const idx of indices) {
    if (entryPosByServerIndex.has(idx)) n++;
  }
  return n;
}

/**
 * Build the ordered list of navigable hit targets.
 *
 * `serverIndices` is `null` until the debounced full-transcript query resolves
 * (or when it failed / the tab has no server session yet); in that window we
 * fall back to the in-window matches so typing still highlights instantly.
 *
 * Tool results are folded into their parent assistant bubble, so several
 * server indices (the assistant call and its result) can resolve to the SAME
 * render-entry position. Those collapse to one target — otherwise "next" would
 * select a duplicate and appear to do nothing.
 *
 * An unanchored window (`windowStartServerIndex < 0`) cannot translate server
 * indices reliably, so it too uses in-window matches only.
 */
export function buildJumpTargets(opts: {
  serverIndices: number[] | null;
  entryPosByServerIndex: Map<number, number>;
  localEntryPositions: number[];
  windowStartServerIndex: number;
}): SearchJumpTarget[] {
  const { serverIndices, entryPosByServerIndex, localEntryPositions, windowStartServerIndex } = opts;
  const fallback = () =>
    localEntryPositions.map((p) => ({ serverIndex: -1, entryPos: p }));
  if (serverIndices === null || windowStartServerIndex < 0) return fallback();

  const out: SearchJumpTarget[] = [];
  const seenEntryPos = new Set<number>();
  for (const si of serverIndices) {
    const pos = entryPosByServerIndex.get(si) ?? -1;
    if (pos >= 0) {
      if (seenEntryPos.has(pos)) continue;
      seenEntryPos.add(pos);
    }
    out.push({ serverIndex: si, entryPos: pos });
  }
  return out;
}
