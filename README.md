# ocode

**The fastest, lightest AI coding agent in your terminal — and on your desktop.** A single static Go binary — under 50MB RAM, zero runtime dependencies, instant startup. The same agent powers the TUI, a React web UI, and a native Wails desktop shell.

> Started as an opencode clone, now diverged. See [Differences from opencode](#differences-from-opencode) for what changed and why.

> **Built with the Tencent HY3 model** — this repository was developed and maintained with assistance from Tencent's HY3 large language model.

[![Go Version](https://img.shields.io/badge/Go-1.26.1-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Documentation](https://img.shields.io/badge/Docs-GitHub%20Pages-blue?logo=readthedocs)](https://u007.github.io/ocode-docs/)

> 📚 **Full documentation at [u007.github.io/ocode-docs](https://u007.github.io/ocode-docs/)** — TUI/web guides, config, tools, slash commands, screenshots. Source at [u007/ocode-docs](https://github.com/u007/ocode-docs).

---

## Why ocode?

### 🚀 50× lighter than the alternatives
JS-based coding agents routinely consume 500MB+ just to sit in a terminal. ocode is a **statically compiled Go binary** that sips 30–50MB of memory — even with a 1000-message conversation. Faster startup, lower overhead, more room for your actual work. No npm, no Bun, no node_modules.

### ⚡ Sub-millisecond transcript rendering
Our custom **FastViewport** component renders 1000 message pairs in **0.73ms** — a 41× improvement over the standard bubbles viewport. While other agents stutter on long conversations, ocode stays buttery smooth.

### 🧠 Multi-provider, multi-model, zero lock-in
OpenAI, Anthropic, Google Gemini, Zhipu Z.AI, Alibaba, GitHub Copilot, Novita AI, OpenRouter, OrcaRouter, AIHubMix — bring your own model or use the one best suited to the task. Switch mid-conversation with `/model`. Use a cheap model for compaction and a powerful one for code. No vendor lock-in, no gatekeeping.

### 🔒 Permissions you can trust
First-class permission modes (`normal` / `yolo` / `locked`) with per-tool rules, bash-prefix granularity, scope confinement, and an optional **LLM auto-permission model** that makes smart allow/deny decisions so you stay in flow. The advisor module catches risky operations before they happen. No silent `rm -rf`.

### 🔧 Extensible by design
A clean Go package architecture makes it trivial to add providers, tools, plugins, commands, and skills. The skill ecosystem, plugin registry, and custom command loader mean ocode grows with your workflow — not the other way around.

---

## Quick Start

```bash
# Build and run — that's it
go build -o ocode .
./ocode                 # TUI (default)
./ocode serve           # HTTP server + web UI
./ocode serve --open    # server + open browser
./ocode run "fix tests" # headless / CI
./ocode acp             # Zed editor integration
```

| Entry point | What it does |
|-------------|--------------|
| `ocode` | Interactive Bubble Tea TUI — six tabs, slash commands, streaming agent loop |
| `ocode serve` / `ocode web` | In-process HTTP server + React SPA + SSE event bus; `web` is `serve --open` |
| `ocode run [flags] [prompt]` | Headless non-interactive run (CI, scripts, cron jobs) |
| `ocode acp` | Agent Client Protocol server over stdio for Zed |
| `bin/ocode-desktop` | Native Wails v3 desktop shell — same web UI, plus tray/dock/notifications |

- **Setup:** See [SETUP.md](SETUP.md) for prerequisites, installation, and configuration
- **Contributing:** See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and PR guidelines
- **Testing:** See [TESTING.md](TESTING.md) for feature coverage and known issues
- **Man pages:** `docs/man/ocode*.1`

---

## Releases

Pre-built binaries and installers are available in the [Releases folder](https://drive.google.com/drive/folders/168zPGETI9ofdENAOO9k9Z3XKAGOyylzC?usp=sharing) on Google Drive.

---

## Features

### 💬 Chat & Agent Loop

| Feature | Detail |
|---------|--------|
| **Multi-Provider LLM** | OpenAI, Anthropic (Claude thinking / extended thinking + prompt caching), Google Gemini, Z.AI (GLM), Alibaba (Qwen), GitHub Copilot, Novita AI, OpenRouter, OrcaRouter, AIHubMix, Zhipu, DeepSeek (opencode-go route), Minimax |
| **Live Model Resolution** | OpenRouter and Novita AI models are resolved from each provider's live API at startup (30s TTL cache, graceful fallback to the static registry) so context sizes, pricing, and vision support are accurate for models absent from or renamed in models.dev (e.g. `openrouter/tencent/hy3:free`) |
| **Model Display Names** | `agent.ModelDisplayName` surfaces models.dev `name`; TUI picker shows `id — Name`, web `/api/models` exposes `display_name` |
| **Reasoning Effort** | Toggle thinking budget on Claude models via `Alt+T` (off/low/med/high/xhigh/max) or `/effort`; per-turn effort via `ocode run --effort` |
| **Prompt Caching** | Anthropic `cache_control` markers on system messages and large tool results; OpenAI server-side caching. Tools → system → messages prefix order is cache-stable. |
| **Context-Aware Compaction** | Ratio-triggered automatic summarization with custom model support, anchor-update across multiple compactions, `safeCut` invariant, and `max_summary_input_tokens` batching |
| **Custom Compaction / Recap Models** | Offload summarization to a cheap/fast model while chatting on a powerful one — `compact.summary_provider/model` and `recap` model in `ocodeconfig.json`; `/recap` to drive manually |
| **Session Title Generation** | LLM-generated titles with race guard; `/title` to set/reset |
| **Async Sub-Agent Runs** | Launch background agents with transcript capture, process registry, detail-view drill-in, and a DAG scheduler (`id` / `depends_on`) that streams predecessor output into dependents |
| **Task Contracts** | Optional `expected_output` per `task` dispatch — small-model verifier checks shape and retries once in place |
| **Slash Command Queue** | Commands entered while streaming/compacting are queued and drained automatically — only instant UI commands bypass (see [Slash Commands](#-slash-commands)) |
| **Persistent Todo Plans** | `todowrite` / `todoread` / `todo_update` backed by `.ocode/todo/<session>.md` (revision + flock, snapshot-captured, re-anchored every turn) |
| **In-Chat Find Bar** | `Ctrl+F` / `/search` / `/find` on the chat tab |

### 🔧 Tool System — 39 Built-ins

| Tool | Description |
|------|-------------|
| `read`, `write`, `edit`, `delete` | Full file I/O with path confinement and permissions |
| `replace_lines`, `multiedit`, `multi_file_edit`, `apply_patch` | Line-range, multi-edit, multi-file, and structured diff patching |
| `undo_file_change` | Roll back any `write`/`edit`/`multiedit`/`multi_file_edit`/`replace_lines`/`delete` by `tool_call_id` (10-step window; refuses if a newer same-file write would conflict). Backed by `internal/snapshot`. |
| `bash` | Shell execution with background support, circular output buffer (256KB), `Ctrl+B` foreground→background, **live streaming** of combined stdout/stderr into the transcript |
| `bash_output`, `kill_shell`, `list_processes` | Inspect / kill / list background shells |
| `grep`, `glob`, `list`, `repo_overview`, `repo_clone` | Search (ripgrep-backed), file glob, directory listing, repository analysis & cloning |
| `lsp`, `lsp_diagnostics`, `ast`, `ast_grep`, `format` | Go-to-definition / hover / symbol search / diagnostics; semantic `ast` (auto-enabled when any LSP server is on PATH); structural `ast_grep` (opt-in `plugins.ast`); formatter dispatch |
| `agent` / `task` | Delegate work to sub-agents with permission isolation, DAG deps, and contract verification |
| `websearch`, `webfetch` | Web search and page fetching via DuckDuckGo |
| `github_pr`, `github_issue`, `github_workflow` | GitHub PR / issue / workflow inspection |
| `ocr` | Extract text from images (openai-compat, LM Studio native chat, or paddle); reuses stored `auth.json` creds |
| `imagegen` | Generate images via configured provider |
| `cron` | Manage scheduled jobs from the agent (create/list/remove) |
| `plan_enter`, `plan_exit` | Structured planning phase (creates `plan.md`) |
| `todowrite`, `todoread`, `todo_update` | Persistent todo list (see above) |
| `skill`, `load_skill` | Load skill definitions on demand (alias pair) |
| `question` | Interactive user prompts with selectable options (TUI + web bridge) |
| `skill` + `load_skill` + plugin `CustomTool` | Plugins can contribute additional tools from a `tools/` directory |

Tool permissions are enforced per-tool and per bash-prefix (two-word granularity, e.g. `git push` allowed while `git push --force` always asks). See [Permissions](#-permissions-system).

### Git Integration

Full git capability built into the TUI — no context-switching to a separate tool:

| Feature | Shortcut |
|---------|----------|
| **Status & Diff** | Real-time diff with line numbers and soft-wrap |
| **Stage / Unstage / Discard** | `Ctrl+S` / `Ctrl+U` / `Ctrl+D` |
| **AI Commit Messages** | `Ctrl+G` auto-generates from staged changes (configurable `commit_msg_model`) |
| **Push / Pull / Fetch** | `Ctrl+O` / `Ctrl+P` / `Ctrl+G` |
| **Branch Management** | Create, delete, checkout branches |
| **Stash Operations** | Stash, apply, list stashes |
| **AI Code Review** | `/review` reviews working directory, files, commits, branches, or GitHub PRs |

Web parity: the **Git** tab (`GET /api/git/*`) renders the same diff with file status.

### 📁 File Browser

A full-featured tree-based file explorer with:

- **Tree navigation** with vertical scrollbar and scrollbar drag support
- **Preview panel** with syntax-highlighted content (progressive, binary-detected — routes to system opener)
- **Inline vim-style editor** (`i` insert, `:w`, `:q`, `:wq`) and external editor (tmux-split / tmux-window / plain exec via `/editor` + `/editor-mode`)
- **Content search** across files with incremental streaming results and `Ctrl+F`
- **Fuzzy file finder** overlay with `Ctrl+G`
- **In-file search** with highlighted matches and `n`/`N` jumping
- **Hidden files toggle** (`Ctrl+H`) — hidden entries visually dimmed
- **Multi-select delete** with depth-sorted ordering (children before parents)
- **Double-click directory** opens in OS file explorer
- **Rename overwrite confirmation** — double-enter to confirm

Web parity: **Files** top tab (`GET /api/files/tree`, Monaco editor via `GET /api/files/content`, `PUT /api/files/content`, undo/redo).

### 🔄 Changes Tab (Per-Session)

- Lists **every file the current session touched** (main agent + sub-agents), derived from the snapshot store — not `git status`.
- Per-file unified diffs and **file-level or block-level undo**.
- TUI: `internal/tui/changes_model.go` + `internal/snapshot.Store`; Web: `ChangesPanel` via `GET /api/changes` + `POST /api/changes/undo-*`.

### 🎨 Theme System

- **Built-in themes:** Tokyo Night, Tokyo Night Storm, Catppuccin Mocha, LCARS (Star Trek-inspired), Pip-Boy (Vault Boy ASCII)
- **JSON theme loading** — drop custom themes on disk, no recompilation needed
- **Live theme preview** in the picker as you type
- **Theme sync endpoint** — Web UI maps theme colors to CSS variables (`GET /api/themes`, `GET /api/theme`)
- **`/theme`** / **`/themes`** command to switch instantly; `web/src` respects the same tokens

### 🧩 MCP Client

- **Local + Remote MCP servers** with full lifecycle management
- **OAuth support** for remote server authentication (`/mcp-auth`)
- **CLI:** `/mcp list`, `/mcp enable`, `/mcp disable`, `/mcp-auth`; headless `ocode mcp [add|list|ls|auth]`
- **Configurable timeouts** per server (`mcp` in `ocodeconfig.json` / `opencode.json`)

### 🪢 Plugin System

See **[docs/plugins.md](docs/plugins.md)** for the complete reference.

- **Provider plugin interface** for custom model backends (e.g. `internal/plugin/codex`, `grok`)
- **Plugin registry** with `list`, `install`, `update`, `remove`, `enable`, `disable`, `sync` operations (`/plugin`); Git-based (no marketplace)
- **Custom command loader** — plugins can register new slash commands from Markdown files (`internal/commands`)
- **Custom tools** — plugins can provide executable tools in a `tools/` directory
- **LLM instructions** — plugins inject context into the system prompt
- **MCP auto-registration** — plugins can register and unregister MCP servers on install/remove
- **Bundled plugins** under `.opencode/plugins` (embedded in the binary via `go:embed`)

### 🎯 Skills

- **On-demand skill loading** — skills are lightweight `SKILL.md` definitions loaded when relevant; backed by a retrieval corpus (discovery)
- **`/skills`** to browse and activate; **`/learn`** to list project-root skills and guide skill creation/update
- **Skill-as-command** — any installed skill is also available directly as a slash command: `/<skill-name>` (e.g. `/agent-browser`, `/pdf`) loads its `SKILL.md` as the run prompt (extra words become context)
- **`/discover`** — toggle retrieval-based skill/MCP discovery, pick the query-embedding model, manage ignored paths
- **Discovery corpus** — markdown docs + skills indexed via small-model summaries (cached at `.ocode/md-summaries.json`); names-index is system-cached, full content is volatile tail
- **Discovery API:** `GET/PUT /api/config/ocode/discovery` + `GET /api/skills` + `GET /api/commands`
- **Bundled skill library** — ~60+ bundled skills covering Google Workspace (`gws-*`), PDF tooling, context compression (`compress`, `caveman*`), browser automation (`agent-browser`, `htrcli`), web QA/testing (`webapp-qa`, `webapp-testing`), framework migration (`nextjs-to-tanstack`), and skill authoring (`skill-creator`)
- **Skill installer** with status detection (`/plugin`, `skill` tool)

### 🧠 LSP Integration

- **Eager server warm-up** — language servers start at app init based on project file extensions
- **Two-phase lifecycle** (`starting` → `ready`) with sidebar status display (`/lsp`, `GET /api/lsp/statuses`)
- **Go-to-definition**, hover documentation, symbol search, diagnostics (`lsp` + `lsp_diagnostics` + `ast` tools)
- **Shared LSP broker** (`internal/lsp/broker`) — authenticated loopback TCP, bounded framing, multiplexed RPC, optional multi-client sharing (gated)
- **Install hints** — actionable instructions when LSP binary is missing
- **Sidebar telemetry** and debug log entries for LSP events

### 🔐 Permissions System

| Mode | Behavior |
|------|----------|
| **Normal** | Follow tool & bash-prefix rules; project-confined writes auto-allowed; delete, shell, network, delegation ask |
| **YOLO** | Allow permission-gated tools without prompting (respecting hard safety blocks); `ocode --yolo` / `/yolo` |
| **Locked** | Read/search-only — no mutations |

- **Per-tool rules:** `allow` / `ask` / `deny` for every tool
- **Bash prefix rules:** Granular two-word subcommand control (e.g. `git push` allowed, `git push --force` always asks); managed via `/ban` (deny list) and `/permissions`
- **Path scope confinement** with tilde expansion and temp-directory auto-allow; project-scoped `extra_allowed_paths` in `.ocode/settings.json`
- **LLM auto-permission** — configure a fast model to handle allow/deny decisions autonomously (`permissions.auto_permission_model`)
- **Advisor module** — catches risky operations with configurable strictness and `advisor.checkpoints` (`["plan","done"]` by default)
- **`/permissions`** to view/set rules interactively; **`/ban`** is a shortcut for the bash deny list (supports multi-word prefixes, `list`/`add`/`remove`/`clear` with confirmation)
- **Web parity:** `PermissionsForm` + `PermissionDialog` + `POST /api/permissions/resolve` + `POST /api/rc/permission/resolve`

### 🖱️ TUI Mouse Support

- **Clickable chrome** — tabs, sidebar buttons, file tree, menus
- **In-app text selection** — click-drag to select content, auto-copies to clipboard (`selection.go` + `clipboard.WriteAll`)
- **Hover effects** — underline-on-hover for clickable paths and UI elements (`MouseModeAllMotion`)
- **Scrollbar click + drag** — mouse-driven scrolling on all scrollable surfaces
- **Clickable file paths & URLs** — file-path tokens open in `$EDITOR`; URLs show a Y/N confirmation dialog before opening (`url_dialog.go`)
- **Clickable status/activity rows** clamped to one line so long content can't warp the layout

### 🖥️ Web UI

A React + shadcn/ui + Tailwind SPA that mirrors the TUI experience, served by the same Go binary (`internal/server` + `web/` embed). All state streams over SSE (`GET /api/events` with 20s `: ping` keepalive, multiproject tagged bus).

| Surface | What it does | Key files / API |
|---------|--------------|-----------------|
| **Chat** | Streaming agent responses with markdown, syntax highlighting, code blocks, tool-call cards, thinking blocks, todo plan, clickable paths/URLs | `ChatPanel`, `MessageBubble`, `TurnParts`; `POST /api/chat`, `POST /api/sessions/{id}/message`, `GET /api/chat/stream` |
| **Sessions & Tabs** | Session list/creation/resume, per-session sub-tabs (chat/agents), open-session bar, tab hierarchy, pagination & delete & share | `SessionSubTabs`, `OpenSessionBar`, `SessionTabSync`; `GET /api/sessions`, `GET /api/sessions/{id}/state`, `POST /api/sessions/{id}/compact` |
| **File Browser** | Tree + Monaco editor (uncontrolled, `defaultValue` + `lastEmittedRef`), preview, open/save/undo/redo | `FileTree`, `FileEditor`, `EditorTabBar`; `GET /api/files/tree`, `GET/PUT /api/files/content` |
| **Changes** | Per-session diff list with file/block undo | `ChangesPanel`; `GET /api/changes`, `POST /api/changes/undo-*` |
| **Git** | Repo-wide status & diff | `GitPanel`; `GET /api/git/*` |
| **Terminal** | xterm.js tabs over WebSocket, process list, scrollback, font/shell config | `TerminalTabs`, `TerminalPanel`; `GET /api/terminal/ws`, `GET /api/terminal/processes` |
| **Cron / Scheduler** | Scheduled jobs management (list/create/remove, outbox, targets) | `CronPanel`; `GET/POST /api/cron/*` |
| **Assets / Uploads** | Drag-and-drop uploads, upload dir, image preview | `AssetsPanel`; `POST /api/uploads`, `/api/files/open` |
| **Agents** | Parallel agent monitoring, runs stream | `AgentsPanel`; `GET /api/agents/runs/stream` |
| **Logs** | SSE-backed live log streaming with reconnection, retention cap, background buffering toggle | `LogPanel`, `LogsForm`; `GET /api/logs/stream`, `GET /api/logs` |
| **Settings** | 20-section settings overlay (profiles, models, compact, permissions, security, terminal, logs, OCR, discovery, TUI, editor, paths, features, limits, imagegen, plugins, themes, MCP) | `SettingsPanel`; `GET/PUT /api/config/ocode/*`, `/api/config/*` |
| **Model Selection** | Model dialog with main/small/advisor tabs, display names | `ModelDialog`; `GET /api/models` |
| **Permissions / Questions** | Interactive allow/deny + question prompts, server-bridged | `PermissionDialog`, `QuestionDialog`; `POST /api/permissions`, `/api/questions` |
| **Remote Control** | `/rc` mirrors a TUI session to the browser in real time (multiproject event bus) | `POST /api/rc/*`, `GET /api/events` |
| **Slash Commands** | Autocomplete popup with keyboard nav, merges `GET /api/commands` + `GET /api/skills` | `ChatInput`, `CommandPalette` |
| **Live Status** | Real-time model, context, LSP, spending, modified files | `StatusPanel`, `CoworkSidebar`; `GET /api/tui-status`, `/api/spending` |
| **Web Shell** | `!` prefix runs local shell commands inline with live stdout/stderr streaming | `POST /api/shell` |
| **Theming** | CSS variables auto-mapped from TUI theme | `GET /api/themes`, `GET /api/theme` |
| **Mobile** | Overlay sidebar with backdrop on viewports < 768px | `App.tsx` `matchMedia` |

### 📚 Knowledge Bundle (`/docs` — OKF v0.1)

An optional **OKF (Open Knowledge Format)** knowledge bundle at `docs/` that the agent curates automatically. When active, the agent's system prompt includes a `[ocode:knowledge]` index and a `knowledge_lookup` tool for semantic retrieval.

| Command | Purpose |
|---------|---------|
| `/docs on` / `/docs off` | Toggle the knowledge system flag |
| `/docs init` | Create bundle marker (`docs/index.md` + `docs/log.md`) + dispatch the `context` subagent to annotate existing docs |
| `/docs status` | Show bundle presence, doc counts, last log entry |
| `/docs update [focus]` | Force a maintenance pass (scan for staleness, duplicates) |
| `/docs cleanup [--yes]` | List deprecated docs; `--yes` confirms deletion |

Knowledge curation happens via the dedicated `context` subagent (the sole automated writer), which verifies doc claims against code before writing and deprecates rather than deletes. The main agent never gains `doc_write` or `doc_deprecate` tools. Docs: `docs/knowledge-bundle.md`, `docs/log.md`.

### 🗓️ Scheduled Jobs / Cron

Persistent, disk-backed cron engine + headless agent dispatcher. Jobs fire in the **long-lived host** (`ocode serve` / `ocode web` / desktop), not in the TUI.

- Create cron or one-shot jobs that run a prompt as a headless agent; results land in the outbox.
- TUI: `/cron [list|describe <id>|remove <id>|add <kind> <args> <message...>]`
- Web: **Cron** top tab (`CronPanel`, `CronJobDialog`, `CronOutboxPanel`, `CronTargetsPanel`) via `GET/POST /api/cron/*`
- Agent tool: `cron` (create/list/remove from within a turn)
- Storage: `scheduler.DefaultStorePath` (project-scoped); runner is `internal/server/scheduler_runner.go` / `internal/desktop/scheduler.go`
- Docs: `docs/scheduled-jobs.md`

### 🪟 Desktop App

A native desktop shell (`cmd/ocode-desktop`) that opens the same web UI in a Wails v3 window over an **in-process server** — no separate `ocode serve` step. The server binds `127.0.0.1` on a random port with a fresh per-launch auth token; all app logic stays behind the same HTTP/SSE API the browser uses.

**Native extras:**

- **System tray** — Show / Quit (`internal/desktop` + `cmd/ocode-desktop/native.go`)
- **Dock badge** with the running-agent count (macOS/Windows; Linux has no badge)
- **Finished/failed-run notifications** when the window is unfocused (clicking one focuses the window) — driven by `internal/desktop/watch.go` poll-and-diff watcher keyed by session+run
- **Custom app menu** — `Cmd+Q` gets a native "Quit ocode?" confirmation, while `Cmd+W` is deliberately left unbound so it falls through to the webview and closes the active session tab instead of quitting
- **Native error dialog** if the in-process server fails to boot (stderr is invisible from a Finder-launched `.app`)
- **Finder/Dock launch anchoring** — working directory anchored to `$HOME` instead of `/`, and that project's `opencode.json` is reloaded so provider overrides and file trees resolve correctly
- **Cron host** — scheduled jobs run inside the long-lived desktop host (`internal/desktop/scheduler.go`); manage them from the web UI's Cron panel
- **Embedded assets** — the React SPA (`web.FS()`), bundled skills, bundled plugin manifests, and bundled model configs are embedded via `go:embed` so the desktop binary is self-contained
- **Dev hot-reload** — set `OCODE_DESKTOP_DEV_URL` to your Vite dev server; the API still runs in-process and its address + token are logged on startup for the Vite `/api` proxy
- **Window lifecycle** — Wails v3 `application` + `dock` + `notifications`; `StartServer` (`internal/desktop/boot.go`) picks a sticky random loopback port, mints a 16-byte hex token, and exposes `Handle{URL,Token,Srv}`

```bash
make desktop        # build bin/ocode-desktop (requires cgo + platform SDK)
make desktop-app    # macOS: bundle bin/ocode.app via scripts/bundle-macos.sh
```

Platform prerequisites: macOS — Xcode Command Line Tools; Linux — `webkit2gtk-4.1-dev`, `libgtk-3-dev` (no dock badge on Linux); Windows — WebView2 runtime (bootstrapped by Wails). Pinned to `github.com/wailsapp/wails/v3 v3.0.0-alpha2.111` (alpha — API may drift).

The macOS bundle is unsigned (Gatekeeper prompts on other machines); signing/installers are tracked in `TODO.md`.

### 🔌 IDE & Editor Integrations

- **VS Code `/ide`** — lock discovery, WebSocket + MCP client, selection/open-editor/mention streaming, auto-attach; sidebar chip shows connection state; toggle via `/ide [claude|off|status]`
- **Zed / ACP (`ocode acp`)** — Agent Client Protocol server over stdio so ocode appears in Zed's agent panel alongside Claude Code / Codex; spec at `docs/acp-zed-spec.md`, guide at `docs/zed.md`
- **External editor** — `/editor [command]` + `/editor-mode [external|tmux-split|tmux-window]`; web Monaco editor with `cursorSmoothCaretAnimation:off` and external-sync guard

### 🤖 Telegram Bot (Optional)

Headless Telegram bridge (`docs/telegram-bot.md`) — run ocode as a Telegram bot that responds to messages via the same agent loop. Configure via env / `ocodeconfig.json`.

### 🎮 Slash Commands — Complete Reference

Type `/` in the chat input to open the palette. Commands execute inline or via `ocode run -command <name>`. Custom plugin commands and skill-as-command entries are merged in automatically.

| Command | Aliases | Purpose |
|---------|---------|---------|
| `/model` | `/models`, `/m` | List and switch LLM provider/model with fuzzy search |
| `/advisor` | | Set the advisor model for strategic guidance; `advisor.checkpoints` config (default `["plan","done"]`, set `[]` to disable) enforces advisor review of the first write batch (reviewed *after* it is applied, so the model never regenerates the write) and of completion claims. A checkpoint shows as a transcript line while it runs, and Escape cancels it |
| `/agents` | | Show active/queued subagents and the concurrency limit, or set it with `/agents limit <n>` (0 = unlimited; persisted to `ocodeconfig.json`) |
| `/agent` | | Switch agent definition (`build`, `plan`, `review`, `debug`, `docs`, …) |
| `/compact` | `[focus]` | Manually compact context; optional focus guides summary |
| `/recap` | `[model|status|enable|disable]` | Summarize conversation / manage recap model |
| `/context` | | Show context window token budget and system prompt, plus Knowledge Bundle, Memory, and Discovery sections |
| `/review` | `[file|commit|branch|pr]` | AI code review with actionable findings |
| `/standup` | `/catchup` | Caveman summary of recent commits + pending changes, with sorted TODOs and missed stubs |
| `/changes` | | Analyze repo changes: diffs, LSP errors, and in-progress specs |
| `/session` | `/sessions`, `/resume`, `/s` | List, pick, resume sessions |
| `/connect` | | Show/Set provider API keys |
| `/login` | | Log in and enable encrypted config sync |
| `/logout` | `/sync-logout` | Log out and stop config sync |
| `/new` | `/clear` | Start a fresh session |
| `/export` | | Save chat as Markdown |
| `/export-claude` | | Append chat to Claude Code JSONL |
| `/share` | | Generate shareable session link |
| `/title` | `[text]` | Set session title (no arg = reset to auto-generate) |
| `/rc` | `/remote-control` | Start/stop web UI to remote-control this session (`/rc off` to stop) |
| `/cd` | `/cwd` | Change the project root to another directory |
| `/add-dir` | `/add-dirs` | Add a directory to extra allowed paths |
| `/paths` | | Show all relevant filesystem paths: root, extra allowed paths, config files, and data directories |
| `/upload` | `/uploads` | Show or set the file upload directory |
| `/search` | `/find` | Find a message by keyword (opens the in-chat find bar) |
| `/btw` | `/by-the-way` | Add a quick aside to the conversation (`/btw <message>`) |
| `/docs` | `/doc-mode` | Manage OKF knowledge bundle (on, off, status, init, update, cleanup) |
| `/doc-sync` | `[session|all]` | Update `AGENTS.md`/rules/skills to reflect current changes (small model) |
| `/mem` | `[on|off|status|update [user|project|global] [focus]]` | Toggle memory context injection, inspect memory files, or update a memory scope |
| `/discover` | `[enable|disable|status|model [name]|ignore [add|remove|clear] [path]]` | Toggle retrieval-based skill/MCP discovery, pick the query-embedding model, manage ignored paths |
| `/goal` | `<goal>` | Run the multi-agent orchestration pipeline on a coding goal |
| `/init` | `[focus]` | Analyze project and generate `AGENTS.md` |
| `/learn` | `[focus]` | List project-root skills and guide skill creation/update |
| `/lsp` | `[show|open <path>|errors|all]` | Show current LSP diagnostics and error counts |
| `/undo` | | Revert last file change (snapshot-backed) |
| `/redo` | | Restore last undone change |
| `/ide` | `[claude|off|status]` | Connect to VS Code (Claude Code extension) for live file/selection context |
| `/theme` | `/themes` | List and switch themes (`/themes [name]`); live preview as you type |
| `/permissions` | `[auto-add|auto-remove|mode|auto|model|<tool>]` | View or set tool, bash auto-allow, and LLM auto-permissions |
| `/ban` | `[list|add <cmd...>|remove <cmd...>|clear]` | Manage banned bash command prefixes (multi-word prefixes supported; no prefixes banned by default) |
| `/yolo` | `[on|off|status]` | Toggle YOLO permissions mode |
| `/small-model` | `[model]` | Show or switch the small model (used for lightweight tasks: title, discovery, compaction fallback) |
| `/github` | `<pr|issue|workflow> [args]` | GitHub PR / issue / workflow commands |
| `/plugin` | `[list|install <url[@ref]>|remove <name>|enable <name>|disable <name>|info <name>|create <name> [desc]|sync [name]|update [name]|confirm|cancel]` | Plugin management |
| `/skills` | | List available skills |
| `/commands` | | List all available commands (built-in + custom + skill-as-command) |
| `/mcp` | `[list|enable <server>|disable <server>]` | List or toggle MCP servers |
| `/mcp-auth` | `<server>` | Authenticate with a remote MCP server via OAuth |
| `/editor` | `[command]` | Choose default external editor |
| `/editor-mode` | `[external|tmux-split|tmux-window]` | Set editor open mode |
| `/sidebar` | | Toggle sidebar |
| `/thinking` | | Toggle visibility of agent thoughts (reasoning blocks) |
| `/effort` | `[off|low|med|high|xhigh|max]` | Show or set the reasoning effort (thinking budget) level |
| `/sound` | `[on|off|test]` | Toggle terminal bell on task completion |
| `/details` | | Toggle tool execution details in the transcript |
| `/max-step` | `/max-steps` `[n]` | Show or set the max tool-call steps before auto-summary |
| `/mask` | `[on|off|status|mode [lenient|full]|model [name]|list]` | Secret redaction: toggle, set mode, pick tier-2 local model, or list detected secrets |
| `/ocr` | `[status|enable|disable|model [name]]` | OCR status / toggle / model selection (from LM Studio) |
| `/image` | `[status|enable|disable|model [provider/model]]` | Image generation status / toggle / model selection |
| `/localmodel` | | Manage locally-run chat/completion model instances (e.g. Bonsai 8B 1-bit) that LM Studio can't serve |
| `/usage` | `[hour|day|week|month|last-month|last-3-month|all]` | Show LLM token usage summary by model and date range |
| `/cron` | `[list|describe <id>|remove <id>|add <kind> <args> <message...>]` | Manage scheduled jobs (see `docs/scheduled-jobs.md`) |
| `/help` | | Show help for all commands |
| `/exit` | `/quit`, `/q` | Quit the app (always instant, even while streaming) |

> **Queuing note:** `/exit`/`/quit`/`/q` and ~30 read-only UI commands (`/model`, `/themes`, `/help`, `/thinking`, `/details`, `/sidebar`, `/context`, `/commands`, `/permissions`, `/yolo`, `/small-model`, `/editor`, `/lsp`, `/usage`, `/share`, `/connect`, `/agent`, `/mcp`, `/advisor`, `/mask`, `/rc`, `/search`, `/find`, `/docs`, `/goal`, `/agents status`, …) run instantly. Everything else — and any command that mutates persistent state mid-stream (`/add-dir`, `/doc-sync`, `/agents limit`) — queues behind the current turn and drains via `agentStreamDoneMsg` / `compactFinishedMsg`. `handleCommand` is the single chokepoint; keep it in sync with `AGENTS.md`.

### 📊 Debug & Observability

- **Debug log panel** — filterable by kind (agent, tool, LSP, git, auth, MCP, plugin); ring cap 500, background buffering toggle + retention cap (web `LogsForm`)
- **Sidebar telemetry** — context window gauge, model info, LSP status, token counts, modified-files list
- **Token usage & costing** — `/usage` with per-model, per-date-range breakdown; `internal/pricing` + `internal/usage` tracks spend across providers; web `GET /api/spending`
- **Session export** — `/export` (Markdown) and `/export-claude` (Claude Code JSONL); `GET /api/sessions/{id}/export*`
- **TUI Output Safety** — `log.SetOutput(debugLogWriter)` while the alt-screen is live; no raw `fmt.Print` over the frame; subprocess output captured via buffer
- **Prompt cache stability** — tools→system→messages prefix order, deterministic tool ordering, sticky discovery set, split stable/volatile tail injection (`[ocode:discovery]` user-role coalesces without busting the system cache)

### 🛠️ Background Processes & Concurrency

- **Foreground → background** — `Ctrl+B` during a running bash tool moves it to the background, freeing the agent to continue
- **256KB circular output buffer** — tail long-running processes without OOM
- **Lifecycle tools** — `bash_output`, `kill_shell`, `list_processes`, `agent_status`, `task_status`, `wait`
- **Sub-agent process registry** — nested run tree with detail-view drill-in; concurrency limit via `/agents limit` + `AgentRunRegistry`
- **In-batch task DAG** — `task(id, depends_on)` builds a wave scheduler that injects predecessor output into dependents; cycle / duplicate-id / unknown-id / background-dep validation
- **Web terminal** — `GET /api/terminal/ws` WebSocket + `GET /api/terminal/processes` with scrollback, font/shell config

---

## CLI Reference

```bash
# TUI
ocode                          # start TUI
ocode -session <id>            # resume specific session
ocode -continue                # resume most recent session
ocode --yolo                   # auto-approve permissions
ocode --permission-mode auto|off
ocode --effort off|low|med|high|xhigh|max

# Server + Web UI
ocode serve [--port 8080] [--open] [--host 127.0.0.1]
ocode web                      # alias for serve --open

# Headless / CI / Cron target
ocode run [prompt...] [flags]
  -prompt, -p <text>           # prompt text
  -model, -m <provider/model>  # model override
  -agent <name>                # agent profile
  -effort <level>              # reasoning effort
  -session, -s <id>            # resume session
  -continue, -c                # continue last session
  -fork                        # fork from last session
  -file, -f <path>             # attach file(s) to message
  -format default|json|summary # output format
  -title <text>                # session title
  -attach <url>                # attach remote serve URL
  -port <n>                    # server port (when attaching)
  -yolo, --dangerously-skip-permissions
  -command <name>              # run a slash command headlessly
  -dir <path>                  # project root override
  -username, -u / -password, -P # basic auth for remote attach
  -timeout <seconds>
  --permission-mode auto|off
  -profile <name>              # config profile

# Integrations
ocode acp                      # ACP server over stdio (Zed)
ocode mcp [add|list|ls|auth]   # MCP server management
ocode models                   # list available models
ocode skills                   # skill management
ocode goal <goal>              # orchestration pipeline
ocode lsp-daemon               # (internal) LSP broker daemon

# Desktop
make desktop                   # bin/ocode-desktop
make desktop-app               # bin/ocode.app (macOS bundle)
OCODE_DESKTOP_DEV_URL=http://localhost:5173 bin/ocode-desktop  # dev hot-reload
```

---

## Config

Configuration lives in two files, loaded from `~/.config/opencode/` and the nearest project root. Project-level `opencode.json` overrides global; `ocodeconfig.json` is the ocode-only writable store.

| File | Role |
|------|------|
| **`opencode.json`** | Upstream-compatible settings (provider creds, model prefs, `mcp` servers). **Read-only** — ocode never writes to it. Can be checked into git. |
| **`ocodeconfig.json`** | ocode-only state (permissions, editor, compaction, model history, profiles, discovery, limits, features). **Written by ocode** to persist runtime state. `.gitignore`-friendly. |

Additional stores:

| Path | Purpose |
|------|---------|
| `.ocode/settings.json` | Project-scoped `extra_allowed_paths` (additive with global `ocodeconfig.json`) |
| `.ocode/todo/<session>.md` | Persistent todo plan (revision + flock) |
| `.ocode/md-summaries.json` | Discovery markdown summary cache (mtime+size gate, sha256 key) |
| `~/.local/share/opencode/project/<slug>/sessions/` | Chat session JSON/ojsonl (slug = SHA-256 prefix of git root) |
| `~/.local/share/opencode/usage/records.jsonl` | Token usage records |
| `~/.local/share/opencode/auth.json` | Provider API keys and OAuth tokens |
| `~/.local/state/opencode/model.json` | `last_model` + recent models (cross-process lockfile) |

### Profiles

Named config overlays (`profiles` in `ocodeconfig.json`) that swap model / provider / permissions / editor / MCP / terminal / TUI in one switch. Managed via web **Settings → Profiles** and `GET/PUT /api/config/ocode/*` + `GET /api/profiles`.

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

### Server / Web API

The server exposes a REST + SSE surface under `/api/*` (see `internal/server/server.go:registerRoutes`). Highlights:

- **Chat & sessions:** `POST /api/chat`, `GET /api/sessions`, `GET /api/sessions/{id}/state`, `POST /api/sessions/{id}/message`, `POST /api/sessions/{id}/compact`, `GET /api/sessions/{id}/export*`, `GET /api/chat/stream` (SSE)
- **Models & agents:** `GET /api/models`, `GET /api/agents/runs/stream`
- **Files & git:** `GET /api/files/tree`, `GET/PUT /api/files/content`, `GET /api/git/status|diff`, `GET /api/changes`, `POST /api/changes/undo-*`
- **Terminal & shell:** `POST /api/shell`, `GET /api/terminal/ws` (WebSocket), `GET /api/terminal/processes`
- **Config:** `GET/PUT /api/config/ocode/*` (11 sections: recap, commit-msg, compact, permissions-auto, discovery, tui, editor, imagegen, paths, limits, features) + `/api/config/{model,thinking-budget,small-model,terminal,advisor,ocr,mask,agents}`
- **Permissions / questions / RC:** `GET/POST /api/permissions`, `POST /api/questions`, `POST /api/permissions/resolve`, `POST /api/rc/*`
- **MCP / plugins / skills / commands:** `GET /api/mcp`, `GET /api/plugins`, `GET /api/skills`, `GET /api/commands`
- **Events & logs:** `GET /api/events` (SSE, multiproject tagged bus), `GET /api/logs/stream`, `GET /api/tui-status`, `GET /api/spending`, `GET /api/lsp/statuses`
- **Cron:** `GET/POST /api/cron/*` (jobs, outbox, targets)
- **Projects:** `GET/POST /api/projects`, `DELETE /api/projects/{path...}`
- **Uploads:** `POST /api/uploads` → `<project>/.ocode/uploads`
- Auth via `?token=` for EventSource/WS; rate-limited; loopback-bound by default.

---

## Differences from opencode

ocode shares opencode's overall shape (TUI agent, multi-provider, MCP, sessions) but diverges deliberately in language, architecture, and feature set.

### Language and runtime

| Area | opencode | ocode |
|------|----------|-------|
| Language | TypeScript + Bun + Effect | **Go 1.26.1** |
| TUI | Solid-based custom renderer | **Bubble Tea / Lipgloss v2** |
| Distribution | npm + Bun runtime | **Single static binary** |
| Memory | 500MB+ typical | **30–50MB typical** |
| Transcript render | O(N) per frame | **O(1) FastViewport (41× faster)** |

### Permissions

ocode adds **first-class permission modes** (`normal`, `yolo`, `locked`) with per-tool rules, bash-prefix granularity, scope confinement, path expansion, and LLM-driven auto-permission decisions — stored in `ocodeconfig.json`. opencode handles permissions inline with less granularity.

### Sessions

- ocode can **list, pick, and resume** opencode sessions **and** Claude Code sessions (cloned as `claude-<id>`)
- Auto-save, session picker, `Ctrl+Y` retry on LLM timeout
- Session pagination, delete, share, and `.ojsonl` append-only storage with cross-project resume guard
- Per-session **Changes** tab backed by snapshot store (not just `git status`)

### TUI features unique to ocode

- **FastViewport** — custom 0.73ms transcript rendering vs standard viewport
- **Extended thinking toggle** on Claude models (`Alt+T`) + `/effort` levels
- **Foreground bash → background** (`Ctrl+B`)
- **Inline vim-like editor** in Files tab + external editor modes
- **Async sub-agent runs** with DAG, contracts, and drill-in detail view
- **ModalStack** — composable overlay system (permission dialogs, pickers, dialogs, list boxes)
- **In-app text selection** — click-drag copies, plain click activates
- **Hover effects** on all clickable surfaces + scrollbar drag
- **Clickable file paths & URLs** (URLs confirm before opening)
- **In-chat find bar** (`Ctrl+F` / `/search`)
- **Persistent todo plan** (`.ocode/todo/<session>.md`)
- **Six tabs:** Chat, Agents, Files, Changes, Git, Log

### Compaction

| Aspect | opencode | ocode |
|--------|----------|-------|
| Custom summary model | No — uses session model | **Yes** — separate `summary_provider`/`summary_model` |
| Tool-pair safety | Implicit | **Explicit `safeCut`** — proven symmetry on both sides |
| Markers | Typed message part | **Sentinel-tagged system message** `[ocode:compaction-summary]` |
| Batching | Single pass | **Multi-batch** when middle exceeds `max_summary_input_tokens` |
| Recap model | — | **Separate `recap` model** (`/recap`) |

### Added in ocode (not in opencode)

- **Theme system** with JSON loading and live preview (Tokyo Night, Catppuccin, LCARS, Pip-Boy)
- **Plugin system** with provider interface and Git-based registry
- **Skill system** with retrieval-based discovery, corpus indexing, and skill-as-command
- **Knowledge bundle** (`/docs`, OKF v0.1) with `context` subagent curation
- **Scheduled jobs / Cron** — persistent disk-backed dispatcher
- **VS Code `/ide` integration** with lock discovery + Zed/ACP (`ocode acp`)
- **Web UI** — full React SPA (chat, files, changes, git, terminal, cron, assets, agents, logs, settings) with SSE event bus, multiproject routing, and `/rc` remote control
- **Desktop app** — native Wails v3 shell (tray, dock badge, notifications, cron host) with embedded SPA
- **AI code review** (`/review`) for working dir, commits, branches, PRs + GitHub CLI (`/github`)
- **LSP eager warm-up** with sidebar status, install hints, and shared broker
- **`/usage`** — per-model token cost tracking + `GET /api/spending`
- **File content search** with incremental streaming + fuzzy file finder
- **Custom command loader** from plugin Markdown files
- **Debug log panel** with filtered kinds and word-wrap + web Logs panel
- **Session pagination, delete, and share**
- **`commit_msg_model`** config for AI commit message generation
- **Safe built-in deny rules** for `git push --force` and `rm -rf`
- **Progressive file preview** with binary detection
- **OCR & Image generation** tools (`/ocr`, `/image`)
- **Local model instance manager** (`/localmodel`) for models LM Studio can't serve
- **Secret redaction** (`/mask`) with tier-1 regex + tier-2 local LLM scanning, lenient/full modes, local-only endpoint guard
- **Telegram bot** bridge
- **Memory system** (`/mem`) — user / project / global scopes with injection toggle

### What ocode does **not** have (vs opencode)

- No plugin marketplace (plugin system is Git-based)
- Smaller skill ecosystem (growing; bundled ~60+ skills)
- No `plan-reminder` / `build-switch` prompt overlays (replaced by todo plans + discovery)

---

## Stack

- **Go 1.26.1** — single static binary, zero runtime deps
- **Bubble Tea / Bubbles / Lipgloss v2** — Charm ecosystem for TUI
- **React + shadcn/ui + Tailwind + Monaco + xterm.js** — Web UI frontend (Vite)
- **Wails v3** — Desktop shell (webview + Go backend)
- **FastViewport** — custom O(1) transcript viewport
- **MCP** — Model Context Protocol client (local + remote, OAuth)
- **SSE + WebSocket** — Streaming (chat, logs, agents, terminal)

---

## Layout

```
main.go                    Entry point (TUI + serve/web/run/acp/mcp/models/skills/goal)
cmd/ocode-desktop/         Wails v3 desktop shell (native.go, main.go, embedded-assets)
internal/acp/              Agent Client Protocol server (Zed integration)
internal/agent/            LLM client, agent registry, permissions, tool truncation, title gen
internal/auth/             Multi-provider OAuth + keychain
internal/bundled/          Embedded plugin/skill/model-config FS helpers
internal/cli/              goal orchestration CLI
internal/commands/         Custom command loader (plugin Markdown)
internal/config/           Config loading (opencode.json / ocodeconfig.json + profiles)
internal/debuglog/         Shared debug log for TUI and non-TUI consumers
internal/desktop/          Desktop boot helper, window watcher, scheduler host
internal/github/           GitHub API client (PR, issues, workflows)
internal/hooks/            Git hooks integration
internal/ide/              VS Code / IDE lock discovery and client
internal/lsp/              Language server manager + shared broker (broker/)
internal/mcp/              MCP client (local + remote, OAuth)
internal/mcpcli/           MCP CLI integration
internal/memory/           User / project / global memory scopes
internal/models/           Model registry and pricing (models.dev + live resolution)
internal/pathscope/        Path scope utilities (temp dirs, expansion)
internal/plugin/           Plugin provider interface and registry
internal/plugins/          Plugin loader, manager, sync
internal/pricing/          LLM token cost tracking
internal/redact/           Secret redaction (tier-1 regex + tier-2 LLM)
internal/runcli/           Headless CLI mode
internal/scheduler/        Cron engine + job store
internal/server/           HTTP server (web UI, APIs, SSE, /rc, scheduler runner)
internal/session/          Session management (save, resume, export, migrate, ojsonl)
internal/skill/            Skill loader, installer, discovery corpus
internal/snapshot/         Per-agent snapshot store for undo_file_change
internal/theme/            Theme system (JSON loading, definitions)
internal/tool/             39 built-in tools
internal/tui/              Bubble Tea TUI (model, view, update, components, commands)
internal/usage/            Token usage tracking
internal/version/          Version info
internal/ocr/              OCR backends
web/                       React SPA (Vite + Tailwind + shadcn + Monaco + xterm.js)
docs/                      Design specs, plans, OKF knowledge bundle, man pages
skills/                    Bundled skill definitions (SKILL.md)
.opencode/plugins/         Bundled plugin manifests (embedded in binary)
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

## Credits

- **Pip-Boy / Vault Boy ASCII art** — three variants shown randomly on the pipboy theme empty screen, sourced from [emojicombos.com/fallout-ascii-art](https://emojicombos.com/fallout-ascii-art)
- **LCARS / Star Trek ASCII art** — five variants shown randomly on the lcars theme empty screen

---

## License

MIT
