---
name: ocode-usage
description: Comprehensive guide on how to use ocode — the AI coding agent. Covers installation, configuration, TUI mode, headless run mode, web server, MCP servers, models, skills, and common workflows. Use this when users ask "how do I use ocode", "getting started with ocode", "ocode tutorial", or need a reference for any ocode feature.
when_to_use: When the user asks for help using ocode, wants a tutorial, needs to understand available commands, or asks "how do I..." questions about ocode features. Also triggered by: "ocode tutorial", "getting started", "how to use", "ocode guide", "ocode help".
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

Supported providers: **OpenAI**, **Anthropic**, **Google (Gemini)**, **Z.AI**, **Alibaba (Qwen)**, **GitHub Copilot**, **DeepSeek (opencode-go)**, **Minimax**

Configure via `apiKeys` in config or provider-specific env vars:
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `GOOGLE_API_KEY`
- `ZHIPUAI_API_KEY` (Z.AI)
- `DASHSCOPE_API_KEY` (Alibaba)
- `GITHUB_COPILOT_TOKEN`
- `OPENCODE_API_KEY` (opencode-go / DeepSeek)

**Global override:** Set `OPENCODE_AUTH_TOKEN` to use a single token for all providers, bypassing per-provider configuration. Useful for CI/CD or proxy setups.

---

## 3. Modes of Operation

### 3.1 Interactive TUI (Default)

```bash
ocode                    # Start fresh session
ocode -continue          # Resume last session
ocode -session <id>      # Resume specific session
ocode -yolo              # Auto-approve all permissions
ocode --permission-mode off  # Disable permissions entirely
```

**TUI Navigation:**
- `Tab` / `Shift+Tab` — Switch tabs (chat, files, git, log)
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

**Key Flags:**
| Flag | Short | Description |
|------|-------|-------------|
| `-prompt` | `-p` | Prompt text |
| `-model` | `-m` | Override model |
| `-session` | `-s` | Session ID |
| `-continue` | `-c` | Continue last session |
| `-fork` | | Fork from last session |
| `-file` | `-f` | Attach file(s) |
| `-format` | | `default` or `json` |
| `-yolo` | | Auto-approve permissions |
| `-command` | | Run slash command |

### 3.3 Web Server Mode

```bash
# Start server (default: http://localhost:4096)
ocode serve

# Start and open browser
ocode web
ocode serve --open

# Remote control a running TUI session
# (In TUI: /rc starts the web UI; /rc off stops it)

# Custom host/port
ocode serve -host 127.0.0.1 -port 8080

# With basic auth (via env vars)
OPENCODE_SERVER_USERNAME=admin OPENCODE_SERVER_PASSWORD=secret ocode serve

# With Tailscale (auto-exposes via tailscale serve)
ocode serve --tailscale
```

**Web UI features:**
- **Mobile sidebar** — Overlay with backdrop on viewports < 768px
- **Web shell** — `!` prefix in chat input runs local shell commands
- **Live status panel** — Real-time model, context, LSP, spending, modified files
- **File uploads** — Drag-and-drop uploads; directory set via `/upload` command

**Endpoints:**
- `GET /` — Web UI
- `POST /api/chat` — Send message
- `GET /api/chat/stream` — Stream response (SSE)
- `POST /api/shell` — Run a local shell command (used by `!` prefix)
- `GET /api/sessions` — List sessions (supports `?limit=&offset=` pagination)
- `GET /api/sessions/:id` — Session detail with live model/context info
- `GET /api/models` — List models
- `GET /api/small-model` — Small model status (includes `enabled` field)
- `GET /api/files/tree` — File tree
- `GET /api/git/status` — Git status
- `GET /api/tui-status` — Live TUI state (model, advisor, IDE, CWD, context, LSP, modified files)
- `GET /api/spending` — LLM token spending
- `GET /api/lsp/statuses` — LSP server statuses
- `GET /api/files/modified` — Modified files list
- `POST /api/uploads` — Upload endpoint configuration
- `POST /api/uploads/file` — File upload endpoint

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
| OpenAI | gpt-4o, gpt-4o-mini, o1, o1-mini, o3-mini |
| Anthropic | claude-3-5-sonnet, claude-3-5-haiku, claude-3-opus, claude-4-5-sonnet, claude-opus-4-8 |
| Google | gemini-1.5-pro, gemini-1.5-flash, gemini-2.0-flash |
| Z.AI | glm-4, glm-4v, glm-4-plus |
| Alibaba | qwen-max, qwen-plus, qwen-turbo |
| GitHub Copilot | gpt-4o, claude-3-5-sonnet (via Copilot) |
| DeepSeek (opencode-go) | deepseek-v4-flash, deepseek-v4 |
| Minimax | minimax-m3 |

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

