# Part 6: Tool Unmask + Secret-Aware Permission Prompt

Spec §7. Single substitution point in `HandleToolCall` (internal/agent/agent.go:1089). Permission prompt switches to high-visibility mode when tokens present; egress commands escalate; allowlists never skip the prompt when tokens are present. File write-back with corrupted token fails loudly.

Prereq: Parts 1, 4.

### Task 6.1: Token detection + unmask in HandleToolCall

**Files:**
- Modify: `internal/agent/agent.go` — `HandleToolCall` (:1089): scan `args json.RawMessage` for session-nonce tokens; resolve via registry just before `tool.Execute`
- Test: `internal/agent/agent_test.go` (match existing HandleToolCall test conventions)

- [ ] Write failing tests: bash tool args containing `[[OCSEC:<nonce>:1]]` execute with the real value substituted (stub tool records received args); foreign-nonce tokens left literal; sessions without redactor unaffected.
- [ ] Implement `resolveToolArgs(args) (resolved json.RawMessage, secretsUsed []SecretRef)` — substitution on the raw JSON string (tokens are ASCII-safe inside JSON), returning refs (index, kind, masked preview) for the prompt layer.
- [ ] Tests PASS. Commit `feat(agent): unmask OCSEC tokens at tool execution`.

### Task 6.2: Secret-aware permission prompt + egress escalation

**Files:**
- Modify: permission-request flow — where tool permission requests are built (follow `req.Args` to its producer in `internal/agent`), add `SecretsUsed []SecretRef` + `EgressRisk bool`; `internal/tui/model.go` `renderPermissionRequestBody` (:8018-8068) renders the high-visibility banner
- Create: `internal/redact/egress.go` — `IsEgressCommand(cmd string) bool` heuristics (curl/wget/nc/ssh/scp/rsync, URL-bearing args)
- Test: `internal/redact/egress_test.go`, `internal/tui/model_test.go`, `internal/agent/agent_test.go`

- [ ] Write failing tests (egress.go): positives (`curl https://…`, `wget`, `nc host port`, `ssh user@host`, command containing `http(s)://` URL), negatives (`ls`, `go test`, `git status`).
- [ ] Write failing tests (agent): when `secretsUsed` non-empty, the tool call ALWAYS produces a permission request even if tool/prefix is allowlisted/auto-allowed (assert bypass paths — yolo excluded per existing yolo semantics? **No**: spec says never skipped; include yolo in the always-prompt assertion); request carries refs + egress flag for bash commands.
- [ ] Write failing TUI render test: prompt body lists each secret as `#<idx> <kind> hun•••r2 — real value will be injected`; egress flag adds hard warning banner line; rows clamped `.Width(w).MaxHeight(1)`.
- [ ] Implement masked-preview helper in `internal/redact` (first 3 + last 2 chars for len≥8, otherwise full mask).
- [ ] Tests PASS. Commit `feat: secret-aware permission prompt with egress warning`.

### Task 6.3: Corrupted-token guard on file write-back

**Files:**
- Modify: file-writing tool implementation(s) in `internal/agent` (Write/Edit tool `Execute`) — before writing resolved content, detect partial/corrupted OCSEC fragments
- Create: `internal/redact/corrupt.go` — `FindCorruptTokens(text, nonce) []string` (e.g. `[[OCSEC:` prefix without valid close, token with valid shape but unknown index)
- Test: `internal/redact/corrupt_test.go`, file-tool test file

- [ ] Write failing tests: `[[OCSEC:a3f9c2:` (truncated), `[[OCSEC:a3f9c2:99]]` (unknown index) flagged; valid known token not flagged.
- [ ] Write failing tool test: write/edit with corrupted token in content returns an error (write blocked, error surfaced to model + user), valid tokens resolve and write succeeds.
- [ ] Implement; error message names the fragment position, logged via the debug sink (offsets only).
- [ ] Tests PASS. Commit `feat(agent): block file write-back on corrupted secret tokens`.
