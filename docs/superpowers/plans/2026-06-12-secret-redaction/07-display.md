# Part 7: Display Re-substitution (TUI surfaces)

Spec §6. Real values rendered at display time with distinct styling (dim underline). Surfaces: message text, tool-call argument hints, permission prompt body (normal mode), clipboard copy. Web API and `/export-claude` deliberately serve placeholders (no change needed — verified by test only). Screen-share mode shows placeholders as `secret#N` chips.

Prereq: Parts 1, 4.

### Task 7.1: Render helper

**Files:**
- Create: `internal/tui/redact_render.go` — `renderSecrets(text string, m *model) string`: resolves session-nonce tokens to real values wrapped in the vaulted style, or to `secret#N` chips in placeholder display mode
- Test: `internal/tui/redact_render_test.go`

- [ ] Write failing tests: token → styled real value (assert value present, ANSI styling applied); placeholder mode → `secret#1` chip, raw value absent; foreign-nonce token untouched; no redactor → identity.
- [ ] Implement using `redact.TokenPattern` + registry `Lookup`; style via a new lipgloss style next to existing sidebar styles (model.go:916-919 area convention).
- [ ] Tests PASS. Commit `feat(tui): secret render helper`.

### Task 7.2: Wire surfaces

**Files:**
- Modify: `internal/tui/model.go` — `displayTextForAgentMessage` call sites (:5910) so transcript text passes through `renderSecrets`; permission prompt body rendering (:8018) normal mode; `internal/tui/tool_render.go` — `formatToolCallHint` (:25)
- Test: `internal/tui/model_test.go`, `internal/tui/tool_render_test.go`

- [ ] Write failing tests: transcript message containing a token renders real value; tool-call hint (bash command with token) renders real value in hint line; permission prompt (non-secret-aware path) renders via helper too; **selection-copy and clipboard copy paths copy the real value** (extractSelectionText operates on rendered lines — verify rendered = resolved).
- [ ] Implement — single `renderSecrets` call per surface, applied before width-clamping so clamp math sees final text.
- [ ] Tests PASS. Commit `feat(tui): re-substitute secrets across render surfaces`.

### Task 7.3: Placeholder-passthrough surfaces (verification only)

**Files:**
- Test: `internal/server/handler_test.go` (or existing server test file), `internal/tui/model_test.go` (`/export-claude` path, :6027)

- [ ] Write tests asserting: `GET /api/sessions/{id}` body contains the placeholder and NOT the raw value; `/export-claude` output file contains placeholder only. (No production change expected — these lock the spec's deliberate behavior.)
- [ ] Tests PASS. Commit `test: lock placeholder passthrough for web API and export`.
