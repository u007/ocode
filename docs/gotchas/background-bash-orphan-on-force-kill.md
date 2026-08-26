---
type: Gotcha
title: run_in_background Bash Commands Orphaned on Force-Killed ocode
description: 'Background bash commands started via the bash tool had no self-cleanup and would survive kill -9 on ocode; wrapped in the same parent-monitor mechanism local models already used'
tags:
  - bash-tool
  - process-supervisor
  - orphan-process
  - force-kill
  - parent-monitor
timestamp: 2026-08-26T09:45:00Z
---
## Background bash commands weren't protected against a force-killed ocode

**Files:** `internal/tool/exec.go` (`BashTool.ExecuteStreamCtx`), `internal/tool/process.go` (`StartBackgroundDisplay`)

### Symptom / gap

Graceful ocode exit (Ctrl+C, `/quit`) already calls
`supervisor.Shutdown`/`TerminateAll` from `cleanupCurrentSession`, which
terminates tracked background processes. But that's a `defer` in ocode's own
process — it never runs on `kill -9 <ocode-pid>` or a crash. Local-model
servers were already immune to this (see
[local-model-tty-hijack.md](local-model-tty-hijack.md) for the unrelated bug
found alongside this one) because `internal/agent/local_models.go` and
`discovery_glue.go` wrap their spawn command in
`tool.WrapWithParentMonitor` before calling `StartBackground`. Plain
`run_in_background: true` bash commands (`internal/tool/exec.go` line ~144)
called `StartBackground` directly, with no such wrapper — so a command
backgrounded this way would survive ocode being force-killed, reparented to
launchd/init, running forever.

### Why not rely on process groups alone

`Setpgid`/`Setsid` (see the adjacent tty-hijack gotcha) isolate a child from
signals sent to *ocode's* process group, but do nothing to make the child
notice ocode dying — there's no macOS/Darwin equivalent of Linux's
`PR_SET_PDEATHSIG`, so a poll-based approach is the only portable option in
this codebase (Linux, Darwin, and Windows all supported).

### Fix

Wrap the command with `WrapWithParentMonitor` before starting it, same as
local models:

```go
p := t.Procs.StartBackgroundDisplay(WrapWithParentMonitor(params.Command), params.Command)
```

`ParentMonitorWrap` (`internal/tool/parent_monitor.go`) polls `kill -0
$ppid` every 0.5s in a bash subshell; when ocode's PID disappears (including
via SIGKILL, which the polling loop doesn't need a signal for — it just
notices the PID is gone), it kills the real child. Verified directly with a
standalone repro (spawn a fake "parent", force-kill it, confirm the wrapped
child dies within ~1s) independent of ocode.

**Watch out:** `StartBackground(command)` stores `command` verbatim as the
`Process.Command` field, which `bash_output`/`kill_shell` show to the model.
Passing the *wrapped* monster string there would leak ugly scaffolding into
that listing. `Process.Command` is documented as "write-once, safe to read
without holding `mu`" — so it must never be mutated after construction, not
even by a synchronous caller-side reassignment right after `StartBackground`
returns (a real data race with the pump/wait goroutines and any concurrent
listing, even though this specific instance was never caught by
`-race` locally). Fixed properly by adding `StartBackgroundDisplay(execCmd,
displayCmd string)`, which sets `Process.Command` to `displayCmd` at
construction time, before the `Process` is ever published to the registry or
supervisor. `StartBackground` is now a one-line wrapper calling
`StartBackgroundDisplay(command, command)`.

### Remaining gap (deferred, see `TODO.md`)

A command that starts in the foreground and is *promoted* to background
mid-flight (exceeds its timeout, or is moved via `bgRequestCh`) is
`cmd.Start()`ed before any wrap/no-wrap decision is made, so it never gets
the parent-monitor wrapper. Force-killing ocode while such a promoted
command is still running will still orphan it. Fixing this needs either
always wrapping from the start regardless of eventual foreground/background
fate, or attaching a monitor at promotion time — bigger change, not done
here.

### Lesson

"This field is write-once" is a real invariant other code relies on for
lock-free reads (`Process.Command`, per its own doc comment) — a field being
`string` (not a pointer, not obviously shared) does not make a
happens-after-construction mutation safe. When a caller needs a different
display value than the value that must actually execute, thread the two
through the API (`StartBackgroundDisplay`) instead of writing to the
struct after the fact.
