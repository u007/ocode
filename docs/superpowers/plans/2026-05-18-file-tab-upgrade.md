# File Tab Upgrade Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the Files tab from a read-only previewer into a practical file workspace with inline editing, file actions, mouse support, metadata, syntax labels, and git badges.

**Architecture:** Keep `filesModel` as the state owner. Add small state modes for normal viewing, inline editing, prompts, and confirmations. Preserve external-editor behavior and layer inline editing on top of the existing tree + right-pane layout.

**Tech Stack:** Go 1.23/1.26, Bubble Tea v2, Bubbles viewport/textarea, Lipgloss, standard library file and git commands.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/tui/files_model.go` | Modify | File-tab state, actions, preview, inline edit, tree rendering |
| `internal/tui/model.go` | Modify | Route file-tab mouse wheel and refresh after editor exit |
| `internal/tui/files_model_test.go` | Create | Unit tests for file-tab behavior |
| `docs/superpowers/specs/2026-05-18-file-tab-upgrade-design.md` | Create | Design record |

## Task 1: Test File-Tab Core Behavior

- [ ] Add tests for preview metadata/path display.
- [ ] Add tests for inline edit save and cancel.
- [ ] Add tests for create file, create directory, rename, and delete confirmation.
- [ ] Add tests for git-status parsing and language-label detection.
- [ ] Add tests for refreshing the selected preview.
- [ ] Run targeted tests and verify they fail for missing behavior.

## Task 2: Implement Files Model Features

- [ ] Add file-tab modes for normal view, inline edit, name prompt, and delete confirmation.
- [ ] Add textarea-backed inline editing for text files only.
- [ ] Add file action handlers and disk operations with visible status messages.
- [ ] Add selected-path metadata and language labels.
- [ ] Add git status parsing and tree badges.
- [ ] Add refresh helpers that rebuild the visible tree without surprising cursor jumps.
- [ ] Run targeted tests and verify they pass.

## Task 3: Wire Root TUI Events

- [ ] Route mouse wheel events to the Files preview or inline editor when the Files tab is active.
- [ ] Refresh the Files preview after an external editor exits.
- [ ] Update Files-tab status hints to include new actions.
- [ ] Run targeted tests and verify they pass.

## Task 4: Full Verification

- [ ] Run `go test ./internal/tui/...`.
- [ ] Run `go test ./...`.
- [ ] Fix regressions with test-first changes if new failures appear.
- [ ] Review diff for unrelated changes and keep scope surgical.

## Self-Review

- Spec coverage: all requested file-tab fixes are covered.
- Placeholder scan: no deferred implementation remains in this plan.
- Scope check: syntax treatment is intentionally lightweight to avoid dependency churn.
