---
title: Delayed Chat Input Consolidation
status: approved
date: 2026-09-09
tags: [tui, web, chat, input, debounce]
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
- If an LLM turn is already streaming, ordinary chat submissions bypass the debounce and retain the existing injection/queue behavior.
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
