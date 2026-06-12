# Agent Instructions - ocode

## Tech Stack
- Go 1.23
- Charm TUI (Bubble Tea, Lipgloss)
- LLM Providers: OpenAI, Anthropic, Google, Z.AI, Alibaba

## Coding Standards
- Use modular packages in `internal/`.
- Respect `.gitignore` and `watcher.ignore`.
- Follow Go best practices and standard formatting.

## TUI Output Safety (alt-screen)
The TUI runs in Bubble Tea's alt-screen, so any raw write to `os.Stdout`/`os.Stderr`
from a path the running TUI invokes paints over the rendered frame and corrupts it
(text overlap / "hairwire" at the bottom of the chat, status line off-screen). When
debugging rendering glitches, suspect raw writes, not just layout.
- In any code reachable while the TUI is live (agent loop, tools, hooks, session,
  plugins, auth, config reload): **never** `fmt.Print*`, `fmt.Fprint*(os.Stdout|os.Stderr,…)`,
  or `println` for diagnostics. Use `agent.emitDebug` / `agent.DebugAppendf` inside the
  `agent` package, or the stdlib `log` package elsewhere — `tui.Run()` redirects `log`
  into the debug panel via `log.SetOutput(debugLogWriter{})`. `emitDebug` falls back to
  stderr only when no sink is set (headless `run`/`serve`/`acp`).
- Capture subprocess output (`cmd.Stdout = &buf`); never inherit the terminal
  (`cmd.Stdout = os.Stdout`).
- Clamp one-line status/activity rows with `.Width(w).MaxHeight(1)` so long content
  can't wrap and push the bottom chrome past the terminal height.

## TUI Mouse: clickable chrome vs selectable content
Terminal mouse capture is **global per frame** (`tea.View.MouseMode` is one flag for the
whole screen, not per-region). Enabling capture makes tabs/menus/buttons clickable but
**blocks native terminal text selection** — the two are mutually exclusive and can't be
scoped to a region. Never disable `MouseMode` to regain native selection; that kills
every click target.
- To make a region both clickable and selectable: **keep mouse ON and implement
  selection in-app.** Per surface, a `selectionState`; **press** starts a drag, **motion**
  highlights via `applySelectionHighlight(styled, raw, …)`, **release** copies via
  `extractSelectionText` + `clipboard.WriteAll` when a drag happened, else clears and
  **falls through to the click handler**. Working copies: transcript, log tab, files
  preview, git diff, sidebar, agent-detail. Track styled + `stripANSI` raw lines in the
  same screen-row/col coordinate space (bordered box left chrome = 2 cols).
- **Hover** effects need `MouseModeAllMotion` — `CellMotion` emits motion only while a
  button is held (no plain-hover events). The motion handler must process `MouseNone`
  motion (don't early-return on `Button != MouseLeft` first), stay cheap (read cached
  hit-test data from render), and redraw only when the hovered target changes.

## Tools
- `read`, `write`, `delete`: Basic file operations.
- `grep`, `glob`: Advanced search tools.
- `bash`: Shell execution (cross-platform).
- `lsp`: Code intelligence (supports goToDefinition, hover).
- `agent`: Sub-agent delegation.
- `websearch`: Web search via DuckDuckGo.
- `question`: Pause for user input.

## Context Loading
- `AGENTS.md`, `CLAUDE.md`, and `.cursorrules` are loaded at session start.
- If a context file is tracked by git AND has unstaged modifications, the committed `HEAD` version is used instead of the working-tree copy. This keeps the base prompt stable across edits; commit the changes to make them effective. A line is logged to stderr when this swap occurs.

## User Interaction
- TUI supports `/commands` and `!shell`.
- **Slash command queuing:** All slash commands entered while the agent is streaming or compacting must be queued (`queuedCommands []string`) and executed one-at-a-time after the current work ends — not run immediately. Only `/exit`, `/quit`, `/q` bypass the queue by default. Synchronous local UI/config commands that do not start a new agent request may also bypass the queue; keep any such exceptions centralized in `handleCommand` (single chokepoint covering all callers: enter key, palette, keybinds, leader shortcuts, hotkeys) and document them here. Drain `queuedCommands` in `agentStreamDoneMsg` and `compactFinishedMsg` handlers, after `queuedInputs` are processed, so a command never fires while another stream is in flight.
- Use `ctrl+x` for leader keys and `ctrl+p` for palette.
- Avoid introducing raw shortcuts that are likely to conflict with host terminals like Warp, Ghostty, and iTerm2; prefer `ctrl+x` leader sequences for non-essential UI toggles.
- Sessions are automatically saved and resumed.

## Data Storage
All persistent state lives under a single cross-platform global directory resolved by
`internal/paths.GlobalDataDir()`:

| Platform | Path |
|----------|------|
| macOS | `~/Library/Application Support/opencode` |
| Linux | `$XDG_DATA_HOME/opencode` (or `~/.local/share/opencode`) |
| Windows | `%LOCALAPPDATA%\opencode` |

Sub-directories:
- `project/{slug}/sessions/` — chat session JSON files (one per session)
- `usage/` — LLM token usage records (`records.jsonl`)
- `auth.json` — provider API keys and OAuth tokens

The `{slug}` is a SHA-256 prefix of the git repo root path, making sessions
project-scoped even when working from different checkouts.

## Environment Prompt
The LLM receives environment context at the start of each session via `internal/agent/prompt.go`:

```
<env>
  Working directory: /path/to/project
  Workspace root folder: /path/to/project
  Is directory a git repo: yes
  Git branch: main
  Platform: darwin
  Today's date: Fri Jun 12 2026
</env>
```

The git branch is resolved via `git rev-parse --abbrev-ref HEAD` when the workspace is a git repo.
