# ocode

Terminal AI coding agent — opencode clone in Go.

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

Base TUI scaffold only. No LLM wired yet.

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

## Stack

- Go 1.26.1
- Bubble Tea / Bubbles / Lipgloss (Charm TUI)

## Layout

```
main.go              entry
internal/tui/        Bubble Tea model, view, update
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
- `!command` hands the terminal to the process (interactive programs like `vim`, `less`, `git diff` work). Output is not captured into the chat transcript.
