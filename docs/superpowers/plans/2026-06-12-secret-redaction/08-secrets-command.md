# Part 8: `/mask` Slash Command

Spec §9. Mirrors `/permissions` (internal/tui/commands.go:85, handler `runPermissionsCmd` ~:583). Subcommand-style like /permissions rather than a brand-new modal framework — reuse whatever interaction pattern `/permissions model` already uses for its picker.

Prereq: Parts 3, 7.

### Task 8.1: Command registration + on/off/status

**Files:**
- Modify: `internal/tui/commands.go` — add entry to command table (:85 area) `{name: "/mask", usage: "/mask [on|off|status|model|list|add|display]", handler: runMaskCmd}`; new handler in same file or `internal/tui/secrets_cmd.go` if commands.go is already unwieldy
- Test: `internal/tui/command_test.go`

- [ ] Read `runPermissionsCmd` fully first; mirror its arg-dispatch shape.
- [ ] Write failing tests: `/mask status` prints enabled state + model; `/mask on` / `off` flips config via `SaveSecurityRedaction` and updates live redactor + sidebar.
- [ ] Implement.
- [ ] Tests PASS. Commit `feat(tui): /mask command with on/off/status`.

### Task 8.2: Model picker (local-only)

**Files:**
- Modify: `/mask model` subcommand — reuse the model-listing/picker mechanism `/permissions model` uses (internal/tui/picker.go conventions); filter to local providers
- Test: `internal/tui/command_test.go`

- [ ] Write failing tests: picker list contains ONLY local providers — LM Studio models first, then Ollama; cloud providers absent entirely (spec hard rule); selecting persists `security.redaction.model`; `/mask model <provider/model>` direct-set rejects non-local values with an explanatory message.
- [ ] Implement: source LM Studio live model list via the existing LM Studio detection (localhost:1234/v1 enumeration already in codebase) and Ollama equivalent; ordering LM Studio → Ollama, each alphabetical.
- [ ] Tests PASS. Commit `feat(tui): local-only security model picker`.

### Task 8.3: Secret list, manual add, display toggle

**Files:**
- Modify: `/mask list|add|display` subcommands
- Test: `internal/tui/command_test.go`

- [ ] Write failing tests: `list` shows index-ascending table (idx, kind, masked preview, source), paginated past one screen (sorted+paginated per global rule); `add <word>` registers a custom word (persisted to `customWords` AND registered in live session registry so it masks immediately); `display` toggles real-values vs placeholder chips (drives Part 7 mode flag, session-scoped, not persisted).
- [ ] Implement.
- [ ] Tests PASS. Commit `feat(tui): /mask list, add, display`.

### Task 8.4: Help text

**Files:**
- Modify: `internal/tui/commands.go` help/usage block (~:199-207 pattern)

- [ ] Add `/mask` examples alongside `/permissions` examples.
- [ ] `go build ./...` clean. Commit `docs(tui): /mask help text`.
