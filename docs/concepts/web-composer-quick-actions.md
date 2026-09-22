---
type: Concept
title: Web Composer Quick-Actions Strip
description: 'Quick-action pills below the web/desktop chat composer send row: Compact, Continue/Resume, Recap. Hidden when the session has no conversation content yet.'
tags: []
timestamp: 2026-09-21T08:19:59Z
---
# Web Composer Quick-Actions Strip

**Type:** Concept  
**Description:** Quick-action pills below the web/desktop chat composer send row: Compact, Continue/Resume, Recap. Hidden on brand-new/empty sessions.  

---

# Web Composer Quick-Actions Strip

The web/desktop chat composer (`web/src/components/Chat/ChatInput.tsx`) renders a strip of quick-action pills directly **below the send row** — Compact, Continue/Resume, and Recap. The strip is **hidden entirely when the session has no conversation content yet** (a brand-new `new-*` draft tab, or a session with zero committed messages and zero live/streaming parts). It appears as soon as the first message is committed or the first live turn part streams (including mid-first-turn).

The gate is `hasConversation` from `useChat()`: `true` when the session slice has `messages.length > 0 || live.length > 0`. It is a narrow boolean selector so streamed deltas never re-render consumers.

Component: `web/src/components/Chat/QuickActionsBar.tsx` — purely presentational, props `{ actions: QuickActionItem[], onSelect(id) }`, rendered as a `role="toolbar" aria-label="Quick actions"`. Disabled pills are dimmed and ignore clicks. It is deliberately **not** a dropdown/popover: the whole point is that the actions stay one click away.

## The three actions

| Pill | Behaviour |
|---|---|
| **Compact** | Dispatches `/compact`. Disabled with the tooltip "Compaction already in progress" while a compaction is running, so a queued duplicate cannot re-compact an already-compacted session. |
| **Continue / Resume** | Context-aware. When the turn was interrupted (`wasInterrupted`) the pill reads **Resume** and calls `useChat().resume()` — it does not send a message. Otherwise it sends the literal message `continue`. |
| **Recap** | Dispatches `/recap`. |

The label flips between Continue and Resume solely on `wasInterrupted`, which is also what flips the send-row button into its Resume state.

The strip sits directly below the composer send row when shown; its DOM placement is unchanged.

## Delivery reuses the typed-command pipeline

`runQuickDispatch(text, kind)` mirrors the barrier checks `handleSend` applies before dispatching: it queues via `pushQueued` when `effectiveBusy` is true, when the composer is mid-drain (`drainingRef`), or when a compaction is active. When idle it calls the shared `dispatchCommand`, which routes slash commands through `onSlashCommand` and plain text through `sendMessage`. A quick action therefore behaves exactly like a typed command: it queues behind a running turn rather than interleaving with it.

## Gotcha: quick actions never touch the composer draft

Unlike `handleSend` — which clears the input and attaches the `@`-ref/editor context — a quick action must **never** clear the composer draft or add context refs. Clicking a quick action while the user has a half-written message in the box must leave that message intact; the action is an independent dispatch.

This invariant is guarded by the test "sends the literal 'continue' message and leaves the draft untouched" in `web/src/components/Chat/ChatInput.quickActions.test.tsx`. A mutation that added `setInput(""); clearDraft(sessionTabId);` to `runQuickDispatch` is caught by that test (verified by temporary revert).

## Slash commands are unaffected

The gate applies **only** to the pill strip. The slash commands themselves — `/compact`, `/recap`, and typing `continue` — remain fully available on an empty session via the composer and its slash autocomplete. The pills were simply mid-conversation nudges with nothing to act on in an empty session, where they were noise; the commands had a legitimate use there from the start.

## Tests

- `web/src/components/Chat/QuickActionsBar.test.tsx` — toolbar render; disabled entries ignore clicks.
- `web/src/components/Chat/ChatInput.quickActions.test.tsx` — routes `/compact` and `/recap`; sends the literal `continue` and preserves the draft; Resume-not-send when interrupted; queues `/compact` while streaming and runs it on the busy→idle edge; Compact disabled while compacting; strip renders below the send row (DOM order); **hides the whole strip on a new or empty session**; **reveals the strip once the session has conversation content**.
- `web/src/hooks/useChat.test.tsx` — describe `useChat.hasConversation`: false on a brand-new session, true after a committed message, false when cleared, true with a streaming live buffer (a mid-turn stream counts as conversation content).

All three load-bearing behaviours were mutation-verified. Full web suite 185 files / 1627 tests green; `tsgo --noEmit` and `vite build` clean.
