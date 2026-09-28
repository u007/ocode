---
type: Gotcha
title: TUI Input Queueing Behind a ! Shell Command — Gates That Only Knew About Streaming
description: Input-queue gates only checked m.streaming, so text typed during a ! shell command fell into the 1.5s debounce and fired at the LLM while the shell ran; the queue also never drained on shellFinishedMsg. Four fixes (including drainReplacementQueueIfReady) + shellInFlight() helper + an invariant stated as a named-gate checklist.
tags:
  - gotcha
  - tui
  - shell
  - input-queue
  - streaming
  - debounce
  - race-condition
timestamp: 2026-09-28T17:30:51Z
---
## Problem

In the TUI, a `!` shell command and an LLM turn are two independent subsystems, but every input-queueing gate only knew about the LLM turn.

`m.streaming` is the flag the queue gates consult, and it is set in exactly **one** place: the `streamStartedMsg` handler in `internal/tui/model.go`. A `!` command never sets it. `!cmd` goes `startShellExecution` → `runStreamingShell`, a completely separate process loop that emits `shellChunkMsg` / `shellFinishedMsg` while `m.shellStreamCmd` stays non-nil.

So a message typed while `!sleep 600` was running hit `if m.streaming` (false), fell through to `queueDelayedChatInput` (the 1.5s debounce), and was dispatched to the LLM 1.5s later — while the shell was still running.

Four distinct defects, all now fixed:

1. **`handleChatKeys` "enter" case** gated plain input on `m.streaming` only. Text typed during a `!` command was never queued as busy work; it went to the debounce buffer and then fired at the agent. Now: `m.streaming || m.shellInFlight()`.
2. **`delayedChatInputMsg` handler** (the 1.5s debounce flush) re-checked only `streaming`/`compacting`. Now also `m.shellInFlight()`. This is **independent of #1, not redundant**: text typed while *idle* legitimately sits in the debounce buffer, and a `!` command can start during that 1.5s window before the tick fires. Fixing only #1 leaves this hole open.
3. **`shellFinishedMsg` handler never called `drainQueuedItems()`.** The drain was only reachable from `streamDoneMsg`, `compactFinishedMsg`, and the agent-replacement drain. Anything queued behind a `!` command therefore sat in the queue row forever. This also broke the `!`-behind-`!` case, which had *always* queued correctly at the input gate (that gate has a `m.shellStreamCmd != nil` clause added specifically to serialize `!` commands) — the second command queued but was never released.
4. **`drainReplacementQueueIfReady` guarded on `m.streaming` only.** The replacement gate in `handleChatKeys` (`m.modelSwitchPending || m.sessionResetPending || m.replacementQueuePending`) is evaluated **before** the shell gate, so input typed while BOTH a `!` command is running and a model/session replacement is pending never consults `shellInFlight()` on the way in — it lands in the replacement queue. The replacement drain then dispatched it to the LLM while the shell was still running: the same bug class as #1 and #2, one layer down, in a drain rather than an input gate. Now: `m.streaming || m.shellInFlight()`. This is the **second concrete instance** of the "a gate that checks only one flag is the bug" invariant below — which is why that invariant now names gates rather than merely describing them.

A new helper centralises the condition:

```go
func (m model) shellInFlight() bool {
	return m.shellStreamCmd != nil
}
```

## Solution

### Every busy gate checks both flags

Three gates that route or dispatch from `queuedItems` now test `m.streaming || m.shellInFlight()`:

- `handleChatKeys` "enter" case (immediate submission path)
- the `delayedChatInputMsg` debounce-flush handler
- `drainReplacementQueueIfReady` — the model/session-replacement drain

The third one matters even though it is a *drain* rather than an input gate: it consumes the same `queuedItems`, so it must honour the same busy set as the gates that fill them.

The replacement drain's guard deliberately mirrors the existing `m.streaming` guard, release path included. When `m.streaming` blocks the drain, the queue is released by `streamDoneMsg`'s own `drainQueuedItems()` call; when `m.shellInFlight()` blocks it, `shellFinishedMsg`'s drain is the release path (added in defect #3). `replacementQueuePending` latching true after a deferred drain is pre-existing behaviour in the `m.streaming` case too, so the shell case is consistent with it rather than a new quirk.

The `shellFinishedMsg` drain was added guarded by `!m.queueDrainBlocked()` (question/permission dialogs own the turn and drain via their own path) — matching the guard on the `streamDoneMsg` drain. A `!` command finishing is a busy→idle transition exactly like a stream ending.

`shellInFlight()` is cleared only in the `shellFinishedMsg` handler; nothing else assigns `m.shellStreamCmd = nil`.

### Subtlety: `EnqueueInjection` stays conditional on `m.streaming`

While an LLM turn runs, typed input is both queued in `queuedItems` *and* handed straight to the agent via `EnqueueInjection`, which splices it into the Step loop at the next tool-call boundary (the `streamMsgEvent` "user" branch then removes the queue entry).

During a `!` command there is **no agent Step loop**, so an injection would never be consumed and the queue entry would never be removed. The call is therefore `if m.streaming && m.agent != nil` — queued items during a shell simply wait for `shellFinishedMsg` to drain them.

### Cross-surface: the web composer already did this right

`web/src/components/Chat/ChatInput.tsx` computes:

```ts
const busy = isStreaming || shellInFlight || !!pendingPermission || compacting;
```

