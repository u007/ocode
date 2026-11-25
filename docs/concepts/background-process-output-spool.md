---
type: Concept
title: Background Process Output Ring and Spool
description: Bash process stdout and stderr keep a 50 KB in-memory ring; overflow is spooled in full to the tool-results cache, and every truncation marker names that file or says it is unavailable.
resource: internal/tool/process.go
tags:
  - bash
  - background-process
  - process-output
  - spool
  - truncation
timestamp: 2026-11-25T17:14:18Z
status: active
---

# Background Process Output Ring and Spool

Covers bash processes run through `ProcessRegistry` (background and foreground). Their output is read by `bash_output`, by the job completion notice, and by the TUI detail view.

Why the spool exists: a failing `go test ./internal/server` tail once became a 262 KB job completion notice. It was re-sent on every later turn and had already lost the failure lines. The ring is now small, and the full stream stays on disk for the agent to read.

## Ring

- `procBufferCap` = 50 KB (`internal/tool/process.go`) bounds the in-memory stdout+stderr ring per process.
- `appendOutput` drops the oldest bytes past the cap and counts them in `dropped`.

## Spool

- At the first overflow, `openSpoolLocked` writes the full stream from byte 0 to `<tool-results cache dir>/proc-<ownerpid>-<unixnano>-<supKey or ID>.log`. `ownerpid` is the pid of the ocode process that wrote the file. The cache dir is `toolResultCacheDir()`, normally `~/.local/state/opencode/tool-results`.
- The file is mode 0600, opened with `O_EXCL`, and created lazily. Output that never overflows makes no file.
- `procSpoolCap` = 32 MiB per process. At the cap, `spoolWriteLocked` writes one marker line and saves nothing more. The ring keeps working.
- `finishProcess` closes the spool handle and keeps `spoolPath`, so the file stays discoverable after the process exits.
- If the directory or file cannot be created, or a write fails, the error is logged, the process keeps running with in-memory output only, and `spoolPath` is cleared.
- Cleanup: `agent.CleanupToolResults(maxAge)` (`internal/agent/truncate.go`) removes a spool only when all three hold:
  1. Its process is not running in this ocode process. `tool.SpoolIsOpen` reads the `openSpools` set, which a spool joins when the file is created and leaves when its handle closes. A running spool is kept at any age.
  2. Its name does not name another live ocode owner. `tool.SpoolOwnerPID` parses the owner pid from the name, and `spoolOwnerAlive` checks it with gopsutil `PidExists`; a lookup error keeps the file. This rule skips a spool whose owner is this process.
  3. Its mtime is older than `maxAge`.

  So a finished spool written by this process ages out on the same 48-hour rule as any other. Names that do not parse, such as the nanosecond-only `proc-<unixnano>-…` names from earlier builds, skip rule 2 and get only the age rule. If a dead owner's pid is reused by a live process, the file is kept longer, which is the safe direction.
- Who sweeps: `Server.Start` runs `CleanupToolResults(48h)` once. `sweepToolResultsLoop` (`internal/server/server.go`) runs it hourly for the life of the server. The TUI runs it at quit, from `cleanupProgramModel`'s `cleanupCurrentSession` (`internal/tui/model.go`), and hourly while it runs. `Init` arms a `toolResultsSweepTickMsg` every `toolResultsSweepInterval` (`time.Hour`); `Update` runs `sweepToolResultsCmd` and re-arms the tick. The TUI does not sweep at startup, so its first hourly sweep comes an hour after launch.

## Markers and readers

`truncationMarkerLocked` builds every truncation marker:

- with a spool: `[…truncated N bytes; full output: <path> (read it with the read tool, using offset/limit)]`
- without one: `[…truncated N bytes; full output unavailable]`

Readers that emit it:

- `readSince`, used by `bash_output` and by the job completion notice (`a.procs.Output` in `agent.go`, `SetOnDone`).
- `Dump`, used by the timeout result, the wait tool (`internal/agent/wait_tool.go`) and the TUI detail view.

`bash_output` adds ` log=<path>` to its header once a spool path exists (`internal/tool/process_tools.go`). The tool definition does not change, so the cached tools prefix stays stable.

## Invariant

A truncation marker names a path that exists while the spool is live. Markers take the path only from `spoolPath`, which is cleared when spooling fails and is set only after the file is created.

This holds for a running process and for a spool owned by another live ocode process, because cleanup skips both (see Spool).

It does **not** hold for a finished spool written by this process once it is older than 48 hours, while its process is still in its agent's `ProcessRegistry`:

- A finished process stays in the registry. The only registry delete is a failed background start. The registry goes away only when the agent is dropped: `SessionManager.EvictIdle` drops a session idle for the idle timeout (30 minutes by default, `defaultSessionIdleTimeout`), and a session rebuild drops it too. In the TUI the agent is swapped out only by `replaceAgent` (`internal/tui/model.go`) or by the `/connect` rebuild (`internal/tui/connect.go`), or when the process quits.
- `spoolPath` stays set after the process finishes, and it is read at emit time, not at spool time.
- Two live emitters name it. `bash_output` prints ` log=<path>` whenever `spoolPath` is set (`internal/tool/process_tools.go:53`), and `Dump`, which the wait tool calls (`internal/agent/wait_tool.go:151`), emits a marker for any dropped output.

So in a session that stays active for more than 48 hours, a `bash_output` or `wait` on an old finished job can name a file the sweep has already removed. The TUI sweeps hourly as well (see Who sweeps), so a long-running TUI session can hit the same case. `readSince` can also emit a marker, but the job completion notice normally reads the output first, so it rarely does.

The breach is the price of the age rule. The sweep's stated purpose is to release finished spools on a long-running server or TUI without a restart (`sweepToolResultsLoop` on the server, `toolResultsSweepTickMsg` in the TUI). The tree does not guard the path. A marker copied into a saved transcript after its owner has exited is likewise not covered by any guarantee, and its file falls under the plain 48-hour age rule.
