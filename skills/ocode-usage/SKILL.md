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
git clone https://github.com/jamesmercstudio/ocode
cd ocode
go build -o ocode .

# Or install to PATH
go install github.com/jamesmercstudio/ocode@latest
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

`~/.config/opencode/config.json`

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
| `OPENCODE_SERVER_USERNAME` | Basic auth for web server |
| `OPENCODE_SERVER_PASSWORD` | Basic auth for web server |
| `NO_COLOR` | Disable colored output |

### Provider API Keys

Supported providers: **OpenAI**, **Anthropic**, **Google (Gemini)**, **Z.AI**, **Alibaba (Qwen)**

Configure via `apiKeys` in config or provider-specific env vars:
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `GOOGLE_API_KEY`

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
- `Ctrl+P` — Command palette
- `Ctrl+X` — Leader key (then `h` for help, `t` for theme, etc.)
- `Esc` — Exit slash popup / cancel
- Mouse — Click tabs, scroll, select text

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

# Custom host/port
ocode serve -host 127.0.0.1 -port 8080

# With basic auth (via env vars)
OPENCODE_SERVER_USERNAME=admin OPENCODE_SERVER_PASSWORD=secret ocode serve
```

**Endpoints:**
- `GET /` — Web UI
- `POST /api/chat` — Send message
- `GET /api/chat/stream` — Stream response (SSE)
- `GET /api/sessions` — List sessions
- `GET /api/models` — List models
- `GET /api/files/tree` — File tree
- `GET /api/git/status` — Git status

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
| OpenAI | gpt-4o, gpt-4o-mini, o1-preview, o1-mini |
| Anthropic | claude-3-5-sonnet, claude-3-5-haiku, claude-3-opus |
| Google | gemini-1.5-pro, gemini-1.5-flash |
| Z.AI | glm-4, glm-4v |
| Alibaba | qwen-max, qwen-plus |

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
| `team-onboarding` | Team onboarding generator |
| `custom-model-prompt` | Model-specific prompt config |

### Creating Custom Skills

1. Create `~/.config/opencode/skills/my-skill/SKILL.md`
2. Follow the [skill specification](https://github.com/jamesmercstudio/ocode/blob/main/skills/README.md)
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

## 9. Slash Commands (TUI & Run Mode)

| Command | Description |
|---------|-------------|
| `/explain` | Explain code |
| `/refactor` | Refactor code |
| `/test` | Generate tests |
| `/review` | Code review |
| `/fix` | Auto-fix issues |
| `/lint` | Lint code |
| `/git` | Git operations |
| `/files` | File operations |
| `/help` | Show all commands |

In TUI: Type `/` to see autocomplete.

In run mode: `ocode run -command explain -file main.go`

---

## 10. Permissions & Safety

### Permission Modes

| Mode | Behavior |
|------|----------|
| `auto` (default) | Prompt for each tool use |
| `off` | Auto-approve all (dangerous) |

### YOLO Mode

```bash
ocode -yolo                    # TUI
ocode run -yolo "..."          # Run mode
ocode run --dangerously-skip-permissions "..."  # Alias
```

**⚠️ Warning:** YOLO mode allows the agent to run any shell command without confirmation.

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

- **GitHub**: https://github.com/jamesmercstudio/ocode
- **Issues**: https://github.com/jamesmercstudio/ocode/issues
- **Man Pages**: `man ./docs/man/ocode.1` (after build)
- **Source**: `internal/` — Go packages
- **TUI Code**: `internal/tui/`
- **Agent Core**: `internal/agent/`