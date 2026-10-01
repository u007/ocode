---
type: Concept
title: Sandbox Permission Mode
description: 'Updated sandbox permission mode concept doc with read-vs-write sensitive-path split, new predicate names, and code references'
tags: []
timestamp: 2026-09-30T07:37:19Z
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
- Web: the chat sidebar's Permission pill and `/yolo` + `/sandbox` commands, scoped to the active chat session; the Settings→Permissions form owns only the persisted default.
- Cron is unaffected: each job resolves its own per-job permission mode independently via `resolveCronPermissionMode` (`internal/server/scheduler_runner.go:172`); blank → `normal`.

## Per-session live mode (web/server, 2026-09-18)

The **live** mode is per chat session, not per process. `PUT /api/permissions/mode` and `PUT /api/permissions/yolo` require a `session_id` (body or `?session_id=`) and apply only to that session's live agent; `GET /api/permissions` and `GET /api/permissions/yolo` accept the same query param. A session-less write is a `400` (there is deliberately no process-global path).

- Override storage: the session transcript's metadata key `permission_mode` (via `session.UpdateMetadataForDir`), mirroring the per-session `model` override, so it survives resume and server restart.
- Applied at every agent build: `buildAgentSession` and `registerAgentSession` (`internal/server/agent_session.go`) read the session's own metadata; other sessions never inherit it.
- Status: per-session snapshot builders stamp `permission_mode` / `permission_sandbox_supported` / `permission_effective_behavior` via `applySessionPermissionFields` (`internal/server/handler_permissions.go`); the process-wide `buildStatusSnapshot` reports only the persisted default.
- Bridged TUI sessions resolve through the RC bridge agent and are persisted by the TUI, not the server.
- New-session seeding: `POST /api/chat` accepts `permission_mode` so a draft (`new-*`) tab's pick is persisted when the first message creates the session.
- The persisted default (`PUT /api/config/ocode/permissions-mode`) still governs sessions with no override and is edited in Settings.

## Destructive git routing

In `PermissionManager.Decide` (`internal/agent/permissions.go`), three guards fire **before** the sandbox auto-allow:

1. **`isHarmfulForceCommand(command)`** → `bashPermissionRequest(..., "sandbox.harmful_force")` → `PermissionAsk`. Covers force-flagged `git push`/`git pull`.

2. **`isExfiltrationRiskBash(command)`** → `bashPermissionRequest(..., "sandbox.exfiltration_risk")` → `PermissionAsk`. Covers curl/wget/httpie/nc exfiltration forms (file upload, env var in URL/flag position, subshell). Checked before the git gate only so the Ask carries an accurate label — `IsHarmfulBashCommand` also includes this family and would otherwise report it as `sandbox.harmful_git`. The loopback exemption (`subprocessTargetsLocalhost`) applies only when every target token is loopback — any other URL (even a `-e`/`--proxy` value), a whole-word `$`/backtick expansion that could be the target (`$URL`, `"$2"`), or a scheme-less dotted host voids it. Env vars used as data to loopback (header/query/`-u`/`-d` values) keep the exemption. A curl whose URL or flags arrive through a variable or function argument (`curl $A "$2"`) therefore cannot be proven loopback and always lands here.

3. **`IsHarmfulBashCommand(command)`** → `bashPermissionRequest(..., "sandbox.harmful_git")` → `PermissionAsk`. Covers the destructive git family: `git stash pop/apply/drop/clear`, `git checkout`, `git reset`, `git clean`, `git restore`, `git switch`.

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

`sandboxSensitivePath` (`permissions.go:3227`) classifies a resolved path against the sandbox sensitive set:

- **auth.json / auth.profiles.json** (read or write) → Ask
- **ocode config dir** (write only) → Ask — guards self-escalation via config rewrite
- **~/.ssh** (read or write) → Ask
- **Secret material** (`isSecretMaterialPath`) → Ask (read or write). Bare relative dotfiles (`cat .env`, `grep X .env`) are path args (`isLikelyPathArg`), same as `./.env`.
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
- `isHarmfulForceCommand`: `internal/agent/permissions.go:723`
- `IsHarmfulBashCommand`: `internal/agent/permissions.go:1414`
- `isReadOnlyGitStashForm`: `internal/agent/permissions.go:577`
- `isSensitivePath`: `internal/agent/permissions.go:2790`
- `isSecretMaterialPath`: `internal/agent/permissions.go:2799`
- `isRepoMetadataPath`: `internal/agent/permissions.go:2852`
- `sandboxSensitiveTargets`: `internal/agent/permissions.go:2919`
- `sandboxSensitivePath`: `internal/agent/permissions.go:3227`

## Enforcement details (moved from CLAUDE.md)

`sandbox` is the fourth permission mode (besides `normal`/`yolo`/`locked`),
toggled from the TUI permission-mode click cycle, `/sandbox`, or the web
sidebar permission pill / `/sandbox` command. On the web/desktop server the
live mode is **per chat session** (persisted in the session's metadata;
`PUT /api/permissions/mode` and `/yolo` require a `session_id`), so toggling
one chat never changes another. It is **write-integrity confinement only** —
it does NOT protect secrets or prevent exfiltration.

