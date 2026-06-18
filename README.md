# ocode

**The fastest, lightest AI coding agent in your terminal.** A single static Go binary — under 50MB RAM, zero runtime dependencies, instant startup.

> Started as an opencode clone, now diverged. See [Differences from opencode](#differences-from-opencode) for what changed and why.

[![Go Version](https://img.shields.io/badge/Go-1.26.1-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## Why ocode?

### 🚀 50× lighter than the alternatives
JS-based coding agents routinely consume 500MB+ just to sit in a terminal. ocode is a **statically compiled Go binary** that sips 30–50MB of memory — even with a 1000-message conversation. Faster startup, lower overhead, more room for your actual work. No npm, no Bun, no node_modules.

### ⚡ Sub-millisecond transcript rendering
Our custom **FastViewport** component renders 1000 message pairs in **0.73ms** — a 41× improvement over the standard bubbles viewport. While other agents stutter on long conversations, ocode stays buttery smooth.

### 🧠 Multi-provider, multi-model, zero lock-in
OpenAI, Anthropic, Google Gemini, Zhipu Z.AI, Alibaba, GitHub Copilot — bring your own model or use the one best suited to the task. Switch mid-conversation with `/model`. Use a cheap model for compaction and a powerful one for code. No vendor lock-in, no gatekeeping.

### 🔒 Permissions you can trust
First-class permission modes (`normal` / `yolo` / `locked`) with per-tool rules, bash-prefix granularity, scope confinement, and an optional **LLM auto-permission model** that makes smart allow/deny decisions so you stay in flow. The advisor module catches risky operations before they happen. No silent `rm -rf`.

### 🔧 Extensible by design
A clean Go package architecture makes it trivial to add providers, tools, plugins, commands, and skills. The skill ecosystem, plugin registry, and custom command loader mean ocode grows with your workflow — not the other way around.

---

## Quick Start

```bash
# Build and run — that's it
go build -o ocode .
./ocode
```

- **Setup:** See [SETUP.md](SETUP.md) for prerequisites, installation, and configuration
- **Contributing:** See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and PR guidelines
- **Testing:** See [TESTING.md](TESTING.md) for feature coverage and known issues

---

## Features

### 💬 Chat & Agent Loop

| Feature | Detail |
|---------|--------|
| **Multi-Provider LLM** | OpenAI, Anthropic (Claude thinking/extended thinking), Google Gemini, Z.AI (GLM), Alibaba (Qwen), GitHub Copilot |
| **Extended Thinking** | Toggle thinking budget on Claude models via `Alt+T` (off/low/med/high) |
| **Prompt Caching** | Anthropic `cache_control` markers on system messages and large tool results; OpenAI server-side caching |
| **Context-Aware Compaction** | Ratio-triggered automatic summarization with custom model support, anchor-update across multiple compactions, `safeCut` invariant, and `max_summary_input_tokens` batching |
| **Custom Compaction Model** | Offload summarization to a cheap/fast model while chatting on a powerful one — configured in `ocodeconfig.json` |
| **Async Sub-Agent Runs** | Launch background agents with transcript capture, process registry, and detail-view drill-in |
| **Undo/Redo** | Session history undo/redo stack for file changes |
| **Slash Command Queue** | Commands entered while streaming are queued and drained automatically — only `/exit` bypasses |

### 🔧 Tool System

| Tool | Description |
|------|-------------|
| `read`, `write`, `edit`, `delete` | Full file I/O with path confinement and permissions |
| `bash` | Shell execution with background support, circular output buffer (256KB), and `Ctrl+B` to foreground→background |
| `grep`, `glob`, `repo_overview` | Advanced search and repository analysis |
| `lsp` | Go-to-definition, hover docs, symbol search, diagnostics |
| `agent` | Delegate work to sub-agents with permission isolation |
| `websearch`, `webfetch` | Web search and page fetching via DuckDuckGo |
| `question` | Interactive user prompts with selectable options |
| `apply_patch` | Structured multi-file diff patching |
| `skill` | Load skill definitions on demand |
| `plan`, `todo` | Project planning and todo list management |

### Git Integration

Full git capability built into the TUI — no context-switching to a separate tool:

| Feature | Shortcut |
|---------|----------|
| **Status & Diff** | Real-time diff with line numbers and soft-wrap |
| **Stage / Unstage / Discard** | `Ctrl+S` / `Ctrl+U` / `Ctrl+D` |
| **AI Commit Messages** | `Ctrl+G` auto-generates from staged changes (configurable model) |
| **Push / Pull / Fetch** | `Ctrl+O` / `Ctrl+P` / `Ctrl+G` |
| **Branch Management** | Create, delete, checkout branches |
| **Stash Operations** | Stash, apply, list stashes |
| **AI Code Review** | `/review` reviews working directory, files, commits, branches, or GitHub PRs |

### 📁 File Browser

A full-featured tree-based file explorer with:

- **Tree navigation** with vertical scrollbar and scrollbar drag support
- **Preview panel** with syntax-highlighted content
- **Inline vim-style editor** (`i` insert, `:w`, `:q`, `:wq`)
- **External editor** with tmux-split, tmux-window, or plain exec modes
- **Content search** across files with incremental streaming results and `Ctrl+F`
- **Fuzzy file finder** overlay with `Ctrl+G`
- **In-file search** with highlighted matches and `n`/`N` jumping
- **Hidden files toggle** (`Ctrl+H`) — hidden entries visually dimmed
- **Multi-select delete** with depth-sorted ordering (children before parents)
- **Binary file detection** — auto-routes to system opener
- **Double-click directory** opens in OS file explorer
- **Rename overwrite confirmation** — double-enter to confirm

### 🎨 Theme System

- **Built-in themes:** Tokyo Night, Tokyo Night Storm, Catppuccin Mocha
- **JSON theme loading** — drop custom themes on disk, no recompilation needed
- **Live theme preview** in the picker as you type
- **Theme sync endpoint** — Web UI maps theme colors to CSS variables
- **`/theme`** command to switch instantly

### 🧩 MCP Client

- **Local + Remote MCP servers** with full lifecycle management
- **OAuth support** for remote server authentication
- **CLI management commands:** `/mcp list`, `/mcp enable`, `/mcp disable`, `/mcp-auth`
- **Configurable timeouts** per server

### 🪢 Plugin System

See **[docs/plugins.md](docs/plugins.md)** for the complete reference.

- **Provider plugin interface** for custom model backends
- **Codex plugin** — OpenAI Codex with browser + device auth flows
- **Plugin registry** with `list`, `install`, `update`, `remove`, `enable`, `disable`, `sync` operations
- **Custom command loader** — plugins can register new slash commands from Markdown files
- **Custom tools** — plugins can provide executable tools in a `tools/` directory
- **LLM instructions** — plugins inject context into the system prompt
- **MCP auto-registration** — plugins can register and unregister MCP servers on install/remove

### 🎯 Skills

- **On-demand skill loading** — skills are lightweight SKILL.md definitions loaded when relevant
- **`/skills`** command to browse and activate
- **`/learn`** command to list project-root skills and guide skill creation/update
- **Skill installer** with status detection
- **Custom skill creation** via the skill-creator skill

### 🧠 LSP Integration

- **Eager server warm-up** — language servers start at app init based on project file extensions
- **Two-phase lifecycle** (`starting` → `ready`) with sidebar status display
- **Go-to-definition**, hover documentation, symbol search
- **Diagnostics** with error/warning counts per file
- **Install hints** — actionable instructions when LSP binary is missing
- **Sidebar telemetry** and debug log entries for LSP events

### 🔐 Permissions System

| Mode | Behavior |
|------|----------|
| **Normal** | Follow tool & bash-prefix rules; project-confined writes auto-allowed; delete, shell, network, delegation ask |
| **YOLO** | Allow permission-gated tools without prompting (respecting hard safety blocks) |
| **Locked** | Read/search-only — no mutations |

- **Per-tool rules:** `allow` / `ask` / `deny` for every tool
- **Bash prefix rules:** Granular two-word subcommand control (e.g. `git push` allowed, `git push --force` always asks)
- **Path scope confinement** with tilde expansion and temp-directory auto-allow
- **LLM auto-permission** — configure a fast model to handle allow/deny decisions autonomously
- **Advisor module** — catches risky operations with configurable strictness
- **`/permissions`** command to view/set rules interactively

### 🖱️ TUI Mouse Support

- **Clickable chrome** — tabs, sidebar buttons, file tree, menus
- **In-app text selection** — click-drag to select content, auto-copies to clipboard
- **Hover effects** — underline-on-hover for clickable paths and UI elements
- **Scrollbar click + drag** — mouse-driven scrolling on all scrollable surfaces
- **Clickable file paths** — file-path tokens in transcript open in `$EDITOR`
- **`MouseModeAllMotion`** enables hover without sacrificing text selection

### 🖥️ Web UI (Beta)

A React-based web interface that mirrors the TUI experience:

| Feature | Status |
|---------|--------|
| **Chat with streaming** | ✅ Live agent responses with markdown, syntax highlighting, code blocks |
| **File browser** | ✅ Tree-based with preview and editing |
| **Git panel** | ✅ Real diff rendering with file status |
| **Logs panel** | ✅ SSE-backed live log streaming with reconnection |
| **Session management** | ✅ Resume, history, routing |
| **Model selection** | ✅ Model dialog with main/small/advisor tabs |
| **Permissions dialog** | ✅ Interactive allow/deny prompts |
| **Remote control** | ✅ `/rc` command mirrors TUI session to browser in real time |
| **Live status panel** | ✅ Real-time model, context, LSP, spending, modified files |
| **File uploads** | ✅ Drag-and-drop or `/upload` command to set directory |
| **Web shell** | ✅ `!` prefix runs local shell commands inline |
| **Mobile sidebar** | ✅ Overlay with backdrop on viewports < 768px |
| **Theme sync** | ✅ CSS variables auto-mapped from TUI theme |
| **Agent cowork panel** | ✅ Parallel agent monitoring sidebar |
| **Slash commands** | ✅ Autocomplete popup with keyboard navigation |

### 🎮 Slash Commands

Type `/` in the chat input to open the palette. Commands execute inline or via `ocode run -command <name>`:

| Command | Aliases | Purpose |
|---------|---------|---------|
| `/model` | `/m` | Switch LLM provider/model with fuzzy search |
| `/advisor` | | Set the advisor model for strategic guidance |
| `/compact` | `[focus]` | Manually compact context; optional focus guides summary |
| `/review` | | AI code review of working dir, file, commit, branch, or PR |
| `/standup` | `/catchup` | Caveman summary of recent commits + pending changes, with sorted TODOs and missed stubs |
| `/session` | `/s`, `/resume` | List, pick, resume sessions |
| `/export` | | Export session as JSON (full transcript) |
| `/export-claude` | | Export in Claude Code compatible format |
| `/share` | | Generate shareable session link |
| `/rc` | `/remote-control` | Start/stop web UI (`/rc off`) to mirror this session |
| `/cd` | `/cwd` | Change the project root to another directory |
| `/context` | | Show context window token budget and system prompt |
| `/upload` | `/uploads` | Show or set the file upload directory |
| `/search` | `/find` | Find a message by keyword (opens the in-chat find bar) |
| `/add-dir` | `/add-dirs` | Add a directory to extra allowed paths |
| `/ide` | | Connect to VS Code (Claude Code extension) |
| `/theme` | `/themes` | Switch themes instantly |
| `/permissions` | | View/set tool and bash permissions |
| `/yolo` | | Toggle YOLO mode on/off |
| `/git` | | Git operations from command line |
| `/github` | | PR, issue, and workflow commands |
| `/plugin` | | Plugin management (install, sync, list, etc.) |
| `/skills` | | Browse available skills |
| `/learn` | | List project-root skills and guide skill creation/update |
| `/undo` | `/redo` | Undo/redo file changes |
| `/lsp` | | LSP diagnostics and status |
| `/mcp` | `/mcp-auth` | MCP server management |
| `/editor` | `/editor-mode` | External editor configuration |
| `/small-model` | | Switch the small model for lightweight tasks |
| `/usage` | | LLM token usage by model and date range |
| `/new` | `/clear` | Start a fresh conversation |
| `/help` | `/?` | Show all available commands |

### 🔧 IDE Integration

- **VS Code `/ide`** — Lock discovery, WebSocket + MCP client, selection/open-editor/mention streaming, auto-attach
- **IDE status chip** in sidebar — shows connection state
- **IDE mode config** — toggle via sidebar click or `/ide status`

### 📊 Debug & Observability

- **Debug log panel** — filterable by kind (agent, tool, LSP, git, auth, MCP, plugin)
- **Sidebar telemetry** — context window usage, model info, token counts
- **Token usage tracking** — `/usage` command with per-model, per-date-range breakdown
- **LLM costing** — pricing module tracks spend across providers
- **Session export** — full JSON transcripts for debugging or migration

### 🛠️ Background Processes

- **Foreground → background** — `Ctrl+B` during a running bash tool moves it to the background, freeing the agent to continue
- **256KB circular output buffer** — tail long-running processes
- **Lifecycle tracking** — `bash_output`, `agent_status`, `task_status`, `wait` tools
- **Sub-agent process registry** — nested run tree with detail-view drill-in

---

## Config

Configuration lives in two files, loaded from `~/.config/opencode/` and the nearest project root:

| File | Role |
|------|------|
| **`opencode.json`** | Upstream-compatible settings (provider creds, model prefs). **Read-only** — ocode never writes to it. Can be checked into git. |
| **`ocodeconfig.json`** | ocode-only state (permissions, editor, compaction, model history). **Written by ocode** to persist runtime state. `.gitignore`-friendly. |

### Quick config examples

```jsonc
// ocodeconfig.json — compaction with a separate summary model
{
  "compact": {
    "enabled": true,
    "summary_provider": "anthropic",
    "summary_model": "claude-haiku-4-5",
    "token_threshold": 0.75,
    "keep_recent_turns": 3,
    "summary_timeout_seconds": 30,
    "summary_max_retries": 1,
    "max_summary_input_tokens": 50000
  }
}
```

```jsonc
// ocodeconfig.json — auto-permissions with a fast judge model
{
  "permissions": {
    "auto_permission_model": "deepseek:deepseek-v4-flash",
    "auto_permission_deny_unsafe": true,
    "mode": "normal",
    "tools": {
      "read": "allow",
      "write": "allow",
      "bash": "ask"
    },
    "bash": {
      "prefixes": { "git": "allow", "make": "ask", "rm": "deny" }
    }
  }
}
```

---

## Differences from opencode

ocode shares opencode's overall shape (TUI agent, multi-provider, MCP, sessions) but diverges deliberately in language, architecture, and feature set.

### Language and runtime

| Area | opencode | ocode |
|------|----------|-------|
| Language | TypeScript + Bun + Effect | **Go 1.26.1** |
| TUI | Solid-based custom renderer | **Bubble Tea / Lipgloss** |
| Distribution | npm + Bun runtime | **Single static binary** |
| Memory | 500MB+ typical | **30–50MB typical** |
| Transcript render | O(N) per frame | **O(1) FastViewport (41× faster)** |

### Permissions

ocode adds **first-class permission modes** (`normal`, `yolo`, `locked`) with per-tool rules, bash-prefix granularity, scope confinement, path expansion, and LLM-driven auto-permission decisions — stored in `ocodeconfig.json`. opencode handles permissions inline with less granularity.

### Sessions

- ocode can **list, pick, and resume** opencode sessions **and** Claude Code sessions (cloned as `claude-<id>`)
- Auto-save, session picker, `Ctrl+Y` retry on LLM timeout
- Session pagination and delete support

### TUI features unique to ocode

- **FastViewport** — custom 0.73ms transcript rendering vs standard viewport
- **Extended thinking toggle** on Claude models (`Alt+T`)
- **Foreground bash → background** (`Ctrl+B`)
- **Inline vim-like editor** in Files tab
- **Async sub-agent runs** with drill-in detail view
- **ModalStack** — composable overlay system (permission dialogs, pickers, dialogs, list boxes)
- **In-app text selection** — click-drag copies, plain click activates
- **Hover effects** on all clickable surfaces
- **File tree scrollbars** with drag support

### Compaction

| Aspect | opencode | ocode |
|--------|----------|-------|
| Custom summary model | No — uses session model | **Yes** — separate `summary_provider`/`summary_model` |
| Tool-pair safety | Implicit | **Explicit `safeCut`** — proven symmetry on both sides |
| Markers | Typed message part | **Sentinel-tagged system message** `[ocode:compaction-summary]` |
| Batching | Single pass | **Multi-batch** when middle exceeds `max_summary_input_tokens` |

### Added in ocode (not in opencode)

- **Theme system** with JSON loading and live preview
- **Plugin system** with provider interface and registry
- **Skill system** with installer and CLI
- **VS Code `/ide` integration** with lock discovery
- **Web UI** with `/rc` remote control, Git panel, Logs panel, theme sync
- **AI code review** (`/review`) for working dir, commits, branches, PRs
- **GitHub integration** — PR, issue, workflow commands
- **LSP eager warm-up** with sidebar status, install hints
- **`/usage`** — per-model token cost tracking
- **File content search** with incremental streaming
- **Custom command loader** from plugin Markdown files
- **Debug log panel** with filtered kinds and word-wrap
- **Session pagination, delete, and share**
- **`commit_msg_model`** config for AI commit message generation
- **Safe built-in deny rules** for `git push --force` and `rm -rf`
- **Progressive file preview** with binary detection

### What ocode does **not** have (vs opencode)

- No desktop frontends — terminal and web UI only
- No plugin marketplace (plugin system is Git-based)
- Smaller skill ecosystem
- No `plan-reminder` / `build-switch` prompt overlays

---

## Stack

- **Go 1.26.1** — single static binary, zero runtime deps
- **Bubble Tea / Bubbles / Lipgloss** — Charm ecosystem for TUI
- **React + shadcn/ui + Tailwind** — Web UI frontend
- **FastViewport** — custom O(1) transcript viewport
- **MCP** — Model Context Protocol client (local + remote)

---

## Layout

```
main.go                    Entry point
internal/acp/              Agent Client Protocol server (Zed / ACP integration)
internal/agent/            LLM client, agent registry, permissions, tool truncation
internal/auth/             Multi-provider OAuth + keychain
internal/commands/         Custom command loader
internal/config/           Config loading (opencode.json / ocodeconfig.json)
internal/debuglog/         Shared debug log for TUI and non-TUI consumers
internal/github/           GitHub API client (PR, issues, workflows)
internal/hooks/            Git hooks integration
internal/ide/              VS Code / IDE lock discovery and client
internal/lsp/              Language server manager with eager warm-up
internal/mcp/              MCP client (local + remote)
internal/mcpcli/           MCP CLI integration
internal/models/           Model registry and pricing
internal/pathscope/        Path scope utilities (temp dirs, expansion)
internal/plugin/           Plugin provider interface and registry
internal/plugins/          Plugin loader, manager, sync
internal/pricing/          LLM token cost tracking
internal/runcli/           Headless CLI mode
internal/server/           HTTP server (web UI, APIs, SSE, /rc)
internal/session/          Session management (save, resume, export, migrate)
internal/skill/            Skill loader and installer
internal/snapshot/         Session snapshotting
internal/theme/            Theme system (JSON loading, definitions)
internal/tool/             Built-in tools (read, write, edit, bash, grep, glob, LSP, etc.)
internal/tui/              Bubble Tea TUI (model, view, update, components)
internal/usage/            Token usage tracking
internal/version/          Version info
docs/                      Design specs, plans, and enhancement proposals
```

---

## Disclaimer

### `/mask` — Secret Redaction

ocode includes a **secret redaction system** (`/mask`) that detects and masks common credential patterns (API keys, tokens, private keys, etc.) before they are sent to the LLM provider. It supports tier-1 regex detection, tier-2 local model scanning, and custom words in a local vault.

#### `/mask mode` — Controlling LLM Scan Aggressiveness

The `/mask mode` command controls how aggressively the tier-2 LLM scanner is invoked on typed user messages:

| Surface | lenient (default) | full |
|---------|-------------------|------|
| Typed user message | tier-2 LLM only if input contains a sensitive keyword or a known value-pattern (QuickScan) | tier-2 LLM **always** |
| Sensitive file read (`.env`, `*.pem`, …) | tier-2 **LLM** always | tier-2 **LLM** always |
| Other tool results (DB/bash/normal reads) | chat-mode **regex** only (no LLM) | chat-mode **regex** only (no LLM) |
| All messages, every step | tier-1 regex safety net (unchanged) | tier-1 regex safety net (unchanged) |

- **Mode** governs only the *typed user message* aggressiveness.
- **Sensitive file reads** (`.env`, `.pem`, `id_rsa*`, etc.) always use the LLM in both modes — these files often contain values without known formats that only the LLM catches.
- **DB/bash output** uses fast keyword+entropy regex only (no LLM). Known gaps: a value after a keyword (`password`, `secret`, …) is only flagged when it is high-entropy, so low-entropy/dictionary passwords (`password=hunter2`) and values containing shell metacharacters (e.g. `$`) are missed, as is tabular output with no `=`/`:` delimiter (e.g. `| password | hunter2 |`).
### `/mask model` — Configuring the Tier-2 Scanning Model

The `/mask model [name]` command sets the local LLM used for tier-2 contextual secret scanning.

- **Provider auto-detection:** when you select a model from a known local provider (e.g. `lmstudio/ternary-bonsai-8b-mlx`), the scanner's `base_url` is automatically set to the provider's default endpoint (`http://localhost:1234/v1` for LM Studio). The persisted/display name is normalized to `lmstudio/<name>` even if you typed a bare model id, and the scanner strips the prefix when sending the request. This matches how the main `/model` command works — no manual URL configuration needed.
- **Manual override:** set a custom `base_url` by editing `security.redaction.base_url` in your config (e.g. `"base_url": "http://localhost:11434"` for Ollama). Once manually set, the auto-detection is skipped for your custom URL.
- **Security:** only local endpoints (localhost, 127.0.0.1, ::1) are accepted by default. To allow a remote endpoint for the tier-2 scanner, set `security.redaction.allow_remote_tier2: true` in your config.
- **No model configured:** if redaction is enabled but no tier-2 model is set, scanning is regex-only (tier-1 + chat-mode tool-result regex). Set a model with `/mask model` to enable LLM tier-2.

**However, no automated system is perfect.** While we actively work to improve coverage, the redactor may occasionally miss secrets, especially non-standard formats, user-specific tokens, or credentials embedded in unusual contexts. **The `/mask` feature is a best-effort safeguard — it does not guarantee 100% prevention of secret exposure.** Always review what you share with LLM providers and rotate credentials regularly.

---

## License

MIT
