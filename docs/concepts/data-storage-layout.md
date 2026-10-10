---
type: Concept
title: Data Storage Layout
description: Cross-platform global data directory, its sub-directories, and how the project slug keys sessions.
resource: CLAUDE.md
tags:
  - storage
  - paths
  - sessions
timestamp: 2026-09-30T07:37:09Z
---
# Data Storage Layout

All persistent state lives under a single cross-platform global directory
resolved by `internal/paths.GlobalDataDir()`:

| Platform | Path |
|----------|------|
| macOS    | `~/.local/share/opencode` |
| Linux    | `$XDG_DATA_HOME/opencode` (or `~/.local/share/opencode`) |
| Windows  | `%LOCALAPPDATA%\opencode` |

Sub-directories:
- `project/{slug}/sessions/` — chat session JSON files (one per session)
- `usage/` — LLM token usage records (`records.jsonl`)
- `logs/` — `internal/paths.LogsDir()`, shared by every ocode process; each
  uses its own filename: TUI `tui-crash.log` (stderr), `compact.log`,
  `tokens.log`; desktop app `desktop.log` (the standard `log` + default `slog`
  output, which a double-clicked `.app` would otherwise send to `/dev/null`;
  rotates to `.1` at 5 MB via `internal/logfile`). Always resolve the dir via
  `paths.LogsDir()`, never `filepath.Join(GlobalDataDir(), "logs")`.
- `auth.json` — provider API keys and OAuth tokens
- `projects.json` / `project_groups.json` — web sidebar project list
- `tabs.json` — open session tabs per project for the web/desktop UI
  (`GET/PUT /api/tabs`). Server-side, never `localStorage`: localStorage is
  per-origin, so a shared URL or a different port would otherwise open with
  zero tabs. Every server process on the machine shares this one file, so the
  store merges under a cross-process lock (`internal/tabs`): a `PUT` replaces
  only the projects it names, an empty tab list deletes a project, and
  projects absent from the body are preserved. Never turn this back into a
  whole-map replace — that silently drops another window/process's projects.

The `{slug}` is a SHA-256 prefix of the git repo root path, making sessions
project-scoped even when working from different checkouts. The TUI's
`m.workDir` is the source of truth for project resolution (set via
`/cd`, `--dir`, or `session.SetWorkDir`); `os.Getwd()` is not — `/cd`
can change the project root without changing the process CWD on every
caller.
