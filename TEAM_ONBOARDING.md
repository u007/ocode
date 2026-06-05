# ocode — Team Onboarding Guide

> **Project**: `github.com/u007/ocode` · v0.2.1  
> **Language**: Go 1.26 · TUI: Charm (Bubble Tea, Lipgloss)  
> **LLM Providers**: OpenAI, Anthropic, Google, Z.AI, DeepSeek, Alibaba, and more  
> **License**: MIT

---

## 1. What is ocode?

ocode is a **terminal-native AI coding assistant** — a TUI (Terminal User Interface) that runs LLM agents with tool-use capabilities (file I/O, shell execution, LSP, web search, MCP integrations). It supports multiple LLM providers, a plugin system, skills, and a web UI.

Think of it as a CLI-native alternative to Cursor/Copilot that runs entirely in your terminal.

---

## 2. Project Structure

```
ocode/
├── main.go                    # Entry point: CLI dispatch (TUI, run, serve, acp, mcp, version)
├── go.mod                     # Go module definition
├── Makefile                   # Build targets (web-build)
├── internal/                  # All application packages (private)
│   ├── agent/                 # LLM agent loop, provider clients, tool orchestration
│   │   ├── registry.go        # AgentSpec definitions (build, plan, review, debug, etc.)
│   │   ├── client.go          # LLMClient: provider routing, streaming, message building
│   │   ├── small_model.go     # Auto-select cheap models for lightweight tasks
│   │   ├── agent_loader.go    # Load custom agents from ~/.config/opencode/agents/ and .opencode/agents/
│   │   └── prompts/           # System prompt templates
│   ├── auth/                  # Credential store (API keys, OAuth tokens)
│   │   ├── providers.go       # Provider registry: 22+ providers (OpenAI, Anthropic, Google, etc.)
│   │   ├── store.go           # Encrypted local credential storage
│   │   └── cloudflare.go      # Cloudflare Workers AI auth
│   ├── config/                # Configuration loading & validation
│   │   ├── config.go          # Config struct, JSON parsing, MCP/plugin/TUI config
│   │   └── ocodeconfig.go     # ocode.json schema, provider model lists
│   ├── commands/              # /slash command loader
│   │   └── loader.go
│   ├── hooks/                 # Pre/post tool hooks (shell commands)
│   │   ├── hooks.go           # Hook execution engine
│   │   └── pipeline.go        # Hook pipeline with stdin/stdout piping
│   ├── lsp/                   # Language Server Protocol client
│   │   └── client.go          # LSP child process management
│   ├── mcp/                   # Model Context Protocol client
│   │   └── client.go          # MCP server connection & tool bridging
│   ├── mcpcli/                # `ocode mcp` CLI subcommand
│   ├── models/                # Model registry and metadata
│   ├── plugins/               # Plugin system (git repos with tools/agents)
│   │   ├── loader.go          # Plugin discovery and loading
│   │   └── manager.go         # Install/remove/enable/disable plugins
│   ├── pricing/               # Token pricing calculations
│   ├── runcli/                # `ocode run` headless CLI
│   ├── server/                # `ocode serve` HTTP server mode
│   ├── session/               # Session persistence (save/resume conversations)
│   │   └── session.go         # JSONL session files, Claude export support
│   ├── skill/                 # Skills system (bundled + installed)
│   │   ├── installer.go       # Skill installation from git/local
│   │   └── loader.go          # Skill discovery and SKILL.md parsing
│   ├── snapshot/              # File snapshot/diff utilities
│   ├── tool/                  # Tool implementations (20+ tools)
│   │   ├── tool.go            # Tool interface + LoadBuiltins()
│   │   ├── file.go            # read, write, delete, edit, multi-edit, patch
│   │   ├── exec.go            # bash tool
│   │   ├── search.go          # grep, glob tools
│   │   ├── ast.go             # AST tool (LSP-backed)
│   │   ├── lsp_tool.go        # LSP tool (goToDefinition, hover, etc.)
│   │   ├── agent.go           # Sub-agent delegation tool
│   │   ├── question.go        # User prompt tool
│   │   ├── web.go             # webfetch, websearch, repo-clone tools
│   │   ├── todo.go            # Todo list tool
│   │   ├── custom.go          # Custom tool loading from .opencode/tools/
│   │   └── git.go             # git-diff, git-commit tools
│   ├── tui/                   # Bubble Tea TUI (the main UI)
│   │   ├── tui.go             # Run() entry, program lifecycle
│   │   ├── model.go           # Model struct, Update/View, state management
│   │   ├── commands.go        # Slash command handling
│   │   ├── files_model.go     # Files browser panel
│   │   ├── git_model.go       # Git diff panel
│   │   ├── editor_mode.go     # Inline editor mode
│   │   ├── selection.go       # In-app text selection
│   │   └── ...                # Many more panels and components
│   ├── usage/                 # Token usage tracking and aggregation
│   └── version/               # Version constant
├── skills/                    # Bundled skills (SKILL.md files)
│   ├── custom-model-prompt/
│   ├── ocode-tui/
│   └── team-onboarding/
├── web/                       # React web UI (Vite + Tailwind)
│   ├── src/
│   │   ├── App.tsx
│   │   ├── api/               # Backend API client
│   │   ├── components/        # React components
│   │   ├── hooks/             # React hooks
│   │   └── stores/            # State management
│   └── package.json
└── docs/                      # Documentation
    └── superpowers/           # Planning/spec docs
```

