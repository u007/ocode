# Agent Instructions - ocode

## Tech Stack
- Go 1.23
- Charm TUI (Bubble Tea, Lipgloss)
- LLM Providers: OpenAI, Anthropic, Google, Z.AI, Alibaba

## Coding Standards
- Use modular packages in `internal/`.
- All file modifications must use `snapshot.Backup`.
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

## User Interaction
- TUI supports `/commands` and `!shell`.
- Use `ctrl+x` for leader keys and `ctrl+p` for palette.
- Sessions are automatically saved and resumed.
