---
name: ocode-permissions
description: ocode permission policies — modes, tool rules, bash-prefix rules, path scoping, auto-permission layer, hard blocks, exfiltration detection, and sensitive-path guards. Use this when working on permission logic, adding new tools, changing bash auto-allow lists, modifying hard-block rules, or debugging permission decisions.
when_to_use: When the user asks about permissions, permission modes, YOLO mode, locked mode, auto-permissions, bash prefix rules, sensitive paths, exfiltration detection, tool permission levels, or anything under internal/agent/permissions.go or internal/agent/agent_permissions.go.
---

# ocode Permissions Field Guide

A dense map of the ocode permission system — what gates tool calls, how decisions are made, and where to change behaviour.

## 1. Architecture overview

Permission evaluation lives in two files:

| File | Role |
|---|---|
| `internal/agent/permissions.go` | Core `PermissionManager` — mode, rules, patterns, bash-prefix logic, path scoping, exfiltration detection, hard blocks, the `Decide()` entry point |
| `internal/agent/agent_permissions.go` | Agent-definition bridge — `buildPermissionManagerFromAgent()` translates per-agent permission maps into a `PermissionManager` |

Config structs live in `internal/config/ocodeconfig.go` (`PermissionConfig`, `AutoPermissionConfig`, `BashPermissionConfig`).

The TUI `/permissions` command and `/yolo` toggle are in `internal/tui/model.go` (slash-command handlers). The web UI has `POST /api/permissions` for SSE-driven permission dialogs.

## 2. Permission modes

Four session-level modes, stored in `ocodeconfig.json` → `permissions.mode`:

| Mode | Behaviour |
|---|---|
| `normal` (default) | Follow tool and bash-prefix rules. Read/edit tools allowed; delete, bash, webfetch, websearch, task ask by default. |
| `yolo` | Allow all permission-gated tools without prompting. Still respects hard safety blocks and agent-mode restrictions. |
| `locked` | Read/search tools only. All write/edit/bash/network tools denied. |
| `sandbox` | Bash runs without prompts, but the OS confines **writes** to the classified allowed roots (workspace/extra paths, opencode data dir, language caches, `~/.claude`, temp dirs). Write-integrity only — reads, exec, and network egress stay open. Secrets (`auth.json`, `~/.ssh`, `.env`, keys/certs) and ocode-config writes still **ask** (routed to the auto-judge when `auto` is on). Repo-metadata dirs (`.git/`, `.github/workflows/`) ask on **write only** — listing/reading them (`ls .git/`, `cat .git/config`) auto-allows, matching normal mode. Fail-closed: with no OS backend (Windows) it degrades to normal prompting. |

Toggle via `Ctrl+O` / `/yolo [on|off|status]` (yolo), `/sandbox [on|off|status]` (sandbox), the TUI permission-mode cycle, or the web mode selector. All four persist to `ocodeconfig.json` — `SavePermissionModeSwitch()` writes the mode verbatim, sandbox included (there is no longer a sandbox→normal clamp on the persist path).

## 3. Permission levels

Every tool/prefix rule resolves to one of:

| Level | Meaning |
|---|---|
| `allow` | Auto-grant, no prompt |
| `ask` | Prompt user for approval |
| `deny` | Hard-block, never proceed |

## 4. Default tool rules

Hardcoded in `NewPermissionManager()` (`permissions.go:1281`):

```
Always allow (no prompt):  read, glob, grep, rgrep, list, lsp, lsp_diagnostics,
                           skill, load_skill, question, todoread, todowrite,
                           todo_update, advisor, task, task_status, agent_status,
                           repo_overview, plan_enter, plan_exit, wait,
                           bash_output, kill_shell, list_processes, ocr, cron

Default allow:             write, edit, multiedit, multi_file_edit,
                           replace_lines, apply_patch, format, imagegen

Default ask:               delete, bash, webfetch, websearch, repo_clone, mcp_*, computer
```

Override per-tool in `ocodeconfig.json` → `permissions.tools`:

```json
{ "permissions": { "tools": { "bash": "allow", "delete": "deny" } } }
```

Agent definitions can override tool permissions for child sessions via `agent_permissions.go`.

## 5. Bash permission evaluation

Bash commands go through a multi-layer evaluation pipeline in `Decide()`:

