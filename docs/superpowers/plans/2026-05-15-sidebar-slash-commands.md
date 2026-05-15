# Sidebar, Slash Commands, and Telemetry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a right sidebar, slash-command registry/autocomplete, `/model` suggestions, session telemetry, and clickable changed-file rows.

**Architecture:** One command registry will power execution, palette filtering, autocomplete, and help. The TUI will render a responsive split layout with a live sidebar fed by model state, session-scoped snapshot data, session-scoped todo state, and tool state. Token usage will come from provider usage fields and spend will be computed from a bundled models.dev pricing table.

**Tech Stack:** Go 1.23, Bubble Tea, Bubbles textarea/viewport, Lipgloss, existing agent/snapshot/tool packages.

---

### Task 1: Add command registry and autocomplete helpers

**Files:**
- Create: `internal/tui/commands.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write the failing test**

Add table-driven tests that assert:

- `/help`, `/session`, `/compact`, `/undo`, `/redo`, `/export`, `/new`, `/thinking`, `/models`, `/details`, `/init`, `/sidebar`, and `/model` all exist in the registry.
- registry aliases resolve to the same command.
- palette suggestions include registry commands instead of a hard-coded list.
- `/m` resolves to `/model`.
- `/model ` returns model-name suggestions.

- [ ] **Step 2: Run the focused tests and confirm they fail**

Run: `go test ./internal/tui -run 'Test(Command|Palette|Autocomplete)' -v`

Expected: fail because the registry and autocomplete helpers do not exist yet.

- [ ] **Step 3: Implement the smallest registry**

Create a `commandSpec` slice in `internal/tui/commands.go` with canonical name, aliases, help text, `takesModelArg`, and handler lookup. Keep the list static and small.

- [ ] **Step 4: Wire execution and suggestions to the registry**

Update `handleCommand`, palette rendering, and a new autocomplete helper so they all read the same registry data.

- [ ] **Step 5: Re-run the focused tests**

Run: `go test ./internal/tui -run 'Test(Command|Palette|Autocomplete)' -v`

Expected: PASS.

### Task 2: Parse provider usage and compute spend from models.dev pricing

**Files:**
- Modify: `internal/agent/client.go`
- Create: `internal/pricing/modelsdev.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/tui/model.go`
- Test: `internal/agent/client_test.go` or `internal/tui/model_test.go`

- [ ] **Step 1: Write the failing test**

Add tests that verify:

- OpenAI responses with a `usage` object are parsed.
- Anthropic responses with input/output token usage are parsed.
- spend is computed from exact tokens and a known models.dev price.
- unknown models do not fabricate a price.
- cost labels remain explicit when pricing is missing.

- [ ] **Step 2: Run the focused tests and confirm they fail**

Run: `go test ./internal/agent ./internal/tui -run 'Test(Usage|Spend|Pricing)' -v`

Expected: fail because usage is not tracked and pricing lookup does not exist.

- [ ] **Step 3: Add exact usage tracking**

Extend the provider response decoding in `internal/agent/client.go` so the client captures prompt/output token usage when the provider returns it.

- [ ] **Step 4: Add a pricing lookup table**

Create a small `internal/pricing` package with a bundled models.dev pricing snapshot for the models this app already surfaces. Use exact model names and keep unknown models explicit.

- [ ] **Step 5: Surface the values in the sidebar state**

Store the latest usage and computed spend on the model side so the sidebar can render them live.

- [ ] **Step 6: Re-run the focused tests**

Run: `go test ./internal/agent ./internal/tui -run 'Test(Usage|Spend|Pricing)' -v`

Expected: PASS.

### Task 3: Add responsive sidebar layout and toggle controls

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write the failing test**

Add tests that verify:

- `Ctrl+B` toggles sidebar visibility.
- `/sidebar` toggles sidebar visibility.
- wide terminals render a sidebar column.
- narrow terminals render the old single-column layout.

- [ ] **Step 2: Run the focused tests and confirm they fail**

Run: `go test ./internal/tui -run 'Test(Sidebar|Toggle)' -v`

Expected: fail because the sidebar state and split layout do not exist yet.

- [ ] **Step 3: Add sidebar state and layout logic**

Add sidebar visibility state, a width threshold, and split-pane rendering in `View()`. Keep the existing main transcript and input behavior intact.

- [ ] **Step 4: Wire keybinding and slash command**

Add `Ctrl+B` handling in `Update()` and register `/sidebar` in the command registry.

- [ ] **Step 5: Re-run the focused tests**

Run: `go test ./internal/tui -run 'Test(Sidebar|Toggle)' -v`

Expected: PASS.

### Task 4: Show MCP/LSP status, changed files, TODO, and clickable file rows

**Files:**
- Modify: `internal/snapshot/snapshot.go`
- Create: `internal/tui/sidebar.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tool/lsp.go`
- Modify: `internal/agent/agent.go`
- Test: `internal/snapshot/snapshot_test.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Write the failing test**

Add tests that verify:

- the snapshot package can report the current session changed-file list,
- the sidebar renders changed files,
- clicking a changed file dispatches an editor open action for that path,
- MCP/LSP status blocks show configured/loaded state text.
- TODO state updates from the in-session `todowrite` tool output.

- [ ] **Step 2: Run the focused tests and confirm they fail**

Run: `go test ./internal/snapshot ./internal/tui -run 'Test(ChangedFiles|Sidebar|Editor)' -v`

Expected: fail because there is no public snapshot listing or sidebar click handling.

- [ ] **Step 3: Expose snapshot state**

Add a read-only accessor in `internal/snapshot/snapshot.go` that returns the current session snapshot paths, plus a reset path for new sessions, so the sidebar can list only session-local changed files.

- [ ] **Step 4: Add sidebar section rendering**

Render the session/id, context, spend, MCP, LSP, changed files, TODO, and command hint sections from one sidebar renderer.

- [ ] **Step 5: Wire changed-file clicks to the editor**

Add mouse handling that detects file rows and launches the configured external editor with the selected path.

- [ ] **Step 6: Add session TODO state wiring**

Wire `todowrite` tool results into the session todo state used by the sidebar. The sidebar must read the live session state, not `TODO_OCODE.md`.

- [ ] **Step 7: Re-run the focused tests**

Run: `go test ./internal/snapshot ./internal/tui -run 'Test(ChangedFiles|Sidebar|Editor)' -v`

Expected: PASS.

### Task 5: Full verification

**Files:**
- All files touched above

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 2: Do a manual TUI smoke test**

Start the app, verify `Ctrl+P`, `Ctrl+B`, `/sidebar`, `/model ` completion, wide/narrow layout switching, and sidebar file clicks.

- [ ] **Step 3: Commit**

```bash
git add internal docs/superpowers
git commit -m "feat: add sidebar and slash command flow"
```
