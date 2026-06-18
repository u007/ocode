# Agent Instructions — ocode

This file is the canonical, always-on briefing for any LLM agent (ocode, Claude
Code, Cursor, etc.) that loads `AGENTS.md` from the repo root. It is loaded
silently and unconditionally at session start by `internal/agent/context.go` —
see "Context Loading" below. Keep the content here focused on **cross-cutting
rules that affect more than one file**: recurring bug classes, architectural
constraints, and process rules. Feature descriptions belong in `README.md` or
the `skills/ocode-*` catalog, not here.

`CLAUDE.md` is a thin pointer to this file (kept for tools that auto-load it by
name). Do not duplicate content between the two — update here only.

## Tech Stack
- Go 1.23
- Charm TUI (Bubble Tea, Lipgloss v2 — note v2 wraps each rune in its own
  SGR sequence; substring assertions on rendered output need `stripANSI` from
  `internal/tui/selection.go`)
- LLM providers: OpenAI, Anthropic, Google, Z.AI, Alibaba, plus the
  `opencode-go` (DeepSeek) and Minimax routes

## Git Worktrees
The default location for `git worktree` checkouts is `.worktrees/` in the
project root. The directory is gitignored — worktree contents are
developer-local state and must never be committed.
```bash
git worktree add .worktrees/feature-branch feature-branch
```

## Coding Standards
- Use modular packages in `internal/`.
- Respect `.gitignore` and `watcher.ignore`.
- Follow Go best practices and standard formatting (`gofmt`, `go vet`).
- **Avoid `git stash` / `git reset --hard` / `git checkout -- <file>` /
  `git clean -fd` as a default coping strategy.** They destroy user state
  the user may not be unable to recover. If a change conflicts, stop and
  ask; do not unwind the user's working tree.

## Context Loading
- `AGENTS.md`, `CLAUDE.md`, `OCODE.md`, and `.cursorrules` are loaded at
  session start by `internal/agent/context.go::LoadContext`.
- If a context file is tracked by git AND has unstaged modifications, the
  committed `HEAD` version is used instead of the working-tree copy. This
  keeps the base prompt stable across edits; commit the changes to make
  them effective. A line is logged to stderr when this swap occurs.
- When reading files, show only the relevant excerpts needed for the
  current task — do not dump entire files.

## File Reading
When reading files, show only the relevant excerpts needed for the current
task. Whole-file dumps waste the context window and obscure the signal.

## TUI Output Safety (alt-screen)
The TUI runs in Bubble Tea's alt-screen. Any raw write to `os.Stdout` /
`os.Stderr` from a path the running TUI invokes paints directly over the
rendered frame and corrupts it (text overlap / "hairwire" at the bottom of
the chat, status line pushed off-screen). This is a recurring bug class —
when fixing rendering glitches, suspect raw writes, not just layout.

In any code reachable while the TUI is live (agent loop, tools, hooks,
session, plugins, auth, config reload):

- **Never** `fmt.Print*`, `fmt.Fprint*(os.Stdout|os.Stderr, …)`, `println`,
  or raw `os.Stderr.Write` for diagnostics. Use `agent.emitDebug` /
  `agent.DebugAppendf` inside the `agent` package, or the stdlib `log`
  package elsewhere — `tui.Run()` calls `log.SetOutput(debugLogWriter{})`,
  so `log.Printf` lands in the debug panel, never the terminal. `emitDebug`
  falls back to stderr only when no sink is set (headless `run`/`serve`/`acp`).
- **Capture subprocess output** (`cmd.Stdout = &buf`) — never inherit the
  terminal with `cmd.Stdout = os.Stdout`. Surface captured output via
  `log`/the error, not the inherited fd.
- **Clamp one-line status/activity rows** with `.Width(w).MaxHeight(1)` so
  long content can't wrap and grow the bottom chrome past the terminal
  height.

## TUI Mouse: clickable chrome vs selectable content
Terminal mouse capture is **global per frame** — `tea.View.MouseMode` is
one flag for the whole screen, not per-region. Enabling capture makes
tabs/menus/buttons clickable but **blocks native terminal text selection**.
The two are mutually exclusive and cannot be scoped to a region. Never
disable `MouseMode` to regain native selection — that kills every click
target.

