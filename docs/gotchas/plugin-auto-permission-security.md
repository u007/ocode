---
type: Gotcha
title: Plugin Auto-Permission — Arbitrary Execution Risk
description: 'Security gotcha: auto-permission prompt tightened to read-only inspection only — no package install/run/exec/lifecycle/registry commands, no blanket OS temp access.'
tags:
  - plugins
  - security
  - permissions
  - auto-allow
  - npm
  - gotcha
timestamp: 2026-08-28T07:08:20Z
---
# Plugin Auto-Permission — Arbitrary Execution Risk

## The Problem

The `BundledAutoPermissionPromptBody` (the auto-approval rules for the permission system) previously included blanket allowances for:
- `npm publish` — arbitrary package publishing
- `pnpm dlx` / `bunx` — arbitrary remote code execution
- Any executable on `$PATH`
- Unrestricted `/tmp` access

These permissions bypass human approval entirely. A malicious or compromised plugin could craft a bash command matching one of these patterns to execute arbitrary code, exfiltrate data, or publish packages without the user's knowledge.

## Impact

- **Remote code execution** via `pnpm dlx` / `bunx` — a plugin can run any npm package without approval.
- **Supply chain attack** via `npm publish` — a plugin could publish the project's source or secrets to a public registry.
- **Arbitrary binary execution** — any executable on PATH is allowed, not just known-safe ones.
- **Unrestricted temp access** — `/tmp` can be used for staging exfiltrated data.

## The Fix

The auto-permission rules in `internal/config/auto_permission_prompt.go` were tightened to allow **only read-only inspection commands**:

1. **Git**: Read-only commands only — `status`, `diff`, `log`, `show`, `blame`, `ls-files`, `branch` (listing only), `remote -v`, `stash list`, `rev-parse`, `describe`.
2. **Package managers**: Inspection commands only — `npm ls`, `npm audit`, `pnpm list`, `bun pm ls`, version queries. **All** install, run, exec, dlx, publish, lifecycle hooks, and registry mutations require explicit approval.
3. **curl/wget**: Loopback-only traffic (`localhost`, `127.0.0.0/8`, `::1`) — cannot exfiltrate off-machine.
4. **Temp paths**: Arbitrary `/tmp` and `$TMPDIR` access is no longer auto-allowed; only explicit project-scoped paths in pre-authorized roots may be auto-approved.
5. **Executables**: No blanket executable allowlist. Only the narrowly scoped commands above are permitted without approval.

## Code Reference

- `internal/config/auto_permission_prompt.go` — `BundledAutoPermissionPromptBody` (lines 30–58)

## Current Allowlist (v1.5.0)

| Category | Allowed without approval | Requires approval |
|----------|-------------------------|-------------------|
| Git | `status`, `diff`, `log`, `show`, `blame`, `ls-files`, `branch` (list), `remote -v`, `stash list`, `rev-parse`, `describe` | `add`, `commit`, `push`, `pull`, `merge`, `rebase`, `checkout`, `reset`, `clean`, `stash` (save/pop/drop) |
| Package managers | `npm ls`, `npm audit`, `pnpm list`, `bun pm ls`, version queries | `install`, `run`, `exec`, `dlx`, `publish`, `ci`, `test`, `add`, `remove`, `link`, lifecycle hooks |
| HTTP | curl/wget to `localhost`/`127.0.0.0/8`/`::1` | Any remote target |
| Filesystem | Explicit project-scoped pre-authorized roots | `/tmp`, `$TMPDIR`, arbitrary paths |
| Executables | (none) | Any executable on `$PATH` |

## Gotcha

When adding new auto-allowed commands, apply the **principle of least privilege**:
- Is the command read-only? If it can write to external systems (registries, remote servers, shared state), it should NOT be auto-approved.
- Does the command accept user-controlled input that could be weaponized? Shell metacharacters, URLs, or package names controlled by a plugin are injection vectors.
- Scope filesystem access narrowly — project-scoped paths only, never system-wide directories.
- **Never add package-manager exec/run/test/dlx/publish commands** — these are the primary attack surface for supply-chain and code-execution vectors.

The auto-permission layer is a security boundary. Every addition must be reviewed against these criteria.

## Related

- [Plugin Install Rollback Bug](plugin-install-rollback-bug.md) — incomplete rollback on failed installs.
- [Plugin Removal — Root Directory Deletion Risk](plugin-removal-root-deletion.md) — path containment in removal.