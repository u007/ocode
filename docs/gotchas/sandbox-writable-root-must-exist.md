---
type: Gotcha
title: Sandbox Writable-Root Must Exist on Disk
description: 'Gotcha: sandbox backend validates writable-root directories before command execution and fails if they don''t exist on disk, preventing mkdir -p inside sandbox'
tags:
  - gotcha
  - sandbox
  - security
  - shell
timestamp: 2026-09-03T03:14:16Z
---
# Sandbox Writable-Root Must Exist on Disk

## The Problem

The sandbox backend validates writable-root directories **before** command execution. If a configured writable root does not exist on disk, the sandbox fails the command entirely — even for simple commands like `echo hi`. This means you cannot use `mkdir -p` inside the sandbox to create the directory, as the sandbox has not yet been fully initialized for that command's execution context.

## Impact

- Commands fail immediately with a sandbox validation error when any configured writable root path is absent from the filesystem.
- Workflows that dynamically determine writable roots at runtime cannot reliably `mkdir -p` inside the sandbox as a fallback.
- The sandbox backend (sandbox-exec on macOS, bwrap on Linux) performs this check as part of its startup/initialization sequence, prior to launching the user's command.

## Mitigation

1. **Pre-create all writable roots** on disk before starting ocode in sandbox mode. Ensure paths like `~/.cache/bun`, `~/tmp`, or any env-var-derived roots exist.
2. **Adjust sandbox configuration** to remove or revise writable roots that may not always exist, or use paths that are guaranteed to be present.
3. **Do not rely on `mkdir -p` inside sandboxed commands** to create missing writable roots — this will not work.

## Related Gotchas

- [`shell-sandbox-writable-root-validation.md`](shell-sandbox-writable-root-validation.md) — covers canonicalization and rejection of volume roots to prevent confinement defeat.
- [`shell-sandbox-writable-root-validation.md` Gotcha: Subdirectory-of-Root Is Not Safe Either] — discusses why setting `/var` or `/tmp` as writable is almost as bad as `/`.