---

## 3. Build & Run

### Prerequisites
- Go 1.26+
- Node.js 18+ (for web UI)
- Git

### Build the binary

```bash
go build -o ocode .
```

### Build the web UI

```bash
make build        # runs: web-build
# or manually:
cd web && npm install && npm run build
```

### Run

```bash
# Interactive TUI
./ocode

# Headless CLI run
./ocode run "your prompt here"

# HTTP server mode
./ocode serve

# MCP server mode
./ocode mcp

# Version
./ocode version
```

---

## 4. Configuration

### Config file: `~/.config/opencode/ocode.json`

Key sections:

| Key | Purpose |
|-----|---------|
| `provider` | Per-provider settings (apiKey, baseURL, model overrides) |
| `model` | Default model selection |
| `smallModel` | Model for lightweight tasks (title gen, etc.) |
| `theme` | TUI theme name |
| `mcp` | MCP server definitions |
| `plugins` | Plugin sources |
| `hooks` | Pre/post tool hooks |
| `editor` | Editor configuration |
| `permissions` | Tool permission modes (auto, off, ask); includes exfiltration-risk detection for URL-calling commands |

### Environment variables

Providers are configured via env vars:

| Provider | Env Var |
|----------|---------|
| OpenAI | `OPENAI_API_KEY` |
| Anthropic | `ANTHROPIC_API_KEY` |
| Google | `GOOGLE_API_KEY` |
| DeepSeek | `DEEPSEEK_API_KEY` |
| Alibaba | `DASHSCOPE_API_KEY` |
| Cloudflare | `CLOUDFLARE_API_KEY` |
| Z.AI | `ZAI_API_KEY` |
| OpenRouter | `OPENROUTER_API_KEY` |

### Context files

These are loaded at session start:
- `AGENTS.md` — Agent instructions and coding standards
- `CLAUDE.md` — Project-specific instructions
- `.cursorrules` — Cursor compatibility rules

---

## 5. LLM Providers

ocode supports **22+ providers** (see `internal/auth/providers.go`):

**OAuth-capable**: OpenAI, Anthropic, Google, GitHub Copilot  
**API key only**: OpenCode Zen, OpenCode Go, OpenRouter, Z.AI, Moonshot, MiniMax, Alibaba, DeepSeek, NVIDIA, LM Studio, Cloudflare  
**Local**: LM Studio (localhost)

**Keyless fallback**: `opencode/mimo-v2.5-free` — used as a reliable fallback for the small model.

Provider routing happens in `internal/agent/client.go`. The model string format is `{provider-id}/{model-name}` (e.g., `openai/gpt-4o`, `anthropic/claude-sonnet-4-20250514`).

---

## 6. Agent System