# Auto-fix
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

### What the Palette Looks Like

```
┌─────────────────────────────────────────────────────────────┐
│  /  █                                                       │
├─────────────────────────────────────────────────────────────┤
│  🗑️  /clear         Clear conversation history              │
│  ⚙️  /model         Open model selector                     │
│  📦  /compact       Compact conversation context            │
│  📄  /recap         Generate session recap                  │
│  ⬇️  /export        Export session as JSON                 │
│  🔗  /share         Share session link                      │
│  ❓  /help          Show available commands                 │
└─────────────────────────────────────────────────────────────┘
  ↑/↓ navigate  Enter select  Esc cancel
```

| Command | Aliases | When to Use | Notes |
|---------|---------|-------------|-------|
| `/model` | `/m` | Switch LLM providers/models | Fuzzy search; shows recent/favorite models first |
| `/advisor` | | Set the advisor model for strategic guidance | Used by the `advisor()` tool during code reviews |
| `/small-model` | | Show or switch the small model for lightweight tasks | Small model gets an intent-analysis prompt fragment |
| `/compact` | `[focus]` | Manually trigger context compaction | Uses configured summary model (separate from chat model) |
| `/review` | | AI code review (working dir, file, commit, branch, or PR) | Uses parallel agents with shared notes bus |
| `/standup` | `/catchup` | Caveman summary of recent commits + pending changes | Reviews last 5 commits + working-tree changes |
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
| `/yolo` | | Toggle YOLO permissions mode on/off | Auto-approves permission-gated tools (respects hard blocks) |
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

### Command Palette (`Ctrl+P`)

| Palette | Trigger | Description |
|---------|---------|-------------|
| **Slash Commands** | `/` in chat input | Filter and execute slash commands with icons/descriptions |
| **Command Palette** | `Ctrl+P` | Fuzzy-search all commands, sessions, models, files, git actions |

### Headless Mode (`ocode run`)

```bash
# Run a slash command non-interactively
ocode run -command compact
ocode run -command export -session abc123
ocode run -command recap        # Not yet implemented in headless
```

### Status of `/recap`

| Interface | Status |
|-----------|--------|
| **Web UI** | Frontend implemented (`/recap` in palette), backend endpoint **planned** |
| **TUI** | Accepts `/recap` as input but treats it as a normal prompt (no special handling) |
| **Headless** | `-command recap` not yet wired |

The backend implementation is tracked in [the serve full-API plan](docs/superpowers/plans/2026-06-02-serve-cmd-full-api.md). When implemented, it will generate a structured recap using the small/summary model.

---

## 10. Permissions & Safety

### Permission Modes

| Mode | Behavior |
|------|----------|
| `normal` (default) | Follow tool rules — some auto-allow, some prompt |
| `yolo` | Auto-approve all permission-gated tools (dangerous) |
| `locked` | Read-only — all write/edit/bash/network tools denied |

### YOLO Mode

```bash
ocode -yolo                    # TUI
ocode run -yolo "..."          # Run mode
ocode run --dangerously-skip-permissions "..."  # Alias
```

**⚠️ Warning:** YOLO mode allows the agent to run any shell command without confirmation.

### Auto-Permission Layer (Optional)

An LLM-based layer that auto-approves/denies permission prompts without user interaction:

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

### Tool Permission Levels

Every tool/prefix rule resolves to one of:

| Level | Meaning |
|-------|----------|
| `allow` | Auto-grant, no prompt |
| `ask` | Prompt user for approval |
| `deny` | Hard-block, never proceed |

Default tool rules:
```
Always allow:  read, glob, grep, list, lsp, skill, question, todoread, todowrite,
              advisor, task, task_status, agent_status, repo_overview, plan_enter,
              plan_exit, wait, bash_output, kill_shell

Default allow: write, edit, multiedit, multi_file_edit, replace_lines,
              apply_patch, format

Default ask:  delete, bash, webfetch, websearch, repo_clone, mcp_*
```

