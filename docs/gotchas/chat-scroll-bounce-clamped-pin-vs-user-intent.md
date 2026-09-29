---
type: Gotcha
title: 'Chat transcript scroll-bounce: clamped pin vs. user intent'
description: 'Chat transcript scroll-bounce: four stacked root causes (object-identity virtual keys, live→committed height hole, clamped pin mistaken for user intent, competing scrollTop writers) and the durable fix rules.'
tags:
  - chat
  - scroll
  - virtualizer
  - web
  - ui-glitch
  - autoscroll
  - state
timestamp: 2026-09-29T04:48:47Z
---
**Type:** Gotcha
**Files:** `web/src/components/Chat/ChatPanel.tsx`, `web/src/lib/chatItemKeys.ts`, `web/src/stores/chatStore.tsx`
**Status:** Active (all four causes fixed)
**Related:** `gotchas/autoscroll-bounce.md` (an earlier, distinct ChatPanel scroll bug: competing smooth-scroll animations + turn-end growth follow-up)

## Symptom

The web/desktop chat transcript viewport jumped **upward** repeatedly — on initial render of a long transcript and at the end of every agent turn ("bouncing the scroll position up after it render finish or agent loop finish"). The view would follow the tail during streaming, then leap up mid-stream and stop following.

Four independent causes stacked on top of each other. Fixing only one is not enough: each either *created* the height change, *amplified* it, or *misread* it as user intent.

## Root cause 1 — virtual items keyed by message-object identity

`ChatPanel.tsx` virtualizes the committed transcript with `@tanstack/react-virtual` 3.17.11. `getItemKey` used to key rows by message-object identity via a WeakMap.

The turn boundary is a `SET_MESSAGES` (`web/src/stores/chatStore.tsx`), dispatched by the `messages` SSE event handler in `web/src/lib/sessionEvents.ts`. The reducer **replaces the whole `messages` array with freshly parsed JSON objects AND clears `live` in the SAME reducer update** — so every message got a brand-new key at the end of every turn, which emptied the virtualizer's `itemSizeCache`. (That handler does have an earlier branch that dispatches `ADD_MESSAGE` per message instead, but only when the loaded window has `hasMore` and the incoming snapshot is a strict prefix that *extends* what is already loaded; on a normal turn boundary that prefix check is false, so `SET_MESSAGES` runs.)

