---
type: Gotcha
title: Foreground Bash Commands — Parent-Death Protection Before Start
description: Foreground POSIX Bash commands must be wrapped with WrapWithParentMonitor before cmd.Start() to ensure promotion to background does not orphan processes; the monitor must be applied at spawn time, not at promotion time.
tags:
  - bash-tool
  - process-supervisor
  - orphan-process
  - foreground-promotion
  - parent-monitor
  - WrapWithParentMonitor
timestamp: 2026-08-26T15:28:56Z
---
## Foreground commands can be promoted to background mid-flight

**Files:** `internal/tool/exec.go` (`BashTool.ExecuteStreamCtx`), `internal/tool/parent_monitor.go` (`WrapWithParentMonitor`, `ParentMonitorWrap`)

### The promotion window

A bash command started in the foreground can be moved to background mid-flight through two paths:

1. **Timeout promotion** — the command exceeds its timeout; the foreground handler fires `<-ctx.Done()`, kills the process group, and the command is re-registered as a background process.
2. **`bgRequestCh` promotion** — the model explicitly requests backgrounding via `bgRequestCh`; the foreground handler fires `<-proc.bgRequestCh`, stops streaming, and returns the background ID.

In both cases the command is already `cmd.Start()`ed — the process is running before the promotion decision is made.

### The orphan risk

Before this fix, `WrapWithParentMonitor` was only applied to commands started via `run_in_background: true` (the background path at `exec.go:150`). Foreground commands were started raw:

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

### Integration test

`TestBashToolForegroundPromotionParentDeath` in `internal/tool/process_parent_death_unix_test.go` verifies the complete lifecycle across a process boundary. The test:

1. **Spawns a helper process** that acts as a "mini-ocode" — it starts a foreground command, registers it, then promotes it to background via `bgRequestCh`.
2. **Kills the helper** with SIGKILL (simulating `kill -9` on ocode) — no graceful shutdown, no supervisor cleanup runs.
3. **Asserts both the monitor wrapper and its child command disappear** — confirming the parent-death monitor fires correctly when the original parent is killed without graceful shutdown.

The test is Unix-only (`//go:build !windows`) because it relies on `kill -0` polling and SIGKILL semantics. Windows retains prior lifecycle behavior without parent-death protection.

### Lesson

**Wrap at spawn, not at promotion.** When a resource (parent-death protection) is needed regardless of a later state transition (foreground → background), apply it at the earliest point where the resource can be attached — in this case, before `cmd.Start()`. Trying to retrofit the protection at promotion time either doesn't work (can't wrap an already-running process) or introduces fragile, hard-to-test sidecar mechanisms. The same pattern applies to any resource that must survive a state change: apply it unconditionally at construction, not conditionally at transition.
