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

Toggle via `Ctrl+O` / `/yolo [on|off|status]` (yolo), `/sandbox [on|off|status]` (sandbox), the TUI permission-mode cycle, or the web sidebar permission pill / `/sandbox` command. The **persisted default** (the mode a brand-new session starts in) lives in `ocodeconfig.json` → `permissions.mode` — `SavePermissionModeSwitch()` writes it verbatim, sandbox included (there is no longer a sandbox→normal clamp on the persist path).

On the web/desktop **server** the *live* mode is **per chat session**, not per process: `PUT /api/permissions/mode` and `PUT /api/permissions/yolo` require a `session_id` (body or `?session_id=`) and apply only to that session's agent; `GET /api/permissions` / `yolo` accept the same query param, and a session-less write is `400`. The override is stored in the session transcript's metadata key `permission_mode` (via `session.UpdateMetadataForDir`, mirroring the per-session `model` override) so it survives agent eviction, resume, and server restart. `buildAgentSession` / `registerAgentSession` reapply only that session's own value, and every per-session status snapshot stamps `permission_mode` via `applySessionPermissionFields` — so toggling one chat never changes another chat or project. A draft (`new-*`) tab holds its pick locally and sends it as `permission_mode` on the first `POST /api/chat` (which persists it at creation). The TUI is single-session and unaffected; the Settings→Permissions form owns only the persisted default.

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

The tool rule is authoritative for **path-scoped** tools too (`read`, `write`,
`edit`, `delete`, `multiedit`, `multi_file_edit`, `replace_lines`, `apply_patch`,
`format`, `lsp`, …): `deny` is a `HardDeny` the auto-permission judge cannot
override; `ask` prompts (then routes to the judge when auto is on); `allow`
auto-grants an in-scope, non-sensitive target. This is what makes the dialog's
"always allow this tool" durable — it persists `permissions.tools.<tool> =
"allow"` (`config.SaveSingleToolRule`) and a new process reloads it through
`LoadFromOcode`. Because `delete`'s default is `ask`, any non-`ask` delete rule
is necessarily user-set, so no provenance flag is needed.

A **plain** `allow` (the built-in `write`/`edit` defaults, or a config
`delete: "allow"`) still does NOT bypass the out-of-scope or sensitive-path
gates — only a **user-confirmed** allow (the in-session dialog choice,
`SetUserConfirmedRule`) does. Sandbox mode auto-allows the remaining in-workdir
asks (its OS write-wall confines the effect); an explicit `delete: "ask"` is
indistinguishable from the default, so sandbox auto-allows that too.

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
- `wget` with `--post-file`, `--body-file`, `-i` (file reads), or `--post-data`/`--body-data` containing `$ENV_VAR` — inline `--post-data`/`--body-data` is judgeable (mirrors curl `-d`)
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

### 5h. Claude Code settings (`.claude/settings.json`)

`LoadClaudePermissions` (`claude_settings.go`) merges global `~/.claude/settings.json` with project `.claude/settings.json` + `.claude/settings.local.json`; only `permissions.allow/deny/ask` entries for `Bash(...)` are honored. **Deny wins over ask over allow**, and a deny is a `HardDeny`: it is checked per sub-command at the top of `Decide` (before dangerous-rm/harmful, YOLO, and sandbox short-circuits), so no allow rule, auto-allow, or auto-permission judge can override it.

Pattern semantics: `*` matches any character sequence (including empty), so `Bash(git stash *)` matches **every** `git stash …` form — including the read-only `git stash list`/`show` that ocode's own `/ban add git stash` and `IsHarmfulBashCommand` deliberately carve out. A bare `Bash` (no pattern) denies every bash command. To ban only the mutating stash family the way ocode's carve-out does, use granular patterns (`Bash(git stash)`, `Bash(git stash -*)`, `Bash(git stash pop*)`, …) plus `allow` for `Bash(git stash list*)` / `Bash(git stash show*)`.

`PermissionDecision.DenyReason` is populated by `Decide` on every static-deny path (Claude deny pattern, user bash ban prefix, locked mode, tool/path/webfetch rules, hard blocks) and rendered into the tool-error text by `denyToolMessage` (`agent.go`), so a blocked call names the offending rule instead of a generic "permission rules" message. Remote SSH projects run the agent on the host, so **the host's** `.claude/settings.json` is the one that applies — a host carrying an over-broad `Bash(git stash *)` deny blocks read-only stash inspection even when the local machine's file allows it.

## 6. Path-based permissions

### 6a. Out-of-scope paths

Any absolute path outside the working directory → `ask`. Temp dirs, immutable
read roots, and a **user-confirmed** allow (`SetUserConfirmedRule`) are the only
carve-outs; a plain `allow` rule (including a config `delete: "allow"`) does not
bypass this gate.

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

