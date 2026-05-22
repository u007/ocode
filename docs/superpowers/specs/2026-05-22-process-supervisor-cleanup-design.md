# Process Supervisor Cleanup Design

## Overview

ocode needs one lifecycle owner for every spawned child process so exit, reset,
agent rebuild, and signal-driven termination all clean up consistently. The
current behavior is close for background jobs, but cleanup ownership is split
between TUI exit paths, `Agent.Shutdown()`, and individual process helpers.
That split makes it easy to miss a path and leak child processes.

This design introduces a session-scoped process supervisor that tracks all
spawned children for the lifetime of the TUI session, preserves finished logs
for the full session, and provides one idempotent shutdown entry point.

## Goals

- Eliminate zombie and orphaned child processes started by ocode.
- Centralize teardown so every exit path uses the same cleanup logic.
- Track both background and foreground children.
- Keep finished process logs visible for the full TUI session.
- Make `/new` kill all tracked work before starting the next session.

## Non-Goals

- Persisting child process state across app restarts.
- Refactoring unrelated TUI rendering or agent orchestration.
- Changing the existing user-facing process log UI beyond cleanup behavior.

## Ownership Model

Add a dedicated session-scoped process supervisor. It is the single source of
truth for child lifecycle state.

The supervisor owns:
- background `bash(run_in_background)` processes
- subagent-owned background processes
- foreground interactive `!command` processes
- external editor processes launched by ocode

The supervisor does not replace existing UI registries outright. Instead:
- UI-facing registries remain as views/adapters for status, output, and detail panes.
- lifecycle authority moves to the supervisor
- spawn helpers register children with the supervisor immediately after process creation
- finish events flow back into supervisor state first, then into UI registries

This removes split ownership: registries display state; the supervisor decides
how a process is tracked, terminated, waited on, and retained.

## Child Record Shape

Each tracked child stores:
- stable id
- display name / command text
- child kind: background bash, subagent bash, interactive shell, editor
- process handle and pid
- whether the child owns a process group
- start time / end time
- terminal state: running, exited, killed, failed-to-start
- exit code if known
- output retention hooks for existing log buffers
- whether graceful shutdown is allowed for this child kind

The record stays available for the full TUI session even after completion so
logs remain viewable and cleanup stays idempotent.

## Spawn Rules

Every ocode-owned process must be created through a path that registers it with
the supervisor before the caller hands control elsewhere.

Affected spawn sites:
- `internal/tool/process.go` background shell launch
- TUI interactive `!command` execution
- external editor launch paths
- any subagent path that starts its own shell process

The practical rule is: if ocode can start it, the supervisor must know about it.

## Shutdown Order

Supervisor shutdown is idempotent and runs in a fixed order:

1. Freeze registration so no new children are accepted.
2. Snapshot all tracked children.
3. Signal agent/subagent cancellation so new work stops being scheduled.
4. Send termination to still-running children using the per-kind policy.
5. Wait a short bounded interval for clean exit.
6. Force kill any remaining live children.
7. Reap/wait on processes so they do not remain as zombies.
8. Mark final terminal states in supervisor records.

Freezing before cancellation avoids races where teardown drops ownership before
the remaining processes are fully terminated and reaped.

## Termination Policy

Termination policy is explicit by child kind.

- Background bash / subagent bash:
  - terminate the process group when available
  - graceful signal first on Unix, then hard kill after timeout
- Interactive `!command`:
  - same policy as other shell children, but still tracked as foreground work
- External editors:
  - attempt graceful terminate first
  - hard kill only after the same bounded timeout if still alive

On platforms where graceful group signaling is not available, fall back to the
strongest supported process kill path while keeping the same supervisor API.

## Exit Coverage

All of these routes must call the same TUI cleanup entry point, which delegates
to the supervisor:
- `/exit`
- double `ctrl+c`
- leader-key quit
- mouse exit button
- `/new` / `/clear`
- agent rebuild / model rebuild / session reset paths that replace the current agent
- OS `SIGINT`
- OS `SIGTERM`
- deferred TUI shutdown after `Run()` returns

`/new` is explicit: kill all tracked jobs, preserve finished logs only until the
current session is discarded, then start the new session clean.

## Agent Integration

`Agent.Shutdown()` remains as the public agent cleanup hook, but it no longer
owns process termination policy itself.

After this change:
- agent cancellation remains responsible for stopping agent work and subagent loops
- supervisor shutdown remains responsible for child process termination and reaping
- background process tools (`bash_output`, `kill_shell`, `wait`) read process state
  from supervisor-backed records

This keeps the agent API stable while removing duplicated cleanup responsibilities.

## TUI Integration

Add one generic cleanup method on the model for:
- saving the session
- invoking supervisor shutdown once
- shutting down the current agent once

All quit/reset handlers call this method instead of duplicating teardown logic.

`tui.Run()` also installs signal handling for `SIGINT` and `SIGTERM` and routes
those signals through the same cleanup path. Signals must not bypass shutdown by
calling `os.Exit` immediately; forced exit happens only after bounded cleanup if
needed.

## Log Retention

Finished process logs remain visible for the entire TUI session.

That means:
- completion marks a child terminal but does not remove its record
- `bash_output` / process detail views can still show retained output after exit
- cleanup skips already-finished children without discarding their logs
- record pruning, if any, happens only when the session ends or the session is replaced

## Error Handling

- Failed-to-start children are registered with terminal state and retained logs.
- One child failing to terminate does not stop cleanup of the rest.
- Double cleanup calls are harmless.
- Shutdown should report which children exited cleanly vs required force kill.
- Unknown child ids continue to return normal tool errors, not panics.

## Testing

Write failing tests first for:
- supervisor idempotent shutdown
- finished child not killed again during shutdown
- running child terminated during shutdown
- mixed running/finished children cleaned up correctly
- shutdown order preserving ownership until process reap completes
- `/new` calling the shared cleanup path and killing all tracked jobs
- TUI exit command paths delegating to the same cleanup method

Process-focused tests should use deterministic short-lived and long-lived test
commands and assert supervisor state transitions, wait behavior, and force-kill
fallback.

Manual verification:
- start a background bash job
- start an interactive `!command`
- open an external editor through ocode
- exit via `/exit`, double `ctrl+c`, leader quit, mouse quit, `/new`, and `SIGTERM`
- confirm no child processes remain owned by ocode after shutdown completes

## Files Likely To Change

- `internal/tool/process.go`
- `internal/tool/process_tools.go`
- `internal/tool/exec.go`
- `internal/agent/agent.go`
- `internal/agent/agent_runs.go`
- `internal/tui/model.go`
- `internal/tui/tui.go`
- targeted tests around process lifecycle and TUI cleanup paths

## Notes

- This is intentionally a larger refactor than the minimal patch because the
  requirement is one generic cleanup function everywhere plus full-session log
  retention.
- The key implementation constraint is to route every spawn path through a
  supervisor-aware constructor before touching shutdown logic.