### 5a. Hard blocks (always deny)

`IsHarmfulBashCommand()` (`permissions.go:1175`) — these can never be auto-allowed or persisted as "always allow":

**Git destructive prefixes** (any args):
- `git revert`, `git stash`, `git reset`, `git clean`, `git checkout`, `git restore`, `git switch`

**Git force-flagged commands:**
- `git push --force` / `git push -f`
- `git pull --force` / `git pull -f`

**Data exfiltration risk** (curl, wget, httpie, netcat):
- `curl` with `-d`, `--data`, `--data-binary`, `--data-raw`, `--data-urlencode`, `-F`, `--form`, `--upload-file`, `-T` + `@file` or env var refs
- `curl` with `-H`/`--header` containing `$ENV_VAR`
- `curl` with `--config`/`--proxy`/`--socks5`/`--socks4` + `@file` or env var
- `curl` URL containing `$ENV_VAR`
- Any curl/wget/httpie command with `$(...)` or `` `...` `` subshell expansion
- `wget` with `--post-file`, `--post-data`, `--body-data`, `--body-file`, `-i`
- `httpie`/`http`/`https` with `file@` pattern, env var headers, or `--auth`/`-a` + env var
- `nc`/`ncat` with host+port (no `-z` scan flag) or stdin redirect `< file`

**Sandbox extension (HEAD `760b2037`):** in sandbox mode `Decide()` also returns `ask` for the force-flagged git push/pull forms (`isHarmfulForceCommand()`) and for any `IsHarmfulBashCommand()` match. The OS write-wall confines *file* writes but is blind to a repo mutation that stays inside the workdir (history rewrite, branch switch, stash create/drop, untracked removal), so those do not ride the sandbox auto-allow. Read-only `git stash list`/`show` are excluded from the harmful set and still auto-allow. Normal mode is unchanged; YOLO remains the promptless escape hatch.

**Sandbox sensitive split (read vs write):** the sandbox sensitive gate distinguishes *secret material* from *repo metadata*. `.env`/keys/certs/`.aws/` and `~/.ssh`/`auth.json` ask on read or write; `.git/` and `.github/workflows/` ask only on write (a planted hook/workflow is the threat), so `ls .git/`, `cat .git/config`, and `ls .github/workflows/` auto-allow like any in-workdir read. The per-target write map from `sandboxSensitiveTargets` is fail-closed — an unrecognized command (e.g. `truncate`, `chmod`, a custom script) marks its path args as writes, and a parse failure marks every target as a write. `git ls-files` and `git status` were already in `bashSubcommandAllow` and are unaffected.

### 5b. YOLO mode shortcut

If mode is `yolo` and the command is not hard-blocked → auto-allow.

### 5c. Compound command parsing

Bash commands are parsed into constituent sub-commands (respecting pipes, semicolons, `&&`, `||`, subshells). Each sub-command is evaluated independently. If any returns `deny`, the whole command is denied. If any returns `ask`, the first `ask` is returned.

### 5d. Prefix-level rules

Each sub-command is checked against:
1. `bashSubcommandAllow` — built-in safe subcommands (git read-only, gh read-only, go/cargo/npm build+test, docker read-only, make, etc.)
2. `bashAutoAllowPrefixes` — built-in safe single-word prefixes (cat, grep, ls, jq, diff, etc.)
3. User-configured `permissions.bash.prefixes` in `ocodeconfig.json`
4. User-configured `permissions.bash.auto_allow_prefixes` (extends the built-in set)

### 5e. Path-scoping for auto-allow

For auto-allowed bash prefixes, all detected path arguments must resolve inside the current working directory. If any path escapes the workdir, the command falls back to `ask`.

`find` and `fd` have additional unsafe-flag checks (`-exec`, `-execdir`, `-delete`, `-x`, etc.) that force `ask`.

### 5f. Dependency-local binaries (project bin dirs)

Dependency-local interpreters (`node_modules/.bin/tsc`, `.venv/bin/pytest`, `./bin/foo`, `~/go/bin/...`, `./gradlew`, `bundle exec`, …) get **two layers with different trust**:

