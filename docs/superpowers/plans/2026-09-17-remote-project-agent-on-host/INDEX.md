# Remote Project Chat Agent on the Host — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A sidebar remote SSH/WSL project's chat agent runs on an `ocode serve --remote` server on that host, reached through a host-prefixed proxy on the local server, so bash, `$HOME`, files, and permissions all resolve on the remote.

**Architecture:** The local desktop/web server keeps one lazily connected `remote.RemoteWorkspace` per host and reverse-proxies `/api/remote/{host}/api/*` to it with the remote token injected. The SPA derives the host per session from the tab's project binding and prefixes chat, session, event, and permission calls. The remote server expands `~` against its own home. Terminal, Files, git, `!`, and port forwards are untouched.

**Tech Stack:** Go (`internal/server`, `internal/remote`, `internal/projects`, `internal/desktop`), system `ssh`/`wsl.exe`, `net/http/httputil`, React + TypeScript SPA (`web/src`), vitest.

**Spec:** `docs/superpowers/specs/2026-09-17-remote-project-agent-on-host-design.md`

## Global Constraints

- Plans are high-level: tasks name files and functions, never code.
- TDD per task: failing test first, minimal implementation, green, then commit.
- No fallbacks: a remote connect failure blocks the turn with an error; never run a remote project's agent locally.
- Only hosts present in the saved project store (`projects.Project.Host`) may be proxied.
- The remote token never reaches the browser.
- The local server never expands a remote path; only the machine owning `$HOME` expands `~`.
- No new raw process spawns outside `tool.ProcessSupervisor` (`tool.StartSupervised`).
- Every caught error is logged with what was attempted; no empty catch.
- Match existing style; surgical diffs; no adjacent refactors.
- Go: `go test ./internal/...`; SPA: `cd web && pnpm test`.
- Do not commit unless the user asks; each task ends at "ready to commit" with a suggested message.

## Parts

1. `01-remote-side-foundations.md` — `~` expansion on the server, shared proxy builder in `internal/remote`, `RemoteWorkspace` sync stage and WSL branch. Three tasks.
2. `02-local-server-registry-and-route.md` — per-host workspace registry on `Handler`, the `/api/remote/{host}/api/*` route with admission, token rewrite, 502 handling, remote project registration, shutdown. Two tasks.
3. `03-frontend-session-routing.md` — `remoteApiBase`, host-aware client helpers, per-session host in `useChat`, per-host event streams, session list and header calls. Four tasks.
4. `04-docs-and-verification.md` — prompt wording, AGENTS.md and architecture docs, CHANGES.md, manual SSH and WSL verification. One task.

Execute parts in order. Parts 1 and 2 are backend and can be verified with `go test` alone. Part 3 depends on the route from part 2 existing. Part 4 is last.
