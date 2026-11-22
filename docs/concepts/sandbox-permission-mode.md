---
type: Concept
title: Sandbox Permission Mode
description: 'Updated sandbox permission mode concept doc with read-vs-write sensitive-path split, new predicate names, and code references'
tags: []
timestamp: 2026-11-21T17:45:00Z
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

`internal/agent/permissions.go` defines four modes (`PermissionMode*` constants, ~line 42):

- `normal` — prompts for writes outside allowed roots; auto-permission judge active
- `yolo` — no prompts, no confinement; auto-permission layer disabled
- `locked` — read-only; all writes blocked
- `sandbox` — no prompts, OS write confinement; auto-permission layer kept active

## Jev judge gate on every bash call

Sandbox's auto-allow does not skip the decision judge. In `handleToolCall` (`internal/agent/agent.go`, tier `sandbox_judge_gate`), any bash call `Decide` allowed in sandbox mode — allowlist, prefix rule, tool rule or the sandbox blanket allow alike — is converted to an Ask (`sandbox.judge`) when auto-permission is on and the judge model is a decision model (`isDecisionModel`: Jev/clef). The normal auto-judge branch then decides; a denial or judge failure falls through to the human ask. Hard denies stay denies. No decision model configured: unchanged auto-allow. **Exception: a line the user allow-listed end to end** (`userAllowListCoversLine`, `permissions.go`) is not converted. Every segment must be a user allow-level prefix rule or one of a fixed set of inert glue commands (`cd`, `sleep`, `head`, `tail`, `wc`), at least one segment must be the user's own rule, and no segment may carry an env assignment or a non-device redirection. A deny rule on any segment disqualifies the line. `verifyAutoGrant` still runs on the skip path; a veto sends the line to the judge. The built-in auto-allow list is deliberately not used for this (it holds `curl`). When the judge does run, the state carries `user_allowed_segments` (`userAllowedSegments`): the segments ocode matched against the user's allow prefixes, told to the judge as already user-approved facts.

## Persistence (Decision 2 superseded)

Sandbox was originally per-session only: `persistPermissions()` (`internal/tui/model.go`) clamped `sandbox` to `normal` on the persist path (Decision 2 in `docs/superpowers/plans/2026-08-31-shell-sandbox/INDEX.md`). This has been superseded: sandbox now persists like any other mode.

- `persistPermissions()` calls `config.SavePermissionModeSwitch(string(pm.Mode()))` (`model.go`) — writes the mode verbatim, no clamp.
- `SavePermissionModeSwitch` (`internal/config/ocodeconfig.go`) sets `cfg.Permissions.Mode = mode` with no sandbox-specific logic.
- TUI: `/sandbox` bare toggles, `/sandbox on|off|status`, permission-mode click cycle.
- Web: the chat sidebar's Permission pill and `/yolo` + `/sandbox` commands, scoped to the active chat session; the Settings→Permissions form owns only the persisted default.
- Cron is unaffected: each job resolves its own per-job permission mode independently via `resolveCronPermissionMode` (`internal/server/scheduler_runner.go`); blank → `normal`.

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

Read-only forms (`git stash list`/`show`) are excluded via `isReadOnlyGitStashForm` (`permissions.go`) and still auto-allow.

## Sensitive-path carve-outs

Sandbox mode does **not** bypass the existing sensitive-path Ask guards, but it now distinguishes **read vs write** to avoid false-positive Asks on normal repo inspection.

### Predicate split

`isSensitivePath` (`permissions.go`) is the OR of three named predicates:

- **`isSecretMaterialPath()`** — credential-bearing material: `.env` + `.env.*` variants (safe templates excluded), `.netrc`/`.npmrc`/`.pypirc`, SSH private-key filenames (`id_rsa`/`id_ed25519`/`id_ecdsa`/`id_dsa`), certificate/key suffixes (`.pem`/`.key`/`.p12`/`.pfx`/`.secrets`), and `.aws/`. **Ask on read OR write** — a read can exfiltrate a credential.

- **`isRepoMetadataPath()`** — `.git/` and `.github/workflows/`. **Ask on WRITE only** — a planted `.git/hooks/*` or workflow executes arbitrary code and stays inside the workdir where the OS write-wall is blind. Reading/listing them (`ls .git/`, `cat .git/config`) auto-allows, matching normal mode.

- **`isLoginStatePath()`** — the browser login-state directory, `<GlobalDataDir>/browse/login-states`, which holds a box's cookies and storage. It matches at a path-segment boundary and by resolved path, ignoring case on darwin and windows. **Ask on read OR write**, and it is not relaxed by the secrets concern: the model must never see or alter a login state.

### Per-target write classification

`sandboxSensitiveTargets` (`permissions.go`) returns a per-target write map (`map[target]bool`) instead of a single command-wide bool. It is **fail-closed**: a target counts as written unless the fragment is provably read-only over its path args (`commandReadsPathsOnly`), or is copy-like (`cp`/`install`/`ln` destination only; `mv` marks every positional, since it deletes its sources). Unrecognized commands (`truncate`, `chmod`, `dd`, custom scripts) mark their path args as writes; parse failure marks every target as a write.

