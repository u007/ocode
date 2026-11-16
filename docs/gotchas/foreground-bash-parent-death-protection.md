---
type: Gotcha
title: Foreground Bash Commands — Parent-Death Protection Before Start
description: Foreground POSIX Bash commands must be wrapped with WrapWithParentMonitor before cmd.Start() to ensure promotion to background does not orphan processes; the monitor must be applied at spawn time, not at promotion time. Also covers the stop result text after a timeout or cancel, which lists still-running processes in the command's group.
tags:
  - bash-tool
  - process-supervisor
  - orphan-process
  - foreground-promotion
  - parent-monitor
  - WrapWithParentMonitor
  - stop-result
timestamp: 2026-11-16T07:28:44Z
---
## Foreground commands can be promoted to background mid-flight

**Files:** `internal/tool/exec.go` (`BashTool.ExecuteStreamCtx`), `internal/tool/parent_monitor.go` (`WrapWithParentMonitor`, `ParentMonitorWrap`)

### The promotion window

A bash command started in the foreground can be moved to background mid-flight through `bgRequestCh`. The only sender is `ProcessRegistry.RequestBackgroundLatest`, which closes the channel and is called only by the TUI when the user presses ctrl+b (`backgroundLatestForegroundBash` in `internal/tui/model.go`). The foreground handler then fires `<-proc.bgRequestCh`, stops streaming, and returns the background ID. No model tool and no timer sends on it.

A timeout is not a promotion. When the exec timeout or a cancel fires `<-ctx.Done()`, the handler freezes the process group, lists it, kills it and returns a stop result (see "Stop result" below). Nothing re-registers the command as a background process.

In the promotion case the command is already `cmd.Start()`ed — the process is running before the promotion decision is made.

### The orphan risk

Before this fix, `WrapWithParentMonitor` was only applied to commands started via `run_in_background: true` (the background branch, `params.RunInBackground` at `exec.go:171`). Foreground commands were started raw:

```go
// BEFORE — no parent-death protection for foreground commands
cmd = exec.CommandContext(ctx, "bash", "-c", params.Command)
```

If ocode was force-killed (`kill -9 <ocode-pid>`) while a foreground command was running — or after it was promoted — the command had no monitor and would survive, reparented to init. The `background-bash-orphan-on-force-kill.md` gotcha documents this gap and identifies it as a remaining item.

### Why wrapping at promotion time doesn't work

A monitor can't be retroactively attached to an already-running process. `WrapWithParentMonitor` works by wrapping the command string in a bash subshell that spawns the real child as a background job and polls `kill -0 $ppid`. This only works if the wrapper is the *command that gets executed* — it must be the outer shell, not something bolted on after `cmd.Start()`.

The process the registry and supervisor track is the one that `cmd.Start()` creates. Replacing it mid-flight would require either:
- Killing the original process and starting a new wrapped one (losing state, output, and the PID the model knows about)
- Spawning a sidecar monitor and somehow linking it to the existing process (no portable way to do this on macOS/Darwin — `PR_SET_PDEATHSIG` is Linux-only)

Both approaches are fragile and error-prone. The only clean solution is to wrap before `cmd.Start()`.

### The fix

Wrap foreground commands with `WrapWithParentMonitor` **before** `cmd.Start()`, regardless of whether the command starts in the foreground or background:

```go
// AFTER — parent-death protection from the start
if t.Procs != nil {
    command = WrapWithParentMonitor(params.Command)
}
cmd = exec.CommandContext(ctx, "bash", "-c", command)
```

This means:
- A foreground command that stays foreground is protected.
- A foreground command promoted to background is already protected.
- A command started as background is protected (same as before).

The `StartBackgroundDisplay(execCmd, displayCmd)` API (from the background orphan fix) ensures `bash_output`/`kill_shell` listings show the original command, not the monitor wrapper — this is critical because `Process.Command` is write-once after construction.

### Why the check is `t.Procs != nil`

The nil guard exists because `BashTool` can operate without a process registry (headless mode, or when the tool is invoked without supervisor infrastructure). When there's no registry, there's no supervisor to track the process, so wrapping is pointless — the command runs synchronously and `cmd.Run()` handles cleanup.

### Interaction with `setProcGroup`

After wrapping, `setProcGroup(cmd)` puts the wrapped process in its own process group. This is orthogonal to parent-death protection:
- **Process groups** handle signal delivery (kill the whole tree on timeout/cancellation).
- **Parent monitor** handles ocode death detection (the polling loop).

Both are needed. A process group without a parent monitor protects against ocode sending signals but not against ocode dying. A parent monitor without a process group protects against ocode death but not against orphaned grandchild processes when the direct child is killed.

