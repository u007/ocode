# ocode - opencode clone in Go

## Progress

- [x] Initialize `todo.md`
- [x] Implement Configuration System (JSONC supported, MCP config, TUI config, Watcher ignore)
- [x] Implement File Tools (`read`, `write`, `list`, `delete`)
- [x] Implement Search Tools (recursive `glob`, pure Go `grep`)
- [x] Implement Command and Edit Tools (`bash`, `edit`, `multiedit`)
- [x] Implement Patch and Todo Tools (`patch`, `todowrite`, `todoread`)
- [x] Implement Skill and Question Tools (`skill`, `question`)
- [x] Implement Web Tools (`webfetch`, DuckDuckGo `websearch`)
- [x] Implement LLM Client (OpenAI, Anthropic, OpenRouter, Google, Z.AI, Moonshot, MiniMax, Alibaba, Chutes, Requesty, 302.AI, DeepSeek, Groq, Mistral, OpenCode Zen/Go)
- [x] Full Anthropic Tool Support (Schema translation)
- [x] Support for Coding Plans (Z.AI Coding, Alibaba Coding, Chutes AI Coding)
- [x] Implement Agent Reasoning Loop (Tool execution, pause for questions)
- [x] Support `AGENTS.md` and `CLAUDE.md` context
- [x] Support for Subagents (`agent` tool)
- [x] Implement Slash Commands (`/model`, `/connect`, `/session`, `/compact`, `/undo`, `/redo`, `/export`, `/new`, `/thinking`, `/models`, `/details`, `/init`, `/editor`, `/exit`, `/themes`, `/share`, `/help`)
- [x] TUI Bash Shortcut (`!command`) and Fuzzy File References (`@path`)
- [x] Leader Key Support (`ctrl+x`) and Command Palette (`ctrl+p`)
- [x] Integrated `opencode.json` provider settings into LLM client
- [x] Support for Global Custom Tools (`~/.config/opencode/tools/*.json`)
- [x] MCP Support (Local stdio and Remote HTTP/SSE)
- [x] Support OpenCode sessions (Auto-save and Resume)
- [x] Snapshots (Undo/Redo file changes)
- [x] `.gitignore`, `.ignore`, and `watcher.ignore` awareness for tools
- [x] Update TUI State and Rendering (Status bar, Themes: `opencode`, `tokyonight`)
- [x] Cross-platform support (Windows/Unix aware)
- [x] Core unit tests for reasoning and tools
- [x] Advanced Context compaction logic (auto-summarize)
- [x] Advanced TUI features (scroll acceleration, mouse support)
- [x] Persistent session storage optimization (indexing, sorting)
- [x] Hybrid LSP tool implementation
- [x] Final Integration and Verification

## Pending Items

- [ ] Full LSP client protocol (persistent server sessions)
- [ ] Direct Google OAuth2 flow (local server callback)
