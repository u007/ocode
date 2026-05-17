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
- The TUI restores the most recently selected model from opencode state unless `OPENCODE_MODEL` is set.

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
      "write": "ask",
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

- `normal`: follow tool and bash-prefix rules.
- `yolo`: allow permission-gated tools without prompting, while still respecting agent mode restrictions and hard safety blocks.
- `locked`: allow read/search-style tools only.

Use `/permissions` to view or set rules, `/permissions bash:git allow` for shell prefixes, and `/yolo [on|off|status]` to toggle YOLO mode. The TUI also accepts `--yolo`/`-yolo`; `ocode run` accepts `--yolo`.

## Stack

- Go 1.26
- Bubble Tea / Bubbles / Lipgloss (Charm TUI)

## Layout

```
main.go              entry
internal/tui/        Bubble Tea model, view, update
```

## Sessions

- `/session`, `/sessions`, and `/resume` open a picker with current-project ocode and Claude Code sessions, sorted newest first.
- `/session list` still prints saved sessions, and `/session load <id>` loads one directly.
- Claude Code sessions are marked `[claude]`; resuming one clones it into ocode history as `claude-<id>`.
