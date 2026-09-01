---
type: Gotcha
title: Shell Execution Must Set cmd.Dir to Agent Workdir
description: 'Gotcha: shell execution must set cmd.Dir to the agent/session workdir for both foreground and background commands, not relying on inherited process cwd.'
tags:
  - gotcha
  - sandbox
  - shell
  - working-directory
timestamp: 2026-08-31T17:26:30Z
---
# Shell Execution Must Set cmd.Dir

## The Problem

When ocode spawns a shell subprocess (`bash -c "..."`), the command inherits the parent process's working directory if `cmd.Dir` is not explicitly set. In a desktop/web session, the process CWD is `/` (the macOS `.app` launcher or systemd unit starts there). Any sandbox confinement that relies on relative paths or CWD-relative rules is silently operating from `/`, not the project directory.

## Affected Paths

- **Foreground Bash**: `cmd.Start()` → if `cmd.Dir` is unset, the shell runs at `/` on desktop/web.
- **Background Bash**: Same problem, worse because background processes are long-lived and their CWD affects every command they run.
- **Subagent Bash**: A child agent's BashTool inherits the parent's CWD unless explicitly overridden — same trap.

## Required Fix

Every shell spawn point must set:

```go
cmd.Dir = agentWorkDir  // or sessionWorkDir
```

For background commands, also store the workdir in the `Process` record so the UI and kill logic can reference it.

## Gotcha: os.Getwd() Is Not a Solution

On desktop/web, `os.Getwd()` returns `/` — the process CWD is not the project root. The agent's `SetWorkDir` sets `agent.workDir` but does not `os.Chdir()`. Never call `os.Getwd()` to determine the working directory; always use the agent/session workdir stored in the agent struct.

## Gotcha: Relative Path Rules Depend on cmd.Dir

The permission system's path-scoping rules (e.g. "allow writes only within `<project>/src/`) assume relative paths resolve against the project root. Without `cmd.Dir`, they resolve against `/` — and a command like `mkdir src/newdir` writes to `/src/newdir` instead of `<project>/src/newdir`. The path scope check passes (it canonicalized the relative path against the wrong root), and the sandbox is bypassed.