### Stop result: what the model is told

`finalizeExecResult` (`internal/tool/exec.go`) formats the result of a stop. The `ctx.Done()` branch of the foreground handler runs on the exec timeout or on a cancel (Stop, or server shutdown), and the two read differently:

- **Timeout** (`ctx.Err() == context.DeadlineExceeded`): `Command timed out after <timeout>. Output so far:` followed by the captured output.
- **Cancel**: `Command was stopped after <elapsed>, before it finished (the turn was cancelled or the server shut down; it did not time out).` The branch used to pass `timedOut=true` unconditionally, so a cancelled command was reported as a timeout.

Both append `Still running when stopped:` with one line per process still alive in the command's process group, `- pid N, <etime> elapsed: <command>`, sorted by pid, at most 8 lines, then `- and N more` if there are more.

The stop branch works in a fixed order, and the order is the point:

1. `freezeProcessGroup` (`internal/tool/proc_unix.go`) sends SIGSTOP to the whole group, so a runaway command cannot make progress while the list is taken.
2. `liveGroupProcesses` lists the group with `ps -axww -o pid=,ppid=,pgid=,etime=,args=`, on its own 1 s deadline because the caller's ctx is already done. A ps failure is logged (never printed, the TUI owns the terminal) and yields no list.
3. `killProcessGroup` sends SIGKILL. That ends the listed processes, stopped ones included.

Membership is decided by parentage, not by command text. `parseLiveGroupProcesses` drops the group leader (pid equal to pgid). The monitor is the wrapper-shaped row (`( ppid=`) whose parent is the leader, because no user process is a direct child of the leader. A poll sleep is dropped only when its parent is that monitor and its argv is one of the two spellings `ParentMonitorWrap` uses, `sleep 0.50` and `sleep 0.20`. The wrapper spells them that way so a user's own `sleep 0.5` stays listed. `TestParentMonitorWrap_PollSleepSpellings` keeps the spellings and the filter in step. Each command is clipped at 200 runes. Windows (`proc_windows.go`) has no process groups: the freeze is a no-op and nothing is listed.

Known limits of the list:
- busybox `ps` accepts `args=` but rejects `-axww`. On a busybox host the list is empty (the failure is logged) and the kill still happens.
- The filter assumes the monitor is the leader's direct child. A runner that interposes a process above the leader (a sandbox binary that forks rather than execs) moves the wrapped shell one level down, so the list then names that shell as the monitor. This is not verified on the Linux bwrap sandbox backend.

On the registry path `cmd.Cancel` is a no-op (set in `BashTool.ExecuteStreamCtx` inside the `t.Procs != nil` branch). exec's default Cancel SIGKILLed only the leader the instant ctx was done. That raced the stop branch and orphaned the group before it could be frozen and listed, so the stop branch owns the whole-group kill. A command moved to background (`bgRequestCh`) keeps the old "killed when its ctx ends" behaviour through an explicit `killProcessGroup` in the goroutine that waits for it.

The agent always builds a process registry (`a.procs` in `internal/agent/agent.go`), so the stop branch is the production path. A `BashTool` without a registry runs `cmd.Run` with no stop branch: a timeout is reported without the process list, and a cancel is a plain `Command failed`, because `deadlineStop` reports only the deadline.

### Integration test

`TestBashToolForegroundPromotionParentDeath` in `internal/tool/process_parent_death_unix_test.go` verifies the complete lifecycle across a process boundary. The test:

1. **Spawns a helper process** that acts as a "mini-ocode" — it starts a foreground command, registers it, then promotes it to background via `bgRequestCh`.
2. **Kills the helper** with SIGKILL (simulating `kill -9` on ocode) — no graceful shutdown, no supervisor cleanup runs.
3. **Asserts both the monitor wrapper and its child command disappear** — confirming the parent-death monitor fires correctly when the original parent is killed without graceful shutdown.

The test is Unix-only (`//go:build !windows`) because it relies on `kill -0` polling and SIGKILL semantics. Windows retains prior lifecycle behavior without parent-death protection.

### Lesson

**Wrap at spawn, not at promotion.** When a resource (parent-death protection) is needed regardless of a later state transition (foreground → background), apply it at the earliest point where the resource can be attached — in this case, before `cmd.Start()`. Trying to retrofit the protection at promotion time either doesn't work (can't wrap an already-running process) or introduces fragile, hard-to-test sidecar mechanisms. The same pattern applies to any resource that must survive a state change: apply it unconditionally at construction, not conditionally at transition.
