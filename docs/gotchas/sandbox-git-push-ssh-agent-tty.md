---
type: Gotcha
title: Sandbox git push — SSH agent inheritance and fail-closed TTY prompts
description: 'Sandbox mode git push behavior: why plain push auto-allows, why force flags ASK, root causes of "ssh key not available" failures, and user-facing fixes'
tags:
  - sandbox
  - ssh
  - git
  - seatbelt
  - security
  - macOS
timestamp: 2026-09-06T08:41:48Z
---
# Sandbox git push — SSH agent inheritance and fail-closed TTY prompts

## Verified claims (2026-09-06)

**1. sandbox mode does NOT block git push over SSH** — Empirical proof with an exact replica of the generated seatbelt profile (`sandbox-exec -f`): `ssh-add -l` lists agent keys (launchd unix-socket `SSH_AUTH_SOCK` reachable in-sandbox), `git ls-remote origin HEAD` exit 0, `git push --dry-run origin` exit 0. Profile
`internal/shell/sandbox/profile.go:23-77`: `(allow default)`, `(deny file-write*)` with per-root write allows + `/dev/null`, `(allow file-read*)` global, `(allow network*)`, and `/dev/tty` deliberately NOT granted.

**2. Permission layer: plain `git push` AUTO-ALLOWS in sandbox mode** — sandbox auto-allow
`internal/agent/permissions.go:~1433` fires before the normal pipeline, and
`sensitiveSandboxDecision` `permissions.go:2607-2643` only ASKs on STATIC
`~/.ssh`/`.env/auth.json` references (path args/redirects), which plain push lacks — the key read happens inside git's ssh child and is invisible to static extraction (documented residual). Force-flagged `git push/pull` ASKs via the 2026-09-06 sandbox gate
`isHarmfulForceCommand`; `TestDecideSandboxGitPush`: push=Allow, force=Ask). In normal (non-sandbox) mode plain push still goes through the auto-permission judge.

**3. Reported failure "git push fails: ssh key not available" under sandbox is NOT the sandbox denying key access** (reads are globally open by design — cite
`architecture/shell-sandbox-integrity-only-mode.md`). Two real causes:

- **(a) Passphrase-protected key not loaded into ssh-agent + no TTY**: the profile intentionally denies fresh `/dev/tty` opens
  `profile.go:~47-61` comment: passphrase/password prompts fail closed), and the bash tool's captured pipes provide no TTY either → ssh cannot prompt → "Permission denied (publickey)" / "Load key ... error in libcrypto". This machine: `id_ed25519` passphrase-free (`ssh-keygen -y exit 0`), `id_rsa` encrypted (`exit 255`).

- **(b) Launch context env**: desktop `.app` (launchd), RC bridge, or web server processes may be launched WITHOUT `SSH_AUTH_SOCK` in their environment. `buildBashCmd`
  `internal/tool/bash_build.go:38` never scrubs env (foreground
  `exec.go:222` and background `process.go:294` inherit `os.Environ()`), so if the PARENT lacks `SSH_AUTH_SOCK` the sandboxed ssh sees no agent. Diagnose by checking `SSH_AUTH_SOCK` inside the failing ocode process, not just the interactive terminal.

**4. User-facing fixes**: `ssh-add --apple-use-keychain ~/.ssh/<key>` in the user's own terminal before the session (agent socket IS reachable from the sandbox — verified); use a passphrase-free default key; or run the push via the unsandboxed `!` shell path (interactive PTY terminal and web `!shell` run unsandboxed per
`AGENTS.md`).

**5. Safety verdict + hard NOs**: allowing git push in sandbox mode adds no new local read exposure (integrity-only design) and does not weaken local write confinement — the remote mutation is the explicitly requested action and is outside any local sandbox's reach. **HARD NOs**: never grant `~/.ssh` as a writable root / allow writes there (authorized_keys planting → persistence escape); never add `/dev/tty` to the seatbelt profile (alt-screen TUI corruption class + hanging interactive prompts).

**6. ocode's own code emits no "ssh key not available" string** (grepped) — that error text is from ssh/git itself.

## Related gotchas

- [`sandbox-writable-root-must-exist.md`](sandbox-writable-root-must-exist.md) — writable root validation and pre-creation
- [`shell-sandbox-integrity-only-mode.md`](../architecture/shell-sandbox-integrity-only-mode.md) — integrity-only, not confidentiality
- [`auto-permission-dependency-bin-policy.md`](auto-permission-dependency-bin-policy.md) — dependency binary policy