### Carve-out rules

`sandboxSensitivePath` (`permissions.go`) classifies a resolved path against the sandbox sensitive set:

- **auth.json / auth.profiles.json** (read or write) → Ask
- **ocode config dir** (write only) → Ask — guards self-escalation via config rewrite
- **~/.ssh** (read or write) → Ask
- **Secret material** (`isSecretMaterialPath`) → Ask (read or write). Bare relative dotfiles (`cat .env`, `grep X .env`) are path args (`isLikelyPathArg`), same as `./.env`.
- **Repo metadata** (`isRepoMetadataPath`) → Ask on write only; read/list auto-allows
- **Browser login-state directory** (`isLoginStatePath`, checked in `sandboxSensitivePathRelaxed`) → Ask on read or write, not relaxed by the secrets concern

These route through the auto-permission judge when `auto` is on, else a human prompt. The login-state directory is the exception: its Ask is human-only and never reaches the judge (see "What still asks").

## Security model

Sandbox is **write-integrity only**, not confidentiality. Reads, exec, and network egress stay open. A sandboxed `python`/`node` can still read `.env`, `~/.ssh`, `auth.json` or a browser login-state file and POST them anywhere. See `architecture/shell-sandbox-integrity-only-mode.md` for the full security model.

The deterministic guards every mode shares (unknown-tool rejection before the permission layer, no stat-based access decision outside the read-target gate, a nonexistent path inside an allowed root staying allowed for non-read tools, and the read-target check running first) are anchored on [Auto-Permission Enforced Categories → Deterministic guards before any judge](concepts/auto-permission-enforced-categories.md#deterministic-guards-before-any-judge-unknown-tools-path-scope).

## Platform support

| Platform | Backend | Status |
|----------|---------|--------|
| macOS | Seatbelt (`sandbox-exec`) | Real confinement |
| Linux | Landlock (≥5.13) with bwrap fallback | Real confinement |
| Windows | No backend | Degrades to `normal` prompting |

Fail-closed on macOS/Linux: if mode is `sandbox` and a backend is supported but unavailable, the command fails before starting (no silent unsandboxed execution).

## Code references

- Mode constants: `internal/agent/permissions.go`
- `Decide` (whole function): `internal/agent/permissions.go`
- `persistPermissions`: `internal/tui/model.go`
- `SavePermissionModeSwitch`: `internal/config/ocodeconfig.go`
- `runSandboxCmd`: `internal/tui/commands.go`
- `resolveCronPermissionMode`: `internal/server/scheduler_runner.go`
- `isHarmfulForceCommand`: `internal/agent/permissions.go`
- `IsHarmfulBashCommand`: `internal/agent/permissions.go`
- `isReadOnlyGitStashForm`: `internal/agent/permissions.go`
- `isSensitivePath`: `internal/agent/permissions.go`
- `isSecretMaterialPath`: `internal/agent/permissions.go`
- `isRepoMetadataPath`: `internal/agent/permissions.go`
- `sandboxSensitiveTargets`: `internal/agent/permissions.go`
- `sandboxSensitivePath`: `internal/agent/permissions.go`

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
  sessions, snapshots, md-summaries, memory; `auth.json` and the browser login-state directory are carved out of it as non-writable, and the login-state directory is created first so the carve-out holds), language dependency caches
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
- browser login-state directory (`<GlobalDataDir>/browse/login-states`): file tools and bash lines that name it → Ask in normal and sandbox, Deny in locked, Allow in yolo. Yolo does not guard it. The Ask is human-only: no judge, no always-allow choice, nothing persisted, and Pulse's `permission_resolve` refuses to approve it. Paths built from shell variables, or read inside an interpreter, are not caught by the Ask. The OS write-wall still blocks a sandboxed write into the directory, because it is a non-writable root (see "What it confines" above).
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
- the body of a **quoted, terminated heredoc** (`<<'EOF'`, `<<"EOF"`) fed to a
  non-shell program is not parsed by the sandbox gate
  (`sandboxGateParseTarget`, `permissions_wrappers.go`): the shell never
  expands it, so markdown backticks or `$x` inside a `python3 - <<'PY'` body
  are data, not commands. The body stays gated line by line when the heredoc
  is unquoted or unterminated, when its operator may sit inside a quote,
  comment or `$((…))`, when two heredocs share a line, or when any command in
  the line is a shell or shell-like consumer (`bash`, `sh`, `cat … | sh`,
  `ssh`, `source`, `eval`). Lines after the terminator are always gated.
  Consequence: an interpreter heredoc body is not inspected in sandbox mode
  (previously a body line that happened to read `git reset …` asked).
