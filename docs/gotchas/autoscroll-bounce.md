---
type: Gotcha
title: ChatPanel & LogPanel Autoscroll Bounce / Freeze
description: 'Root cause analysis of the autoscroll bounce/freeze bug: smooth-scrolling every live token without at-bottom state tracking causes competing animations that lock up the scroll position. Updated with virtualizer-growth follow-up (2026-09-17), threshold correction (120→200px per code), synchronous scroll lock + lock re-arm on transcript reset (2026-09-18), and LogPanel parity (2026-09-18).'
resource: "web/src/components/Chat/ChatPanel.tsx, web/src/components/Logs/LogPanel.tsx"
tags: [autoscroll, chat, logs, web, ui-glitch, performance]
timestamp: 2026-09-18T15:51:26Z
---
# ChatPanel & LogPanel Autoscroll Bounce / Freeze

**Type:** Gotcha
**Files:** `web/src/components/Chat/ChatPanel.tsx`, `web/src/components/Logs/LogPanel.tsx`
**Status:** Active (three distinct causes for ChatPanel, all fixed; LogPanel parity landed 2026-09-18)

## Symptoms

While the agent is streaming a response, the chat panel either:

1. **Bounces**: scroll position oscillates down-up-down-up during active streaming (each new token triggers a smooth-scroll animation that competes with the previous one).
2. **Freezes**: after enough competing animations, `Element.scrollIntoView({behavior:'smooth'})` stops making progress entirely — the viewport locks at a position partway down the conversation. The user must manually scroll or use the "jump to bottom" button.
3. **Scroll-back after turn ends** (2026-09-17): after the user is pinned at the bottom, the turn-end `SET_MESSAGES` broadcast swaps the streamed tail out of the live block into the virtualized list. The virtualizer corrects each committed entry from `estimateSize: 96` to measured height over subsequent frames, growing the container AFTER the one-shot `[messages, live]` pin already ran. The existing ResizeObserver watches the scroll ELEMENT's box and deliberately ignores non-zero→non-zero changes, so nothing follows the growth → viewport stays above the bottom.
4. **Can't stay scrolled up while streaming** (2026-09-18): a user scrolls up during active streaming, but a token landing in the same frame re-pins before the deferred check runs, swallowing the scroll-up. Also: after a transcript reset (truncate/clear) or when content stops overflowing, the stale lock from the previous context prevents tail-following on new growth.

## Cause 1 — Competing smooth-scroll animations (Fixed 2026-07-09)

### Root Cause

The original implementation called `scrollIntoView({behavior:'smooth'})` on the bottom sentinel element (`bottomRef.current`) inside a `useEffect` that ran on every message/live update:

```typescript
// BAD — prior to fix
useEffect(() => {
  if (!initialized) return;
  if (atBottomRef.current) {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }
}, [messages, live, initialized]);
```

**Why this breaks:**

- Each `scrollIntoView({behavior:'smooth'})` starts a CSS smooth-scroll animation in the browser.
- During streaming, the effect fires on every delta — up to dozens of times per second.
- Each new `.smooth()` call **interrupts** the previous animation and restarts from the current (mid-animation) scroll position toward the new target.
- These competing animations create a race: the scroll position oscillates between the target (bottom) and where the previous animation was interrupted.
- Eventually the browser's scroll-animation queue gets confused and the position stops converging — it freezes at an intermediate point.

### The Fix

#### 1. Instant scroll for live tokens

Replace smooth-scrolling with an **instant** (non-animated) scroll for live content. Smooth scrolling is reserved for the explicit "jump to bottom" button:

```typescript
// GOOD — current code
useEffect(() => {
  if (!initialized) return;
  const el = scrollRef.current;
  if (!el) return;
  if (atBottomRef.current) {
    el.scrollTop = el.scrollHeight;  // instant, no animation
  }
}, [messages, live, initialized]);

// "jump to bottom" button uses smooth scrolling
const scrollToBottom = useCallback((smooth = false) => {
  const el = scrollRef.current;
  if (!el) return;
  scrollElementTo(el, el.scrollHeight, smooth ? "smooth" : "auto");
  requestAnimationFrame(() => {
    atBottomRef.current = true;
    lastScrollTopRef.current = el.scrollTop;
    setShowJumpToBottom(false);
  });
}, []);
```

