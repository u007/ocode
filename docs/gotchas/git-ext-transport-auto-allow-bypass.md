---
type: Gotcha
title: 'Git ext:: Transport Auto-Permission Bypass'
description: 'Critical security gotcha: transparent stripping of git -c config overrides in IsHarmfulBashCommand allows ext:: transport to bypass auto-permission allowlist, enabling arbitrary shell command execution via read-only git subcommands like ls-remote.'
tags:
  - security
  - permissions
  - git
  - auto-permission
  - gotcha
timestamp: 2026-08-30T15:21:57Z
---
## The Problem

The transparent stripping of `git -c <k>=<v>` config overrides in `IsHarmfulBashCommand` / `matchSubcommandAllow` (permissions.go:1789) allows an allow-listed read-only git subcommand (e.g. `git ls-remote`) to execute arbitrary shell commands via the `ext::` transport when paired with a crafted command string.

The auto-permission guard sees `git ls-remote` → ALLOW, while git actually shells out to the `ext::` transport to execute arbitrary commands.

## Attack Vector

```bash
# This passes auto-permission checks — guard sees "git ls-remote" (ALLOW)
# But git actually executes the shell command after "ext::"
git -c protocol.ext.allow=always ls-remote "ext::echo PWNED"
```

The allowlist works by:
1. Stripping `-c key=value` config overrides (intended to be safe — config doesn't change the subcommand)
2. Matching the remaining subcommand against the allowlist

The `ext::` transport bypasses this because `ls-remote` is on the allowlist, but `ext::` tells git to invoke an arbitrary command as the "remote".

## Root Cause

Permissions.go strips `-c <k>=<v>` to normalize the command for allowlist matching. This is correct for most git config, but `protocol.ext.allow=always` specifically enables the `ext::` transport, which executes arbitrary shell commands. The normalization treats it as harmless config when it fundamentally changes git's execution behavior.

## Mitigation Options

1. **ext:: transport detection**: After stripping `-c` overrides, inspect the full command string for `ext::` in any argument. Reject or warn if detected.
2. **Command-string-level inspection**: Before stripping, check if any `-c` argument sets `protocol.ext.allow=always` (or `*` or a pattern matching `ext`). If so, treat the entire command as dangerous regardless of the subcommand.
3. **Removal of ext:: from allowed paths**: The allowlist for `ls-remote` and similar fetch commands could reject any invocation that includes `ext::` in the remote URL argument.

## Severity

**Critical** — this allows arbitrary code execution through what the auto-permission system considers a safe, read-only git operation. Any user with auto-permissions enabled is vulnerable if a malicious prompt or context induces this command.

## References

- `internal/agent/permissions.go:1789` — `IsHarmfulBashCommand` / `matchSubcommandAllow`
- Git documentation on `ext::` transport and `protocol.ext.allow`
- `docs/gotchas/plugin-auto-permission-security.md` — related auto-permission security gotcha