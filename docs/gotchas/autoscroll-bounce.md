---
type: Gotcha
title: ChatPanel Autoscroll Bounce/Freeze
description: 'Root cause analysis of the autoscroll bounce/freeze bug: smooth-scrolling every live token without at-bottom state tracking causes competing animations that lock up the scroll position.'
resource: web/src/components/Chat/ChatPanel.tsx
tags:
  - autoscroll
  - chat
  - web
  - ui-glitch
  - performance
timestamp: 2026-07-08T16:40:07Z
---
# ChatPanel Autoscroll Bounce / Freeze

**Type:** Gotcha  
**File:** `web/src/components/Chat/ChatPanel.tsx`  
**Status:** Fixed (2026-07-09)

## Symptoms

While the agent is streaming a response, the chat panel either:

1. **Bounces**: scroll position oscillates down-up-down-up during active streaming (each new token triggers a smooth-scroll animation that competes with the previous one).
2. **Freezes**: after enough competing animations, `Element.scrollIntoView({behavior:'smooth'})` stops making progress entirely — the viewport locks at a position partway down the conversation. The user must manually scroll or use the "jump to bottom" button.

## Root Cause

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

## The Fix

Two changes (commit not yet referenced):

### 1. Instant scroll for live tokens

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

### 2. At-bottom state tracking

The `atBottomRef` (a `useRef(true)`) is the authoritative source for whether autoscroll should follow. It is updated by the `onScroll` handler:

```typescript
const handleScroll = useCallback(() => {
  const el = scrollRef.current;
  if (!el) return;
  const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
  const atBottom = distanceFromBottom < 120;  // 120px threshold
  atBottomRef.current = atBottom;
  setShowJumpToBottom(!atBottom);
  // ...
}, [/* deps */]);
```

**Design rules:**
- Autoscroll **only** fires when `atBottomRef.current === true` (user is already at the bottom).
- When the user scrolls up (e.g., to read earlier output), `atBottomRef` flips to `false` and autoscroll stops.
- The "jump to bottom" button calls `scrollToBottom(true)` with smooth scroll and resets `atBottomRef`.
- The 120px threshold gives tolerance for small rounding errors without losing the "pinned" state.

## Related

- The same pattern (instant scroll + at-bottom ref) should be used for any scrollable surface that receives live-streaming content (logs, diffs, etc.).
- See also `web/src/components/Logs/LogPanel.tsx` for a similar pattern (it uses `scrollIntoView` but without smooth).
- CSS `scroll-behavior: smooth` on the container would have the same competing-animation problem and must not be set globally.