Setting `el.scrollTop = el.scrollHeight` is synchronous and non-animated — a burst of 20 token deltas sets it 20 times without any animation conflict.

#### 2. At-bottom state tracking

The `atBottomRef` (a `useRef(true)`) is the authoritative source for whether autoscroll should follow. It is updated by the `onScroll` handler via two passes — a synchronous fast-path and a deferred recompute:

```typescript
const lastScrollTopRef = useRef(0);
const rafRef = useRef<number>(0);
const handleScroll = useCallback(() => {
  const el = scrollRef.current;
  if (!el) return;

  // --- Synchronous fast-path (same tick as the scroll event) ---
  // A scroll event whose offset DECREASED can only come from the user
  // (our own pins always increase it). Unpinning here, in the same tick,
  // is what stops a streamed token landing before the deferred recompute
  // from re-pinning and swallowing the scroll-up.
  const top = el.scrollTop;
  const prevTop = lastScrollTopRef.current;
  lastScrollTopRef.current = top;
  if (top < prevTop - 1) {
    atBottomRef.current = false;
  }

  // --- Deferred pass (requestAnimationFrame) ---
  // Re-arms the lock when the user scrolls back near the bottom, and
  // handles the content-growth race (token lands before the effect runs).
  cancelAnimationFrame(rafRef.current);
  rafRef.current = requestAnimationFrame(() => {
    lastScrollTopRef.current = el.scrollTop;  // record any pin that happened
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distanceFromBottom < 200;  // 200px threshold
    atBottomRef.current = atBottom;
    setShowJumpToBottom(!atBottom);
    setShowJumpToTop(el.scrollTop > 200);
  });
  // ...
}, [/* deps */]);
```

**Why the synchronous check exists:** Before this fix, `atBottomRef` was only recomputed inside the `requestAnimationFrame` callback. During streaming, the `[messages, live]` auto-scroll effect runs synchronously after the React render — if a user scroll-up and a streamed token land in the same frame, the effect reads the still-`true` `atBottomRef`, re-pins (`scrollTop = scrollHeight`), and *then* the rAF fires and sees the token-pinned position (now at bottom) → sets `atBottomRef` back to `true`. The user's scroll-up was swallowed. The synchronous decrease check (`top < prevTop - 1`) sets `atBottomRef = false` before the auto-scroll effect can run, breaking the cycle.

**Design rules:**
- Autoscroll **only** fires when `atBottomRef.current === true` (user is already at the bottom).
- When the user scrolls up, `atBottomRef` flips to `false` synchronously (same tick) and autoscroll stops.
- The "jump to bottom" button calls `scrollToBottom(true)` with smooth scroll and resets `atBottomRef` in the following rAF.
- The 200px threshold gives tolerance for small rounding errors without losing the "pinned" state.

## Cause 2 — Virtualizer growth after turn ends (Fixed 2026-09-17)

### Distinct from Cause 1

This is a **separate, later bug** with a different root cause. The existing code already writes `scrollTop` instantly (no competing animations). The problem is that the scroll pin runs once at turn-end, but the virtualizer corrects item heights asynchronously afterward.

### Root Cause

