# Part 4: Ingestion Wiring (4 carriers + title call-site)

Spec §1. A shared `*redact.Redactor` is constructed per session and injected into each carrier. Every carrier redacts text **before** appending to its history slice / persisting. Fail-mode (`ErrScannerUnavailable`) raises the block modal in TUI; headless carriers degrade per config `failMode`.

Prereq: Parts 1–3.

### Task 4.1: Redactor lifecycle + session integration

**Files:**
- Create: `internal/agent/redaction.go` (constructor helper `NewSessionRedactor(cfg, sessionID, projectSlug)` — builds Registry with loaded-or-new nonce, vault path, scanner from config)
- Modify: `internal/session/session.go` — `DeleteSession` path also deletes vault; session load restores registry from vault
- Test: `internal/agent/redaction_test.go`, `internal/session/session_test.go`

- [ ] Write failing tests: new session → fresh nonce persisted on first secret; reopening a session reloads vault and resolves prior tokens; deleting session removes `<ses_id>.vault.json`.
- [ ] Implement; vault base from `redact.DefaultVaultBase()`.
- [ ] Tests PASS. Commit `feat(agent): per-session redactor lifecycle`.

### Task 4.2: TUI carrier

**Files:**
- Modify: `internal/tui/model.go` — user-message assembly before `m.messages = append(...)` (~:1957); tool-result ingestion point where tool output becomes a history message; fail-mode modal (reuse permission-modal component style at :8018)
- Test: `internal/tui/model_test.go`

- [ ] Write failing tests: with redaction enabled, submitted user text containing a `ghp_…` token is stored in `m.messages` (and session file via save) as placeholder; tool output echoing the same secret reuses the same index; `ErrScannerUnavailable` triggers the block modal with Send-regex-only / Cancel / Retry actions (Send proceeds with tier-1-masked text; Cancel aborts send; Retry re-invokes scan).
- [ ] Implement: call `redactor.RedactChat` at both ingestion sites; modal per existing permission-prompt pattern.
- [ ] Tests PASS. Commit `feat(tui): ingestion redaction + fail-mode modal`.

### Task 4.3: ACP bridge carrier

**Files:**
- Modify: `internal/acp/bridge.go` — append site (:105)
- Test: `internal/acp/bridge_test.go`

- [ ] Write failing test: bridge with enabled redactor stores placeholder in `b.messages` for an injected known-format secret. Headless fail-mode: `failMode:"block"` + scanner error → message send returns an error to the ACP client (no interactive modal available); document in test name.
- [ ] Implement: `RedactChat` before append; on `ErrScannerUnavailable` honor configured failMode (`block` → error; only explicit user config `regex-only`-equivalent proceeds).
- [ ] Tests PASS. Commit `feat(acp): ingestion redaction`.

### Task 4.4: Web/SSE carrier

**Files:**
- Modify: `internal/server/handler_sse.go` — `agentSession` append/save (:225) and the user-message intake for that session
- Test: `internal/server/handler_sse_test.go` (create if absent, matching existing server test conventions)

- [ ] Write failing test: POSTed message with known-format secret → persisted session JSON contains placeholder only; API response (`handler.go:166` style) serves placeholders as-is (no vault access — assert raw value absent from response body).
- [ ] Implement same pattern as 4.3 (headless fail-mode).
- [ ] Tests PASS. Commit `feat(server): ingestion redaction for web sessions`.

### Task 4.5: Child-session carrier + title call-site

**Files:**
- Modify: `internal/agent/child_session.go` — persist callback; `internal/agent/title.go` (:30) call sites in TUI/agent that pass raw strings
- Test: `internal/agent/child_session_test.go`, `internal/agent/title_test.go`

- [ ] Write failing tests: child-session persisted messages contain placeholders; `GenerateTitleAsync` callers pass placeholder-substituted strings (test: spy client records payload, assert no raw secret).
- [ ] Implement: child persist callback runs parent redactor (child shares parent session's registry/nonce — same vault); title call sites substitute via `redactor.Registry.Substitute` before passing strings.
- [ ] Tests PASS. Commit `feat(agent): child-session + title redaction`.
