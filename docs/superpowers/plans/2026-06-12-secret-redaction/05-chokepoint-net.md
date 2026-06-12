# Part 5: Chokepoint Safety Net + Tripwire

Spec §1 (net + tripwire). Regex-only scan of the final assembled payload — including system prompt, context files, LSP-diagnostic injections — inside `GenericClient.ChatWithContext` (internal/agent/client.go:354). Covers advisor's separate client instance automatically (net lives in the method, not a wrapper).

Prereq: Parts 1, 3.

### Task 5.1: Net hook in ChatWithContext

**Files:**
- Modify: `internal/agent/client.go` — `GenericClient` gets optional `Redaction *redact.NetHook` field; scan at top of `ChatWithContext` before provider-specific serialization
- Create: `internal/redact/net.go` — `NetHook{Registry, Enabled, OnTripwire func(kinds []string)}` with `ScanMessages(msgs)` applying known-format-only detection
- Test: `internal/redact/net_test.go`, `internal/agent/client_test.go` (match existing client test conventions)

- [ ] Write failing tests (net.go): enabled hook redacts a known-format secret found in any message role (system/user/tool) and registers it (`source: "net"`); keyword-entropy heuristics NOT applied (known-format only — system prompts are file-like); already-tokenized text untouched.
- [ ] Write failing test (client): a message slice containing a raw `AKIA…` key reaches the captured outbound request body as placeholder (use the package's existing fake-server test pattern); nil hook → no-op (all existing client tests still pass unchanged).
- [ ] Implement. Scan runs on a copy of message contents — never mutates caller's history slice (history correctness owned by ingestion layer).
- [ ] `go test ./internal/agent/ ./internal/redact/` → PASS. Commit `feat(agent): regex safety net in ChatWithContext`.

### Task 5.2: Tripwire mode (feature disabled)

**Files:**
- Modify: `internal/redact/net.go`; `internal/tui/model.go` (one-time prompt); wiring where `GenericClient` instances are constructed (main agent, advisor `advisor_tool.go`, title client `title.go`)
- Test: `internal/redact/net_test.go`, `internal/tui/model_test.go`

- [ ] Write failing tests: with `Enabled: false`, `ScanMessages` does NOT modify text but invokes `OnTripwire(kinds)` once per session on first high-confidence (known-format) hit; subsequent hits in same session do not re-fire.
- [ ] Write failing TUI test: tripwire callback enqueues a one-time prompt offering "Enable secret redaction?" (Yes → flips config via `SaveSecurityRedaction` + enables live redactor; No → dismissed for session). Prompt must go through Bubble Tea messaging (no direct stdout — CLAUDE.md alt-screen rule).
- [ ] Implement; ensure every `GenericClient` construction site receives the hook (grep for `GenericClient{` / constructor calls; advisor + title clients included).
- [ ] Tests PASS. Commit `feat(redact): tripwire prompt when disabled`.
