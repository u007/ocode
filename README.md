# ocode

Terminal AI coding agent in Go — started as an opencode clone, now diverged. See [Differences from opencode](#differences-from-opencode) for what changed and why.

## Why ocode?

We built ocode to address a few key needs:

- **Lightweight & efficient** — A single Go binary that uses under 50MB of memory (excluding MCP), compared to JS-based coding agents that often exceed 500MB. Lower memory footprint, faster startup, more room for your actual work.
- **Auto-permissions & advisor** — First-class permission modes (normal, yolo, locked) and an advisor module for automated decision-making, similar to Claude Code's approach.
- **Optimized for extensibility** — A clean architecture that makes it easy to add new providers, tools, and features without accumulating tech debt.

## Quick Start

- **Setup:** See [SETUP.md](SETUP.md) for prerequisites, installation, and configuration
- **Contributing:** See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, code conventions, and PR guidelines
- **Testing:** See [TESTING.md](TESTING.md) for tested features, known issues, and what needs validation

## Run

```bash
go run .
```

## Build

```bash
go build -o ocode .
./ocode
```

## Status

**TUI (Terminal):** Production-ready AI coding agent with multi-provider LLM support (OpenAI, Anthropic,
Google, Z.AI, Alibaba, Copilot), MCP client, session management, git integration, LSP
intelligence, theme system, and extensible agent system.

**Web UI:** Work-in-progress and not yet tested. HTTP server mode (`ocode serve`) available for early exploration.

## Features

- **Multi-Provider LLM** — OpenAI, Anthropic (incl. Claude thinking/extended thinking), Google, Z.AI, Alibaba, GitHub Copilot
- **Separated Agent System** — Registry-based agent definitions with permission isolation and child session tracking
- **Anthropic Prompt Caching** — Automatic `cache_control` markers on system messages and large tool results
- **OpenAI Caching** — Automatic server-side for API-key models (GPT-5.4, etc.); OAuth/codex backend (GPT-5.3-codex) doesn't surface cache token counts
- **Extended Thinking** — Toggle thinking mode on supporting Anthropic models via `Ctrl+T` (off/low/med/high)
- **Tool Result Truncation** — Large tool outputs (>100 lines) are truncated in-context and written to disk for retrieval
- **Context Window Tracking** — Registry-backed model context windows with sidebar telemetry
- **MCP Client** — Local + remote MCP server support with OAuth, CLI management commands, timeouts
- **Git Integration** — Full git UI within TUI: status, diff, staging, commits, branches, stashes, push/pull/fetch
- **File Browser** — Tree-based file explorer with preview panel, inline vim editor, external editor, add-to-context
- **Session Management** — Auto-save/resume, session picker, Claude Code session cloning
- **LSP Integration** — Go-to-definition, hover docs, symbol search
- **Theme System** — Built-in themes (tokyonight, tokyonight-storm, catppuccin-mocha), loadable from disk
- **Permissions System** — Granular allow/ask/deny per tool + bash prefix rules, YOLO mode, locked mode
- **Slash Commands & Palette** — Extensible `/commands` and `Ctrl+P` command palette
- **Mouse Support** — Clickable tabs, sidebar, file tree, transcript scrolling, input text selection
- **Undo/Redo** — Session history undo/redo stack
- **Async Agent Runs** — Launch and monitor background subagent executions with transcript capture, process registry, and detail view drill-in
- **Background Process Management** — Spawn and tail shell processes (256KB circular output buffer) with wait tool and lifecycle tracking; press `Ctrl+B` during a foreground `bash` tool call to move it into the background and let the main agent continue the turn

## Shortcut policy

- Avoid single-stroke shortcuts that commonly conflict with host terminals such as Warp, Ghostty, and iTerm2.
- Prefer the existing leader sequence (`Ctrl+X`, then a second key) for non-essential UI toggles.
- Sidebar toggle uses `Ctrl+X`, then `S`.
- `Ctrl+B` is reserved for moving a running foreground `bash` tool call into the background.
- Background jobs and subagents report completion back into the main conversation; live state remains available through `bash_output`, `agent_status`, `task_status`, and `wait`.

## Slash Commands

Type `/` in the chat input (TUI or Web) to open the slash command palette. Commands can also be run via `ocode run -command <name>`.

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
| `/clear` | | Start a fresh conversation context in the current session | Keeps session history on disk; only clears in-memory messages |
| `/compact` | | Manually trigger context compaction when approaching token limits | Uses the configured summary model (default: active model) |
| `/recap` | | Generate a structured session recap (Goal, Progress, Decisions, Next Steps, Files) | **Web UI only** — backend endpoint planned (`POST /api/sessions/{id}/recap`) but not yet implemented. In TUI, acts as a normal prompt. |
| `/export` | | Export the full session as JSON (messages, metadata, token usage) | Output includes full transcript for backup or migration |
| `/export-claude` | | Export session in Claude Code compatible format | For importing into Claude Code |
| `/share` | | Generate a shareable link to the current session | Requires `ocode serve` running; creates a session URL |
| `/model` | `/m` | Open the model picker to switch LLM providers/models | Shows recent/favorite models first; fuzzy search supported |
| `/session` | `/s` | Switch or resume a different session | Lists recent sessions with preview |
| `/config` | | Open configuration editor (TUI) | Edit compact, permissions, editor settings |
| `/help` | `/?` | Show this help / available commands | |

### Slash Command Palette (Ctrl+P)

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

## Config

Config files are loaded from two locations:

1. **Global config** — `~/.config/opencode/` (your home config directory)
2. **Project config** — Current working directory or nearest ancestor containing `opencode.json`

### File roles

**`opencode.json`** — Upstream-compatible settings (read by both opencode and ocode).
- Contains LLM provider credentials, model preferences, and settings opencode understands.
- Can be checked into git for team-shared config.
- ocode **never writes** to `opencode.json` — it remains read-only after initial creation.

**`ocodeconfig.json`** — ocode-only settings and runtime state.
- Contains ocode-exclusive config: permissions, editor settings, compaction behavior, auto-permission model.
- **Written by ocode** to persist state: most recently selected model, session history, and editor mode.
- Should typically be `.gitignore`'d (contains personal state like auth tokens).
- If missing, ocode auto-creates it with compact defaults.

### Config loading precedence

Both files are loaded from global (`~/.config/opencode/`) and project roots. Settings from project-level configs override global settings. The TUI restores the most recently selected model from `ocodeconfig.json`, falling back to `opencode.json` unless `OPENCODE_MODEL` env var is set.

Compact defaults:

- `enabled`: `true`
- `trigger_ratio`: `0.75`
- `max_ratio`: `0.85`
- `min_free_tokens`: `4096`
- `summary_provider`: unset, use current provider
- `summary_model`: unset, use current model

### Compaction behavior

Compaction triggers automatically when used context exceeds the configured ratio of the active model's window. Every compaction:

1. **Prunes** oversized tool results in the slice being summarised (capped at ~2KB each — full output remains available via on-disk tool-result truncation, see [Tool result handling](#tool-result-handling)).
2. **Produces a structured summary** following a fixed Markdown template (Goal / Constraints / Progress / Decisions / Next Steps / Critical Context / Relevant Files). Every section is required, even if "(none)".
3. **Anchors to the prior summary.** When a previous compaction summary exists in the session, the next compaction passes it as `<previous-summary>` and instructs the model to update it in place — merging new facts, removing stale ones, preserving still-true context. This keeps continuity across multiple compactions in the same session.
4. **Replaces, not appends.** The prior summary message is overwritten by the new one. After N compactions there is still exactly one summary message in the session, not N.
5. **Drops the old history.** Once a summary replaces a range of messages, the original messages are removed from the session. They are not re-sent to the LLM on the next turn. The TUI banner ("📦 Compacted N earlier messages") only shows where the cut happened.
6. **Safe-cuts** the boundary so every assistant `tool_call` and its matching `tool_result` end up on the same side of the cut — no orphans.

The summary is tagged with `[ocode:compaction-summary]` so subsequent compactions can find it. This tag is internal; you don't need to manage it manually.

### Custom compaction model

Compaction can use a different model than the active chat model. This is useful when you want a cheap/fast model to do summarisation while the main agent runs on something more capable (e.g. summarise with Haiku while chatting with Opus).

```json
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

- Leave `summary_provider` / `summary_model` empty to summarise with the active model.
- If only one of the two is set, the other falls back to the active client's value.
- The compaction client respects the same auth/keychain entries as the main client.

### opencode.json (shared config)

Example global or project-level config for upstream compatibility:

```json
{
  "apiKey": "sk-...",
  "provider": "openai",
  "model": "gpt-4o",
  "temperature": 0.7,
  "context_window": 128000
}
```

Keys here are provider/model credentials and settings that opencode understands and may read/write.

### ocodeconfig.json (ocode-only state)

This file contains ocode-exclusive runtime state and configuration. It is **created and maintained by ocode**, and persists across sessions:

```json
{
  "compact": { ... },
  "permissions": { ... },
  "editor": "nvim",
  "editor_mode": "tmux-split",
  "auto_permission_model": "deepseek:deepseek-v4-flash",
  "model_memory": { ... },
  "_model_history": ["gpt-4o", "claude-3-5-sonnet"],
  "_last_session_id": "abc123..."
}
```

**Written by ocode:**
- `_model_history` — Recently used models (for quick switching)
- `_last_session_id` — Last opened session (for resumption)
- Model-specific overrides from `/config` commands

**User editable:**
- `compact` — Compaction behavior and custom summary model
- `permissions` — Permission modes, tool rules, bash prefixes
- `editor` / `editor_mode` — External editor and open mode
- `auto_permission_model` — Model for automated permission decisions
- `commit_msg_model` — Model for AI-generated commit messages in the Git tab (default: uses small model fallback)

Permissions live in `ocodeconfig.json` because they are ocode-only runtime policy:

```json
{
  "permissions": {
    "mode": "normal",
    "tools": {
      "read": "allow",
      "write": "allow",
      "edit": "allow",
      "patch": "allow",
      "bash": "ask"
    },
    "bash": {
      "prefixes": {
        "git": "allow",
        "make": "ask",
        "rm": "deny"
      },
      "auto_allow_prefixes": [
        "jq",
        "stat"
      ],
      "prefix_modes": {
        "sed": "mutating",
        "jq": "read_only",
        "python": "never_auto"
      }
    }
  }
}
```

Permission levels are `allow`, `ask`, and `deny`. Modes are `normal`, `yolo`, and `locked`.

For `permissions.bash.prefix_modes`, supported values are:

- `read_only`: auto-allow in-root calls and persist a project-scoped in-root rule.
- `mutating`: auto-allow in-root calls once, but do not auto-persist.
- `never_auto`: disable auto-allow for that prefix.

`permissions.bash.auto_allow_prefixes` extends the built-in safe prefix set. Added prefixes still require all detected path arguments to resolve inside the current project root.

The built-in safe prefix set is OS-aware: Unix gets the usual `awk`/`sed`/`grep`/`ls` family, while Windows adds `cmd.exe`-compatible commands like `dir`, `type`, `findstr`, `more`, `tree`, `where`, `cls`, `md`, and `mkdir`. Temp-directory auto-allow also follows the host OS (`/tmp` and `/var/tmp` on Unix, `os.TempDir()` on Windows).

- `normal`: follow tool and bash-prefix rules. Project-confined file writes/edits/patches/formats are allowed by default; delete, shell, network, and delegation tools still ask.
- `yolo`: allow permission-gated tools without prompting, while still respecting agent mode restrictions and hard safety blocks.
- `locked`: allow read/search-style tools only.

Use `/permissions` to view or set rules, `/permissions bash:git allow` for shell prefixes, and `/yolo [on|off|status]` to toggle YOLO mode. The TUI also accepts `--yolo`/`-yolo`; `ocode run` accepts `--yolo`.

### Auto-permissions

Permission prompts can be automated by configuring an `auto_permission_model` that makes accept/deny decisions without user interaction. This is useful for hands-off agent operation and reduces context overhead during extended runs. Add to `ocodeconfig.json`:

```json
{
  "permissions": {
    "auto_permission_model": "deepseek:deepseek-v4-flash",
    "auto_permission_deny_unsafe": true
  }
}
```

- `auto_permission_model` — `provider:model-id` to use for automatic permission decisions (e.g. `openai:gpt-4o-mini`, `anthropic:claude-3-5-haiku`, `deepseek:deepseek-v4-flash`). If unset, permission prompts remain interactive.
- `auto_permission_deny_unsafe` — If `true`, the auto-permission model is instructed to conservatively deny any operation it cannot confidently approve. Default is `false`.

**Recommendation:** Use a **fast, cost-effective model** like Deepseek's `deepseek-v4-flash` or OpenAI's `gpt-4o-mini` for auto-permissions. These models are inexpensive, fast enough for sub-second latency on permission decisions, and sufficiently capable for the narrowly-scoped task of approving/denying tool calls. This keeps permission overhead minimal while the main agent runs on a more capable model.

Editor config also lives in `ocodeconfig.json`:

```json
{
  "editor": "nvim",
  "editor_mode": "tmux-split"
}
```

- `editor` — External editor command (e.g. `nvim`, `code --wait`). Priority: config > `$VISUAL` > `$EDITOR` > `vi`.
- `editor_mode` — How the editor opens from the Files tab:
  - `external` (default) — Plain `exec.Command(editor, path)`.
  - `tmux-split` — Opens via `tmux split-window` (horizontal split at width ≥120, vertical otherwise).
  - `tmux-window` — Opens via `tmux new-window`.
- Explicit tmux modes fail fast at startup if you are not inside a tmux session — no silent fallback.

In the Files tab, `i` opens a minimal vim-like inline editor for editable text files. It supports `i`/`a` insert, `esc` normal mode, `:w`, `:q`, `:q!`, and `:wq`. Use `e` or `enter` for the configured external editor.

Use `/editor [command]` to set the default editor and `/editor-mode [mode]` to set the open mode. Both open a picker when called without arguments.

## Differences from opencode

ocode shares opencode's overall shape (TUI agent, multi-provider, MCP, sessions) but diverges in a few deliberate places. Upstream-compatible config (`opencode.json`) is preserved; ocode-only behavior lives in `ocodeconfig.json`.

### Language and runtime

| Area | opencode | ocode |
|---|---|---|
| Language | TypeScript + Bun + Effect | Go 1.26.1 |
| TUI | Solid-based custom renderer | Bubble Tea / Bubbles / Lipgloss |
| Distribution | npm + Bun runtime | Single static binary |

### System prompts

opencode swaps the **entire** system prompt per model family. It ships seven full prompts in `packages/opencode/src/session/prompt/` (`anthropic.txt`, `gpt.txt`, `gemini.txt`, `beast.txt`, `codex.txt`, `kimi.txt`, `trinity.txt`, `default.txt`) — ~1146 lines total — selected by model-ID substring (e.g. `gpt-4`/`o1`/`o3` → `beast.txt`).

ocode runs a **hybrid**: one shared base prompt + a small **model-ID-routed fragment** loaded from `internal/agent/prompts/*.txt` (embedded via `//go:embed`). Routing checks the model ID first, then falls back to provider:

- `o1` / `o3` / `o4-mini` / `*-thinking` → `reasoning.txt` (suppress chain-of-thought)
- `claude-*` → `claude.txt`
- `gemini-*` → `gemini.txt`
- `kimi-*` → `kimi.txt`
- `gpt-*` → `gpt.txt`
- provider `copilot` / `github` → `copilot.txt`
- unknown → no fragment (mode prompt only)

Trade-off vs opencode: ocode keeps a smaller token footprint and one prompt to maintain, with a reasoning-model split that opencode handles via its larger `beast.txt` prompt.

### Permissions

ocode adds first-class permission modes (`normal`, `yolo`, `locked`) with per-tool and bash-prefix rules, stored in `ocodeconfig.json`. opencode handles permissions inline in the agent loop. See `/permissions` and `/yolo`.

### Sessions

- ocode can list, pick, and resume opencode sessions and **Claude Code sessions** (cloned into ocode history as `claude-<id>`). opencode does not read Claude Code session files.
- Auto-save with `/session`, `/sessions`, `/resume`, and a sidebar picker.
- `Ctrl+Y` retries the last LLM timeout or I/O failure without resending the error as context.

### TUI features unique to ocode

- **Foreground bash → background:** `Ctrl+B` during a running `bash` tool call moves it to the background and frees the agent to continue the turn.
- **Inline vim-like editor** in the Files tab (`i`, `:w`, `:q`, `:wq`) plus tmux-split / tmux-window external editor modes.
- **Async subagent runs** with transcript capture, process registry, and detail-view drill-in.
- **Background process management** with a 256KB circular output buffer and `wait` tool.
- **Sidebar telemetry** for context window usage, cached via keystroke-debounced recompute.
- **Extended thinking toggle** (`Ctrl+T` → off/low/med/high) on supporting Anthropic models.

### Git tab features

- **AI commit message generation** — Press `Ctrl+G` in the commit input to auto-generate a commit message from staged changes. Uses the small model by default (or `commit_msg_model` from config).
- **Stage/unstage/discard** — `Ctrl+S` stage, `Ctrl+U` unstage, `Ctrl+D` discard changes.
- **Push/pull/fetch** — `Ctrl+O` push, `Ctrl+P` pull, `Ctrl+G` (in changes section) fetch.
- **Branch management** — Create, delete, checkout branches from the Branches section.
- **Stash operations** — Stash and apply stashes from the Stash section.

### Compaction

Both projects compact long sessions, but with different mechanics:

| Aspect | opencode | ocode |
|---|---|---|
| Trigger | `tokens.total >= usable(input − reserved)` | ratio threshold against model window (default 0.75) |
| Anchored summary | yes | yes — single summary updated in place across multiple compactions |
| Output template | fixed Markdown sections | fixed Markdown sections (same as opencode) |
| Tool-pair safety | implicit (cuts at user-turn boundaries) | explicit `safeCut` — proves tool-call/result symmetry on both sides |
| Prune-before-summary | yes (`TOOL_OUTPUT_MAX_CHARS=2000`) | yes (same cap) |
| Custom summary model | no — uses session model | yes — `summary_provider` / `summary_model` in `ocodeconfig.json` |
| Compaction marker in history | typed message part | sentinel-tagged system message (`[ocode:compaction-summary]`) |
| Post-compaction history sent to LLM | replaced range is dropped | replaced range is dropped (verified by test) |

ocode's compaction pipeline is intentionally smaller than opencode's, with three behaviors opencode does not have: per-compaction custom model selection, an explicit `safeCut` invariant, and reasoning-content-aware token estimation. See [Compaction behavior](#compaction-behavior) for the full pipeline and [Custom compaction model](#custom-compaction-model) for configuration.

### Anthropic prompt caching

ocode adds explicit `cache_control` markers on system messages and large tool results (`internal/acp/`). opencode relies on provider-side caching defaults.

### Tool result handling

Outputs over ~100 lines are truncated in-context and written to disk; the agent reads back via path. opencode keeps full output in-context.

### Config layout

- `opencode.json` — upstream-compatible settings, read but never written by ocode.
- `ocodeconfig.json` — ocode-only overrides (permissions, editor, compaction, model memory).
- Both loaded from `~/.config/opencode/` and the nearest project root.
- TUI model selection persists in `ocodeconfig.json`, falling back to opencode state unless `OPENCODE_MODEL` is set.

### What ocode does **not** have (vs opencode)

- No desktop frontends — terminal and WIP web UI only.
- No plugin marketplace.
- Smaller skill ecosystem.
- No `plan-reminder` / `build-switch` prompt overlays (one mode prompt does both jobs).

## Stack

- Go 1.26.1
- Bubble Tea / Bubbles / Lipgloss (Charm TUI)

## Layout

```
main.go                  entry point
internal/acp/            Anthropic prompt caching
internal/agent/          LLM client, agent registry, permissions, tool truncation
internal/auth/           Multi-provider OAuth + keychain
internal/config/         Config loading (opencode.json / ocodeconfig.json)
internal/mcp/            MCP client (local + remote)
internal/server/         HTTP server mode
internal/tool/           Built-in tools (read, write, edit, bash, grep, glob, etc.)
internal/tui/            Bubble Tea TUI (model, view, update, themes, git, files, etc.)
internal/version/        Version info
docs/                    Design specs and enhancement plans
```

## Sessions

- `/session`, `/sessions`, and `/resume` open a picker with current-project ocode and Claude Code sessions, sorted newest first.
- `/session list` still prints saved sessions, and `/session load <id>` loads one directly.
- On exit, ocode prints the current session ID and a resume command: `ocode -session <id>`.
- Claude Code sessions are marked `[claude]`; resuming one clones it into ocode history as `claude-<id>`.
- `Ctrl+O` toggles YOLO permissions mode. `/yolo [on|off|status]` is also available, and `--yolo` starts in YOLO mode.
- `Ctrl+Y` retries the last LLM timeout or I/O failure without resending the error message as context.
- Messages submitted while the AI is running are shown in a queue and sent automatically when the current response finishes.
- Type `@path` to attach file context. While typing an `@` token, matching files appear in a filtered popup; image files are attached as images and persisted in session history.
- Context files (`AGENTS.md`, `CLAUDE.md`, `.cursorrules`) loaded at session start use the committed `HEAD` version when the working-tree copy has unstaged modifications. This keeps the base prompt stable across edits — commit the change to make it effective. A note is logged to stderr when the swap occurs.
- `!command` hands the terminal to the process (interactive programs like `vim`, `less`, `git diff` work). Output is not captured into the chat transcript.

## Cost Tracking

Cost estimates displayed in the sidebar and session telemetry are calculated based on **API token usage**, not subscription charges. Costs are computed using per-token pricing for each model (input tokens, output tokens, and cached reads) obtained from the provider's pricing data. This gives an accurate representation of actual API costs regardless of subscription tier or cost structure, and applies uniformly across all providers (OpenAI, Anthropic, Google, etc.).

## Support

Thank you for using ocode! There are several ways you can help support the project:

- **Report Issues** — Found a bug or have a feature request? [Open an issue](https://github.com/anthropics/ocode/issues) to help us improve.
- **Test Across Providers** — Try ocode with different LLM providers (OpenAI, Anthropic, Google, Z.AI, Alibaba, Copilot) and share your experience. Compatibility feedback helps us prioritize fixes and enhancements.
- **Support OpenCode** — If you'd like to support the upstream OpenCode project, consider signing up for an [OpenCode Go plan](https://opencode.ai/go?ref=3MB2697263) using our referral link.

Your support means a lot — thank you! 🙏