- **Code level (`matchSubcommandAllow` → `nodeModulesBinTool` + `runnerSafeTools`)**: only project-local `node_modules/.bin` shims whose basename is in the inert safe set (`tsc`, `tsgo`, `eslint`, `prettier`, `biome`, `vitest`, `jest`, `stylelint`) auto-allow, and only after canonical containment — the shim's resolved path must be under the session workDir (symlinked `node_modules` escaping the worktree fails closed; empty workDir fails closed). Everything else — an unknown bin, a familiar-looking name not in the safe set, `.venv/bin/*`, `./bin/*` — is **not** code-allowed and falls to the judge. This is deliberately fail-closed: a bin's path or basename proves nothing about what the script executes.
- **LLM judge (bundled addendum)**: the "judge the ACTION" paragraph tells the judge to allow a dependency-bin invocation when the action is permissible (build/test/lint/format/codegen/local file processing) regardless of the binary's directory, deny when the action itself is not, and **require human approval when the effects cannot be established from argv and the tool's known semantics** — opaque flags, an unreadable script, or an unfamiliar tool never get path-based safety. Package-manager subcommands (`npm/pnpm/bun install/run/exec/dlx`) never fall under this: they follow the package-manager rules (exec/run always human-approved).

Pinned by `TestMatchSubcommandAllow_DependencyBinAction` (code level) and `TestBundledAutoPermissionPrompt_CoversLanguageToolBins` / `TestBundledAutoPermissionPrompt_GitConfigBoundary` (judge level) — keep the two layers in sync when editing either.

### 5g. Bash prefix modes

Configured in `permissions.bash.prefix_modes`:

| Mode | Behaviour |
|---|---|
| `read_only` (default) | Auto-allow in-root calls; persist a project-scoped in-root rule |
| `mutating` | Auto-allow in-root calls once; do NOT persist |
| `never_auto` | Disable auto-allow for that prefix entirely |

## 6. Path-based permissions

### 6a. Out-of-scope paths

Any absolute path outside the working directory → `ask` (unless the tool has an explicit `allow` rule).

### 6b. Sensitive paths

`isSensitivePath()` (`permissions.go:2789`) flags these for `ask`, and is the
OR of two narrower predicates used to split read from write in sandbox:
- **`isSecretMaterialPath()`** — secret on READ or write:
  - Exact filenames: `.env`, `.netrc`, `.npmrc`, `.pypirc`
  - `.env.*` variants (`.env.example`/`.sample`/`.template`/`.dist` excluded)
  - SSH keys: `id_rsa`, `id_ed25519`, `id_ecdsa`, `id_dsa`
  - Cert/key suffixes: `.pem`, `.key`, `.p12`, `.pfx`, `.secrets`
  - Sensitive directory: `.aws/`
- **`isRepoMetadataPath()`** — write-only in sandbox, allow on read:
  - Sensitive directories: `.git/`, `.github/workflows/`

`sandboxSensitivePath()` reuses these: secret material is Ask for reads and
writes, repo metadata is Ask only when the command writes/deletes the target
(`sandboxSensitiveTargets` returns a per-target write map). `git ls-files`
already auto-allows via `bashSubcommandAllow`; repo-metadata **reads**
(`ls .git/`, `cat .git/config`) now auto-allow too, instead of asking because
`isSensitivePath` was blanket-reused for the `.env` case alone.

### 6c. Path-glob patterns

Tool rules can include path-glob patterns for fine-grained control:

```json
{ "permissions": { "tools": { "read": { "**/*.go": "allow", "secrets/**": "deny" } } } }
```

Pattern matching supports `**` (recursive), `*` (single segment), `?`, and character classes.

### 6d. Webfetch domain tracking

First webfetch to a domain prompts `ask`. Once approved/denied, the decision is cached for the session.

## 7. Auto-permission layer

An optional LLM-based layer that auto-approves/denies permission prompts without user interaction.

### Configuration

```json
{
  "permissions": {
    "auto": {
      "enabled": true,
      "model": "deepseek:deepseek-v4-flash",
      "allow_destructive": false,
      "prompt": "Custom system prompt for the auto-permission model",
      "max_context_bytes": 4096,
      "max_context_sources": 2,
      "max_context_lines_per_source": 80,
      "grants": []
    }
  }
}
```

### Key constraints

