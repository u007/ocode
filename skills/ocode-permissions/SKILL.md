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

Three session-level modes, stored in `ocodeconfig.json` → `permissions.mode`:

| Mode | Behaviour |
|---|---|
| `normal` (default) | Follow tool and bash-prefix rules. Read/edit tools allowed; delete, bash, webfetch, websearch, task ask by default. |
| `yolo` | Allow all permission-gated tools without prompting. Still respects hard safety blocks and agent-mode restrictions. |
| `locked` | Read/search tools only. All write/edit/bash/network tools denied. |

Toggle via `Ctrl+O` (TUI), `/yolo [on|off|status]`, or `--yolo` CLI flag. Persists to `ocodeconfig.json`.

## 3. Permission levels

Every tool/prefix rule resolves to one of:

| Level | Meaning |
|---|---|
| `allow` | Auto-grant, no prompt |
| `ask` | Prompt user for approval |
| `deny` | Hard-block, never proceed |

## 4. Default tool rules

Hardcoded in `NewPermissionManager()` (`permissions.go:762`):

```
Always allow (no prompt):  read, glob, grep, list, lsp, skill, question,
                           todoread, todowrite, advisor, task, task_status,
                           agent_status, repo_overview, plan_enter, plan_exit,
                           wait, bash_output, kill_shell

Default allow:             write, edit, multiedit, multi_file_edit,
                           replace_lines, apply_patch, format

Default ask:               delete, bash, webfetch, websearch, repo_clone, mcp_*
```

Override per-tool in `ocodeconfig.json` → `permissions.tools`:

```json
{ "permissions": { "tools": { "bash": "allow", "delete": "deny" } } }
```

Agent definitions can override tool permissions for child sessions via `agent_permissions.go`.

## 5. Bash permission evaluation

Bash commands go through a multi-layer evaluation pipeline in `Decide()`:

### 5a. Hard blocks (always deny)

`IsHarmfulBashCommand()` (`permissions.go:721`) — these can never be auto-allowed or persisted as "always allow":

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

### 5f. Bash prefix modes

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

`isSensitivePath()` (`permissions.go:1018`) flags these for `ask`:
- Exact filenames: `.env`, `.netrc`, `.npmrc`, `.pypirc`
- `.env.*` variants
- SSH keys: `id_rsa`, `id_ed25519`, `id_ecdsa`, `id_dsa`
- Cert/key suffixes: `.pem`, `.key`, `.p12`, `.pfx`, `.secrets`
- Sensitive directories: `.git/`, `.github/workflows/`, `.aws/`

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

## 8. Permission evaluation entry point

`PermissionManager.Decide(toolName, args)` (`permissions.go:866`):

```
1. If locked mode → read-only tools allow, everything else deny
2. If bash tool:
   a. Hard-blocked? → deny
   b. YOLO mode? → allow
   c. Parse compound command → evaluate each sub-command
   d. Return first deny, or first ask, or allow
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
| `NewPermissionManager()` | `permissions.go:762` | Creates PM with defaults |
| `Decide()` | `permissions.go:866` | Main entry point for permission checks |
| `Check()` | `permissions.go:791` | Tool-level rule lookup |
| `CheckPathPatterns()` | (via patterns) | Path-glob pattern matching |
| `IsHarmfulBashCommand()` | `permissions.go:721` | Hard-block detection |
| `IsHarmfulRequest()` | `permissions.go:755` | Wraps bash check for PermissionRequest |
| `isExfiltrationRiskCommand()` | `permissions.go:689` | curl/wget/httpie/nc exfil detection |
| `isSensitivePath()` | `permissions.go:1018` | Sensitive file/dir detection |
| `isWithinWorkDir()` | `permissions.go:996` | Workdir containment check |
| `matchSubcommandAllow()` | `permissions.go:1095` | Safe subcommand matching |
| `buildPermissionManagerFromAgent()` | `agent_permissions.go:3` | Agent-definition PM builder |
| `LoadFromOcode()` | `permissions.go:826` | Load rules from config |
| `LoadFromConfig()` | `permissions.go:805` | Load rules from opencode.json format |
