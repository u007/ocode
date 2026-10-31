---
type: Concept
title: Data Storage Layout
description: Cross-platform global data directory, its sub-directories, and how the project slug keys sessions.
resource: "CLAUDE.md"
tags:
  - storage
  - paths
  - sessions
timestamp: 2026-10-31T09:50:16Z
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
- `servers/` — one `serve-<pid>.json` per live server process
  (`paths.ServersDir()`, `internal/server/serve_registry.go`), written from
  `Server.Serve` once the listener binds (so the desktop shell and the TUI's
  embedded server are recorded too) and removed on a clean exit, with fields
  `pid`, `version`, `addr`, `workdir`, `started_at`. The owner holds an
  advisory lock on the file for its lifetime; the OS drops that lock when the
  process dies, which is what makes the owner provable — a pid alone is
  recyclable. Diagnostic and self-cleaning only: an unlocked record whose pid
  is gone is deleted by the next scan, an undecodable record is deleted only
  when its owner's lock is free AND the pid in its file name is gone (a newer
  build's format is kept), and same-version siblings are silently ignored. A
  locked record whose address is not listening is not announced; an unlocked
  record whose pid is alive is reported as unverified and left on disk.
  Not authoritative state — nothing auto-kills another server. It exists so
  two local servers of different versions that both target the same remote host
  stop retiring each other's remote `ocode serve` on reconnect, which kills
  in-flight turns on that host; a starting server logs a warning naming the
  stale sibling's version, address, pid and cwd. Only a lock-verified sibling
  gets a `stop it: kill <pid>` hint; an unverified one is reported without a
  stop command, because its pid may belong to an unrelated process.

The `{slug}` is a SHA-256 prefix of the git repo root (`git rev-parse --show-toplevel`), so
sessions are scoped per checkout: each linked git worktree has its own slug and its own
`project/{slug}/sessions/` directory and does not share the main checkout's sessions. Only the web
project list groups a linked worktree under its main checkout; storage is unaffected (see
[git-worktrees.md](git-worktrees.md)). The TUI's `m.workDir` is the source of truth for project
resolution (set via `/cd`, `--dir`, or `session.SetWorkDir`); `os.Getwd()` is not — `/cd`
can change the project root without changing the process CWD on every
caller.
