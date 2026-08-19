---
name: ocode-usage
description: Comprehensive guide on how to use ocode — the AI coding agent. Covers installation, configuration, TUI mode, headless run mode, web server, desktop shell, MCP servers, models, skills, sub-agent tasks, and common workflows. Use this when users ask "how do I use ocode", "getting started with ocode", "ocode tutorial", or need a reference for any ocode feature.
when_to_use: When the user asks for help using ocode, wants a tutorial, needs to understand available commands, tools, or features (including desktop, sessions, tasks, todo plans, scheduled jobs), or asks "how do I..." questions about ocode features. Also triggered by: "ocode tutorial", "getting started", "how to use", "ocode guide", "ocode help".
---

# ocode Usage Guide

A complete reference for using ocode — the AI coding agent that lives in your terminal.

---

## 1. Quick Start

### Installation

```bash
# From source
git clone https://github.com/u007/ocode
cd ocode
go build -o ocode .

# Or install to PATH
go install github.com/u007/ocode@latest
```

### First Run

```bash
# Start interactive TUI (default)
ocode

# Run a one-off prompt (headless)
ocode run "Write a hello world in Go"

# Start web server + open browser
ocode web
```

---

## 2. Configuration

### Config File Location

Configuration is split across two files:

| File | Location | Role |
|------|----------|------|
| **`opencode.json`** | Project root or `~/.config/opencode/` | Upstream-compatible settings (provider creds, model prefs). **Read-only** — ocode never writes to it. |
| **`ocodeconfig.json`** | `~/.config/opencode/` (global only) | ocode-only state (permissions, editor, compaction, model history). Written by ocode. |

### Minimal Config

```json
{
  "model": "gpt-4o",
  "apiKeys": {
    "openai": "sk-..."
  }
}
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENCODE_MODEL` | Default model (e.g., `gpt-4o`, `claude-3-5-sonnet`) |
| `OPENCODE_AUTH_TOKEN` | Single token for all providers (bypasses per-provider config) |
| `OPENCODE_SERVER_USERNAME` | Basic auth for web server |
| `OPENCODE_SERVER_PASSWORD` | Basic auth for web server |
| `NO_COLOR` | Disable colored output |

### Provider API Keys

Supported providers: **OpenAI**, **Anthropic**, **Google (Gemini)**, **Z.AI**, **Alibaba (Qwen)**, **GitHub Copilot**, **DeepSeek (opencode-go)**, **Minimax**, **Grok**, **Cloudflare Gateway**

Configure via `apiKeys` in config or provider-specific env vars:
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `GOOGLE_API_KEY`
- `ZHIPUAI_API_KEY` (Z.AI)
- `DASHSCOPE_API_KEY` (Alibaba)
- `GITHUB_COPILOT_TOKEN`
- `OPENCODE_API_KEY` (opencode-go / DeepSeek)

**Global override:** Set `OPENCODE_AUTH_TOKEN` to use a single token for all providers, bypassing per-provider configuration. Useful for CI/CD or proxy setups.

### Per-Project Config & the Terminal

- Config is resolved **per project**: the server/desktop shell anchors a working directory on boot and reloads that project's `opencode.json` (`config.SetWorkDir`), so provider overrides/API keys come from the served project — never assume `os.Getwd()` (a Finder-launched `.app` starts at `/`).
- **Interactive terminal (always enabled)**: the pty-backed terminal — `GET /api/terminal/ws` (WebSocket) and the **Terminal** sub-tab in the web UI — is unconditionally on; there is no `terminal_enabled` config and no way to disable it. The server serves one fixed project workdir; the UI only exposes the terminal when the selected project matches that workdir. Because the terminal hands the browser a real shell, an unauthenticated server may expose it only on a loopback bind; configure server credentials before binding it to a non-loopback address. `terminal_scrollback_lines` defaults to **9999** and is clamped to 100–100000.
- **Bundled skills go stale**: the bundled skills (`ocode-tui`, `ocode-desktop`, `ocode-usage`, …) are snapshotted into the binary at build time. After upgrading ocode, run `ocode skills upgrade` to refresh your installed copies; until then the docs may describe features your binary lacks.

---

## 3. Modes of Operation

### 3.1 Interactive TUI (Default)

```bash
ocode                    # Start fresh session
ocode -continue          # Resume last session
ocode -session <id>      # Resume specific session
ocode -yolo              # Auto-approve all (last resort — prefer the auto-permission layer)
ocode --permission-mode off  # Disable permissions entirely
ocode -effort high       # Set reasoning effort level (off|low|med|high|xhigh|max; persists like /effort)
```

