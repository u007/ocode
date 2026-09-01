---
type: Gotcha
title: Plugin Auto-Permission — Arbitrary Execution Risk
description: 'Updated gotcha: blanket OS temp auto-permission is now a deliberate v1.8.0 policy, not an unresolved regression. Historical v1.5.0 tightening preserved.'
tags:
  - plugins
  - security
  - permissions
  - auto-allow
  - gotcha
  - temp-directory
  - policy-decision
timestamp: 2026-08-31T08:22:09Z
---
# Plugin Auto-Permission — Arbitrary Execution Risk

## The Problem

The `BundledAutoPermissionPromptBody` (the auto-approval rules for the permission system) previously included blanket allowances for:
- `npm publish` — arbitrary package publishing
- `pnpm dlx` / `bunx` — arbitrary remote code execution
- Any executable on `$PATH`

These permissions bypass human approval entirely. A malicious or compromised plugin could craft a bash command matching one of these patterns to execute arbitrary code, exfiltrate data, or publish packages without the user's knowledge.

## Impact

- **Remote code execution** via `pnpm dlx` / `bunx` — a plugin can run any npm package without approval.
- **Supply chain attack** via `npm publish` — a plugin could publish the project's source or secrets to a public registry.
- **Arbitrary binary execution** — any executable on PATH is allowed, not just known-safe ones.

## The Fix (v1.5.0 → v1.8.0)

The auto-permission rules in `internal/config/auto_permission_prompt.go` were tightened to allow **only read-only inspection commands** for git, package managers, and HTTP:

1. **Git**: Read-only commands only — `status`, `diff`, `log`, `show`, `blame`, `ls-files`, `branch` (listing only), `remote -v`, `stash list`, `rev-parse`, `describe`.
2. **Package managers**: Inspection commands only — `npm ls`, `npm audit`, `pnpm list`, `bun pm ls`, version queries. **All** install, run, exec, dlx, publish, lifecycle hooks, and registry mutations require explicit approval.
3. **curl/wget**: Loopback-only traffic (`localhost`, `127.0.0.0/8`, `::1`) — cannot exfiltrate off-machine.
4. **Executables**: No blanket executable allowlist. Only the narrowly scoped commands above are permitted without approval.

## OS Temporary Directory Access — Deliberate Policy (v1.8.0)

The blanket OS temporary-directory auto-permission was **removed** in an earlier tightening pass (the original v1.5.0 fix documented below), but was **intentionally restored** in v1.8.0 after review.

**Current policy (v1.8.0, `auto_permission_prompt.go:59`):**

> Always ALLOW reading and manipulation of any OS temporary directory, including `/tmp`, `/var/tmp`, `$TMPDIR`, `$TMP`, and the platform-specific `os.TempDir()` (and any path beneath them). This covers listing, creating, reading, writing, modifying, moving, copying, and deleting files/directories under temp. This exception applies only to temporary directories; it does not grant unrestricted access to the rest of the filesystem or to the network.

**Rationale:** The team determined that blanket temp access is acceptable because:
- OS temp directories (`/tmp`, `$TMPDIR`) are world-writable by design — any process can already read/write them without privilege escalation.
- Restricting temp in the auto-permission layer adds friction to legitimate developer workflows (build artifacts, test fixtures, scratch files, toolchain caches) without meaningful security gain, since a malicious plugin could already operate within `/tmp` without ocode's permission layer.
- The restriction is scoped to temp directories only; it does not grant broader filesystem or network access.

**Historical context:** The original v1.5.0 tightening (documented below) removed temp access as part of a blanket least-privilege sweep. The v1.8.0 restoration was a deliberate reversal after evaluating the actual threat model — this is a policy decision, not a regression.

## Current Allowlist (v1.8.0)

| Category | Allowed without approval | Requires approval |
|----------|-------------------------|-------------------|
| Git | `status`, `diff`, `log`, `show`, `blame`, `ls-files`, `branch` (list), `remote -v`, `stash list`, `rev-parse`, `describe` | `add`, `commit`, `push`, `pull`, `merge`, `rebase`, `checkout`, `reset`, `clean`, `stash` (save/pop/drop) |
| Package managers | `npm ls`, `npm audit`, `pnpm list`, `bun pm ls`, version queries | `install`, `run`, `exec`, `dlx`, `publish`, `ci`, `test`, `add`, `remove`, `link`, lifecycle hooks |
| HTTP | curl/wget to `localhost`/`127.0.0.0/8`/`::1` | Any remote target |
| Filesystem | OS temp dirs (`/tmp`, `/var/tmp`, `$TMPDIR`, `$TMP`, `os.TempDir()`) — full read/write/create/delete | All other filesystem paths |
| Executables | (none) | Any executable on `$PATH` |

## Gotcha

When adding new auto-allowed commands, apply the **principle of least privilege**:
- Is the command read-only? If it can write to external systems (registries, remote servers, shared state), it should NOT be auto-approved.
- Does the command accept user-controlled input that could be weaponized? Shell metacharacters, URLs, or package names controlled by a plugin are injection vectors.
- **Never add package-manager exec/run/test/dlx/publish commands** — these are the primary attack surface for supply-chain and code-execution vectors.
- OS temp directories are the one filesystem exception: they are auto-allowed because restricting them has no security value (world-writable by OS design) and blocks legitimate workflows.

The auto-permission layer is a security boundary. Every addition must be reviewed against these criteria.

## Related

- [Plugin Install Rollback Bug](plugin-install-rollback-bug.md) — incomplete rollback on failed installs.
- [Plugin Removal — Root Directory Deletion Risk](plugin-removal-root-deletion.md) — path containment in removal.
- [Git ext:: Transport Auto-Permission Bypass](git-ext-transport-auto-allow-bypass.md) — ext:: transport bypasses git allowlist.