Agents are defined in `internal/agent/registry.go` as `AgentSpec` structs:

| Agent | Mode | Purpose |
|-------|------|---------|
| `build` | ModeBuild | Full development — all tools enabled |
| `plan` | ModePlan | Analysis and planning (no file changes) |
| `review` | ModeReview | Code review (read-only access) |
| `debug` | ModeDebug | Focused investigation (bash + read tools) |
| `docs` | ModeDocs | Documentation writing |
| `explore` | ModeExplore | Codebase exploration |
| `general` | ModeGeneral | General-purpose assistant |
| `compaction` | ModeCompact | Conversation compaction |

**Custom agents**: Define in `~/.config/opencode/agents/*.md` or `.opencode/agents/*.md`. See agent_loader.go for the markdown format.

---

## 7. Tool System

All tools implement the `tool.Tool` interface (`internal/tool/tool.go`):

```go
type Tool interface {
    Name() string
    Description() string
    Definition() map[string]interface{}
    Execute(args json.RawMessage) (string, error)
    Parallel() bool
}
```

### Built-in tools

| Tool | Purpose |
|------|---------|
| `read` | Read file contents |
| `write` | Create/overwrite/append files |
| `delete` | Delete files |
| `edit` | Search-and-replace in files |
| `multi_edit` | Multiple edits to a single file |
| `multi_file_edit` | Edit across multiple files |
| `patch` | Apply patch format changes |
| `bash` | Shell command execution |
| `grep` | Plain text/regex search |
| `glob` | File pattern matching |
| `lsp` | LSP operations (goToDefinition, hover, etc.) |
| `ast` | AST symbol navigation |
| `agent` | Delegate to sub-agents |
| `question` | Pause for user input |
| `webfetch` | Fetch URLs as markdown |
| `websearch` | DuckDuckGo web search |
| `repo-clone` | Clone repos for research |
| `todo-write` / `todo-read` | Task list management |
| `skill` | Load skill definitions |

### Custom tools

Add to `.opencode/tools/` — files implementing the tool interface with JSON schema definitions.

---

## 8. TUI Architecture

The TUI uses **Charm's Bubble Tea** framework running in alt-screen mode.

### Key files

| File | Role |
|------|------|
| `tui.go` | `Run()` entry, program lifecycle, signal handling |
| `model.go` | Main `Model` struct, `Update()`/`View()` loop |
| `commands.go` | `/slash` command processing |
| `files_model.go` | File browser panel |
| `git_model.go` | Git diff viewer |
| `editor_mode.go` | Inline code editor |
| `selection.go` | In-app text selection (mouse drag-to-copy) |
| `scrollbar.go` | Scrollbar rendering |
| `debuglog.go` | Debug log panel (log package redirect) |

### Input modes
- **Normal**: Chat with agent
- **Command**: `/` prefix for slash commands
- **Shell**: `!` prefix for direct shell commands
- **Leader**: `ctrl+x` prefix for UI toggles
- **Palette**: `ctrl+p` for action palette
- **Editor**: Inline code editing mode

### Mouse support
Mouse capture is **global per frame** — enables clickable nav but blocks native terminal selection. ocode implements **in-app selection** (press → drag → release) to make content both clickable and selectable.

---

## 9. Sessions

Sessions are persisted as JSONL files in `~/.config/opencode/sessions/`.

- Auto-saved on every agent turn
- Resumable with `./ocode --continue` or `./ocode --session <id>`
- Exportable to Claude format
- Title auto-generated via small model

---

## 10. Hooks System

Pre/post hooks fire shell commands around tool execution:

```json
{
  "hooks": {
    "*": { "pre": ["echo 'before any tool'"], "post": ["echo 'after any tool'"] },
    "bash": { "pre": ["echo 'running shell'"] },
    "write": { "post": ["npm run format -- $FILE"] }
  }
}
```

Hooks support `*` wildcard (all tools) and specific tool names.

---

## 11. Skills System

Skills are markdown-based instructions loaded on demand.

### Bundled skills (in `skills/`)

- `custom-model-prompt` — Create model-specific prompts
- `ocode-tui` — TUI architecture reference
- `team-onboarding` — This onboarding generator