**TUI Navigation:**
- `Tab` / `Shift+Tab` — Switch tabs (chat, agents, files, changes, git, log)
- `Shift+Tab` (while agent running) — Toggle agent strip focus (cycle through running agents)
- `Ctrl+P` — Search and open files (command palette)
- `Ctrl+X` — Leader key (then `h` help, `u` undo, `r` redo, `n` new, `l` list, `c` compact, `t` thinking level)
- `Ctrl+D` — Cycle thinking effort level (off → low → med → high)
- `Ctrl+B` — Move running bash command to background
- `Ctrl+G` — Open process list
- `Ctrl+O` — Toggle YOLO permissions mode
- `Ctrl+Y` — Retry last LLM timeout or I/O error
- `Ctrl+C` — Clear input / Cancel / Double-tap to quit
- `Esc` — Close popup / Exit shell mode / Cancel detail view
- `Up/Down` — Navigate input history
- `Shift+Enter` — New line in input
- `Tab` — Autocomplete slash commands
- Mouse — Click tabs, scroll, select text (click-drag to copy, plain click to activate)
- `!command` — Prefix input with `!` to run a shell command (double-esc to exit shell mode)
- `@path` — Reference a file (attach image, or pass path to model)

### 3.2 Headless Run Mode

```bash
# Basic usage
ocode run "Explain this code" -file main.go

# With specific model
ocode run -model gpt-4o -prompt "Write tests"

# With reasoning effort override (one-shot; does not change the saved level)
ocode run -effort xhigh "Debug this race condition"

# Continue a session
ocode run -continue "Continue from where we left off"

# Fork from last session
ocode run -fork "Try a different approach"

# JSON output for scripting
ocode run -format json "List all functions"

# Attach to running server
ocode run -attach http://localhost:4096 -prompt "Continue remotely"

# Run slash command
ocode run -command explain -file main.go
```

### 3.3 Web Server

```bash
# Start web server + open browser
ocode web

# Start web server only (no auto-open)
ocode serve

# With auth
ocode serve -username admin -password secret

# Custom port
ocode serve -port 8080
```

**API endpoints:**

| Endpoint | Purpose |
|----------|---------|
| `POST /api/chat` | Send message (async, returns session ID) |
| `GET /api/chat/stream` | Stream response (SSE) |
| `GET /api/chat/messages` | Persistent live mirror (SSE) |
| `POST /api/shell` | Run a local shell command (used by `!` prefix) |
| `GET /api/terminal/ws` | Interactive pty terminal WebSocket |
| `GET /api/sessions` | List sessions (supports `?limit=&offset=` pagination) |
| `GET /api/sessions/:id` | Session detail with live model/context info |
| `GET /api/sessions/:id/state` | Session state (messages, model, context) |
| `GET /api/sessions/:id/status` | Session status (TUI status snapshot) |
| `POST /api/sessions/:id/message` | Send message to session |
| `POST /api/sessions/:id/compact` | Compact session context |
| `GET /api/sessions/:id/recap` | Generate session recap |
| `GET /api/sessions/:id/export` | Export session as JSON |
| `GET /api/sessions/:id/export-claude` | Export in Claude Code format |
| `GET /api/sessions/:id/share` | Generate shareable link |
| `POST /api/sessions/:id/btw` | Add aside to conversation |
| `PUT /api/sessions/:id/title` | Set session title |
| `GET /api/sessions/:id/context` | Session context info |
| `GET /api/models` | List models |
| `GET /api/small-model` | Small model status |
| `GET /api/agents/runs` | List agent runs |
| `GET /api/agents/runs/stream` | Stream agent runs (SSE) |
| `GET /api/changes` | List session file changes |
| `GET /api/changes/diff` | Get diff for a change |
| `POST /api/changes/undo-file` | Undo file change |
| `POST /api/changes/undo-block` | Undo code block change |
| `GET /api/git/status` | Git status |
| `GET /api/git/diff` | Git diff |
| `GET /api/theme` | Get current theme |
| `GET /api/themes` | List available themes |
| `GET /api/files/tree` | File tree |
| `GET /api/files/content` | Get file content |
| `PUT /api/files/content` | Save file content |
| `POST /api/files/open` | Open file in editor |
| `GET /api/tui-status` | Live TUI state |
| `GET /api/spending` | LLM token spending |
| `GET /api/lsp/statuses` | LSP server statuses |
| `GET /api/files/modified` | Modified files list |
| `POST /api/files/undo` | Undo file change |
| `POST /api/files/redo` | Redo file change |
| `GET /api/config/model` | Get model config |
| `PUT /api/config/model` | Set model config |
| `GET /api/config/thinking-budget` | Get thinking budget |
| `PUT /api/config/thinking-budget` | Set thinking budget |
| `GET /api/config/small-model` | Get small model config |
| `PUT /api/config/small-model` | Set small model config |
| `GET /api/config/terminal` | Get terminal config |
| `PUT /api/config/terminal` | Set terminal config |
| `GET /api/config/ocode/recap` | Get recap config |
| `PUT /api/config/ocode/recap` | Set recap config |
| `GET /api/config/ocode/commit-msg` | Get commit message config |
| `GET /api/cron` | List scheduled jobs |
| `POST /api/cron` | Add scheduled job |
| `GET /api/cron/{id}` | Get scheduled job |
| `DELETE /api/cron/{id}` | Remove scheduled job |
| `GET /api/cron/outbox` | Get cron outbox |
| `GET /api/cron/targets` | List cron targets |
| `POST /api/cron/targets` | Set cron targets |
| `POST /api/sync/login/start` | Start sync login |
| `POST /api/sync/login/poll` | Poll sync login |
| `GET /api/sync/status` | Get sync status |
| `GET /api/events` | Server-sent events |