When `SET_MESSAGES` fires (turn ends), it clears the `live` array and commits streamed entries into the virtualized list. Each committed entry initially renders at `estimateSize: 96` (the virtualizer's default estimate). The virtualizer then measures each entry's actual height over subsequent frames, growing the list container's `virtualizer.getTotalSize()`. But the one-shot `[messages, live]` pin (`el.scrollTop = el.scrollHeight`) already ran before this growth happened. The existing `ResizeObserver` on the scroll ELEMENT watches the element's own box and deliberately ignores non-zero→non-zero changes (it exists for the `display:none`→visible height reset), so nothing follows the container growth → viewport stays above the bottom.

### The Fix

A new `ResizeObserver` on the virtualized CONTENT container (`listContainerRef`, whose inline height is `virtualizer.getTotalSize()`) re-pins (`el.scrollTop = el.scrollHeight` plus one follow-up `requestAnimationFrame`) while `atBottomRef.current` holds. A user who scrolled up is never yanked back (guarded by `atBottomRef`, same as the existing pin effect). Deps: `[hasList]`.

### Why the old ResizeObserver doesn't catch this

The pre-existing ResizeObserver (`useEffect` ~line 493) watches the scroll ELEMENT's bounding box. It triggers on `display:none` → visible transitions (the main use case). It intentionally skips non-zero→non-zero box changes to avoid re-pinning on every token delta during streaming. But when the virtualizer corrects heights post-turn-end, the SCROLL ELEMENT's box doesn't change — the inner content grows while the outer container stays the same height (it's the scroll element that resizes, and that's what the observer watches). The new observer watches the CONTENT container specifically for this case.

## Cause 3 — Stale lock after transcript reset or content shrink (Fixed 2026-09-18)

### Root Cause

After the user scrolled up (locking `atBottomRef = false`), certain events make the old lock meaningless:

1. **Transcript reset** (TRUNCATE_MESSAGES / clear / replacement): the committed message list shrank relative to the previous render — the user's prior scroll position no longer refers to this content. But `atBottomRef` stayed `false`, so new appended content grew without tail-following.
2. **No scrollbar** (`scrollHeight - clientHeight <= 1`): the content fits entirely in the viewport. There is no position to be locked away from. When new tokens push the content past the viewport edge, growth should track the tail again — but the stale `false` prevented it.
3. **New session**: a `sessionId` change (tab switch, new chat) remounts ChatPanel (it is keyed by tab id in App.tsx), but the explicit reset guards a future non-keyed reuse.

### The Fix

The `[messages, live, initialized]` auto-scroll effect now **re-arms** the lock when carrying it would be meaningless:

```typescript
useEffect(() => {
  if (!initialized) return;
  const el = scrollRef.current;
  if (!el) return;

  // 1. Transcript reset: committed list shrank → old position meaningless
  const prev = prevMessagesRef.current;
  prevMessagesRef.current = { count: messages.length };
  if (prev && messages.length < prev.count) {
    atBottomRef.current = true;
  }

  // 2. No scrollbar: nothing to be locked away from → re-arm the follow
  const hasScrollbar = el.scrollHeight - el.clientHeight > 1;
  if (!hasScrollbar) {
    atBottomRef.current = true;
    setShowJumpToBottom(false);
    setShowJumpToTop(false);
  }

  // 3. Pin if locked (same as before)
  if (atBottomRef.current) {
    el.scrollTop = el.scrollHeight;
    lastScrollTopRef.current = el.scrollTop;
  }
}, [messages, live, initialized]);
```

The scroll-element `ResizeObserver` (hidden→visible re-pin) also re-arms when the window grows to fit (`scrollHeight - clientHeight <= 1`), matching the same no-scrollbar logic.

The `sessionId` effect resets all scroll-tracking refs:

```typescript
useEffect(() => {
  atBottomRef.current = true;
  lastScrollTopRef.current = 0;
  prevMessagesRef.current = null;
  setShowJumpToBottom(false);
  setShowJumpToTop(false);
}, [sessionId]);
```

### Invariant

Every code path that **writes** `scrollTop` (jump-to-bottom, scroll-to-top, search jump, hidden→visible re-pin, virtualizer content observer) also writes `lastScrollTopRef` so the synchronous decrease check never mistakes a programmatic pin for a user scroll-up.

## LogPanel parity (2026-09-18)

The identical synchronous-lock + re-arm semantics were ported from `ChatPanel` to `web/src/components/Logs/LogPanel.tsx`. LogPanel is a flat, unvirtualized append-only list (no turn-end broadcast, no `live` array) so the implementation is simpler — one `autoScroll` ref replaces ChatPanel's `atBottomRef` and also serves as the manual toolbar toggle (↓ button, titles "Disable auto-scroll" / "Enable auto-scroll").

### `LogPanel` specifics

1. **Synchronous scroll-up lock.** `handleScroll` compares `el.scrollTop` to a new `lastScrollTopRef` and, on a DECREASE (our own `scrollTop = scrollHeight` pins only increase it), sets `autoScrollRef.current = false` + `setAutoScroll(false)` synchronously, *before* the existing deferred `requestAnimationFrame` near-bottom check (30 px threshold) runs. Without this, a log envelope arriving in the same frame could re-pin and swallow the scroll-up.

2. **Re-arm on list reset.** The `[logs, autoScroll]` follow effect no longer early-returns when `!autoScroll`; it re-arms the lock when carrying it is meaningless:
   - The log list shrank (`prevLogsCountRef` — only appends / `cap` keep it flat).
   - No scrollbar (`scrollHeight - clientHeight <= 1`).
   - A session-id change (the load effect resets `autoScroll` / refs when `prevSessionRef` differs).

3. **Hidden-panel guard.** The no-scrollbar reset requires `el.clientHeight > 0`, so a hidden (`display:none`) panel reporting 0/0 is not read as "content fits" and does not silently re-arm the lock — important because background-buffered log entries can arrive while the tab is hidden, and on re-open a reset lock would jump to the bottom instead of restoring the saved read position (`savedScrollTopRef`). The same guard was added to `ChatPanel`'s no-scrollbar reset for exactly the same reason.

4. **Pin-site invariant.** `scrollToBottom`, the tab-visibility `rAF` restore / catch-up, and the follow pin all write `lastScrollTopRef` so a pin is never misread as a scroll-up.

## Regression tests

### ChatPanel (`web/src/components/Chat/ChatPanel.test.tsx`) — `describe("scroll lock ...")`

| Test | What it verifies |
|------|-----------------|
| "stays locked when a token lands in the same frame as the scroll-up" | The synchronous `top < prevTop - 1` check prevents re-pinning in the same tick |
| "re-arms the tail follow when the transcript is reset (truncate)" | `prevMessagesRef` shrink detection re-sets `atBottomRef = true` |
| "re-arms the tail follow once the content stops overflowing (no scrollbar)" | `scrollHeight - clientHeight <= 1` path re-arms the follow |
| "starts a new session pinned (no inherited lock)" | `sessionId` change resets `atBottomRef` / `lastScrollTopRef` / `prevMessagesRef` |
| "does not re-arm the lock from a stream event while the tab is hidden" | `el.clientHeight > 0` guard prevents 0/0 false-positive no-scrollbar re-arm |

### LogPanel (`web/src/components/Logs/LogPanel.test.tsx`)

| Test | What it verifies |
|------|-----------------|
| "locks synchronously on scroll-up, before the deferred check runs" | `autoScrollRef` flips to `false` before rAF callbacks run (test re-stubs rAF to hold callbacks and asserts the lock before they execute) |
| "re-arms auto-scroll when the log list is reset (clear)" | `prevLogsCountRef` shrink detection re-sets `autoScrollRef = true` |
| "re-arms auto-scroll once the content stops overflowing" | `scrollHeight - clientHeight <= 1` path re-arms the follow |
| "starts a new session following the tail" | `prevSessionRef` change resets `autoScroll` / refs |

All four LogPanel behavior tests fail against the pre-fix `LogPanel`. The ChatPanel hidden-tab test fails against the pre-fix `ChatPanel` (missing `clientHeight > 0` guard).

## Related

- The same pattern (instant scroll + at-bottom ref + synchronous lock) should be used for any scrollable surface that receives live-streaming content (logs, diffs, etc.).
- `LogPanel` now follows the full pattern: synchronous scroll-up lock, deferred near-bottom re-arm, re-arm on reset/shrink/no-scrollbar, hidden-panel guard, `lastScrollTopRef` pin-site invariant.
- CSS `scroll-behavior: smooth` on the container would have the same competing-animation problem and must not be set globally.
