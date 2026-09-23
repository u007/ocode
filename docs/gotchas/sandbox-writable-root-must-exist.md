---
type: Gotcha
title: Sandbox Writable-Root Must Exist on Disk
description: 'Gotcha: a writable root that does not exist on disk is silently skipped by the sandbox — but a tool that needs to CREATE that root will hit EPERM unless a parent directory is writable'
tags:
  - gotcha
  - sandbox
  - security
  - shell
timestamp: 2026-09-22T18:05:23Z
---
# Sandbox Writable-Root Must Exist on Disk

## What Actually Happens

A configured writable root that does **not** exist on disk is **silently skipped** by every sandbox backend — it is never a hard error, and it never widens the confinement boundary. Binding a missing source would fail startup, and skipping is the safe choice: nothing exists there to write to.

This is deliberate and verified by tests:

- **Seatbelt (macOS)** — `internal/shell/sandbox/profile.go:16-20` documents the skip; the code at `profile.go:37-38` does `if errors.Is(err, os.ErrNotExist) { continue }` inside the root loop.
- **Linux canonicalization** — `internal/shell/sandbox/linux_backend.go:196-201` (`canonicalExistingWritables`) drops any root `filepath.EvalSymlinks` cannot resolve, via `continue`.
- **Linux bwrap argv** — `internal/shell/sandbox/landlock_rules.go:38-42` (`buildBwrapArgv`) skips unresolvable roots with the same `continue` pattern.

Regression tests lock the behavior:

- `TestSeatbeltProfileSkipsMissingRoot` (`internal/shell/sandbox/profile_darwin_test.go:181-196`) — missing root produces no error and no profile entry.
- `TestSeatbeltProfileSkipsMissingKeepsExisting` (`internal/shell/sandbox/profile_darwin_test.go:198-216`) — mixed existing+missing roots: existing gets a write rule, missing is skipped, no error.
- `TestLinuxWrapDropsMissingRoots` (`internal/shell/sandbox/linux_backend_test.go:150-164`) — Linux equivalent.

## The Real Consequence

Skipping is safe **unless a sandboxed tool needs to CREATE that root**. If the parent directory is also not writable, the tool gets `EPERM` trying to create it. The classic example:

- **`~/.cache/bun`** — covered: `~/.cache` is a writable root, so the subpath is writable even if `~/.cache/bun` doesn't exist yet (bun creates it itself).
- **`~/.bun`** — **NOT covered**: `$HOME` is deliberately *not* a writable root, so there is no ancestor inside the sandbox that can create `~/.bun`. Bun's first-run creation fails with `EPERM`.

Pre-create the directory **outside the sandbox** (`mkdir -p ~/.bun` before starting ocode). Granting it via `extra_allowed_paths` does not help while it still doesn't exist — a granted-but-missing root is skipped by the same logic.

## Mitigation

1. **Pre-create all directories** a toolchain needs before starting ocode in sandbox mode (e.g. `mkdir -p ~/.cache/bun ~/.bun`).
2. **Do not rely on `mkdir -p` inside sandboxed commands** to create missing writable roots — there is no writable ancestor to create them against.
3. **Audit `extra_allowed_paths`** for paths that may not exist yet; a granted-but-missing root is silently skipped, not granted.

## Related Gotchas

- [`shell-sandbox-writable-root-validation.md`](shell-sandbox-writable-root-validation.md) — covers canonicalization and rejection of volume roots to prevent confinement defeat.
- [`shell-sandbox-writable-root-validation.md` Gotcha: Subdirectory-of-Root Is Not Safe Either] — discusses why setting `/var` or `/tmp` as writable is almost as bad as `/`.