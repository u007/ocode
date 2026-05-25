# Agent Instructions - ocode

## Tech Stack
- Go 1.23
- Charm TUI (Bubble Tea, Lipgloss)
- LLM Providers: OpenAI, Anthropic, Google, Z.AI, Alibaba

## Coding Standards
- Use modular packages in `internal/`.
- Respect `.gitignore` and `watcher.ignore`.
- Follow Go best practices and standard formatting.

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
- Use `ctrl+x` for leader keys and `ctrl+p` for palette.
- Avoid introducing raw shortcuts that are likely to conflict with host terminals like Warp, Ghostty, and iTerm2; prefer `ctrl+x` leader sequences for non-essential UI toggles.
- Sessions are automatically saved and resumed.
