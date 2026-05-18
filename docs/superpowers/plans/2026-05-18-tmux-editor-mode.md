# Tmux Editor Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add configurable external editor modes, including strict tmux split/window support with startup validation and no fallback for explicit tmux modes.

**Architecture:** Extend existing `OcodeConfig` editor support with `editor_mode`, add focused editor command/picker flows in the TUI, and isolate tmux command construction/validation behind small functions. Files tab continues to own file selection and preview refresh, while config owns persistence.

**Tech Stack:** Go 1.23, Bubble Tea, existing `internal/config` ocodeconfig persistence, existing TUI picker and command systems, tmux CLI.

---

## File Map

- Modify `internal/config/ocodeconfig.go`: add `EditorMode`, valid mode constants, load/save helpers, and startup validation hooks for editor mode data.
- Modify `internal/config/config_test.go`: cover load/save/default behavior for `editor_mode`.
- Modify `internal/tui/commands.go`: change `/editor` semantics and add `/editor-mode` command spec.
- Modify `internal/tui/model.go`: add command handlers, store current editor mode on model state, and route editor picker selections.
- Modify `internal/tui/tui.go`: run strict editor-mode startup validation before `tea.NewProgram`.
- Modify `internal/tui/picker.go`: support editor and editor-mode picker kinds.
- Modify `internal/tui/files_model.go`: open files through an injected opener that understands external/tmux modes; update hints.
- Modify `internal/tui/files_model_test.go`: cover files tab opener dispatch and hints.
- Modify `internal/tui/command_test.go`: cover `/editor` and `/editor-mode` command flows.
- Create: `internal/tui/editor_mode.go`: focused helper for editor-mode validation, tmux command construction, and editor command validation.
- Create: `internal/tui/editor_mode_test.go`: tests for validation and tmux command construction.

## Task 1: Persist `editor_mode`

**Files:**
- Modify: `internal/config/ocodeconfig.go`
- Modify: `internal/config/config_test.go`

- [ ] Write failing tests for default editor mode.
  - Verify `LoadOcodeConfig` returns `external` when no `editor_mode` is set.
  - Run: `go test ./internal/config -run 'Test.*EditorMode'`
  - Expected: fail because `EditorMode` does not exist yet.

- [ ] Write failing tests for loading `editor_mode` from `ocodeconfig.json`.
  - Use temp home/project config pattern already present in `config_test.go`.
  - Verify `tmux-split` and `tmux-window` load exactly.
  - Run: `go test ./internal/config -run 'Test.*EditorMode'`
  - Expected: fail because loader ignores `editor_mode`.

- [ ] Write failing tests for saving `editor_mode`.
  - Verify saved JSON includes `"editor_mode":"tmux-split"` when set.
  - Verify existing `editor` and permissions remain preserved.
  - Run: `go test ./internal/config -run 'Test.*EditorMode'`
  - Expected: fail because saver does not write `editor_mode`.

- [ ] Implement minimal config changes.
  - Add editor mode constants or typed string values for `external`, `tmux-split`, `tmux-window`.
  - Add `EditorMode string` to `OcodeConfig` and JSON file struct.
  - Default empty mode to `external` after load.
  - Parse and delete `editor_mode` from `Extra` like existing `editor` handling.
  - Save `editor_mode` when it is non-empty and not default, or save it explicitly if tests choose explicit persistence.
  - Add `SaveEditorMode(mode string) error` with validation.

- [ ] Run config tests.
  - Run: `go test ./internal/config -run 'Test.*EditorMode'`
  - Expected: pass.

- [ ] Run full config package tests.
  - Run: `go test ./internal/config`
  - Expected: pass.

## Task 2: Add tmux validation helper

**Files:**
- Create: `internal/tui/editor_mode.go`
- Create: `internal/tui/editor_mode_test.go`

- [ ] Write failing tests for explicit tmux validation.
  - Missing `$TMUX` returns an error containing `requires running inside tmux`.
  - Missing `tmux` binary returns an error naming `tmux`.
  - Failing `tmux display-message -p '#S'` returns an error naming the failed command.
  - Missing configured editor returns an error naming the editor.
  - External mode returns no error without tmux.

- [ ] Add test seams for environment, `LookPath`, and command execution.
  - Keep seams package-private and reset them in tests.
  - Avoid global mutation leaks between tests.

- [ ] Implement validation.
  - Validate only when mode is `tmux-split` or `tmux-window`.
  - Resolve editor using existing `config.ResolveEditor`.
  - Split editor command with existing `strings.Fields` behavior.
  - Check binary for first editor command part.

- [ ] Run validation tests.
  - Run: `go test ./internal/tui -run 'Test.*Tmux.*|Test.*EditorMode.*'`
  - Expected: pass.

## Task 3: Wire startup fail-fast behavior

**Files:**
- Modify: `internal/tui/tui.go`
- Test: `internal/tui/tui_test.go` or new focused tests in `internal/tui/editor_mode_test.go`

- [ ] Write failing test or narrow command-level test for startup validation.
  - Explicit tmux mode with missing `$TMUX` exits before TUI program starts.
  - Error output includes the fix text: `start ocode inside tmux` and `editor_mode to external`.

- [ ] Implement startup validation call.
  - After config load and before TUI launch, validate explicit tmux editor mode.
  - Return/print a clear error and exit non-zero on validation failure.
  - Do not fallback to external mode.

- [ ] Run startup-related tests.
  - Run: `go test ./internal/tui -run 'Test.*Startup.*EditorMode|Test.*Tmux.*'`
  - Expected: pass.