### Install custom skills

```bash
./ocode skill install <git-url>
./ocode skill list
./ocode skill status
```

Skills are loaded via the `skill` tool and injected into context.

---

## 12. MCP (Model Context Protocol)

Configure MCP servers in `ocode.json`:

```json
{
  "mcp": {
    "my-server": {
      "type": "local",
      "command": ["npx", "@my/mcp-server"],
      "environment": { "API_KEY": "..." }
    }
  }
}
```

MCP servers provide additional tools that are bridged into the agent's tool set.

---

## 13. Plugin System

Plugins are git repositories providing custom tools/agents:

```json
{
  "plugins": {
    "my-plugin": {
      "source": "github.com/user/repo",
      "dir": "plugins/my-plugin",
      "enabled": true
    }
  }
}
```

Plugin management via `./ocode plugin install/remove/list`.

---

## 14. Testing

### Run tests

```bash
go test ./...
```

### Test files

Tests are co-located with source files (`*_test.go`):

- `internal/tui/*_test.go` — TUI rendering, selection, commands
- `internal/config/*_test.go` — Config parsing
- `internal/auth/*_test.go` — Auth store, OAuth flows
- `internal/skill/*_test.go` — Skill loading and installation
- `internal/snapshot/*_test.go` — File snapshots

---

## 15. Development Workflow

### 1. Fork & clone

```bash
git clone git@github.com:yourname/ocode.git
cd ocode
```

### 2. Create feature branch

```bash
git checkout -b feat/my-feature
```

### 3. Make changes

Follow Go conventions:
- Format: `go fmt ./...`
- Vet: `go vet ./...`
- Tests: `go test ./...`

### 4. Build & test locally

```bash
go build -o ocode .
./ocode  # test the TUI
```

### 5. Commit & push

```bash
git add -A
git commit -m "feat: description"
git push origin feat/my-feature
```

### 6. Open PR

---

## 16. Key Conventions

### TUI output safety
**Never** write to `os.Stdout`/`os.Stderr` from code reachable while the TUI is live. Use:
- `agent.emitDebug()` / `agent.DebugAppendf()` in agent code
- `log.Printf()` elsewhere (redirected to debug panel)

### Capturing subprocess output
Always capture: `cmd.Stdout = &buf` — never inherit the terminal.

### Session context files
If `AGENTS.md` / `CLAUDE.md` has unstaged git modifications, the committed `HEAD` version is used (keeps base prompt stable). Commit changes to make them effective.

### Leader key shortcuts
Prefer `ctrl+x` leader sequences for non-essential UI toggles to avoid conflicts with host terminals (Warp, Ghostty, iTerm2).

---

## 17. Quick Reference Commands

| Command | Description |
|---------|-------------|
| `./ocode` | Launch TUI |
| `./ocode run "prompt"` | Headless CLI run |
| `./ocode serve` | HTTP server |
| `./ocode mcp` | MCP server mode |
| `./ocode version` | Print version |
| `/commands` | List slash commands in TUI |
| `!shell` | Run shell command in TUI |
| `ctrl+x` | Leader key |
| `ctrl+p` | Action palette |
| `ctrl+c` | Cancel/exit |

---

## 18. Architecture Diagram

```
main.go
  │
  ├─► TUI Mode ──► internal/tui/
  │                  ├─► Model (Bubble Tea)
  │                  ├─► Agent loop ──► internal/agent/
  │                  │                   ├─► LLMClient (provider routing)
  │                  │                   ├─► Tool execution ──► internal/tool/
  │                  │                   └─► Session persistence
  │                  ├─► Panels (files, git, debug, editor)
  │                  └─► Selection, scrollbar, keyboard
  │
  ├─► Run Mode ──► internal/runcli/
  │                  └─► Agent loop (headless)
  │
  ├─► Serve Mode ──► internal/server/
  │                   └─► HTTP API
  │
  └─► MCP Mode ──► internal/mcpcli/
                    └─► MCP protocol
```

---

*Generated by the `team-onboarding` skill. Last updated: 2025-06-03*