### 6e. Read-target existence gate (Unicode-space recovery)

Before locked-mode/YOLO/path-rule evaluation, `Decide` checks whether a `read`
target exists on disk (`targetExists`, `permissions.go:1501`); a genuinely
missing target is an immediate `HardDeny` (this is the only filesystem
existence check permitted in the permission system). Two refinements:

- **Unicode-space recovery.** macOS writes screenshot filenames with U+202F
  (NARROW NO-BREAK SPACE) before AM/PM and models routinely re-emit the path
  with a plain ASCII space, so a visually identical path failed to stat. The
  gate now calls `tool.ResolveReadTarget` (`internal/tool/readpath.go`) and
  allows when exactly one sibling matches after
  `NormalizeUnicodeSpaces` (U+00A0, U+2007, U+2009, U+202F, U+FEFF → ASCII
  space). Case is preserved — a case-only mismatch is **not** auto-resolved,
  only hinted. `ReadTool.Execute`/`ExecuteImage` resolve through the same
  helper (`confinedReadPath`), re-confining the recovered sibling, so an
  allowed call actually reads the file. Recovery is read-only: write/edit keep
  the literal path so a create-new-file flow can never clobber a
  differently-spaced existing file.
- **Obvious misses.** A real miss now reports
  `read target does not exist: <path> — resolved to <abs>; similar names in
  <dir>: "…"` (or `the parent directory does not exist`), with non-ASCII
  spaces escaped (`\u202f`) so an invisible character is visible.