### 3.4 ACP Server (Agent Communication Protocol)

```bash
# Start ACP server over stdio
ocode acp
```

Communicates via JSON lines on stdin/stdout:
```json
// Input
{"type": "message", "content": "Hello", "sessionId": "abc123"}

// Output
{"type": "text", "content": "Hi there!", "sessionId": "abc123"}
```

---

## 4. Command Reference

### Global Commands

```bash
ocode --help           # Show help
ocode --version        # Show version
ocode version          # Show version (alias)
```

### Subcommands

| Command | Description |
|---------|-------------|
| `run` | Headless prompt execution |
| `serve` | HTTP server with web UI |
| `web` | Serve + open browser |
| `acp` | ACP protocol server |
| `mcp` | Manage MCP servers |
| `models` | List available models |
| `skills` | Manage skills |

### Help for Any Command

```bash
ocode <command> --help
ocode <command> -h
```

---

## 5. MCP Servers (Model Context Protocol)

MCP servers extend ocode with additional tools.

```bash
# List configured servers
ocode mcp list

# Add a server (interactive wizard)
ocode mcp add myserver

# Authenticate remote server
ocode mcp auth myserver

# Debug connection
ocode mcp debug myserver

# Remove server
ocode mcp logout myserver
```

### Adding a Local MCP Server

1. Run `ocode mcp add myserver`
2. Choose `local`
3. Enter command: `npx -y @modelcontextprotocol/server-filesystem /path/to/dir`

### Adding a Remote MCP Server

1. Run `ocode mcp add myserver`
2. Choose `remote`
3. Enter URL: `https://api.example.com/mcp`
4. Run `ocode mcp auth myserver` to OAuth

---

## 6. Models

```bash
# List all models
ocode models

# Filter by provider
ocode models openai
ocode models --provider anthropic
```

### Supported Providers

| Provider | Models |
|----------|--------|
| OpenAI | gpt-4o, gpt-4o-mini, o1, o1-mini, o3-mini, gpt-5, gpt-5-mini |
| Anthropic | claude-3-5-sonnet, claude-3-5-haiku, claude-3-opus, claude-4-5-sonnet, claude-opus-4-8 |
| Google | gemini-1.5-pro, gemini-1.5-flash, gemini-2.0-flash |
| Z.AI | glm-4, glm-4v, glm-4-plus |
| Alibaba | qwen-max, qwen-plus, qwen-turbo |
| GitHub Copilot | gpt-4o, claude-3-5-sonnet (via Copilot) |
| DeepSeek (opencode-go) | deepseek-v4-flash, deepseek-v4 |
| Minimax | minimax-m3 |
| Grok | grok-3, grok-3-mini (via grok.com subscription) |
| Cloudflare Gateway | Various models via Cloudflare Workers AI |

---

## 7. Skills

Skills are markdown files that add tools, prompts, and workflows.