- The auto-permission model can only emit `allow` or `ask` — it **cannot** emit `deny` or widen scope.
- Hard blocks (`IsHarmfulBashCommand`) are **deterministic and final** — the auto layer cannot override them.
- The auto layer cannot escalate the permission mode or widen past static guardrails.
- `allow_destructive: false` instructs the model to conservatively deny operations it cannot confidently approve.

### AutoGrant persistence

When the auto-permission model approves a request, Go derives a typed `AutoGrant` entry before persisting. Grants are narrow and durable:
- `kind`: `"tool"` or `"bash_prefix"` or `"webfetch_domain"`
- Tool-specific fields: `tool`, `normalized_args`, `normalized_command`, `destructive`, `domain`

### Bundled prompt addendum (`/permissions auto prompt`)

A versioned, installable system-prompt addendum is prepended to the LLM gatekeeper prompt before the user's `permissions.auto.prompt` override. The installed file lives at `~/.config/opencode/auto-permission-prompt.md` (with a `.bundled-hash` sidecar recording the installed version), and ships a default body of known-safe git commands that should always be allowed without further reasoning — read-only inspection forms of `git status`/`diff`/`log`/`show`/`blame`/`ls-files`/`branch` (listing/query only)/`remote -v`/`stash list`/`stash show`/`tag` (listing only)/`worktree list`/`rev-parse`/`describe`/`submodule status`/`config` reads, plus loopback-only curl/wget. mutating git forms (`branch -D`, `tag -d`, `config` writes — including the bare positional `git config <key> <value>` — `worktree remove`, `notes add`) are explicitly carved out as requiring human approval.

Manage it with `/permissions auto prompt <status|install|upgrade> [force]`:
- `status` → `missing` / `up-to-date` / `outdated` / `custom-modified` / `newer`
- `install` → writes the bundled body; upgrades an untouched `outdated` copy automatically
- `upgrade` → same as `install`; `force` overwrites a `custom-modified` or `newer` file (backup `.bak.<ts>` copied first — the live file is never renamed away — then atomic temp+rename write)

No self-heal (since 1.9.1): `LoadAutoPermissionPromptBodyWithStatus()` never writes to disk. A `missing` file serves the embedded bundled body; an `up-to-date`, `custom-modified`, or `newer` file is used **verbatim**; an `outdated` copy (sidecar hash proves the user never touched it) is **superseded** — the load path returns the current embedded rules instead, so the gatekeeper never runs a stale rulebook while the stale copy stays on disk untouched until an explicit `install|upgrade`. The migration surface is an advisory note appended to the gatekeeper prompt (`config.AutoPermissionPromptAdvisory`): an `outdated` copy is annotated "predates bundled vX and is not being served; current bundled rules apply — refresh with `/permissions auto prompt upgrade`", a `custom-modified` copy gets a fall-back-to-shipped-defaults note with the `install force` remediation, and a `newer` copy gets a do-not-widen-allows note. `/permissions auto prompt status` states which rulebook is being served and the remediation — check it when diagnosing judges that ignore rules you know shipped.

The `.bundled-hash` sidecar holds `<hash>\n<version>`; a sidecar version above the running build's `BundledAutoPermissionPromptVersion` reports `newer` and is never downgraded, so two ocode builds sharing `~/.config/opencode` stop reinstalling over each other. `InstallAutoPermissionPrompt` is the only writer: it serializes decide→backup→body-write→sidecar-write under an advisory file lock (`<file>.lock`, `internal/filelock.WithFileLock`) with the status check inside the lock, and replaces both the body and the sidecar atomically — without that, concurrent installs interleave writes and the last sidecar can describe a body the file no longer holds. The bash prompt also lists temp-dir aliases (`/tmp -> /private/tmp`, `$TMPDIR`) next to the resolved roots, and states that executing binaries outside the roots (`/bin`, `sandbox-exec`, …) is not a path violation.

Implementation: `internal/config/auto_permission_prompt.go` (versioned body, status detection, install/upgrade writers), `internal/agent/agent.go` (`autoPermissionAddendum` composes installed-or-embedded body + status advisory; consumed by `askPermissionModel`), `internal/tui/commands.go` (`runAutoPermissionPromptCmd`). The addendum is **separate** from `permissions.auto.prompt` in `ocodeconfig.json` — that field is the user's own free-form override and is never silently clobbered by a bundled update. When editing the prompt body, bump `BundledAutoPermissionPromptVersion` so installed copies are detected as stale.