Pinned by `internal/tool/readpath_test.go`,
`internal/tool/read_unicode_space_test.go`, and
`internal/agent/permissions_read_target_test.go` (mutation-verified: disabling
recovery fails the read, vision, and permission recovery tests).

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
      "grants": [],
      "relaxed_concerns": []
    }
  }
}
```

### Live application (no restart)

The whole `permissions.auto` block is process-wide policy. `PUT /api/config/ocode/permissions-auto`
saves it and **pushes it to every live agent** (`HandleSetAutoPermissionConfig` →
`PermissionManager.SetAutoPermissionConfig`), so unchecking a concern — or changing the judge
model, prompt, budgets, or `enabled` — takes effect on the running chat's next judge call, with
no agent rebuild or app restart.

The live config sits behind atomics (`autoConfig atomic.Pointer[config.AutoPermissionConfig]`,
`autoPermissionEnabled atomic.Bool`) because a turn's judge may be reading it while the Settings
save lands. Read it through `Agent.autoPermissionConfig()` /
`PermissionManager.AutoPermissionConfig()`; never read `a.config.Ocode.Permissions.Auto` at
runtime — that is only the build-time seed (and is a *separate* object for profile-bound
sessions, which is why the push targets the manager, not `a.config`).

### Relaxed concerns (`relaxed_concerns`)

`permissions.auto.relaxed_concerns` is the **negative** enforcement set rendered as the
Settings → Permissions "Categories the judge must enforce" checkboxes: a category listed here
has its enforcement switched OFF, so the judge may auto-approve a call whose ONLY concern is
that category. Empty by default (= everything enforced), and a category added later defaults to
enforced. Go's deterministic guards (hard blocks, dangerous rm, out-of-scope paths) always
apply, so relaxing a category can never auto-grant those. Catalog: `agent.RelaxableConcerns()`.

### Key constraints

- The auto-permission model can only emit `allow` or `ask` — it **cannot** emit `deny` or widen scope.
- Hard blocks (`IsHarmfulBashCommand`) are **deterministic and final** — the auto layer cannot override them.
- The auto layer cannot escalate the permission mode or widen past static guardrails.
- `allow_destructive: false` instructs the model to conservatively deny operations it cannot confidently approve.

### Judge backends (chat vs TypeSafe/Jev)

Two distinct code paths implement the auto-permission judge, and they do **not** share a rulebook:

| Judge model | Path | Rulebook |
|---|---|---|
| chat model (`deepseek:...`, any OpenAI-compatible) | `askPermissionModel` (`agent.go`) | `BundledAutoPermissionPromptBody` / installed `auto-permission-prompt.md` + `permissions.auto.prompt` + `auto-permission-prompt.local.md` |
| `typesafe/<model>` (Jev) | `askPermissionModelTypesafe` (`permission_typesafe.go`) | `typesafeJudgeInstructions` (typed choice: allow/deny + `typesafeConcerns`) |

`consultPermissionModel` routes on `isTypesafeModel(modelName)`. Jev is decision-only (no chat loop, no `read_file`): the request travels as structured `state` (tool, arguments, allowed roots, banned prefixes, interpreter source) and the verdict is a typed `choice` with a confidence floor (`permissions.auto.min_confidence`, default 0.85). Consequences:
- Editing the bundled prompt body does **not** change Jev's behaviour — update `typesafeJudgeInstructions` (and the `concern` labels) instead, and vice versa.
- The confidence floor means Jev must reach ≥ `min_confidence` on the `allow` choice to auto-grant; a request it merely leans-allowed on falls through to the human. Rules that remove hesitation (explicit "this is ordinary and allowed") raise that confidence. An **opaque** request — one whose effects Jev could not establish, so its `concern` answer is `truncated_or_unknown` (an undefined-variable command head like `$g --version`, an unreadable script, a flag whose effect is unknown) — clears a lower floor instead: `autoJudgeOpaqueMinConfidenceDefault` (0.75). That 0.75 is only a DEFAULT: an explicitly configured `min_confidence` (higher or lower) governs opaque requests too, so the relaxation never tightens or loosens the user's own bar. Every other concern (`none`, `secrets`, `network`, …) keeps the normal floor.
- **Reading a credential file is not, by itself, a deny reason.** The `secrets` concern is about *exposure* — the value printed to the command's output, written/redirected to a file, or sent off-host in a URL/header/body/upload. A local read whose value is consumed as an argument (`DBURL=$(grep '^DATABASE_URL=' .env | cut -d= -f2-) && psql "$DBURL" -c "\dt"`) stays on-host and must ALLOW. Without this carve-out Jev leaned allow at ~0.5 on that shape, fell below the 0.85 floor, and ordinary DB tooling surfaced as a spurious "Auto-denied by LLM permission model" banner (the human was still prompted). Pinned by `internal/agent/permission_typesafe_rubric_test.go`; the static sandbox gate still Asks on a `.env` read, so the carve-out changes only whether the judge auto-approves it.
- **Credential material is withheld from Jev's context.** `buildPermissionContext` never embeds sensitive file contents — a sensitive target file gets a `(contents withheld: sensitive file)` marker, while executed custom scripts and referenced files matching the sensitive predicate are skipped entirely. See `docs/gotchas/auto-permission-judge-withholds-credentials.md`.

### AutoGrant persistence

When the auto-permission model approves a request, Go derives a typed `AutoGrant` entry before persisting. Grants are narrow and durable:
- `kind`: `"tool"` or `"bash_prefix"` or `"webfetch_domain"`
- Tool-specific fields: `tool`, `normalized_args`, `normalized_command`, `destructive`, `domain`

### Bundled prompt addendum (`/permissions auto prompt`)

A versioned, installable system-prompt addendum is prepended to the LLM gatekeeper prompt before the user's `permissions.auto.prompt` override. The installed file lives at `~/.config/opencode/auto-permission-prompt.md` (with a `.bundled-hash` sidecar recording the installed version), and ships a default body of known-safe git commands that should always be allowed without further reasoning — read-only inspection forms of `git status`/`diff`/`log`/`show`/`blame`/`ls-files`/`branch` (listing/query only)/`remote -v`/`stash list`/`stash show`/`tag` (listing only)/`worktree list`/`rev-parse`/`describe`/`submodule status`/`config` reads, plus loopback-only curl/wget and non-secret remote HTTP requests (no credential in URL/query/header/body; hard blocks still win). mutating git forms (`branch -D`, `tag -d`, `config` writes — including the bare positional `git config <key> <value>` — `worktree remove`, `notes add`) are explicitly carved out as requiring human approval.

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
   a. Hard-blocked (pipe-to-shell, rm -rf /, sudo chains)? → deny
   b. Claude Code settings deny matches any sub-command? → deny (hard)
   c. YOLO mode? → allow
   d. Sandbox mode → harmful force/git forms + sensitive paths ask; else allow
      (only when the OS backend is present; otherwise falls through to ask)
   e. Parse compound command → evaluate each sub-command
   f. Return first deny, or first ask, or allow
3. If YOLO mode → allow
4. If path-scoped tool:
   a. Check path-glob patterns
   b. Out-of-scope path? → ask (unless explicit allow rule)
   c. Sensitive path? → ask (unless explicit allow rule)
   d. Delete tool? → ask (unless explicit allow rule)
5. If webfetch → check domain cache
6. Check tool-level rule → return ask if unset
```

Deny decisions carry `PermissionDecision.DenyReason` (the matched rule/gate), surfaced in the tool error by `denyToolMessage` (`agent.go`).

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
| `claudeDenyRule()` | `claude_settings.go` | Matched Claude `.claude/settings.json` deny pattern for a command |
| `formatClaudeDenyReason()` | `claude_settings.go` | Renders that pattern for `PermissionDecision.DenyReason` |
| `denyToolMessage()` | `agent.go` | Renders a static Deny into the tool error, including `DenyReason` |