- writes to permission-defining files (`.ocode/settings.json`,
  `.claude/settings.json`, `storage-connectors.json`, ocode config gating files) and loopback requests to
  `/api/permissions*` or to any `/api/config/*` path → Ask (self-escalation guard, all modes).
  The path is taken from the raw request token, cleaned and lowercased before matching, and
  the guard does not percent-decode it: any `%` in a loopback path asks outright. Every command
  on a compound line is checked (`permissionApiLoopback`, `internal/agent/permissions.go`),
  and a line that does not parse is checked from every word position. This is the escalation
  rule (`isPermissionEscalation`, `internal/agent/permissions.go`), and two more cases feed it.
  A network client (curl, wget, http, https) asks when a positional argument is a whole-word
  shell expansion (`curl "$U"`, `curl -s $A https://…`), even beside a literal URL, because curl
  and wget fetch every positional URL and the guard cannot see through the variable. The value
  word of a value-taking option is skipped, so `-o $OUT` is not a positional
  (`bareExpansionTarget`, `internal/agent/permissions.go`). Option arity is per client:
  the curl, wget and httpie tables (`curlValueOptions`, `permissions.go`;
  `wgetValueOptions`, `permissions.go`; `httpieValueOptions`, `permissions.go`) are
  read by `optionTakesNextWord` (`permissions.go`), and an unknown client takes no value,
  so it never skips a following expansion. A
  loopback URL whose path, host or userinfo holds a glob or expansion character (`*`, `?`, `[`,
  `{`, `$`, a backtick or a backslash) also asks. A port expansion is exempt only when the shell
  proves it numeric (`tokenMayReachPermissionRoute`, `permissions.go`, and
  `loopbackAuthoritySafe`, `permissions.go`). The loopback test behind this guard also reads
  the unspecified addresses (`0.0.0.0`, `::`, and the inet_aton zero forms), `localhost.` and any
  `*.localhost` name as loopback (`isLoopbackHostForPermissionGuard`,
  `internal/agent/permissions.go`). Four more rules apply to a network segment. (b) A segment
  with a destination-shaping option (a proxy, `--connect-to`, `--resolve`, a socks, interface, DNS
  or unix-socket option) is judged by `segmentRedirectsURLs` (`permissions.go`): every URL on
  it has its path checked for a permission or config route, or for a glob or expansion character,
  whatever its host. (c) A config or input-source option asks unconditionally: curl `-K` and
  `--config`, and wget `-i`, `--input-file`, `-e`, `--execute` and `--config`, in any spelling,
  including getopt-style abbreviations of four or more characters with the dashes
  (`configSourceFlag` and `longOptionMatch`, `internal/agent/network_guard_typesafe.go` and
  `:624`). (d) Redirect state is line-wide and order-independent. If a line contains a network client
  and anywhere on it (any segment, loop, function or group) there is a redirecting environment
  assignment (prefix or standalone), a `source`, `.` or `eval`, a state-setting builtin or declaration
  aimed at a redirect name, a `${NAME:=…}` expansion of one, or `set -a` (or `set -o allexport`),
  which makes every assignment count, the guard asks. Order does not matter, and `FOO=bar curl …`
  with an unrelated name is not affected. An `export`, `declare`, `typeset`, `local` or `readonly`
  of a redirect name asks whether or not it has a value. The check is the union `lineRedirectSignalAll`
  (`internal/agent/permissions.go`) of a parsed-command view and a raw-word view with unquoted
  operators spaced out (`spaceOutOperators`, `permissions.go`), so an unspaced `;&|(){}` cannot
  hide an assignment. It is called from `permissionApiLoopback` (`permissions.go`), and
  `lineRedirectSignal` (`permissions.go`) is the scan of each view. Assignment names are
  normalised through `assignmentBaseName` (`internal/agent/network_guard_typesafe.go`), so `+=`
  and `[subscript]=` forms resolve to their base variable; an identifier followed by `[` with any `=` in
  the word is treated as an assignment to that identifier, so quotes or expansions inside a
  subscript cannot hide it (fail closed). The names are one shared predicate, `isRedirectEnvName`
  (`internal/agent/network_guard_typesafe.go`): every `*_proxy` name in either case except
  `NO_PROXY`, plus `ALL_PROXY`, `HTTPIE_CONFIG_DIR`, `SYSTEM_WGETRC`, `WGETRC`, `CURL_HOME`,
  `CURL_CA_BUNDLE`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `REQUESTS_CA_BUNDLE`, `XDG_CONFIG_HOME` and
  `HOME`. (e) Wrappers (`env`, `sudo`,
  `command`, `exec`, `nohup`, `nice`, `timeout` and leading assignments) are peeled per segment by
  `effectiveCommandWords`, so the real client is judged. `command`, `builtin` and `exec` are
  transparent before the head word, which `effectiveHead` (`internal/agent/permissions.go`)
  returns. (f) State setters are parsed the way the shell parses them: attached and clustered options
  (`read -rp`, `printf -vX`) are read, `getopts` sets its NAME argument, any `-n` nameref counts
  because it can alias a redirecting variable, and a name that is an expansion counts because it
  cannot be resolved (`stateSetterSticky`, `internal/agent/permissions.go`).

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