## 8. Permission evaluation entry point

`PermissionManager.Decide(toolName, args)` (`permissions.go:1399`):

```
1. If locked mode → read-only tools allow, everything else deny
2. If bash tool:
   a. Hard-blocked? → deny
   b. YOLO mode? → allow
   c. Sandbox mode → harmful force/git forms + sensitive paths ask; else allow
      (only when the OS backend is present; otherwise falls through to ask)
   d. Parse compound command → evaluate each sub-command
   e. Return first deny, or first ask, or allow
3. If YOLO mode → allow
4. If path-scoped tool:
   a. Check path-glob patterns
   b. Out-of-scope path? → ask (unless explicit allow rule)
   c. Sensitive path? → ask (unless explicit allow rule)
   d. Delete tool? → ask (unless explicit allow rule)
5. If webfetch → check domain cache
6. Check tool-level rule → return ask if unset
```

## 9. Configuration file location

Permissions live in `ocodeconfig.json` (ocode-only overrides), loaded from:
1. Global: `~/.config/ocode/ocodeconfig.json`
2. Project: `.ocode/ocodeconfig.json` (project root)

Project config overrides global. The `opencode.json` `permission` field is a separate, simpler format.

## 10. TUI commands

| Command | Action |
|---|---|
| `/permissions` | View current permission rules |
| `/permissions bash:git allow` | Set a bash prefix rule |
| `/permissions bash:rm deny` | Deny a bash prefix |
| `/yolo` or `/yolo status` | Show YOLO mode status |
| `/yolo on` | Enable YOLO mode |
| `/yolo off` | Disable YOLO mode |
| `Ctrl+O` | Toggle YOLO mode |
| `/sandbox [on\|off\|status]` | Enter/leave sandbox mode (bare `/sandbox` toggles) |
| `/plugin enable\|disable <name>` | Toggle opt-in tools |

## 11. Agent-level permissions

Agent definitions can include a `permissions` map that overrides tool rules for child sessions. Built via `buildPermissionManagerFromAgent()` (`agent_permissions.go`):

```yaml
agents:
  my-agent:
    permissions:
      read: allow
      bash: deny
      edit:
        "src/**": allow
        "secrets/**": deny
```

Supported groups: `read`, `edit`, `glob`, `grep`, `bash`, `task`, `webfetch`, `websearch`, `skill`, `question`, `lsp`.

Unknown groups produce a diagnostic warning. Non-shorthand (object-valued) permissions are loaded as path patterns.

## 12. Adding a new tool to the permission system

1. Add the tool name to `NewPermissionManager()` with the appropriate default level.
2. If the tool is path-scoped, add it to `pathScopedTools`.
3. If the tool is read-only, add it to `isReadOnlyTool()`.
4. Add it to the `groupToolMap` in `agent_permissions.go` if it should be overridable by agent definitions.
5. If the tool has bash-like subcommands, consider adding safe subcommands to `bashSubcommandAllow`.

## 13. Key functions reference

| Function | File:Line | Purpose |
|---|---|---|
| `NewPermissionManager()` | `permissions.go:1281` | Creates PM with defaults |
| `Decide()` | `permissions.go:1399` | Main entry point for permission checks |
| `Check()` | `permissions.go:1322` | Tool-level rule lookup |
| `CheckPathPatterns()` | (via patterns) | Path-glob pattern matching |
| `IsHarmfulBashCommand()` | `permissions.go:1175` | Hard-block detection |
| `IsHarmfulRequest()` | `permissions.go:1235` | Wraps bash check for PermissionRequest |
| `isExfiltrationRiskCommand()` | `permissions.go:1128` | curl/wget/httpie/nc exfil detection |
| `isSensitivePath()` | `permissions.go:2677` | Sensitive file/dir detection |
| `isWithinWorkDir()` | `permissions.go:1783` | Workdir containment check |
| `matchSubcommandAllow()` | `permissions.go:3095` | Safe subcommand matching |
| `buildPermissionManagerFromAgent()` | `agent_permissions.go:3` | Agent-definition PM builder |
| `LoadFromOcode()` | `permissions.go:1357` | Load rules from config |
| `LoadFromConfig()` | `permissions.go:1336` | Load rules from opencode.json format |