with the comment *"Shell command in flight, no agent turn running yet — nothing to inject into, so queue as before until it frees up."* The two surfaces had diverged; the TUI now matches web.

## Invariants a future change could break

- **Every input-queueing gate must check BOTH `m.streaming` AND `m.shellInFlight()` — as a checklist of named gates, not a principle.** A gate that checks only one is the bug this doc records, and `drainReplacementQueueIfReady` (defect #4) proved that a second instance can sit in a drain rather than an input gate. Before adding a submission path, a drain trigger, or a new busy condition, walk this list:
  1. **`handleChatKeys` "enter" case** — the immediate-submission path for plain text.
  2. **the `delayedChatInputMsg` flush** — the 1.5s debounce re-entry point; needs its own check because text typed while idle can be overtaken by a `!` command that starts mid-window.
  3. **`drainReplacementQueueIfReady`** — the model/session-replacement drain; it dispatches from the same `queuedItems`, so it is an input-queueing gate for this purpose.

  Then audit the adjacent, differently-conditioned gates under the same discipline: the `!`-prefix serialization gate (`m.shellStreamCmd != nil`), and every drain trigger (`shellFinishedMsg`, `streamDoneMsg`, `compactFinishedMsg`, dialog release). Queueing and draining are separate halves — a fix that covers only one still leaks.
- **`shellInFlight()` cannot latch true and strand the composer.** This is safe only because `runStreamingShell`'s reader goroutine reaches `close(ch)` unconditionally (the close is not conditional on the process outcome) and the final read of a closed channel yields `shellFinishedMsg`. If that ever becomes conditional on success — e.g. an early return that skips the close — the gate would deadlock all input behind a shell that never reports finished. Note the two `start`/pipe-open failure paths in `runStreamingShell` do return a synthetic `shellFinishedMsg` directly, so they never set `shellStreamCmd` at all.

## Where it fits

- [`agent-replacement-input-queuing.md`](agent-replacement-input-queuing.md) owns the unified-`queuedItems` story but is scoped to model/session replacement — a different trigger condition. Defect #4 above is where the two scopes overlap: a replacement pending across a `!` shell.
- [`Delayed Chat Input Consolidation`](../superpowers/specs/2026-09-09-chat-input-consolidation-design.md) defines the 1.5s debounce. As originally written it conditioned busy-queueing on a streaming LLM turn alone — the wording gap defects #1 and #2 close. It was amended 2026-09-29 to count an in-flight `!` shell command as busy and to record fixes #1–#4, cross-linking back here rather than restating this doc's detail.

## Files

| File | Role |
|------|------|
| `internal/tui/model.go` | `shellInFlight()` helper; the `m.streaming \|\| m.shellInFlight()` gates in `handleChatKeys`, the `delayedChatInputMsg` handler, and `drainReplacementQueueIfReady`; `drainQueuedItems()` call in the `shellFinishedMsg` handler; `runStreamingShell` and its unconditional `close(ch)` |
| `internal/tui/shell_input_queue_test.go` | 6 tests, each mutation-verified to fail without its fix: `TestChatInputQueuesWhileShellRunning`, `TestChatInputNotDelayedWhileShellRunning`, `TestDebouncedInputRequeuesIfShellStartsMidWindow`, `TestShellFinishDrainsQueue`, `TestQueuedBangCommandRunsAfterFirstFinishes`, `TestShellFinishDoesNotDrainBehindDialog` |
| `internal/tui/replacement_shell_gap_test.go` | 4 tests pinning defect #4: `TestTypingDuringShellAndModelSwitchQueuesForReplacement` (the gate-order path), `TestReplacementDrainHonoursShell`, `TestReplacementDrainLatchParityWithStreaming`, `TestReplacementQueueReleasedAfterShellFinishes` |
| `web/src/components/Chat/ChatInput.tsx` | The already-correct `busy` expression this change mirrors |

## Edge Cases and Gotchas

- **Defect #2 is not redundant with #1.** The debounce handler is a second, time-shifted entry point: text typed while idle can be overtaken by a `!` command that starts before the tick fires. A test that only covers "typed during shell" (gate #1) will not catch it.
- **Gate order in `handleChatKeys` is load-bearing — an earlier gate is a back door around a later one.** The replacement gate runs before the `m.streaming || m.shellInFlight()` gate, so plain text typed during a `!` command reaches the replacement queue without ever consulting `shellInFlight()`. The input gate being correct does not make the drain correct; `drainReplacementQueueIfReady` needed its own `shellInFlight()` check. When reordering gates, re-audit the invariant checklist above.
- **`!`-behind-`!` always queued but never drained.** The `!` input gate's `m.shellStreamCmd != nil` clause has serialized shell commands for a long time; the missing piece was the drain on `shellFinishedMsg`. Queueing and draining are separate halves — fix both.
- **Drain must stay dialog-guarded.** `shellFinishedMsg` drains only when `!m.queueDrainBlocked()` (no question/permission dialog), matching `streamDoneMsg`. Draining while a dialog owns the turn would start a new turn under an unanswered ask.
- **Injection during a shell is a silent leak, not a crash.** Nothing errors; the entry just never leaves `queuedItems` and the message fires late. Assert `agent.HasPendingInjections()` is false in tests to catch it.
- **The two surfaces had diverged.** Web's `busy` expression was correct before the TUI fix; when adding a busy condition, update both — and the test suite is what pins the TUI side (`shell_input_queue_test.go`, `replacement_shell_gap_test.go`).