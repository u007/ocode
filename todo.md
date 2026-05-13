# ocode - opencode clone in Go

## Progress

- [x] Initialize `todo.md`
- [x] Implement Configuration System (JSONC supported, MCP config)
- [x] Implement File Tools (`read`, `write`)
- [x] Implement Search Tools (recursive `glob`, pure Go `grep`)
- [x] Implement Command and Edit Tools (`bash`, `edit`)
- [x] Implement Patch and Todo Tools (`apply_patch`, `todowrite`)
- [x] Implement Skill and Question Tools (`skill`, `question`)
- [x] Implement Web Tools (`webfetch`)
- [x] Implement LLM Client (OpenAI, Anthropic, OpenRouter, Google, Z.AI, Moonshot, MiniMax, Alibaba, Chutes)
- [x] Full Anthropic Tool Support (Schema translation)
- [x] Support for Coding Plans (Z.AI Coding, Alibaba Coding, Chutes AI Coding)
- [x] Implement Agent Reasoning Loop (Tool execution, pause for questions)
- [x] Support `AGENTS.md` and `CLAUDE.md` context
- [x] Implement Basic Slash Commands (`/model`, `/connect`, `/session`, `/compact`)
- [x] Integrated `opencode.json` provider settings into LLM client
- [x] Support for Global Custom Tools (`~/.config/opencode/tools/*.json`)
- [x] Basic MCP Support (local stdio servers)
- [x] Support OpenCode sessions (Auto-save and Resume)
- [x] Update TUI State and Rendering (Tool visualization, history persistence)
- [x] Cross-platform support (Windows/Unix aware, native grep)
- [x] Final Integration and Verification

## Pending Items

- [ ] Support more providers (Requesty, 302.AI, etc.)
- [ ] Real-time Web Search tool
- [ ] Support Remote/SSE MCP servers
- [ ] LSP integration
- [ ] Snapshots (undo/revert)
- [ ] Advanced Context compaction logic
- [ ] File watcher ignore patterns
- [ ] Advanced TUI features (themes, keybinds)
- [ ] Persistent session storage optimization