## Task 4: Add `/editor` picker and command behavior

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/picker.go`
- Test: `internal/tui/command_test.go`

- [ ] Write failing command tests for `/editor` with no args.
  - Verify it opens picker with kind `editor`.
  - Verify picker includes exactly these built-in choices first: `nvim`, `vim`, `nano`, `code --wait`, `cursor --wait`.

- [ ] Write failing command tests for `/editor <command>`.
  - Verify valid command saves config through a test seam.
  - Verify selected editor is applied to Files model via `SetEditor`.
  - Verify missing editor command reports error and does not save.

- [ ] Implement `/editor` command changes.
  - Update command help from “Reopen the external editor” to “Choose default external editor”.
  - No args opens picker.
  - Args join into editor command string and save globally after binary validation.
  - Keep existing message editor behavior only if another command already covers it; otherwise explicitly retire that behavior in help and tests.

- [ ] Add editor picker support.
  - Add `openEditorPicker` on root model or reuse files picker patterns with a distinct `pickerKind`.
  - On selection, call the same save/apply path as `/editor <command>`.

- [ ] Run command tests.
  - Run: `go test ./internal/tui -run 'Test.*Editor'`
  - Expected: pass.

## Task 5: Add `/editor-mode` picker and command behavior

**Files:**
- Modify: `internal/tui/commands.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/picker.go`
- Test: `internal/tui/command_test.go`

- [ ] Write failing command tests for `/editor-mode` with no args.
  - Verify it opens picker with kind `editor-mode`.
  - Verify picker options are `external`, `tmux-split`, `tmux-window`.

- [ ] Write failing command tests for `/editor-mode <mode>`.
  - Valid mode saves globally and updates in-memory config.
  - Invalid mode reports valid modes and does not save.
  - Tmux mode save invokes validation and reports validation failure without saving.

- [ ] Implement command spec.
  - Add `/editor-mode [external|tmux-split|tmux-window]` to command list/help.
  - Add handler with no fallback behavior.

- [ ] Implement picker selection handling.
  - On selection, run the same command path as typed `/editor-mode <mode>`.

- [ ] Run command tests.
  - Run: `go test ./internal/tui -run 'Test.*EditorMode'`
  - Expected: pass.

## Task 6: Route Files tab external open through editor modes

**Files:**
- Modify: `internal/tui/files_model.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/files_model_test.go`

- [ ] Write failing Files tests for external mode.
  - Pressing `e` or `enter` dispatches the current external editor opener.
  - Preview refresh still happens after external editor returns.

- [ ] Write failing Files tests for tmux split/window mode.
  - With mode `tmux-split`, selected file opens via tmux split command builder.
  - With mode `tmux-window`, selected file opens via tmux new-window command builder.
  - Runtime tmux command failure sets status and does not run external fallback.

- [ ] Refactor opener injection minimally.
  - Keep `filesModel` responsible for selected file and hints.
  - Let root model or a focused helper decide editor mode command.
  - Preserve current `E` per-file editor picker behavior unless replaced by root `/editor` picker in a later task.

- [ ] Implement tmux command construction.
  - Split mode: right split when width is at least 120; bottom split otherwise.
  - Window mode: new tmux window running selected editor and file.
  - Use configured editor command plus file path as final argument.

- [ ] Run Files tests.
  - Run: `go test ./internal/tui -run 'TestFiles.*Editor|TestFiles.*Tmux|TestFiles.*Preview'`
  - Expected: pass.

## Task 7: Update Files hints and fix discovered editor regressions

**Files:**
- Modify: `internal/tui/files_model.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/files_model_test.go`

- [ ] Write failing hint tests.
  - Preview mode shows `i inline edit`, `e external editor`, `E choose editor`, `/editor set default`.
  - Tmux split mode shows `e tmux split: <editor>`.
  - Edit mode shows `ctrl+s save | esc cancel`.

- [ ] Fix misleading hints.
  - Remove `ctrl+s` from normal preview hint.
  - Restore external editor hints removed during earlier Files tab changes.
  - Keep inline edit described as quick edit, not full editor.

- [ ] Fix keyboard digit regression from current branch before finalizing editor work.
  - Do not let bare `1`/`2`/`3`/`4` hijack chat input.
  - Keep visible tab shortcuts working from non-chat tabs or switch them to modifier-only shortcuts if tests indicate conflict.

- [ ] Fix save permission regression from current branch.
  - Preserve existing file mode when saving inline edits.
  - Add test with executable file mode.

- [ ] Run focused TUI tests.
  - Run: `go test ./internal/tui -run 'TestFiles|TestVisibleNumberTabShortcut|TestSidebarViewShowsChangedFilesAndTodoState'`
  - Expected: pass.

## Task 8: Full verification and docs check

**Files:**
- Modify docs only if command help or config docs exist and need updates.

- [ ] Search docs for editor config references.
  - Use `grep` for `editor`, `ocodeconfig`, and `/editor`.

- [ ] Update relevant docs if found.
  - Document `editor` and `editor_mode` values.
  - Document strict tmux startup failure behavior.

- [ ] Run full tests.
  - Run: `go test ./...`
  - Expected: all packages pass.

- [ ] Review diff for unrelated changes.
  - Run: `git diff --stat`
  - Run: `git diff -- internal/config internal/tui docs`
  - Confirm `internal/agent/*` changes are unrelated/pre-existing and not included in this feature unless explicitly requested.

- [ ] Commit only when user explicitly asks.
  - Suggested commit message: `feat: add tmux editor mode`

## Self-Review Notes

- Spec coverage: config persistence, `/editor`, `/editor-mode`, tmux split/window behavior, startup validation, no fallback, hints, and tests are covered by Tasks 1-8.
- Scope: single coherent editor-mode feature; no need to split into separate specs.
- Ambiguity resolved: tmux split uses right split at width >= 120, bottom split below 120; explicit tmux modes never fallback.
