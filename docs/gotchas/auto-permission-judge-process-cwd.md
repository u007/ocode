---
type: Gotcha
title: 'Auto-Permission — Judge Must See the Session WorkDir, Not the Process CWD'
description: 'Desktop/web sessions call Agent.SetWorkDir without chdir-ing the process (the .app launches with cwd "/"). Every cwd-relative step in the auto-permission path must use the agent/permission-manager workDir, or the LLM judge denies in-project relative commands like `cd web && tsc` as escapes from the allowed roots.'
tags:
  - security
  - permissions
  - auto-permission
  - desktop
  - workdir
  - gotcha
timestamp: 2026-08-31T09:00:00Z
---
## The Problem

`cd web && ./node_modules/.bin/tsc --noEmit` run from the desktop app was
auto-denied by the LLM permission judge with:

> attempts to cd into a directory outside the allowed project paths

`web/` is a subdirectory of the project. The deterministic layer had already
passed the path-scope check (the Ask carried a `bash.prefix` rule, not
`bash.path.out_of_scope`), but the judge re-adjudicated it and got it wrong.

Root cause: the desktop/web server sets the project via
`ag.SetWorkDir(projectRoot)` (`internal/server/agent_session.go`) and never
`os.Chdir`s — the `.app` process cwd stays `/`. Several places in the judge
path still read `os.Getwd()`:

- `buildPermissionContext` — printed `Working directory: /` next to
  pre-authorized roots under `/Users/...`, so a relative `cd web` reads as
  `/web`.
- `executePermissionReadFile` — the judge's `read_file` tool resolved relative
  paths against `/`, so it could not even inspect `web/package.json`.
- `permission_interpreter.go` — script-file read, the `cwd` in the interpreter
  payload, and the `CWD` stored on persisted `interpreter_exact` grants.
- `resolveCustomScript` (`script_detection.go`) — custom-script detection for
  compound commands.
- `MatchInterpreterGrant` / `resolvedInterpreterEntrypoint` (`permissions.go`).

## The Fix

All of the above now go through `Agent.effectiveWorkDir()` (workDir override,
else process cwd) or the new `PermissionManager.effectiveWorkDir()`.
`runPermissionModelLoop`, `executePermissionReadFile`, and
`resolvedInterpreterEntrypoint` take an explicit `workDir` argument. The judge
prompt also states that relative paths and `cd` targets resolve against the
listed working directory and that a `cd` into a subdirectory of a
pre-authorized path is not an escape.

Pinned by `TestBuildPermissionContextUsesAgentWorkDirNotProcessCwd`
(`internal/agent/custom_script_test.go`), which chdirs the test process away
from the agent workDir.

## Rules

- Never call `os.Getwd()` in permission / auto-permission code. Use
  `a.effectiveWorkDir()` on the agent, `pm.effectiveWorkDir()` on the
  permission manager, or thread `workDir` into package-level helpers.
- Any test for a cwd-relative permission behaviour should `os.Chdir` the
  process somewhere other than the agent workDir, otherwise the TUI case (where
  both coincide) masks the desktop bug.
- Persisted `interpreter_exact` grants are keyed by `CWD`; grants recorded by
  a desktop build before this fix carry `CWD: "/"` and will re-ask once.
