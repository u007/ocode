---
type: Concept
title: Sandbox Permission Mode
description: 'Updated sandbox permission mode concept doc with read-vs-write sensitive-path split, new predicate names, and code references'
tags: []
timestamp: 2026-09-18T05:05:30Z
---
## Decision

The `sandbox` permission mode is the fourth mode alongside `normal`, `yolo`, and `locked`. It runs bash commands without prompts but confines OS-level filesystem writes to classified allowed roots (write-integrity only). It now **persists as a durable default** like any other mode, and **destructive git commands route to Ask** before the sandbox auto-allow.

## Mode behaviour

| Aspect | Behaviour |
|--------|-----------|
| Bash prompts | None (like YOLO) |
| Filesystem writes | Confined to classified writable roots via OS backend (Seatbelt on macOS, Landlock/bwrap on Linux) |
| Reads, exec, network | Global (not restricted) |
| Auto-permission layer | Kept enabled (unlike YOLO, which disables it) |
| Persistence | **Durable default** — persists to `ocodeconfig.json` via `SavePermissionModeSwitch` like any other mode |

## Four permission modes

`internal/agent/permissions.go` defines four modes (`PermissionMode*` constants, ~line 36):

- `normal` — prompts for writes outside allowed roots; auto-permission judge active
- `yolo` — no prompts, no confinement; auto-permission layer disabled
- `locked` — read-only; all writes blocked
- `sandbox` — no prompts, OS write confinement; auto-permission layer kept active

## Persistence (Decision 2 superseded)

Sandbox was originally per-session only: `persistPermissions()` (`internal/tui/model.go:14734`) clamped `sandbox` to `normal` on the persist path (Decision 2 in `docs/superpowers/plans/2026-08-31-shell-sandbox/INDEX.md`). This has been superseded: sandbox now persists like any other mode.

- `persistPermissions()` calls `config.SavePermissionModeSwitch(string(pm.Mode()))` (`model.go:14755`) — writes the mode verbatim, no clamp.
- `SavePermissionModeSwitch` (`internal/config/ocodeconfig.go:3164`) sets `cfg.Permissions.Mode = mode` with no sandbox-specific logic.
- TUI: `/sandbox` bare toggles, `/sandbox on|off|status`, permission-mode click cycle.
- Web: mode selector in settings, `PUT /api/permissions/mode`.
- Cron is unaffected: each job resolves its own per-job permission mode independently via `resolveCronPermissionMode` (`internal/server/scheduler_runner.go:172`); blank → `normal`.

## Destructive git routing

In `PermissionManager.Decide` (`internal/agent/permissions.go`), two guards fire **before** the sandbox auto-allow:

1. **`isHarmfulForceCommand(command)`** → `bashPermissionRequest(..., "sandbox.harmful_force")` → `PermissionAsk`. Covers force-flagged `git push`/`git pull`.

2. **`IsHarmfulBashCommand(command)`** → `bashPermissionRequest(..., "sandbox.harmful_git")` → `PermissionAsk`. Covers the destructive git family: `git stash pop/apply/drop/clear`, `git checkout`, `git reset`, `git clean`, `git restore`, `git switch`.

Rationale: the OS write-wall confines file writes to classified roots but is blind to repo mutations that stay inside the allowed workdir — history rewrite, branch switch, stash create/drop, untracked removal all mutate the repo within the writable boundary. Normal mode is unchanged (already Ask via `IsHarmfulBashCommand`); YOLO remains the promptless escape hatch.

Read-only forms (`git stash list`/`show`) are excluded via `isReadOnlyGitStashForm` (`permissions.go:577`) and still auto-allow.

## Sensitive-path carve-outs

Sandbox mode does **not** bypass the existing sensitive-path Ask guards, but it now distinguishes **read vs write** to avoid false-positive Asks on normal repo inspection.

### Predicate split

`isSensitivePath` (`permissions.go:2790`) is the OR of two named predicates:

- **`isSecretMaterialPath()`** (line 2799) — credential-bearing material: `.env` + `.env.*` variants (safe templates excluded), `.netrc`/`.npmrc`/`.pypirc`, SSH private-key filenames (`id_rsa`/`id_ed25519`/`id_ecdsa`/`id_dsa`), certificate/key suffixes (`.pem`/`.key`/`.p12`/`.pfx`/`.secrets`), and `.aws/`. **Ask on read OR write** — a read can exfiltrate a credential.

- **`isRepoMetadataPath()`** (line 2852) — `.git/` and `.github/workflows/`. **Ask on WRITE only** — a planted `.git/hooks/*` or workflow executes arbitrary code and stays inside the workdir where the OS write-wall is blind. Reading/listing them (`ls .git/`, `cat .git/config`) auto-allows, matching normal mode.

### Per-target write classification

`sandboxSensitiveTargets` (`permissions.go:2919`) returns a per-target write map (`map[target]bool`) instead of a single command-wide bool. It is **fail-closed**: a target counts as written unless the fragment is provably read-only over its path args (`commandReadsPathsOnly`), or is copy-like (`cp`/`install`/`ln` destination only; `mv` marks every positional, since it deletes its sources). Unrecognized commands (`truncate`, `chmod`, `dd`, custom scripts) mark their path args as writes; parse failure marks every target as a write.

### Carve-out rules

`sandboxSensitivePath` (`permissions.go:3074`) classifies a resolved path against the sandbox sensitive set:

- **auth.json / auth.profiles.json** (read or write) → Ask
- **ocode config dir** (write only) → Ask — guards self-escalation via config rewrite
- **~/.ssh** (read or write) → Ask
- **Secret material** (`isSecretMaterialPath`) → Ask (read or write)
- **Repo metadata** (`isRepoMetadataPath`) → Ask on write only; read/list auto-allows

These route through the auto-permission judge when `auto` is on, else a human prompt.

## Security model

Sandbox is **write-integrity only**, not confidentiality. Reads, exec, and network egress stay open. A sandboxed `python`/`node` can still read `.env`, `~/.ssh`, `auth.json` and POST them anywhere. See `architecture/shell-sandbox-integrity-only-mode.md` for the full security model.

## Platform support

| Platform | Backend | Status |
|----------|---------|--------|
| macOS | Seatbelt (`sandbox-exec`) | Real confinement |
| Linux | Landlock (≥5.13) with bwrap fallback | Real confinement |
| Windows | No backend | Degrades to `normal` prompting |

Fail-closed on macOS/Linux: if mode is `sandbox` and a backend is supported but unavailable, the command fails before starting (no silent unsandboxed execution).

## Code references

- Mode constants: `internal/agent/permissions.go:36-44`
- `Decide` path: `internal/agent/permissions.go:~1430-1505`
- `persistPermissions`: `internal/tui/model.go:14734`
- `SavePermissionModeSwitch`: `internal/config/ocodeconfig.go:3164`
- `runSandboxCmd`: `internal/tui/commands.go:1056`
- `resolveCronPermissionMode`: `internal/server/scheduler_runner.go:172`
- `isHarmfulForceCommand`: `internal/agent/permissions.go:721`
- `IsHarmfulBashCommand`: `internal/agent/permissions.go:1175`
- `isReadOnlyGitStashForm`: `internal/agent/permissions.go:577`
- `isSensitivePath`: `internal/agent/permissions.go:2790`
- `isSecretMaterialPath`: `internal/agent/permissions.go:2799`
- `isRepoMetadataPath`: `internal/agent/permissions.go:2852`
- `sandboxSensitiveTargets`: `internal/agent/permissions.go:2919`
- `sandboxSensitivePath`: `internal/agent/permissions.go:3074`