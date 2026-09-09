# Plan: Delayed chat input consolidation

**Date:** 2026-09-09
**Design:** `docs/superpowers/specs/2026-09-09-chat-input-consolidation-design.md`

## Scope

Implement a 1.5-second trailing debounce for ordinary idle chat submissions in both the TUI and web ChatInput. Preserve current streaming injection/queue behavior and command semantics. Avoid server/API/schema changes.

## Steps

1. Add TUI delayed-input state and a generation-tagged timer message.
   - Store pending ordinary messages, timer generation, and a pending status.
   - Idle ordinary Enter captures input, resets the composer, and schedules the trailing timer.
   - Additional idle ordinary Enter appends and replaces the timer generation.
   - Flush combines messages in order and passes the combined text through `processFileReferences` once.
   - Keep streaming, compaction, replacement, and MCP-gated paths on their existing queue behavior.
   - Flush pending chat before slash/shell command dispatch and invalidate pending state on session replacement/reset.

2. Add TUI tests.
   - Test timer generation/consolidation helper and command-boundary behavior.
   - Test streaming bypass remains queued/injected.
   - Test stale timers cannot submit after reset.

3. Add web delayed state in `ChatInput.tsx`.
   - Keep a per-tab pending batch and generation timer.
   - Capture fully assembled messages, including file/editor/preview refs.
   - Show pending batch status/count.
   - Flush once after quiet period, using existing `sendMessage` and failure recovery.
   - Flush before commands; preserve streaming/busy queue branches.
   - Clear pending state on session-tab change/unmount.

4. Add web tests for consolidation, reset, command boundary, bypass, and failure recovery using existing component/test conventions.

5. Format and validate only touched areas.
   - `gofmt` on touched Go files.
   - `go test ./internal/tui/...` (or focused package tests first).
   - `cd web && npm test`/project-supported targeted Vitest tests and `npm run build`.
   - Check diagnostics and inspect the final diff, leaving unrelated working-tree changes untouched.

## Risks

- TUI timer messages are not cancellable; generation checks must reject stale ticks.
- Command ordering must flush pending chat before command dispatch.
- Web send failures must not lose the combined message.
- Session changes must not let old pending timers submit into a new tab/session.
- Existing user modifications are broad; edits must remain limited to the feature files and new docs/tests.
