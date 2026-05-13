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

## Stack

- Go 1.26
- Bubble Tea / Bubbles / Lipgloss (Charm TUI)

## Layout

```
main.go              entry
internal/tui/        Bubble Tea model, view, update
```
