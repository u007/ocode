---
type: Gotcha
title: ChatPanel Autoscroll Bounce/Freeze
description: 'Root cause analysis of the autoscroll bounce/freeze bug: smooth-scrolling every live token without at-bottom state tracking causes competing animations that lock up the scroll position. Updated with virtualizer-growth follow-up (2026-09-17) and threshold correction (120→200px per code).'
resource: ""
tags:
  - autoscroll
  - chat
  - web
  - ui-glitch
  - performance
  - virtualizer
timestamp: 2026-09-17T05:37:31Z
---
---
type: Gotcha
description: Root cause analysis of the autoscroll bounce/freeze bug: smooth-scrolling every live token without at-bottom state tracking causes competing animations that lock up the scroll position.
tags: [autoscroll, chat, web, ui-glitch, performance]
status: active
okf_version: "0.1"
---

# ChatPanel Autoscroll Bounce / Freeze

**Type:** Gotcha
**File:** `web/src/components/Chat/ChatPanel.tsx`
**Status:** Active (two distinct causes, both fixed)

## Symptoms

While the agent is streaming a response, the chat panel either:

1. **Bounces**: scroll position oscillates down-up-down-up during active streaming (each new token triggers a smooth-scroll animation that competes with the previous one).
2. **Freezes**: after enough competing animations, `Element.scrollIntoView({behavior:'smooth'})` stops making progress entirely — the viewport locks at a position partway down the conversation. The user must manually scroll or use the "jump to bottom" button.
3. **Scroll-back after turn ends** (2026-09-17): after the user is pinned at the bottom, the turn-end `SET_MESSAGES` broadcast swaps the streamed tail out of the live block into the virtualized list. The virtualizer corrects each committed entry from `estimateSize: 96` to measured height over subsequent frames, growing the container AFTER the one-shot `[messages, live]` pin already ran. The existing ResizeObserver watches the scroll ELEMENT's box and deliberately ignores non-zero→non-zero changes, so nothing follows the growth → viewport stays above the bottom.

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
  el.scrollTo({ top: el.scrollHeight, behavior: smooth ? "smooth" : "auto" });
  requestAnimationFrame(() => {
    atBottomRef.current = true;
    setShowJumpToBottom(false);
  });
}, []);
```

Setting `el.scrollTop = el.scrollHeight` is synchronous and non-animated — a burst of 20 token deltas sets it 20 times without any animation conflict.

#### 2. At-bottom state tracking

The `atBottomRef` (a `useRef(true)`) is the authoritative source for whether autoscroll should follow. It is updated by the `onScroll` handler:

```typescript
const handleScroll = useCallback(() => {
  const el = scrollRef.current;
  if (!el) return;
  const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
  const atBottom = distanceFromBottom < 200;  // 200px threshold
  atBottomRef.current = atBottom;
  setShowJumpToTop(el.scrollTop > 200);
  // ...
}, [/* deps */]);
```

**Design rules:**
- Autoscroll **only** fires when `atBottomRef.current === true` (user is already at the bottom).
- When the user scrolls up (e.g., to read earlier output), `atBottomRef` flips to `false` and autoscroll stops.
- The "jump to bottom" button calls `scrollToBottom(true)` with smooth scroll and resets `atBottomRef`.
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

## Related

- The same pattern (instant scroll + at-bottom ref) should be used for any scrollable surface that receives live-streaming content (logs, diffs, etc.).
- See also `web/src/components/Logs/LogPanel.tsx` for a similar pattern (it uses `scrollIntoView` but without smooth).
- CSS `scroll-behavior: smooth` on the container would have the same competing-animation problem and must not be set globally.
