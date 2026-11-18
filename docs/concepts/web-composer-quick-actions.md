---
type: Concept
title: Web Composer Quick-Actions Strip
description: 'Configurable quick-action chips below the web/desktop composer: three click modes (send dispatches, fill replaces the draft, append inserts at the caret); LLM suggestions insert at the caret and never auto-send.'
tags: []
timestamp: 2026-11-17T05:29:09Z
---
# Web Composer Quick-Actions Strip

**Type:** Concept  
**Description:** Configurable quick-action chips below the web/desktop composer with three click modes (`send` dispatches, `fill` replaces the draft, `append` inserts at the caret); LLM suggestions insert at the caret and never auto-send.  

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

All three shipped starters carry `mode: "send"`: they dispatch and never touch the draft. User-configured chips can instead use `fill` or `append`, described below.

## Delivery reuses the typed-command pipeline

`runQuickDispatch(text, kind)` mirrors the barrier checks `handleSend` applies before dispatching: it queues via `pushQueued` when `effectiveBusy` is true, when the composer is mid-drain (`drainingRef`), or when a compaction is active. When idle it calls the shared `dispatchCommand`, which routes slash commands through `onSlashCommand` and plain text through `sendMessage`. A quick action therefore behaves exactly like a typed command: it queues behind a running turn rather than interleaving with it.

## Gotcha: only `send` actions leave the composer draft alone

The original rule was "quick actions never touch the composer draft." It still holds for the three shipped pills, which are all `send` mode: unlike `handleSend` they neither clear the input nor attach `@`-ref/editor context, so a half-written message stays intact and the dispatch is independent. With configurable chips the precise rule is per mode:

| Mode | Draft effect |
|---|---|
| `send` | None — dispatches through `runQuickDispatch` (queues behind a running turn), as above. |
| `fill` | Replaces the whole draft via `updateDraft(chip.message)`, like choosing a slash-command menu entry. |
| `append` | Inserts the chip message at the caret, replacing any highlighted selection. |

`append` is the third mode, added 2026-11-17. It shares the `insertAtCursor` helper in `ChatInput.tsx` with voice transcripts and LLM suggestions, so caret re-anchoring (`pendingCaretRef`) is identical on all three paths; the helper throws if the textarea is not mounted rather than silently dropping text. Users pick the mode in Settings → Quick Actions, where `append` is labelled **Insert at cursor**.

The `send` case is still pinned by "sends the literal 'continue' message and leaves the draft untouched" in `web/src/components/Chat/ChatInput.quickActions.test.tsx`; a mutation that added `setInput(""); clearDraft(sessionTabId);` to `runQuickDispatch` is caught by that test (verified by temporary revert). `fill` and `append` now have their own tests.

## Suggested responses insert at the caret

The `<ocode:suggestions>` row above the strip has **no mode field**: its fixed behaviour is insert-at-caret through the same `insertAtCursor` helper (`runSuggestedResponse`), never replace and never auto-send. Auto-send was and remains out of scope.

## Slash commands are unaffected

The gate applies **only** to the pill strip. The slash commands themselves — `/compact`, `/recap`, and typing `continue` — remain fully available on an empty session via the composer and its slash autocomplete. The pills were simply mid-conversation nudges with nothing to act on in an empty session, where they were noise; the commands had a legitimate use there from the start.

## Tests

- `web/src/components/Chat/QuickActionsBar.test.tsx` — toolbar render; disabled entries ignore clicks.
- `web/src/components/Chat/ChatInput.quickActions.test.tsx` — routes `/compact` and `/recap`; sends the literal `continue` and preserves the draft; Resume-not-send when interrupted; queues `/compact` while streaming and runs it on the busy→idle edge; Compact disabled while compacting; strip renders below the send row (DOM order); **hides the whole strip on a new or empty session**; **reveals the strip once the session has conversation content**; **`append` mode inserts at the caret without replacing the draft**.
- `web/src/components/Chat/ChatInput.suggestedResponses.test.tsx` — clicking a suggestion inserts at the caret and never sends.
- `internal/config/quick_actions_test.go` — Go `Validate` accepts the `append` mode (positive row).
- `web/src/lib/quickActions.test.ts` — `isQuickActionMode` accepts `"append"`.
- `web/src/components/Settings/QuickActionsForm.test.tsx` — the **Insert at cursor** option round-trips.
- `web/src/hooks/useChat.test.tsx` — describe `useChat.hasConversation`: false on a brand-new session, true after a committed message, false when cleared, true with a streaming live buffer (a mid-turn stream counts as conversation content).

All three original load-bearing behaviours were mutation-verified. Full web suite 185 files / 1627 tests green; `tsgo --noEmit` and `vite build` clean. The append-mode change was verified separately: `go test ./internal/config -run QuickAction` pass, Go build of config + server pass, web vitest on the five affected files 108/108 pass, `bun check .` typecheck clean.