The correct pattern (and the only one that satisfies "nav is clickable AND
content is selectable"): **keep mouse capture ON and implement selection
in-app.** Every scrollable/content surface follows the same recipe (see
the transcript, log tab, files preview, git diff, sidebar, and agent-detail
drill-in for working copies):

- A `selectionState{dragging, startLine, startCol, endLine, endCol, active}`
  field per surface.
- **Press** inside the content region → record start + `dragging:true`
  (return handled).
- **Motion** while dragging → update end, set `active` only once the
  anchor actually moved, re-render with `applySelectionHighlight(styled, raw, …)`.
- **Release** → if `active`, `extractSelectionText(rawLines, …)` +
  `clipboard.WriteAll` (log copy errors, never swallow); if **not active**
  (no drag distance) clear and **fall through to the click handler** so a
  plain click still toggles/opens. This press-starts-drag /
  release-decides-click-vs-copy split is what lets one region be both
  clickable and selectable.
- Track the surface's styled + ANSI-stripped (`stripANSI`) visual lines so
  highlight and extract operate on the same coordinate space. Selection
  coords are **screen-row/col relative to the content's top-left**
  (`contentTopY`, left chrome = border(1)+padding(1) = 2 cols for bordered
  boxes).

Mouse-mode gotcha for **hover** effects (underline-on-hover):
`MouseModeCellMotion` only emits motion while a button is held — it
delivers no plain-hover events. Hover requires `MouseModeAllMotion`, and
the motion handler must process `MouseNone` motion (don't early-return on
`Button != MouseLeft` before the hover check). `AllMotion` fires on every
cursor move, so the hover handler must be cheap: read cached
geometry/hit-test maps populated during render, and only return a redraw
when the hovered target actually changes.

## TUI Clickable URLs — confirm before opening
URLs in the chat transcript (markdown `[text](url)` and raw `https?://...`)
are clickable on the chat tab. **A click always opens a Y/N confirmation
dialog before launching the browser** — `m.showURLDialog` in
`internal/tui/model.go`. There is no "trust once for the session"
shortcut. The URL is sanity-checked by `looksLikeURL` (http/https only,
host has a dot or is `localhost`) but is not otherwise sanitized; the
dialog is the safety layer. Adding a new URL surface (sidebar, log tab,
file preview) must follow the same confirm-before-open pattern.

## TUI In-Chat Find Bar
`ctrl+f` on the chat tab opens a find bar above the input area (NOT on
other tabs — the model picker, file search, and the log tab all bind
`ctrl+f` for themselves). The bar is closed when the user leaves the chat
tab (`closeChatSearchIfLeavingChat`). Implementation lives in
`internal/tui/chat_search.go`; do not add a second find surface without
consolidating the dispatch.

## User Interaction
- TUI supports `/commands` and `!shell`.
- **Slash command queuing.** All slash commands entered while the agent is
  streaming or compacting must be queued (`m.queuedCommands`) and
  executed one-at-a-time after the current work ends — not run
  immediately. Only `/exit`, `/quit`, `/q` bypass the queue
  unconditionally. Synchronous local UI/config commands that do not start
  a new agent request may also bypass the queue; keep any such exceptions
  centralized in `handleCommand` (the single chokepoint covering all
  callers: enter key, palette, keybinds, leader shortcuts, hotkeys) and
  **document them in the running list below** (so the next contributor
  knows the rule). Drain `m.queuedCommands` in `agentStreamDoneMsg` and
  `compactFinishedMsg` handlers, after `queuedInputs` are processed, so
  a command never fires while another stream is in flight.
  - Current instant commands: `/model`, `/models`, `/help`, `/thinking`,
    `/details`, `/login`, `/new`, `/clear`, `/sidebar`, `/commands`,
    `/permissions`, `/yolo`, `/small-model`, `/editor`, `/editor-mode`,
    `/themes`, `/theme`, `/lsp`, `/usage`, `/share`, `/connect`, `/agent`,
    `/mcp`, `/advisor`, `/mask`, `/btw`, `/by-the-way`, `/rc`,
    `/remote-control`, `/search`, `/find`.
  - **Queued by design (mutates persistent state mid-stream, so it must
    wait for the current turn to end):** `/add-dir`, `/add-dirs`.
  - The list above is the source of truth; keep the in-code check in
    `handleCommand` in sync.
- Use `ctrl+x` for leader keys and `ctrl+p` for palette.
- Avoid introducing raw shortcuts that are likely to conflict with host
  terminals like Warp, Ghostty, and iTerm2; prefer `ctrl+x` leader
  sequences for non-essential UI toggles.
- Sessions are automatically saved and resumed.

## Data Storage
All persistent state lives under a single cross-platform global directory
resolved by `internal/paths.GlobalDataDir()`:

| Platform | Path |
|----------|------|
| macOS    | `~/.local/share/opencode` |
| Linux    | `$XDG_DATA_HOME/opencode` (or `~/.local/share/opencode`) |
| Windows  | `%LOCALAPPDATA%\opencode` |

Sub-directories:
- `project/{slug}/sessions/` — chat session JSON files (one per session)
- `usage/` — LLM token usage records (`records.jsonl`)
- `auth.json` — provider API keys and OAuth tokens

The `{slug}` is a SHA-256 prefix of the git repo root path, making sessions
project-scoped even when working from different checkouts. The TUI's
`m.workDir` is the source of truth for project resolution (set via
`/cd`, `--dir`, or `session.SetWorkDir`); `os.Getwd()` is not — `/cd`
can change the project root without changing the process CWD on every
caller.

## Environment Prompt
The LLM receives environment context at the start of each session via
`internal/agent/prompt.go`. The exact shape is the ` <env>...</env>` block
in that file; if you are reading the values out of the prompt at runtime,
parse the block — do not assume the example below is current. The
illustrative shape is:

```
<env>
  Working directory: /path/to/project
  Workspace root folder: /path/to/project
  Is directory a git repo: yes
  Git branch: main
  Platform: darwin
  Today's date: <resolved at session start>
</env>
```

The git branch is resolved via `git rev-parse --abbrev-ref HEAD` when the
workspace is a git repo.