```bash
# List all skills
ocode skills list

# Install all bundled skills
ocode skills install

# Install specific skill
ocode skills install ocode-tui

# Upgrade skills
ocode skills upgrade

# Uninstall skill
ocode skills uninstall ocode-tui
```

### Bundled Skills

| Skill | Description |
|-------|-------------|
| `ocode-tui` | TUI architecture guide |
| `ocode-desktop` | Desktop shell (Wails) wiring guide |
| `ocode-web` | Web SPA wiring guide |
| `ocode-tools` | Built-in tool system reference |
| `ocode-permissions` | Permission modes, policies, and configuration |
| `ocode-agent-architecture` | Agent loop, context loading, provider abstraction |
| `ocode-mem` | Persistent memory workflow for user/project/global context |
| `team-onboarding` | Team onboarding documentation generator |
| `review-changes` | AI code review using parallel agents with shared context |
| `custom-model-prompt` | Model-specific prompt configuration |
| `find-docs` | Search for documentation files in the codebase |
| `find-skills` | Discover and install skills by description |
| `skill-creator` | Guide for creating and updating skills |
| `agent-browser` | Browser automation CLI for AI agents |
| `flutter` | Flutter/Dart development with Riverpod, Freezed |
| `compress` | Workspace compression for reducing context size |

### Creating Custom Skills

