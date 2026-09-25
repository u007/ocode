---
type: Decision
title: TUI wheel scroll over chat composer design
description: 'Design spec: treat transcript + chat composer as one wheel-scrolling surface on the TUI chat tab.'
tags:
  - tui
  - mouse
  - scroll
  - design-spec
timestamp: 2026-09-25T05:03:42Z
---
# TUI: wheel scroll over chat composer (2026-09-25)

## Problem

In the ocode TUI chat tab, scrolling the transcript with the mouse wheel only works while the
pointer is over the transcript viewport itself. When the pointer sits over the chat composer
(the bottom input chrome), the wheel event is silently dropped: the transcript does not move.
Users expect the transcript plus the composer to behave as one continuous wheel-scrolling
surface on the chat tab.

## Root cause

`Update()` routes `tea.MouseWheelMsg` through a fallback gate in `internal/tui/model.go:3235`:

```go
if !m.mouseOverTranscriptViewport(msg) { return m, nil }
```

`mouseOverTranscriptViewport` (`internal/tui/model.go:8611`) accepts a wheel event only when
`X` is inside `panelWidth()` and `Y` is between `appHeaderHeight` and
`appHeaderHeight + viewport.Height() + 2` — i.e. the transcript box only. Any `Y` in the
composer region falls outside that range, so the event returns early and never scrolls.

This is purely the TUI's own gate: `textarea.Update` (bubbles v2.1.0) has no wheel handling and
`fastviewport.Update` is a documented no-op, so nothing downstream consumes the event either.

The old behavior is codified by `TestMouseWheelScrollsTranscriptOnlyWhenOverMessages`
(`internal/tui/model_test.go:4318`), which asserts a wheel at `Y: 8` (below the tiny test
viewport) does **not** scroll.

## Approved behavior (option 1)

- Treat the transcript plus the chat composer/bottom chat region as one wheel-scrolling surface
  on the chat tab. Replace/extend `mouseOverTranscriptViewport` with a named predicate (e.g.
  `mouseOverChatWheelRegion(tea.Mouse)`) whose `Y` range runs from `appHeaderHeight` to the
  bottom of the input area (`inputAreaTopY() + inputAreaHeight()`), keeping the same
  `X < panelWidth()` bound and the `activeTab == tabChat` check.
- Preserve existing branch precedence in `Update()`. Popup/sidebar/detail/tab branches stay
  **before** the fallback:
  - permission dialog (`showPermDialog`) and `/btw` dialog (`showBtwDialog`) still scroll their
    own popup viewports when the wheel is over the input area (`inputAreaTopY()` ..
    `inputAreaTopY()+inputAreaHeight()`);
  - outside those dialogs' bounds, chat wheel routing applies (transcript **or** composer);
  - sidebar, other tabs, and detail drill-in routing are unchanged.
- Wheel over the composer scrolls the transcript but does **not** modify composer text,
  selection, or key handling. Other tabs' routing is untouched.

## Files

- `internal/tui/model.go` — predicate (`mouseOverTranscriptViewport` →
  `mouseOverChatWheelRegion`) and its call site at the fallback gate (~line 3235).
- `internal/tui/model_test.go` — replace/extend
  `TestMouseWheelScrollsTranscriptOnlyWhenOverMessages` (~line 4318).

## Test plan

- Extend/replace the existing test so that:
  - wheel over the transcript scrolls (unchanged assertion);
  - wheel over the composer at `inputAreaTopY()+1` **also** scrolls the transcript, and the
    composer textarea value is unchanged after the event;
  - a wheel outside the chat wheel region (e.g. in the status bar / above the panel) does not
    scroll;
  - another tab (e.g. files/agents) is unaffected by the widened gate.
- The composer-scroll case must **fail** against the current production code (mutation check:
  verify failure before applying the fix).
- Validation: focused `go test ./internal/tui/ -run 'MouseWheel'`, then
  `go build ./... && go vet ./internal/tui/ && gofmt -l internal/tui/`, then the full
  `internal/tui` suite if practical.

## Out of scope

- Any change to input text editing, selection, paste, or key handling in the composer.
- Wheel routing for non-chat tabs, sidebar, detail drill-in, or popup dialogs (precedence
  preserved as-is).
- bubbles/fastviewport upstream behavior; no dependency changes.
- Web/desktop frontend scrolling (separate surfaces).
