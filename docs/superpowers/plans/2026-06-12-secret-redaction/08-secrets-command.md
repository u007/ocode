# Part 8: `/mask` Slash Command

Spec §9. Mirrors `/permissions` (internal/tui/commands.go:85, handler `runPermissionsCmd` ~:583). Subcommand-style like /permissions rather than a brand-new modal framework — reuse whatever interaction pattern `/permissions model` already uses for its picker.

Prereq: Parts 3, 7.

### Task 8.1: Command registration + on/off/status

**Files:**
- Modify: `internal/tui/commands.go` — add entry to command table (:85 area) `{name: "/mask", usage: "/mask [on|off|status|model <name>|list]", handler: runMaskCmd}`; new handler in same file or `internal/tui/secrets_cmd.go` if commands.go is already unwieldy
- Test: `internal/tui/command_test.go`

- [ ] Read `runPermissionsCmd` fully first; mirror its arg-dispatch shape.
- [ ] Write failing tests: `/mask status` prints enabled state + model; `/mask on` / `off` flips config via `SaveSecurityRedaction` and updates live redactor + sidebar.
- [ ] Implement.
- [ ] Tests PASS. Commit `feat(tui): /mask command with on/off/status`.

### Task 8.2: Direct tier-2 model set (no picker)

**Files:**
- Modify: `/mask model` subcommand — directly set or show the tier-2 scanning model.
  No model picker: `/mask model <name>` persists `security.redaction.model` via
  `config.SaveSecurityRedaction` and updates the live `m.redactionModel` field.
  `/mask model` with no args shows the current tier-2 model.
- Test: `internal/tui/command_test.go`

- [ ] Write failing tests: `/mask model <name>` persists via `config.SaveSecurityRedaction` and updates `m.redactionModel`; `/mask model` (no args) shows current model or "(not configured - tier-2 scanning disabled)"; `/mask model` with empty config shows the disabled message.
- [ ] Implement: `case "model":` in `runMaskCmd`. When `len(args) > 1`, call `config.SaveSecurityRedaction(func(rc *RedactionConfig) { rc.Model = args[1] })` and set `m.redactionModel = args[1]`. When no args, show current model.
- [ ] Tests PASS. Commit `feat(tui): /mask model direct-set for tier-2 scanning model`.

### Task 8.3: Secret list

**Files:**
- Modify: `/mask list` subcommand
- Test: `internal/tui/command_test.go`

- [ ] Write failing tests: `list` shows index-ascending table (idx, kind, masked preview, source); `list` on empty registry shows "No secrets registered in this session."
- [ ] Implement: `case "list":` iterates `m.redactionRegistry.All()` and prints each entry with index, kind, masked preview, and source. Source falls back to "(unknown)" when empty.
- [ ] Tests PASS. Commit `feat(tui): /mask list with source column`.

### Task 8.4: Help text

**Files:**
- Modify: `internal/tui/commands.go` help/usage block (~:199-207 pattern)

- [ ] Add `/mask` examples alongside `/permissions` examples.
- [ ] `go build ./...` clean. Commit `docs(tui): /mask help text`.
