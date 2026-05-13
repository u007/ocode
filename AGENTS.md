# ocode — agent instructions

## Run

```sh
go run .           # dev
go build -o ocode . && ./ocode  # build + run
```

No test framework wired yet. No lint/format config set up.

## Architecture

```
main.go              → calls tui.Run()
internal/tui/tui.go   → tea.NewProgram(newModel(), tea.WithAltScreen())
internal/tui/model.go → Bubble Tea model (chat transcript viewport + input textarea)
```

Messages are echoed locally (`"(no llm wired yet)"`) — no LLM integration exists.

## Stack

- Go 1.26.1
- Bubble Tea v1, Bubbles v1, Lipgloss v1 (Charm ecosystem)

## Conventions

- `internal/tui/` is the only package — keep it flat until growth justifies splitting
- Model holds all state: `messages []message`, viewport, input. Update/view are methods on `model`.
- Lipgloss styles defined as package-level vars in `model.go`
- No exported API surface beyond `tui.Run()`

## Constraints

- No tests, no CI — anything added should include both
- Single binary — avoid external runtime deps
- Bubble Tea `tea.WithAltScreen()` is set — be mindful of terminal lifecycle
