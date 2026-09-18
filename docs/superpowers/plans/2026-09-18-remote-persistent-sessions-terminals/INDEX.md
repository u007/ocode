# Remote Persistent Sessions and Terminals — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A remote (SSH/WSL) project's terminals run inside the host's `ocode serve --remote` so they survive laptop sleep, app restart, and reconnect; a version-mismatched remote server is reused, shown in the sidebar, and restartable on demand; the sidebar lists the host's chats and terminals for reattach.

**Architecture:** The SPA sends a remote project's terminal websocket and terminal HTTP calls through the existing `/api/remote/{host}/api/*` reverse proxy, so the pty is a child of the remote server. The proxy rewrites the websocket subprotocol token both ways so the remote token never reaches the browser. `EnsureRemoteServer` reuses an alive mismatched server and records `Outdated`; three new local endpoints expose status, connect, and restart per host. The sidebar's remote project row shows version, chat and terminal counts, and expands to a reattach list.

**Tech Stack:** Go (`internal/remote`, `internal/server`), `net/http/httputil`, gorilla websocket, `creack/pty`; React + TypeScript SPA (`web/src`), vitest, xterm.js.

**Spec:** `docs/superpowers/specs/2026-09-18-remote-persistent-sessions-terminals-design.md`

## Global Constraints

- Plans are high-level: tasks name files and functions, never code.
- TDD per task: failing test first, minimal implementation, green, then commit.
- The remote token never reaches the browser, in any header, body, or 101 response.
- Only hosts present in the saved project store (`projects.Project.Host`) may be proxied, connected, or restarted.
- The local server never expands a remote path; only the machine owning `$HOME` expands `~`.
- No new raw process spawns outside `tool.ProcessSupervisor`; remote commands go through the host `remote.Transport`.
- No fallbacks, no optional behaviour flags; a failed remote step returns a 502 with a `stage` field and is logged with what was attempted.
- Every caught error is logged; no empty catch.
- Listings are sorted; the terminal list is unpaginated with an inline comment saying it is bounded by live pty count.
- Match existing style; surgical diffs; no adjacent refactors.
- Restart is unguarded: it kills the remote server even with running turns or open terminals (user decision).
- Go: `go test ./internal/remote/... ./internal/server/...`; SPA: `cd web && pnpm test`.
- Each task ends at "ready to commit" with the suggested message; commit per task only if the user has asked for that, otherwise leave the work staged for the user.

## Parts

1. `01-backend-terminal-through-proxy.md` — websocket subprotocol rewrite in the proxy, 24 h detach TTL in `--remote` mode, `GET /api/terminal` list endpoint. Three tasks.
2. `02-backend-remote-server-policy.md` — reuse a version-mismatched server and record `Outdated`, expose server state from the host registry, `status` / `connect` / `restart` endpoints. Three tasks.
3. `03-frontend-terminal-routing.md` — remote project terminals use the proxied URL with no `host=` param and an `X-Ocode-Project` header; persistence key becomes `host|path` for remote projects. Two tasks.
4. `04-frontend-sidebar-inventory.md` — API client functions and hooks for host status and terminal list, sidebar row status line with Connect / Restart and the expandable chat + terminal reattach list, immediate reconnect on wake. Three tasks.
5. `05-docs-and-verification.md` — AGENTS.md, concept doc, CHANGES.md, manual SSH and WSL verification. One task.

Execute parts in order. Parts 1 and 2 are backend-only and verified with `go test`. Part 3 depends on part 1's proxy rewrite. Part 4 depends on part 2's endpoints and part 1's list endpoint. Part 5 is last.
