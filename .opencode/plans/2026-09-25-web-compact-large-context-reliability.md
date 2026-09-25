# Web/Desktop Large-Context Compaction Reliability

**Date:** 2026-09-25
**Status:** Approved for implementation
**Design spec:** `docs/superpowers/specs/2026-09-25-web-compact-large-context-reliability-design.md`

## Goal

Fix web/desktop `/compact` failures that appear silent, especially for histories above roughly 400k tokens. Preserve the synchronous API and transcript on failure while making large multi-batch compaction resilient to slow first tokens and shared-client delta-hook interference.

## Implementation order (tests first)

1. Add the approved regression tests before implementation:
   - Per-call `GenericClient` delta callback survives another caller invoking `SetOnDelta(nil)`.
   - `runCompact` gives later summary batches a fresh inactivity deadline.
   - First-token grace allows a late first token; post-first-token idle still times out.
   - The fixed overall cap aborts a still-streaming batch and leaves the transcript unchanged.
   - Config field defaults/raw persistence/runtime normalization cover omitted, explicit zero, and explicit 300.
   - Compact timeout maps to HTTP 504 and does not mutate the session.
   - Web compaction rejection shows the sticky inline error and reports the app-wide action error.
2. Add `summary_first_token_timeout_seconds` to config structs, defaults, overlay parsing, runtime resolution, web TypeScript types, CompactForm, and config tests.
3. Add a per-call delta callback context path in `GenericClient` and change `runCompact` to use it instead of mutating `OnDelta`. Leave `chatWithDelta`'s intentional `SetOnDelta`/clear pair unchanged; document why.
4. Refactor compaction timeout handling:
   - fresh per-batch contexts;
   - first-token deadline from the new setting, then configured idle timeout after the first delta;
   - fixed 30-minute overall cap with a test override;
   - distinct `ErrCompactionTimeout` cause.
5. Update `HandleCompactSession` to map only the timeout sentinel to 504, log the session/error, skip response writes when the request is already cancelled, and leave all other failures as 500.
6. Update the web command path to preserve the existing inline sticky error and call `reportActionError` for a fallback app-wide surface.
7. Update documentation: `README.md`, config inline comments, `skills/ocode-agent-architecture/SKILL.md`, `CHANGES.md`, new `docs/concepts/compaction-config.md` via the context agent, and a concrete deferred `TODO.md` bullet.
8. Run formatting, targeted Go tests, targeted web tests, full relevant suites, typecheck/build, and inspect the final diff for concurrent-WIP conflicts.

## Constraints

- Do not add async jobs, sessionStorage recovery, SSE progress, or cancel controls.
- Do not mutate a transcript unless every summary batch succeeds.
- Do not classify every `context.Canceled` as 504.
- The working tree contains concurrent WIP; make targeted edits and inspect each affected diff.
## Implementation status

- Core implementation and regression tests completed on 2026-09-25.
- `go test ./...`, `go vet ./internal/agent ./internal/config`, `go build ./...`, web typecheck, and web build pass.
- Full web Vitest has 253 passing files / 2062 passing tests; 4 unrelated `useChat.remoteHost` host-arity tests still fail. No live provider-backed 400k run was performed.
