# Contributing to ocode

Thanks for your interest in contributing! This document covers how to get set up, the development workflow, and the conventions to follow.

## Prerequisites

- **Go 1.26.1+** — `go version` to verify
- **Git**
- **Node.js + npm** — for the web UI (`web/` directory)
- An LLM provider account (OpenAI, Anthropic, Google, etc.)

## Getting Started

```bash
git clone https://github.com/u007/ocode.git
cd ocode
go mod download
go run .
```

## Development Workflow

### Run in development

```bash
go run .
```

### Build

```bash
go build -o ocode .
```

### Cross-platform builds

```bash
make build-all        # All platforms
make build-darwin     # macOS only
make build-linux      # Linux only
make build-windows    # Windows only
```

### Release build

```bash
make release          # Versioned builds + sha256sums in release/
```

### Web UI

```bash
cd web && npm install && npm run dev    # Dev server
make web-build                          # Production build
```

## Testing

Run the full test suite before opening a pull request. The `make test` target
runs every test in the repository via `go test ./...` and exits non-zero on
any failure, so it is safe to gate a PR (or a pre-commit hook) on it.

```bash
make test                # Run all tests in the repo
```

The equivalent direct command (use this if you don't have `make` available):

```bash
go test ./...
```

Run a specific package while iterating:

```bash
go test ./internal/tui/...
go test ./internal/config/...
```

See [TESTING.md](TESTING.md) for tested features, known issues, and platform support.

## Project Structure

```
.
├── main.go                    # Entry point
├── internal/
│   ├── agent/                 # LLM agent loop, provider clients, tools
│   ├── auth/                  # OAuth flows, credential storage
│   ├── config/                # Config loading (opencode.json, ocodeconfig.json)
│   ├── hooks/                 # Lifecycle hooks
│   ├── runcli/                # CLI runner (headless mode)
│   ├── session/               # Session persistence and resume
│   ├── snapshot/              # Snapshot system
│   ├── skill/                 # Skills system (install, load, CLI)
│   ├── tool/                  # Tool definitions (read, write, bash, grep, etc.)
│   ├── tui/                   # Bubble Tea TUI (views, models, rendering)
│   └── version/               # Version info
├── web/                       # Web UI (WIP)
├── AGENTS.md                  # Agent instructions (loaded into context)
├── CLAUDE.md                  # Project instructions for Claude
├── SETUP.md                   # Setup and configuration guide
└── TESTING.md                 # Test status and known issues
```

## Code Conventions

### Go style

- Follow standard Go formatting (`gofmt`, `go vet`).
- Use modular packages under `internal/`.
- Keep exported APIs minimal — prefer unexported within `internal/`.
- Errors should be wrapped with context where helpful.

### TUI safety (critical)

The TUI runs in Bubble Tea's **alt-screen**. Any raw write to `os.Stdout` or `os.Stderr` from code the TUI invokes will corrupt the rendered frame.

- **Never** use `fmt.Print*`, `fmt.Fprint*(os.Stdout|os.Stderr, ...)`, or `println` for diagnostics in TUI-reachable code.
- Use `agent.emitDebug` / `agent.DebugAppendf` inside the `agent` package, or the stdlib `log` package elsewhere — `tui.Run()` redirects `log` into the debug panel.
- When spawning subprocesses, **capture** output (`cmd.Stdout = &buf`) — never inherit the terminal with `cmd.Stdout = os.Stdout`.
- Clamp one-line status rows with `.Width(w).MaxHeight(1)` to prevent wrapping.

### Mouse handling

- `tea.View.MouseMode` is global per frame — you cannot enable it for one region and disable it for another.
- Keep mouse capture ON and implement text selection in-app if you need both clickable chrome and selectable content.
- Hover effects require `MouseModeAllMotion` (CellMotion only emits while a button is held).

### Context files

- `AGENTS.md`, `CLAUDE.md`, and `.cursorrules` are loaded at session start.
- If a context file is tracked by git AND has unstaged modifications, the committed `HEAD` version is used instead of the working-tree copy. Commit changes to make them effective.

## Submitting Changes

1. Create a feature branch from `main`.
2. Make your changes, keeping commits focused and well-described.
3. Run `make test` and `go vet ./...` before committing. The `make test` target
   runs the full suite (`go test ./...`); do not skip it, even for small changes
   — broken tests anywhere in the repo will fail CI.
4. Open a pull request with a clear description of what changed and why.

### Commit messages

Follow the conventional style used in the repo:

```
feat(scope): short description
fix(scope): short description
docs(scope): short description
chore(scope): short description
```

Examples from the log: `feat(skills): add skills CLI`, `docs: add platform support table`, `chore: ignore session export files`.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
