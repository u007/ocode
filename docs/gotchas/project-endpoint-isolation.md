---
type: Gotcha
title: Project/Endpoint Isolation — one project must never halt another
description: 'Gotcha: one project endpoint must never halt another — Handler.mu map-only invariant, per-item deadlines in shared loops, and the git-status emitter isolation fix.'
resource: internal/server/handler_git.go; internal/server/emitters.go; internal/server/agent_session.go; AGENTS.md
tags:
  - gotcha
  - server
  - isolation
  - git
  - emitter
  - handler-mu
  - deadline
  - concurrency
  - project
timestamp: 2026-09-18T12:27:28Z
---
# Project/Endpoint Isolation — one project must never halt another

The ocode server hosts multiple projects, chat sessions, and remote connections within a single process.
The core invariant: **a slow, wedged, or broken per-project operation must never block another project
or session.** Violations show up as "everywhere hangs" or "the whole app freezes."

## KEY LESSON / GENERAL RULE

1. **`Handler.mu` is a MAP lock, never a work lock.** It guards the `h.agents` map and a few
   config fields for the duration of a map lookup/insert only. Holding it across any slow work
   (LLM call, Step, compaction, agent construction, disk I/O, SSH) freezes every project/session
   in the process — not just the caller. Source: `internal/server/agent_session.go:34-42`.
2. **Any per-project work driven from a shared loop needs a per-item deadline plus per-item
   fan-out.** A bare `exec.Command` without a timeout in a shared loop stalls every item behind
   the slowest one.
3. **Never pin an HTTP connection for the length of a turn.** Browser HTTP/1.1 allows only ~6
   connections per origin; holding one open for the duration of an LLM turn starves every other
   request from that origin. Use async 202 + SSE mirror instead.

## Isolation mechanisms that are already correct (audit baseline)

These are the correct patterns to reference when adding new shared-state code:

- **Turns** (`dispatchTurn`, `agent_session.go`): each turn runs on its own goroutine;
  `sessionTurnLock` serializes per session id only, so different sessions run fully parallel.
- **SessionManager** (`session_manager.go`): its mutex is a map lock; session resolution does
  disk I/O without holding a global lock.
- **remoteHostRegistry** (`remote_hosts.go`): registry `mu` only guards the `byHost` map;
  a per-entry `mu`/`sync.Cond` serializes connects for the SAME host only; `connectBounded`
  has a 10-min backstop (`remoteConnectTimeout`) and disconnects late workspaces.
- **portMapRegistry** (`handler_portmaps.go`): global `mu` only guards the map; per-project
  entries with `sync.Once` autostart.
- **Remote git/files endpoints** resolve via `remoteWorkFor` and shell out per-request
  (`remoteRun`/`runWithContext`, bounded by `remoteExecTimeout=30s`) — no shared registry lock.

## Incident: git-status emitter stalls every viewed project (2026-09-18)

### Symptom

When any viewed project had a wedged git repo (stalled network mount, `index.lock` held by a dead
process, pathological tree), the Git status badge for **every** other viewed project froze. HTTP
requests to `/api/git_status` for the non-wedged projects also hung indefinitely.

### Root cause — two compounding bugs

1. **`gitStatusForDir`** (`internal/server/handler_git.go`) ran `exec.Command("git", …)` with
   **no deadline**. A wedged git process blocked forever, and since the emitter loop was
   single-goroutine, everything behind it was queued.

2. **The git-status emitter** (`watchEmittersLoop`, `internal/server/emitters.go`) computed
   every viewed project **sequentially on one goroutine**. One wedged repo stalled the entire
   pipeline — there was no per-project fan-out or timeout.

### Fix

1. `gitStatusForDir` now runs all probes under one `gitStatusTimeout` (10 s) via
   `exec.CommandContext(ctx, gitBinary, …)`. A wedged git process is killed after the deadline.
2. The emitter fans out through `forEachGitStatusConcurrently`, yielding results as they arrive
   on the loop goroutine so `lastGit` stays single-writer. Each project gets its own goroutine
   with its own timeout.

### Rule for new code

When adding per-project work driven from a shared loop:

```
✗  bare exec.Command in a shared loop        →  one wedge stalls all
✗  exec.CommandContext on the loop goroutine  →  cancellation races with the loop
✓  per-item goroutine + per-item deadline    →  each item independent
✓  fan-out with shared result channel        →  lastGit stays single-writer
```

### Tests

- `internal/server/git_status_timeout_test.go` — verifies git probes respect the 10 s deadline.
- `internal/server/emitters_isolation_test.go` — verifies one wedged repo does not block another.

## Related

- `AGENTS.md` § "Handler.mu is a map lock, never a work lock" and "one project must never block
  another" bullets.
- `docs/gotchas/remote-ssh-connect-hangs-whole-app.md` — same class of bug (SSH `BatchMode`
  missing → invisible prompt → hold `remoteHostRegistry` entry → every proxied request for that
  host stalls). Fixed 2026-09-18.