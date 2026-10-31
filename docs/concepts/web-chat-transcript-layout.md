---
type: Concept
title: Web Chat Transcript Layout — Flex Column and Shrink-Weight Invariant
description: Layout of a single chat tab's vertical flex column in the web SPA (web/src/App.tsx) and the invariant that stops a panel from collapsing the messages pane.
tags:
  - web
  - chat
  - layout
  - transcript
  - agent-preview
  - flex
  - invariant
timestamp: 2026-10-31T23:30:00Z
---
# Web Chat Transcript Layout — Flex Column and Shrink-Weight Invariant

## Purpose

Layout of a single chat tab's vertical flex column in the web SPA
(`web/src/App.tsx`) and the invariant that stops a panel from collapsing the
messages pane.

## The column

A chat tab renders inside an `absolute inset-0 flex flex-col` wrapper. Its
children, top to bottom:

1. `RemoteVersionBanner` — auto-height, deliberately OUTSIDE the confined ask
   surface.
2. `chat-ask-surface` — `relative flex min-h-0 flex-1 flex-col`. Inside it:
   - **Transcript wrapper** — `relative flex-[1_1_10rem] min-h-0`,
     `data-testid="chat-transcript-slot"`. Wraps `ChatPanel` (the virtualized
     message list, the only vertical scroller).
   - `PlanPanel` — a FRAGMENT of two flex items: `plan-panel-header`
     (`shrink-0`) and `plan-panel-body` (`shrink min-h-0 max-h-52
     overflow-y-auto`).
   - `AgentPreview` — the agent-run rail, same two-item shape
     (`agent-preview-header` / `agent-preview-body`).
   - `BtwPanel` — `shrink-0`, only while active.
   - `ChatInput` — the composer, which contains the quick-actions strip. It has
     no `shrink-0`; its `min-height: auto` holds it at min-content.

The surface deliberately has NO `overflow` class: the composer must stay in the
flow. The tab pane around it is `absolute inset-0` inside an `overflow-hidden`
ancestor, so anything that does not fit is CLIPPED at the bottom, not scrolled.

## The invariant: the transcript carries a basis, panels yield from the body

The transcript wrapper and the capped panel bodies are the only items that
shrink. Before the fix, both panels were a single `shrink-0` box while the
transcript was the column's only `flex-1` item, so every pixel a panel added
came straight out of the transcript; on a shorter window the messages pane
collapsed to its `p-4` padding and the chat looked blank (reported as "expand
the agent task and the whole messages screen goes blank").

Four rules, all load-bearing:

1. **Both capped panels split into header + body.** The header is `shrink-0`
   (its toggle stays reachable at any height) and only the body is
   `shrink min-h-0`, capped at `max-h-52` and scrollable. A wrapper with
   `min-h-0` would shrink the header with it; a wrapper without it would not
   shrink at all. `min-h-0` is what lets the body drop below its content.
   The `max-h-52` cap is the RAIL / PANEL body; a run's own transcript box is
   `max-h-72` in `web/src/components/Agents/RunNode.tsx`.
2. **The transcript carries a 10rem BASIS, not a 0 basis.** `flex-[1_1_10rem]`
   grows to fill the column like `flex-1`, but when the column is over-full the
   squeeze is distributed in proportion to each item's base size. A basis of 0
   receives NO share of a shrink, so an expanded panel could take the whole
   column and blank the transcript — the bug this replaces.
3. **The composer and the panel headers never give way.** The composer's
   `min-height: auto` floors it at min-content, and the headers are
   `shrink-0`, so a squeeze can never hide the send row or a panel toggle.
4. **No viewport media query.** The proportion does the work at every height;
   there is no breakpoint floor to tune.

## What is NOT guaranteed

- No pixel measurement is claimed on this page. jsdom has no layout engine, so
  the regression tests pin the class contract.
- `BtwPanel` is a NON-YIELDING sibling: it is `shrink-0` but its content is
  auto-height, so with a plan, an agent rail and a Btw panel all open, the
  transcript still absorbs the whole squeeze. It is deliberately not capped —
  it is a one-line question plus a bounded scroll area, and hiding it would
  break the side-query flow.
- If the column is still over-subscribed, the CLIPPED item is whatever sits
  below the transcript, because the ancestor clips.

## Regression tests

- `web/src/components/Chat/AgentPreview.test.tsx` — "yields height to the
  transcript instead of crushing it" (token-exact `shrink` / `shrink-0`; also
  proves the body follows the rail's own collapse toggle).
- `web/src/components/Chat/PlanPanel.test.tsx` — "keeps the header row
  non-shrinking and makes only the body shrinkable".
- `web/src/App.askDialogScope.test.tsx` — "gives the transcript wrapper a 10rem
  flex BASIS, not a min-height floor", asserted through
  `data-testid="chat-transcript-slot"`.

All three pin classes only. Restoring `shrink-0` on either body, collapsing the
panels back into one `shrink-0` wrapper, or dropping the 10rem basis fails them.

## Related

- `skills/ocode-web/SKILL.md` item 80 — the same rules in skill form.
- `docs/gotchas/chat-scroll-bounce-clamped-pin-vs-user-intent.md` — the separate
  tail-pin / virtualizer scroll contract; do not conflate the two.
