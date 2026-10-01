---
type: Gotcha
title: 'Chat transcript scroll-bounce: clamped pin vs. user intent'
description: 'Chat transcript scroll-bounce: six stacked root causes (object-identity virtual keys, live→committed height hole, clamped pin mistaken for user intent, competing scrollTop writers, un-anchored window slides for a scrolled-up reader, duplicate pane keys from a session open under two projects) and the durable fix rules.'
tags:
  - chat
  - scroll
  - virtualizer
  - web
  - ui-glitch
  - autoscroll
  - state
timestamp: 2026-10-01T01:10:00Z
---
**Type:** Gotcha
**Files:** `web/src/components/Chat/ChatPanel.tsx`, `web/src/lib/chatItemKeys.ts`, `web/src/stores/chatStore.tsx`
**Status:** Active (all six causes fixed)
**Related:** `gotchas/autoscroll-bounce.md` (an earlier, distinct ChatPanel scroll bug: competing smooth-scroll animations + turn-end growth follow-up)

## Symptom

The web/desktop chat transcript viewport jumped **upward** repeatedly — on initial render of a long transcript and at the end of every agent turn ("bouncing the scroll position up after it render finish or agent loop finish"). The view would follow the tail during streaming, then leap up mid-stream and stop following.

Six independent causes stacked on top of each other (5 and 6 were found on 2026-10-01, after the first four were fixed and the report came back as "still bounces"). Fixing only one is not enough: each either *created* the height change, *amplified* it, or *misread* it as user intent.

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
## Root cause 5 — window slides move rows above a scrolled-up reader (2026-10-01)

Stable keys keep *measured heights*; they do not keep the reader's *place*. The
loaded window is not fixed:

- the turn-end `messages` broadcast carries the whole transcript, so on a
  session longer than the 100-row initial page it replaces that page with the
  `MAX_SLICE_MESSAGES` (400) window — ~300 rows land ABOVE the reader at the
  96px estimate (`SET_MESSAGES`, `windowStartServerIndex` 428 → 136 in the trace);
- a few seconds later the reconcile `MERGE_SNAPSHOT` replaces it with a tail page
  again (400 → 100 rows removed from the top);
- once the window sits at the cap, every appended message trims one head row.

None of those touch `scrollTop`, so a reader who scrolled up (the "auto bounce
back up on completion" report) saw their content jump by the height of the rows
that came or went above them. Worse, the shrink tripped the `[messages, live]`
effect's "list got shorter ⇒ transcript reset" rule and **pinned them to the
bottom**. Traced live with a 528-message session: `first row 52 → 182` and
`distance-from-bottom 1.6k → 15.8k` at turn end, then `dist 0` seven seconds later.

**Fix (`ChatPanel.tsx`):** `handleScroll`'s deferred pass records a reader anchor
while unpinned — the virtual item under the top edge (`key` = global transcript
position, `index`, and the pixel `distance` into it). A `useLayoutEffect` keyed on
`[renderEntries, windowStartServerIndex]` finds that key's new index after any
change that is not a pure append and calls `restoreChatDisplayAnchor` before
paint. A pinned reader is untouched (the tail pin owns the offset); the scroll-up
pagination path keeps its own `scrollHeight`-delta restore and flags the prepend
(`prependRestoreRef`) so the effect stays out of it. If the anchored row is gone
(truncate/rewind) the effect does nothing and the old reset rule still re-arms
the follow; if it survived, `anchorSurvivedShrinkRef` tells the `[messages, live]`
effect NOT to treat the shrink as a reset.

`chatDisplayScroll.ts` now asks `getOffsetForIndex(index, "start")` explicitly:
the default `"auto"` alignment answers with the END-aligned offset for a row
below the viewport, which landed the restore 504px short in the test.

## Root cause 6 — one session open under two projects = duplicate React keys (2026-10-01)

`App.tsx` renders a pane per tab across ALL projects keyed `${tab.id}:chat`. A
deep link (`/session/:id`, the desktop `-session` flag) or the session picker
binds the session to the *active* project, so a session that already had a tab
under its own project got a second tab under another path — two panes with the
same key. React then recreated a pane on renders and, with the panes being
`absolute inset-0` stacks, the DOM accumulated orphaned copies (44 after a few
minutes in Safari, 663 in one Chrome run after switching projects); every copy
was a live `ChatPanel` reloading the transcript and pinning to the bottom.

**Fix (`projectStore.tsx`, `App.tsx`):** `ADD_TAB` refuses a second copy and
activates the existing one (`findProjectPathForTab`); `openSessionTab` also
brings the owning project forward so a deep link never lands on an empty bar;
`dropCrossProjectDuplicateTabs` runs on `RESTORE_TABS` and on every server merge
(`mergeExternalTabs`), and `REKEY_TABS` no longer concatenates duplicates. As a
belt-and-braces guard the pane keys (and `visitedTabsRef`) now include
`tab.projectPath`, so a duplicate that still slips in from a shared `tabs.json`
can only waste a pane, never corrupt the DOM.

Tests: `ChatPanel.test.tsx` "keeps an un-pinned reader's row across window
slides" (grow + shrink), `projectStore.test.tsx` "keeps one tab per session
across projects".