What it confines:
- Only the agent **shell tool** (`bash`) is wrapped. The interactive PTY
  terminal (`handler_terminal.go`) and the web `!shell` path run unsandboxed.
- Filesystem **writes** fail at the OS level unless the target is under a
  writable root: workspace/`extra_allowed_paths`, the opencode shared data dir
  (`~/.local/share/opencode`, including per-project `project/**` state —
  sessions, snapshots, md-summaries, memory), language dependency caches
  (npm/pip/cargo/go/maven/gradle), `~/.claude`, the fixed global git-ignore
  files (`paths.GitIgnoreFiles`: `$XDG_CONFIG_HOME/git/ignore` else
  `~/.config/git/ignore`, plus `~/.gitignore_global` and `~/.gitignore` —
  exact files only, never `~/.config`/`$HOME`, never an arbitrary
  `core.excludesFile`), and temp dirs
  (`/tmp`, `/var/tmp`, `os.TempDir()`, plus on macOS the uid-owned
  `/var/folders/*/*/{T,C}` confstr dirs — found by ownership, not `$TMPDIR`,
  so `mktemp`/Python/Node/clang caches work even when ocode runs without
  `TMPDIR`, e.g. under launchd; see `pathscope.DarwinUserDirs`).

What still asks (permission layer, not the OS). The predicate split, per-target
write map and full carve-out list are in "Sensitive-path carve-outs" above.
- sensitive paths: `auth.json`, ocode config dir (writes only), `~/.ssh`, and
  secret material (`isSecretMaterialPath`: `.env`, `.netrc`, SSH keys, `.pem`,
  `.aws/`, …) → Ask on read or write;
  repo-metadata dirs (`.git/`, `.github/workflows/`) → Ask on WRITE only
- danger-`rm` heuristics → Ask
- destructive git forms (`git stash`/`checkout`/`reset`/`clean`/`restore`/
  `switch`, plus force-flagged `git push`/`pull`; read-only
  `git stash list`/`show` still auto-allow) → Ask. The
  check is applied to **every constituent of a compound command**, not just the
  whole line: `cd repo && git stash` asks, because
  `IsHarmfulBashCommand`/`isHarmfulForceCommand` only recognize a command whose
  first word is `git` (the sandbox gate parses first; see `Decide` in
  `internal/agent/permissions.go`).
- explicit user bash deny rules (`permissions.bash.prefixes`, written by
  `/ban add` or the permission dialog) → Deny (hard, never re-considered by the
  auto-judge), enforced in sandbox too. A `git stash` ban targets the mutating
  family (push/pop/apply/drop/clear, bare stash); the read-only inspection
  forms (`list`/`show`) keep auto-allowing (`matchBashPrefixRule`).
- the Ask → auto-judge hand-off (`IsHarmfulRequest` in `agent.go`) is per
  constituent as well: any harmful fragment sends the whole line to a human,
  never to the Jev judge. It judges the **whole line from `req.Args`**, not
  `Request.Command` — Decide fills the latter with only the first segment that
  needed a human, so `curl … && git reset --hard` would otherwise slip past
  (see `docs/gotchas/auto-permission-harmful-segment-masked-by-earlier-ask.md`).
  The same gate runs on the Deny → auto-judge branch.
- wrappers are peeled before those checks (`effectiveCommandWords`,
  `permissions_wrappers.go`): launcher prefixes (`env`, `command`, `nohup`,
  `exec`, `time`, `nice`, `timeout`, `xargs`, `stdbuf`, `sudo`, `doas`, …),
  path-qualified binaries (`/usr/bin/git`), and shell re-exec / `eval` bodies
  (`bash -c 'git stash'`) are judged as the command they really run — for
  both the harmful gate and `/ban` prefix denies. A command whose binary is a
  shell expansion (`$g stash`, `$(which git) stash`) → Ask in sandbox
  (`sandbox.opaque_command`), since nothing static can resolve it.
- writes to permission-defining files (`.ocode/settings.json`,
  `.claude/settings.json`, ocode config gating files) and loopback requests to
  `/api/permissions*` → Ask (self-escalation guard, all modes)

Ask routes to the auto-permission LLM judge when `auto` is on, else a human
prompt. These static checks catch direct commands (`cat auth.json`), but NOT a
read/write hidden inside an interpreter (`python -c ...`) — the backends are
write-walls only and never OS-block secret reads (that would make approval
impossible). Real config/secret changes should be made outside sandbox.

Platform matrix (see "Platform support" above): macOS Seatbelt via
`/usr/bin/sandbox-exec` (trusted absolute path); Linux Landlock (kernel ≥5.13,
ABI-probed, `PR_SET_NO_NEW_PRIVS`) with `bubblewrap` (`/usr/bin/bwrap`)
fallback; Windows has no backend (behaves like `normal`). Fail-closed on
macOS/Linux: no backend available → the command errors before starting.

Sandbox **persists** like any other mode (restart comes back in sandbox). Cron
jobs resolve their own per-job mode (`resolveCronPermissionMode`, blank →
`normal`), so a persisted sandbox default never leaks into scheduled runs.
