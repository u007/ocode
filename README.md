# ocode

Terminal AI coding agent in Go — started as an opencode clone, now diverged. See [Differences from opencode](#differences-from-opencode) for what changed and why.

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

Production-ready TUI AI coding agent with multi-provider LLM support (OpenAI, Anthropic,
Google, Z.AI, Alibaba, Copilot), MCP client, session management, git integration, LSP
intelligence, theme system, and extensible agent system.

## Features

- **Multi-Provider LLM** — OpenAI, Anthropic (incl. Claude thinking/extended thinking), Google, Z.AI, Alibaba, GitHub Copilot
- **Separated Agent System** — Registry-based agent definitions with permission isolation and child session tracking
- **Anthropic Prompt Caching** — Automatic `cache_control` markers on system messages and large tool results
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

## Config

- `opencode.json` stays for upstream-compatible settings.
- `ocodeconfig.json` stores ocode-only overrides and any extra fields opencode would not accept.
- Both are loaded from `~/.config/opencode/` and from the nearest project root beside `opencode.json`.
- If `ocodeconfig.json` is missing, ocode creates it with compact defaults.
- The TUI restores the most recently selected model from `ocodeconfig.json`, falling back to opencode state unless `OPENCODE_MODEL` is set.

Compact defaults:

- `enabled`: `true`
- `trigger_ratio`: `0.75`
- `max_ratio`: `0.85`
- `min_free_tokens`: `4096`
- `summary_provider`: unset, use current provider
- `summary_model`: unset, use current model

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
      }
    }
  }
}
```

Permission levels are `allow`, `ask`, and `deny`. Modes are `normal`, `yolo`, and `locked`.

- `normal`: follow tool and bash-prefix rules. Project-confined file writes/edits/patches/formats are allowed by default; delete, shell, network, and delegation tools still ask.
- `yolo`: allow permission-gated tools without prompting, while still respecting agent mode restrictions and hard safety blocks.
- `locked`: allow read/search-style tools only.

Use `/permissions` to view or set rules, `/permissions bash:git allow` for shell prefixes, and `/yolo [on|off|status]` to toggle YOLO mode. The TUI also accepts `--yolo`/`-yolo`; `ocode run` accepts `--yolo`.

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

ocode keeps **one shared base prompt** and appends a small **provider-keyed fragment** (~3 bullets) from `internal/agent/provider_prompts.go`. Trade-off:

- ocode wins on maintenance: prompt fixes land in one place, smaller per-request token footprint.
- opencode wins on model tuning: bespoke prompts per family capture each model's quirks.
- ocode is moving toward a hybrid: file-backed, model-ID-routed fragments (not full per-family bibles). See planning notes in this repo.

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

- No web/desktop frontends — terminal only.
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
