---
type: Gotcha
title: 'Linux Sandbox Lockout: Confiner Shell Path, /dev/null, and Binary Dispatch'
description: 'Gotcha: four independent Landlock bugs caused total sandbox lockout on Linux — bare shell name execve, /dev/null not writable, desktop binary missing dispatch, and test binary recursion trap'
tags:
  - gotcha
  - sandbox
  - security
  - shell
  - linux
  - landlock
  - confiner
  - reexec
  - test-trap
  - fixed
timestamp: 2026-09-22T18:05:24Z
---
## The Problem

On Linux, sandbox permission mode was completely broken — every sandboxed command failed, even writes to the session project dir or `extra_allowed_paths` roots. Four independent bugs converged to produce a total lockout:

### 1. Bare shell name + execve = ENOENT everywhere

The production bash invocation is `exec.Command("bash", "-c", cmd)`. `cmd.Args[0]` is the bare name `"bash"` while `cmd.Path` is the PATH-resolved absolute path. `linuxWrapper.reexecConfiner` forwarded `cmd.Args` verbatim to the re-exec'd confiner, and `confineEntrypoint` called `syscall.Exec(shell, ...)` with the bare name. `execve` does **no** PATH lookup — bare `"bash"` resolved against the session CWD, producing `sandbox-confine: no such file or directory` for **every** sandboxed command.

### 2. `/dev/null` not writable under Landlock (HISTORICAL)

`/dev/null` sits outside every writable root and Landlock default-denies opens no rule grants. macOS Seatbelt has an explicit `/dev/null` carve-out (`profile.go`), but Landlock had no equivalent — so `cmd 2>/dev/null` failed with `Permission denied`. **This is fixed**: the grants table now lives in `internal/shell/sandbox/landlock_rules.go` (`landlockNullDevice`, `landlockDeviceWriteGrants()`) and is applied at `internal/shell/sandbox/landlock_linux.go:99` (`for _, grant := range landlockDeviceWriteGrants(abi)`). `/dev/null` now gets an explicit write grant (+TRUNCATE at ABI≥3). Note: bwrap is implicitly fine because `--dev /dev` mounts a fresh devtmpfs.

### 3. Desktop binary never dispatched `sandbox-confine`

`os.Executable()` for the confiner resolves to `ocode-desktop`. Without a dispatch case in `cmd/ocode-desktop/main.go`, the child fell through to the Wails bootstrap and opened a second window instead of applying the ruleset.

### 4. Test trap — real Landlock tests silently hung

The confinement tests (`TestLinuxConfinesWrites`, `TestLinuxConfinesMutations`) call `Wrap`, which re-execs `os.Executable()`. Under `go test`, that is the test binary — and a Go test binary's generated main does not know `sandbox-confine`, so it ran the entire test suite again and recursed forever. The tests **hung** and therefore **never validated confinement on a Landlock host**. This is the most broadly applicable lesson of the four.

## Detection

- On Linux in sandbox mode, **any** shell command fails with ENOENT or "no such file or directory", even simple `echo hi` in the project dir.
- **Historical only** — before the `/dev/null` carve-out landed, redirections to `/dev/null` failed with "Permission denied" under Landlock. This is now fixed (see Fix 2); `cmd 2>/dev/null` works in current builds.
- If you suspect the test trap, check whether your Landlock tests actually complete or hang indefinitely — under `go test`, a re-exec-based backend must have its own dispatch or the tests silently recurse.

## Fix (four independent patches)

1. **Shell path resolution**: `Wrap` now forwards `cmd.Path` (the absolute path) as the shell via `resolveConfineShell`. The confiner resolves a bare name via `exec.LookPath` (fail-closed with a named error).

2. **`/dev/null` carve-out**: singleton `path_beneath` rule on `/dev/null` granting write (+TRUNCATE at ABI≥3) in `internal/shell/sandbox/landlock_rules.go` (`landlockNullDevice`, `landlockDeviceWriteGrants()`), applied at `internal/shell/sandbox/landlock_linux.go:99`. `/dev/tty` stays deliberately ungranted to match Seatbelt.

3. **Desktop dispatch**: added `sandbox-confine` case mirroring root `main.go`, alongside the existing `lsp-daemon` case.

4. **Test dispatch**: new `internal/shell/sandbox/main_linux_test.go` `TestMain` routes `argv[1] == "sandbox-confine"` to `confineEntrypoint` before `m.Run()`.

## The Rule

> **Any re-exec-based backend must dispatch the subcommand in every binary that could be the re-exec target.** Under `go test`, `os.Executable()` is the test binary — if it doesn't know the subcommand, real-backend tests silently recurse instead of testing. Always add a `TestMain` dispatch for re-exec subcommands in test binaries.

This rule applies to any future sandbox backend or confinement mechanism that re-execs `os.Executable()`.

## Files

| Area | Path |
|------|------|
| Confiner entry point | `internal/shell/sandbox/confine_linux.go` |
| Linux wrapper (reexec) | `internal/shell/sandbox/linux_backend.go` |
| Landlock rules (grant definitions) | `internal/shell/sandbox/landlock_rules.go` |
| Linux wrapper (applies grants) | `internal/shell/sandbox/landlock_linux.go` |
| Landlock tests | `internal/shell/sandbox/landlock_linux_test.go` |
| Test dispatch | `internal/shell/sandbox/main_linux_test.go` |
| Desktop dispatch | `cmd/ocode-desktop/main.go` |
| Seatbelt carve-outs (macOS ref) | `internal/shell/sandbox/profile.go` |

## Reproduction evidence

Pre-fix in `debian:stable-slim` container (kernel 7.0.12) with roots `[proj, extra]`:
- `sandbox-confine bash -c ...` → ENOENT
- `sandbox-confine /bin/bash -c ...` → proj/extra writable but `/dev/null` denied

Post-fix:
- proj + extra writable, outside dir denied, `/dev/null` writable
- Full Linux sandbox suite green in-container: 15/15