Override per-tool in `ocodeconfig.json`:
```json
{ "permissions": { "tools": { "bash": "allow", "delete": "deny" } } }
```

---

## 10.5. Secret Redaction (`/mask`)

ocode includes a **secret redaction system** that detects and masks common credential patterns before they are sent to the LLM provider.

### `/mask` Subcommands

| Command | Description |
|---------|-------------|
| `/mask` | Show current redaction status (enabled/disabled, tier-2 scanner state) |
| `/mask on` | Enable redaction |
| `/mask off` | Disable redaction |
| `/mask mode` | Show current scan mode |
| `/mask mode lenient` | Set lenient mode (default) — LLM scans only when input contains sensitive keywords/value-patterns |
| `/mask mode full` | Set full mode — LLM scans every typed user message |
| `/mask model [name]` | Set or show the tier-2 scanning model. Auto-configures base_url for known local providers (e.g. lmstudio → http://localhost:1234/v1) |

### Scan Modes

| Surface | lenient (default) | full |
|---------|-------------------|------|
| Typed user message | tier-2 LLM only if input contains a sensitive keyword or known value-pattern | tier-2 LLM **always** |
| Sensitive file read (`.env`, `*.pem`, …) | tier-2 **LLM** always | tier-2 **LLM** always |
| Other tool results (DB/bash/normal reads) | chat-mode **regex** only (no LLM) | chat-mode **regex** only (no LLM) |
| All messages, every step | tier-1 regex safety net | tier-1 regex safety net |

**Known limitations:**
- DB/bash secret detection is regex-only. A value after a keyword (`password`, `secret`, …) is only caught when high-entropy, so low-entropy/dictionary passwords (`password=hunter2`) and values with shell metacharacters (`$`) are missed, as is tabular/CSV output without `=`/`:` delimiters.
- Only the `read` tool gets sensitive-file LLM treatment; `bash cat .env` is treated as generic bash output (regex-only).
- No tier-2 model configured → scanning is regex-only; set a model with `/mask model` to enable LLM tier-2.
- **Provider auto-detection:** when you select a model from a known local provider (e.g. `lmstudio/...`), the scanner's `base_url` is automatically set to the provider's default endpoint (`http://localhost:1234/v1` for LM Studio). The persisted/display name is normalized to `lmstudio/<name>` even if you typed a bare model id, and the scanner strips the prefix when making the request. This matches how `/model` works.
- **Manual override:** set a custom `base_url` via `security.redaction.base_url` in the config. Once set, auto-detection is skipped.
- **Security:** only local endpoints are accepted by default. Set `security.redaction.allow_remote_tier2: true` to allow remote endpoints.

---

## 11. Sessions

Sessions are stored in `~/.local/share/opencode/sessions/`

```bash
# List sessions (in TUI: sidebar)
# Resume last
ocode -continue

# Resume specific
ocode -session <id>

# Fork (new session from existing)
ocode -fork
```

In headless mode:
```bash
ocode run -session <id> "Continue"
ocode run -continue "Continue"
ocode run -fork "New direction"
```

---

## 12. Troubleshooting

### Common Issues

| Problem | Solution |
|---------|----------|
| "No model configured" | Set `OPENCODE_MODEL` or `model` in config |
| "API key invalid" | Check `apiKeys` in config or env vars |
| "Permission denied" | Check file permissions or use `-yolo` |
| "Connection refused" | Ensure server is running (`ocode serve`) |
| TUI rendering issues | Resize terminal, check `TERM` env var |

### Debug Mode

```bash
# Enable debug logging
DEBUG=1 ocode

# View logs in TUI: Tab → Log
# Or check ~/.local/share/opencode/logs/
```

### Reset Configuration

```bash
rm ~/.config/opencode/config.json
ocode  # Will prompt for setup
```

---

## 13. Keyboard Shortcuts (TUI)

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

---

## 14. File Structure

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

## 15. Resources

- **GitHub**: https://github.com/u007/ocode
- **Issues**: https://github.com/u007/ocode/issues
- **Man Pages**: `man ./docs/man/ocode.1` (after build)
- **Source**: `internal/` — Go packages
- **TUI Code**: `internal/tui/`
- **Agent Core**: `internal/agent/`
