---
title: Delayed Chat Input Consolidation
status: approved
date: 2026-09-09
tags: [tui, web, chat, input, debounce]
type: Design
timestamp: 2026-09-28T17:31:14Z
---
# Delayed Chat Input Consolidation

## Goal

When a user submits ordinary chat messages in the TUI or web UI, delay the LLM turn by 1.5 seconds so rapid submissions are consolidated into one LLM input. Preserve the existing behavior for messages submitted while a turn is already streaming.

## Scope and semantics

- Applies to ordinary chat messages only.
- Slash commands and `!shell` commands retain their existing immediate/queued semantics.
- The delay is a trailing debounce: the 1.5-second timer starts after the first accepted idle chat submission and resets after each additional idle chat submission.
- When the quiet period expires, messages are joined in submission order with a blank line and sent through the existing chat submission path.
- If a slash or shell command arrives while a delayed chat batch is pending, flush the pending batch before dispatching the command so input order is preserved.
- If the session is busy — an LLM turn already streaming **or** a `!` shell command in flight — ordinary chat submissions bypass the debounce and are queued immediately: injected into the running turn when one exists, held until the shell finishes when only a shell is running (a shell has no agent Step loop to inject into). *(Busy set amended 2026-09-29 — see the amendment note at the end; as originally written this bullet conditioned on a streaming LLM turn alone.)*
- Whitespace-only messages remain ignored.
- File/editor/preview references remain part of each captured message; consolidation must not drop them.
- A failed consolidated submission restores the entire combined text as one retryable draft/queue item.

## TUI design

Add model-local delayed chat state and a generation-tagged timer message. On an idle ordinary submission, capture the text, clear the composer as today, and schedule a 1.5-second tick. New idle submissions append to the pending batch and replace the generation, making older timer messages harmless. The flush handler concatenates the batch and routes it through the existing file-reference processing path exactly once, which creates the canonical user message and starts the existing agent loop. Busy/streaming paths remain unchanged.

Pending state is transient and is not persisted in session JSON. The UI renders a short pending/batched indication without adding an LLM-visible transcript message before flush. Session reset, agent replacement, cancellation, and shutdown clear or invalidate the pending generation so stale timer messages cannot submit to a new session.

## Web design

Add per-ChatInput delayed state around the existing normal-message send path. Capture the fully assembled message (including attachment/editor/preview references), clear the draft as today, and debounce before calling `sendMessage`. Additional idle submissions append to the same batch. The existing busy/streaming queue paths remain unchanged. A command submitted while a batch is pending flushes the batch first, then dispatches the command. Use a visible pending state/count so the user understands why the LLM has not started yet.

The web layer sends one normal API message after the debounce. No server endpoint, SSE protocol, session schema, or persistence changes are required. New-session rekeying continues to happen when the single consolidated request is accepted.

## Failure and ordering behavior

- Timer callbacks carry a generation/token and are ignored if stale.
- A failed flush leaves the consolidated text available for retry and does not create duplicate queue entries.
- The pending batch is flushed before commands to preserve user submission order.
- A pending batch is isolated per TUI model/session tab; switching or resetting a session invalidates it.
- Web pending state is isolated to the ChatInput/session tab instance and is cleared on successful flush or session change.

## Testing

Add focused unit/component coverage for:

1. one ordinary message waits approximately 1.5 seconds before dispatch;
2. multiple messages in the debounce window become one ordered input;
3. later input resets/invalidates the prior timer;
4. commands flush pending chat before command dispatch;
5. streaming submissions bypass the delay and retain current queue/injection behavior;
6. failed consolidated sends preserve the full combined text for retry;
7. session reset/tab change drops stale pending work;
8. TUI and web builds/tests remain green.

No project documentation beyond this design spec needs behavior updates because this is an internal interaction-timing change with no public API or persistence change.

## Amendment (2026-09-29): an in-flight `!` shell command is also busy

The busy rule above originally read: "If an LLM turn is already streaming, ordinary chat submissions bypass the debounce and retain the existing injection/queue behavior." The busy set was the streaming LLM turn alone. That was incomplete: a `!` shell command is an independent subsystem — `m.streaming` is set only by `streamStartedMsg`, while a shell runs through `startShellExecution` → `runStreamingShell` with `m.shellStreamCmd` non-nil — so a message typed during `!sleep 600` fell through to the debounce and was dispatched to the LLM 1.5 seconds later while the shell was still running, and anything queued behind the shell was never drained.

Four fixes in `internal/tui/model.go` — the first three bringing the TUI in line with the web composer, whose busy set already included the shell (`busy = isStreaming || shellInFlight || !!pendingPermission || compacting` in `web/src/components/Chat/ChatInput.tsx`), the fourth closing the same gap one layer down in the agent-replacement drain:

1. The `handleChatKeys` "enter" case and the `delayedChatInputMsg` debounce flush both gate on `m.streaming || m.shellInFlight()`. The flush needs its own check because text typed while idle can still sit in the debounce buffer when a `!` command starts.
2. `shellFinishedMsg` now calls `drainQueuedItems()` (guarded by `queueDrainBlocked()`, matching the `streamDoneMsg` drain); previously the queue was released only on an LLM-turn boundary, stranding anything queued behind a shell.
3. `EnqueueInjection` stays conditional on `m.streaming` — during a shell there is no agent Step loop to consume an injection.
4. `drainReplacementQueueIfReady` now also guards on `m.shellInFlight()`. The replacement gate in `handleChatKeys` is evaluated before the shell gate, so input typed while both a `!` command was running and a model/session replacement was pending landed in the replacement queue and was dispatched to the LLM mid-shell. The guard deliberately mirrors the `m.streaming` guard, release path included: a blocked drain is released by `shellFinishedMsg` the way `streamDoneMsg` releases the streaming case.

Full write-up, the named-gate invariant checklist, and regression notes: [`docs/gotchas/tui-input-queueing-behind-shell-command.md`](../../gotchas/tui-input-queueing-behind-shell-command.md).