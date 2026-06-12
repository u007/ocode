# Part 9: Edge Cases + End-to-End Integration

Spec §10 + §Testing integration matrix. Final part — everything else merged.

Prereq: Parts 1–8.

### Task 9.1: Compaction token validation

**Files:**
- Modify: compaction/summarization flow in `internal/agent` (locate the compact path that calls `chatWithDelta` for summaries) — post-compact validation step
- Test: corresponding agent test file

- [ ] Write failing test: summary returned by a fake model with a mangled token (`[[OCSEC:a3f9c2:1]` truncated) or an unknown-index token → the affected original message is retained un-summarized; clean summary with intact tokens → accepted.
- [ ] Implement using `redact.FindCorruptTokens` (Part 6.3) + registry index check.
- [ ] Tests PASS. Commit `feat(agent): validate secret tokens after compaction`.

### Task 9.2: Mid-session enable scrub

**Files:**
- Modify: enable path (sidebar toggle + `/mask on`) — when the live session already has messages, offer one-time "scrub existing history?" action; scrub re-runs `RedactChat` (chat-mode) over stored message contents and rewrites the session file
- Test: `internal/tui/model_test.go`

- [ ] Write failing test: session with plaintext secret in history → enable → accept scrub → in-memory messages and re-saved session file contain placeholder; decline → history untouched (tripwire net still covers future sends).
- [ ] Implement; scrub must preserve message ordering/roles and use the vault-before-message write ordering.
- [ ] Tests PASS. Commit `feat: mid-session enable scrub`.

### Task 9.3: End-to-end integration tests

**Files:**
- Create: `internal/agent/redaction_integration_test.go` (or follow existing integration-test placement)

- [ ] Cover the spec integration matrix, one test per row, each with a fake provider server capturing outbound request bodies:
  - Four carriers: secret in → session file on disk has no plaintext (TUI, ACP, SSE, child-session).
  - Quick/advisor/title payloads: placeholders only.
  - Chokepoint net: secret planted in a system/context message → captured outbound body has placeholder; disabled mode → tripwire fired once.
  - Bash unmask round-trip: model emits token in command → tool receives real value → tool output echoing secret re-redacted in next history append.
  - Fail-mode: scanner endpoint down → block behavior per carrier.
  - Same-secret reuse: secret appears in msg 1 and msg 5 → single index.
- [ ] All PASS with `-race`. Commit `test: secret redaction end-to-end matrix`.

### Task 9.4: Docs + wrap-up

**Files:**
- Modify: `README.md` (feature section), ocode-usage skill doc if present in repo, `docs/` feature docs as discovered
- Check: `TODO.md` for any deferred items

- [ ] Document: enabling (sidebar / `/mask on`), local-only security model requirement, vault location, accepted risks (terminal scrollback, clipboard, 0600 plaintext vault), v2 items (keychain, egress hard-block).
- [ ] `go test ./... -race` + `go vet ./...` clean. Manual smoke per INDEX definition-of-done.
- [ ] Commit `docs: secret redaction feature documentation`.