1. Create `~/.config/opencode/skills/my-skill/SKILL.md`
2. Follow the [skill specification](https://github.com/u007/ocode/blob/main/skills/README.md)
3. Run `ocode skills install my-skill`

---

## 8. Common Workflows

### Daily Coding Session

```bash
# Start TUI
ocode

# In TUI: use /commands
/explain    # Explain selected code
/refactor   # Refactor selection
/test       # Generate tests
/review     # Code review
/git        # Git operations
```

### Code Review Pipeline

```bash
# 1. Get diff
ocode run -command git-diff

# 2. Review with specific model
ocode run -model claude-3-5-sonnet -command review

# 3. Output as JSON for CI
ocode run -format json -command review > review.json
```

### Automation / CI

```bash
# Generate tests
ocode run -format json -command test > tests.json

# Check for issues
ocode run -command lint -file main.go

# Auto-fix (prefer the auto-permission layer; use -yolo only as a last resort)
ocode run -yolo -command fix -file main.go
```

### Remote Development

```bash
# On server machine
ocode serve -host 0.0.0.0 -port 4096

# On client machine
ocode run -attach http://server:4096 -prompt "Continue work"
```

---

## 9. Slash Commands (TUI & Web UI & Run Mode)

Type `/` in the chat input to open the slash command palette with autocomplete (↑/↓, Enter, Esc).

| Command | Aliases | When to Use | Notes |
|---------|---------|-------------|-------|
| `/model` | `/m` | Switch LLM providers/models | Fuzzy search; shows recent/favorite models first |
| `/advisor` | | Set the advisor model for strategic guidance | Used by the `advisor()` tool during code reviews |
| `/small-model` | | Show or switch the small model for lightweight tasks | Small model gets an intent-analysis prompt fragment |
| `/compact` | `[focus]` | Manually trigger context compaction | Uses configured summary model (separate from chat model) |
| `/review` | | AI code review (working dir, file, commit, branch, or PR) | Uses parallel agents with shared notes bus |
| `/standup` | `/catchup` | Caveman summary of recent commits + pending changes | Reviews last 5 commits + working-tree changes |
| `/cron` | | Manage scheduled jobs | `list`, `describe <id>`, `remove <id>`, `add <kind> <args> <message>`; jobs fire in the long-lived serve/web/desktop host, not the TUI |
| `/agents` | | Show active/queued subagents, or set max concurrent subagents | `status`, `limit <n>` (0 = unlimited) |
| `/clear` | `/new` | Start a fresh conversation in the current session | Keeps session on disk; only clears in-memory messages |
| `/session` | `/s`, `/resume` | List, pick, or resume sessions | Supports pagination with limit/offset |
| `/export` | | Export session as JSON | Full transcript for backup or migration |
| `/export-claude` | | Export in Claude Code compatible JSONL format | For importing into Claude Code |
| `/share` | | Generate a shareable session link | Requires `ocode serve` running |
| `/cd` | `/cwd` | Change the project root | Resolves relative paths and `~` expansion |
| `/context` | | Show context window token budget and system prompt | Displays model family prompt + token estimate |
| `/upload` | `/uploads` | Show or set the file upload directory | Persisted in config; defaults to `<workDir>/.ocode/uploads` |
| `/rc` | `/remote-control` | Start/stop web UI to mirror this session | `/rc off` stops the server |
| `/ide` | | Connect to VS Code (Claude Code extension) | Lock discovery, WebSocket + MCP client |
| `/theme` | `/themes` | Switch themes instantly | Built-in themes: Tokyo Night, Storm, Catppuccin |
| `/permissions` | | View/set tool and bash permissions | Supports per-tool rules, bash prefix rules, auto-permission model |
| `/yolo` | | Toggle YOLO permissions mode on/off (**last resort**) | Auto-approves permission-gated tools (respects hard blocks) |
| `/git` | | Git operations from command line | Stage, unstage, discard, commit, push, pull, branch |
| `/github` | | PR, issue, and workflow commands | GitHub API integration |
| `/plugin` | | Plugin management (install, sync, list, etc.) | Git-based plugin system with registry |
| `/skills` | | Browse available skills | Lists all installed skills |
| `/learn` | | List project-root skills and guide creation/update | Starts from current project-root skills |
| `/undo` | `/redo` | Undo/redo file changes | Session-level change tracking |
| `/lsp` | | LSP diagnostics and status | Per-file error/warning counts |
| `/mcp` | `/mcp-auth` | MCP server management | Local + remote servers with OAuth support |
| `/editor` | `/editor-mode` | External editor configuration | Supports tmux-split, tmux-window, plain exec |
| `/usage` | | LLM token usage by model and date range | Per-hour, day, week, month, etc. |
| `/mask` | | Toggle/configure secret redaction | Tier-1 regex + tier-2 LLM scanning |
| `/mem` | | Memory context injection | Inspect or toggle user/project/global memory layers |
| `/btw` | `/by-the-way` | Add a quick aside to the conversation | Injects a note without breaking flow |
| `/init` | | Analyze project and generate AGENTS.md | Project initialization |
| `/help` | `/?` | Show all available commands | Auto-generated from registered command specs |

**Web command parity:** the web/desktop UI supports the same slash-command surface as the TUI — `/standup`, `/changes`, `/review`, `/context`, `/lsp`, `/agents`, `/skills`, `/mcp`, `/cron`, `/small-model`, `/advisor`, `/github` all work in the web chat input. The repo-analysis ones (`/standup`, `/changes`, `/review`) build their prompts through the shared `internal/commandctx` package, so TUI and web get byte-identical context.

---

## 10. Permissions & Safety

### Permission Modes

> **Recommendation:** prefer the **auto-permission layer** over YOLO mode. Auto-approval removes most confirmation interruptions while still prompting (or falling back to `ask`) for anything it cannot confidently vet, and it can never override hard safety blocks. YOLO mode blindly approves *everything* and should be reserved as a last resort.

| Mode | Behavior |
|------|----------|
| `normal` (default) | Follow tool rules — some auto-allow, some prompt |
| `auto` (**recommended**) | LLM-based auto-approval layer — auto-allows routine/low-risk ops, prompts for risky ones, respects hard blocks |
| `yolo` (last resort) | Auto-approve all permission-gated tools (dangerous) |
| `locked` | Read-only — all write/edit/bash/network tools denied |

### Auto-Permission Layer (Recommended)

The **recommended** way to cut down on permission interruptions. An LLM-based layer that auto-approves low-risk permission prompts without user interaction, while still prompting for anything it cannot confidently vet:

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

**Key constraints:**
- The auto-permission model can only emit `allow` or `ask` — it **cannot** emit `deny` or widen scope
- Hard blocks (destructive git, data exfiltration) are deterministic and final — the auto layer cannot override them
- `allow_destructive: false` instructs the model to conservatively deny operations it cannot confidently approve
- **Unavailable judge ≠ denial:** if the auto-permission LLM can't be reached, the prompt shows a neutral "permission model unavailable — asking you instead" notice and falls back to the ordinary allow/deny prompt

### Tool Permission Levels

Every tool/prefix rule resolves to one of:

| Level | Meaning |
|-------|----------|
| `allow` | Auto-grant, no prompt |
| `ask` | Prompt user for approval |
| `deny` | Hard-block, never proceed |

Default tool rules:
```
Always allow:  read, glob, grep, list, lsp, lsp_diagnostics, skill, load_skill,
              question, todoread, todowrite, todo_update, advisor, task,
              task_status, agent_status, repo_overview, plan_enter,
              plan_exit, wait, bash_output, kill_shell, list_processes,
              ocr, cron

Default allow: write, edit, multiedit, multi_file_edit, replace_lines,
              apply_patch, format

Default ask:  delete, bash, webfetch, websearch, repo_clone, mcp_*
```

Override per-tool in `ocodeconfig.json`:
```json
{ "permissions": { "tools": { "bash": "allow", "delete": "deny" } } }
```

---

## 11. Secret Redaction (`/mask`)

ocode includes a **secret redaction system** that detects and masks common credential patterns before they are sent to the LLM provider.

### `/mask` Subcommands

| Command | Description |
|---------|-------------|
| `/mask` | Show current redaction status |
| `/mask on` | Enable redaction |
| `/mask off` | Disable redaction |
| `/mask mode` | Show current scan mode |
| `/mask mode lenient` | Set lenient mode (default) |
| `/mask mode full` | Set full mode |
| `/mask model [name]` | Set or show the tier-2 scanning model |

---

## 12. Sub-Agent Tasks, Todo Plans & Background Processes

### Task tool (sub-agent dispatch)

The `task` tool dispatches a sub-agent. Key features:
- **`expected_output` contract** — optional natural-language description of the required result shape. When set, the sub-agent's final result is verified against it and retried **once in place** if it doesn't match.
- **`resume_task_id`** — resume a cancelled or completed sub-agent run instead of starting a new one.
- **In-batch DAG** — `id` + `depends_on` turn a parallel batch into a wave-scheduled DAG.
- **Self-dispatch guard** — an agent can never dispatch another instance of its own type.
- `todowrite`/`todo_update` are filtered out of sub-agent tool sets (main-agent-only).

### Persistent todo plan

- The agent's todo plan lives on disk at `.ocode/todo/<session-id>.md` with a **revision protocol** (targeted updates, destructive-replacement guard, advisory `flock` serialization).

### Background processes

- `bash(..., run_in_background: true)` starts a background process. `list_processes` lists all running background processes, and `bash_output`/`kill_shell` manage them.

---

## 13. Sessions

Sessions are stored in `~/.local/share/opencode/sessions/`

```bash
# Resume last
ocode -continue

# Resume specific
ocode -session <id>

# Fork (new session from existing)
ocode -fork
```

---

## 14. Keyboard Shortcuts (TUI)

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Next/previous tab |
| `Ctrl+P` | Command palette |
| `Ctrl+X` then `h` | Toggle help |
| `Ctrl+X` then `t` | Cycle theme |
| `Ctrl+X` then `m` | Toggle mouse |
| `Esc` | Close popup / cancel |
| `Enter` | Send message |
| `Shift+Enter` | New line in input |
| `↑` / `↓` | History / scroll |
| `Ctrl+C` | Interrupt agent |

**Desktop (Wails) differences:** `Cmd/Ctrl+W` is deliberately unbound in the native menu so the key reaches the webview, where it closes the active **session tab** (not the app). `Cmd/Ctrl+Q` shows a native "Quit ocode?" confirmation before exiting.

---

## 15. Troubleshooting

### Common Issues

| Problem | Solution |
|---------|----------|
| "No model configured" | Set `OPENCODE_MODEL` or `model` in config |
| "API key invalid" | Check `apiKeys` in config or env vars |
| "Permission denied" | Check file permissions, or enable the auto-permission layer |
| "Connection refused" | Ensure server is running (`ocode serve`) |
| TUI rendering issues | Resize terminal, check `TERM` env var |
| Desktop file tree lists the whole filesystem | Fixed by workdir anchoring; ensure `srv.SetWorkDir` ran at boot |
| Terminal sub-tab missing | The terminal is always enabled; check server credentials/bind address |

### Debug Mode

```bash
# Enable debug logging
DEBUG=1 ocode

# View logs in TUI: Tab → Log
# Or check ~/.local/share/opencode/logs/
```

---

## 16. File Structure

```
~/.config/opencode/
├── config.json          # Main config
├── skills/              # Installed skills
│   └── skill-name/
│       └── SKILL.md
~/.local/share/opencode/
├── sessions/            # Session data
├── logs/                # Debug logs
└── mcp/                 # MCP server configs
```

---

## 17. Resources

- **GitHub**: https://github.com/u007/ocode
- **Issues**: https://github.com/u007/ocode/issues
- **Man Pages**: `man ./docs/man/ocode.1` (after build)
- **Source**: `internal/` — Go packages
- **TUI Code**: `internal/tui/`
- **Agent Core**: `internal/agent/`
