---
type: Gotcha
title: Writable-Root Validation Prevents Confinement Defeat
description: 'Gotcha: writable-root validation must canonicalize paths and reject filesystem-volume roots (/) to prevent confinement defeat from env vars like TMPDIR.'
tags:
  - gotcha
  - sandbox
  - security
  - shell
timestamp: 2026-08-31T17:26:30Z
---
# Writable-Root Validation Is Critical to Confinement

## The Problem

Sandbox backends (sandbox-exec, bwrap) confine writes to a set of **writable roots**. If the validation logic is careless, it can set the *entire filesystem* as writable — completely defeating the sandbox.

## How This Happens

The writable-root resolution logic typically reads environment variables (`TMPDIR`, `XDG_CACHE_HOME`, etc.) to determine where the process needs write access. Without validation:

- `TMPDIR` on macOS resolves to `/var/folders/...` — acceptable.
- But if `TMPDIR` is unset or resolves to `/`, the fallback often sets `/` as writable.
- `XDG_CACHE_HOME` defaults to `~/.cache` — fine. But some setups point it at `/tmp/cache` or worse.
- Container environments may set these to root-level paths.

The canonical failure: a process with `TMPDIR=/` gets `writable_roots=["/"]`, and the sandbox becomes meaningless.

## Required Validation

1. **Canonicalize all paths** before adding to writable roots — `filepath.EvalSymlinks`, resolve `..`, expand `~`. Never trust raw env-var strings.
2. **Reject filesystem-volume roots**: `/`, `C:\`, `C:/`, `/mnt`, `/media`, `/run`. Any path that is a mount point or volume root must be rejected.
3. **Reject paths above the session workdir** unless explicitly allowlisted. The default writable set should be: `{workdir, TMPDIR, XDG_CACHE_HOME, XDG_STATE_HOME}` — each validated, never bare `/`.
4. **Log every writable root** added at session start so users can audit confinement scope.

## Gotcha: Subdirectory-of-Root Is Not Safe Either

Setting `/var` or `/tmp` as writable is almost as bad as `/` — these contain other processes' state. Writable roots must be *specific subdirectories*, not their parents. The validator should require at least one non-root path component after canonicalization.