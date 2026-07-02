# ocode Desktop Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cross-platform desktop app (`cmd/ocode-desktop`) that opens a Wails v3 native window over the existing in-process `internal/server` HTTP/SSE API and unchanged `web/` SPA.

**Architecture:** The shell owns its server (loopback, random port, fresh token) and points one webview window at it. Native features (tray, dock badge, notifications, menus) live Go-side: badge/notification state comes from a new exported `Server.RunStates()` snapshot polled by a pure-Go watcher in `internal/desktop`. Only `cmd/ocode-desktop` imports Wails.

**Tech Stack:** Go, Wails v3 alpha (pinned), existing `internal/server`, React SPA in `web/dist`.

**Spec:** `docs/superpowers/specs/2026-07-02-desktop-shell-design.md`

## Global Constraints

- Module path: `github.com/u007/ocode`. Main `ocode` binary must stay pure-Go: nothing outside `cmd/ocode-desktop` may import `github.com/wailsapp/wails/...`.
- Wails v3 must be pinned to an explicit alpha version (never `@latest`). Fallback known-good pin: `v3.0.0-alpha.88`.
- `web/` SPA source and existing `/api/*` handler behavior must not change. Additive Go methods on `internal/server` are allowed; no route or auth changes.
- Wails v3 is alpha: where a step notes an API-verify command (`go doc ...`), run it before writing the code that uses that symbol and adjust symbol names only (not the design).
- Validate with `go build ./...` and `go test ./...` after every task. (Wails compile requires cgo + platform SDK; dev machine is macOS — fine.)
- Commit after every task; end commit messages with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Any deferred/stubbed feature MUST get a `TODO.md` entry (Part 04 consolidates the known ones).

## Execution Order

| Part | File | Tasks |
|------|------|-------|
| 01 | `01-embed-and-runstates.md` | Task 1: shared `web/embed.go`; Task 2: `Server.RunStates()` |
| 02 | `02-desktop-core.md` | Task 3: `internal/desktop` server boot helper; Task 4: run-state watcher (poll + diff) |
| 03 | `03-wails-shell.md` | Task 5: Wails app/window/menus; Task 6: tray, badge, notifications wiring |
| 04 | `04-build-and-docs.md` | Task 7: Makefile target, macOS bundle script, docs, TODO.md |

Parts must run in order (each consumes interfaces produced by the previous), but every part file is self-contained: it restates the exact signatures it consumes.
