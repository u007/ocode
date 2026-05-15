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

Compact defaults:

- `enabled`: `true`
- `trigger_ratio`: `0.75`
- `max_ratio`: `0.85`
- `min_free_tokens`: `4096`
- `summary_provider`: unset, use current provider
- `summary_model`: unset, use current model

## Stack

- Go 1.26
- Bubble Tea / Bubbles / Lipgloss (Charm TUI)

## Layout

```
main.go              entry
internal/tui/        Bubble Tea model, view, update
```