`MERGE_SNAPSHOT` performs the same kind of destructive replacement — brand-new objects for every row, plus `live: s.turnActive ? s.live : []` — but it is **not** the turn boundary: it runs on initial load (`ChatPanel`'s transcript fetch) and from the turn watchdog/reconcile. Both actions were destroying the virtualizer's measurement cache through object-identity keys; the fix below covers both.

The list container's height **is** `virtualizer.getTotalSize()` (`style={{height: ...}}`), so it collapsed from real measured heights back to `count * estimateSize` (96px per row). The tail pin's `el.scrollTop = el.scrollHeight` was then **clamped** by the browser against the shrunken container — a real upward jump.

**Fix:** new pure helper `web/src/lib/chatItemKeys.ts` — `transcriptItemKey(windowStartServerIndex, originalIndex)` returns the **global transcript position**. Stable across the snapshot (same positions, new objects) and across `PREPEND_MESSAGES` (the store walks `windowStartServerIndex` back by exactly the prepended count).

Key stability also holds under the message cap: `MAX_SLICE_MESSAGES` is 400. When a transcript exceeds it the window slides forward — `SET_MESSAGES` recomputes `windowStartServerIndex` from how many rows the cap trimmed off the head, so head rows drop out of the window (they stop rendering) while every surviving row keeps **both** its key and its content; `PREPEND_MESSAGES` then moves the window start by exactly the prepended count, so no existing row's key shifts on either transition.

Side benefit: per-row disclosure state (tool-output collapse) is keyed off the same function, so it no longer resets on every background refresh — which finally makes an existing claim in `skills/ocode-web/SKILL.md` item 35(c) ("keyed by stable message/tool identity") true.

## Root cause 2 — the live→committed swap

Even with stable keys, `live` is rendered **non-virtualized and fully rendered**, so tearing it down while its replacements mount at `estimateSize` is a multi-thousand-pixel height hole in a single frame.

**Fix:** capture the live block's height with a `ResizeObserver` — deliberately **not** an effect, because a `getBoundingClientRect` per stream delta forces a synchronous reflow — and have `estimateSize` hand that height to the rows replacing it, spread proportionally. `measureElement` still corrects each row a frame later.

## Root cause 3 — a clamped pin is not reader intent

`el.scrollTop = el.scrollHeight` is **clamped** whenever the content is shorter than the requested offset, so the effective offset can move **DOWNWARD with no user input at all**.

Both the synchronous upward check in `handleScroll` **and** the deferred rAF recompute used to treat any decrease as "the user scrolled up" and dropped `atBottomRef`, silently killing the tail follow mid-turn and stranding the transcript above the bottom.

**Fix — the durable rule:** unpinning requires a **real gesture** recorded within `USER_SCROLL_INTENT_MS` (250ms):

- a wheel event with `deltaY < 0`
- `touchmove`
- a `pointerdown` in the scrollbar strip (content clicks don't count)
- `ArrowUp` / `PageUp` / `Home` — **skipped when the event target is an `input`/`textarea`/`select`/contentEditable**, since ArrowUp in the composer recalls prompt history and in a tool-output block navigates; counting those as scroll intent would let a clamped pin unpin the follow 250ms later.

**RE-ARMING stays UNCONDITIONAL** — otherwise the follow can never resume when the reader scrolls back down.

This is the rule most likely to be lost: **a bare offset decrease is not evidence of a user gesture.** The browser's own clamping makes the offset move without anyone touching the input.

## Root cause 4 — competing scrollTop writers

Two other components write `scrollTop` and must be suppressed while pinned:

### Native CSS scroll anchoring

The scroll surface carries `[overflow-anchor:none]` (Tailwind arbitrary property). Native anchor selection is unpredictable when rows are absolutely positioned `translateY`-transformed virtual items.

### virtual-core's estimate→measure compensation

virtual-core compensates `scrollOffset += delta` directly when a row's measured size replaces its estimate. This is an **instance field** (`shouldAdjustScrollPositionOnItemSizeChange`), **NOT a `useVirtualizer` option**, and `setOptions` never resets it — so it is assigned **during render**, not in an effect, because `measureElement` is a ref callback and the first measurements happen during the commit phase before any effect runs.

The override **GATES the library's default rather than replacing it**:

- **Pinned** → return `false` (our pin is the only writer).
- **Unpinned** → reproduce virtual-core's own two-branch condition:
  - first measure → `item.start < offset`
  - re-measure → `item.start + item.size <= offset && scrollDirection !== "backward"`

Returning a bare `!atBottomRef.current` **is a bug**: it compensates rows growing BELOW the fold for a scrolled-up reader and drags their view — the exact regression virtual-core's default avoids.

Note: `getScrollOffset()` is private — the public field `scrollOffset` is the equivalent.

## Tests

- `web/src/lib/chatItemKeys.test.ts` — key stability across a snapshot and across prepends, and the negative-window-start fallback.
- `ChatPanel.test.tsx`:
  - two disclosure-persistence tests, one per destructive action: "keeps a row's disclosure state across the turn-end snapshot that replaces every message object" (`MERGE_SNAPSHOT`, initial-load/watchdog path) and "keeps a row's disclosure state across the turn-boundary SET_MESSAGES" (the real turn-end action). Both **FAIL against the old object-identity keying** (each verified by temporary revert);
  - "does NOT unpin when the offset falls without a user gesture (our own clamped pin)" — also mutation-verified;
  - a wheel-up test proving unpinning still works (counterpart to the clamped-pin test, so the intent gate doesn't just permanently latch the pin).
- **Five existing scroll tests were changed** to dispatch a real gesture via a `userScrollsUp(el)` helper. Their comments already said "the reader wheel-scrolls up" but they only moved the offset and fired `scroll` — exactly the false positive this fix removes. Their INTENT was preserved; only the gesture was made explicit.

## Known coverage gap (honest)

The virtual-core fold arithmetic in the `shouldAdjustScrollPositionOnItemSizeChange` override is **NOT regression-covered**: a compiling mutant that drops the fold check survives the whole suite, because jsdom has no layout engine — every row measures 96px via a stubbed `offsetHeight` and there is no real scroll geometry. It needs a real-browser check before it can be considered pinned by tests.