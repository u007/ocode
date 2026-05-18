# Favorite Recent Models Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add favorite model support to the model picker while preserving recent model behavior.

**Architecture:** Persist favorites in the existing opencode model state file beside recents. The config package owns state read/write helpers; the TUI picker owns grouping, favorite toggle interaction, and display.

**Tech Stack:** Go 1.26.1, existing `internal/config` state file helpers, Bubble Tea picker flow.

---

## Files

- Modify `internal/config/state.go`: add favorite load/save/remove helpers.
- Modify `internal/config/config_test.go`: add favorite state tests.
- Modify `internal/tui/model.go`: add model picker `f` toggle key handling and header skip navigation.
- Modify `internal/tui/picker.go`: render favorites, recents, provider groups, and filtered favorites.
- Modify `internal/tui/command_test.go` or `internal/tui/model_test.go`: add picker behavior tests.

## Tasks

- [ ] Add failing config tests for `LoadFavorites`, `SaveFavoriteModel`, `RemoveFavoriteModel`, and preservation of recent/variant fields.
- [ ] Implement favorite state helpers in `internal/config/state.go`.
- [ ] Add failing TUI tests for picker order: favorites first, recents second excluding favorites, then other models by provider.
- [ ] Add failing TUI tests for `f` toggling selected model favorite and rebuilding picker.
- [ ] Add failing TUI tests for header rows not being selectable and filter mode returning flat model matches.
- [ ] Implement picker grouping and header metadata in `internal/tui/picker.go` and `internal/tui/model.go`.
- [ ] Run focused tests: `go test ./internal/config -run 'Test.*Favorite|Test.*Recent'` and `go test ./internal/tui -run 'Test.*ModelPicker|Test.*Favorite'`.
- [ ] Run full verification: `go test ./...`.

## Self-Review Notes

- Favorites and recents are both included.
- Selecting a model keeps existing recent-save behavior through `handleModelCmd`.
- Favorite toggle does not change the current model by itself.
- No tmux/editor behavior changes belong in this task.
