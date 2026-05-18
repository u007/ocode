# Git Tab Overhaul — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all identified bugs and implement missing git workflow features in the TUI git tab.

**Architecture:** All changes are confined to `internal/tui/git_model.go` and `internal/tui/git_model_test.go`. Async git operations are introduced via `tea.Cmd` to avoid blocking the UI goroutine. A `pendingAction` field replaces the stringly-typed confirmation state machine. New features (push/pull/fetch/branch ops/stash ops/hunk staging/diff colors/ahead-behind) are built incrementally on the corrected foundation.

**Tech Stack:** Go, charm.land/bubbletea/v2, charm.land/lipgloss/v2, `os/exec`, `context`

---

## Parts

| File | Scope |
|------|-------|
| [01-bugfixes.md](01-bugfixes.md) | All ship-blockers and strong nits |
| [02-features.md](02-features.md) | Push/pull/fetch, branch create/delete, stash push/pop, hunk staging, diff colors, ahead/behind |

## Execution Order

Run Part 01 fully before Part 02. Each part produces working, testable software independently.
