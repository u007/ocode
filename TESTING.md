# ocode Testing & Status

Track tested features, provider integrations, and known issues.

## Platform Support

| Platform | Status | Notes |
|----------|--------|-------|
| macOS | ✅ Tested | Full TUI support, all features working |
| Linux | ✅ Tested | Full TUI support, all features working |
| Windows | 🆘 Needs volunteer | WSL2 recommended; native Windows testing needed |

## Tested & Working ✅

### Provider Integrations
- [x] OpenAI API (ChatGPT subscription models)
- [x] Deepseek API
- [x] Xiaomi Coding (xiaoMi coding plan)
- [x] Zend Models (opencode zend)
- [x] Anthropic API (Claude models)

### Features
- [x] Compaction with small model (configurable summary provider/model)
- [x] Automatic permission decisions (`auto_permission_model`)
- [x] Extended thinking toggle (`Ctrl+T` on supported models)
- [x] Session auto-save and resume
- [x] MCP client (local + remote servers)
- [x] Git integration (status, diff, staging, commits, branches)
- [x] File browser with inline vim editor
- [x] LSP integration (hover docs, go-to-definition)
- [x] Theme system (tokyonight, catppuccin-mocha, etc.)
- [x] Permissions system (normal, yolo, locked modes)
- [x] Tool result truncation and on-disk retrieval
- [x] Context window tracking and telemetry
- [x] Foreground bash → background (`Ctrl+B`)
- [x] Async agent runs with transcript capture
- [x] Background process management (256KB circular buffer)

### Agents
- [x] Advisor Tool
- [x] Explorer Agents

## Known Issues 🐛

- [ ] *Add known bugs, regressions, or edge cases here*

## Untested / TODO 🔄

### Provider Integrations
- [ ] Google Gemini API (full integration test)
- [ ] Z.AI API (production validation)
- [ ] Alibaba API (production validation)
- [ ] GitHub Copilot OAuth flow under edge conditions
- [ ] Multi-provider fallback chains

### Features
- [ ] Prompt caching hit rates across multiple sessions
- [ ] Thinking mode (o1/o3 models) under high context load
- [ ] Custom compaction model with different providers (e.g., summarize with Haiku while chatting with Opus)
- [ ] Permission rules with complex bash prefix combinations
- [ ] Session cloning from Claude Code (mixed provider scenarios)
- [ ] MCP remote server timeout handling
- [ ] External editor modes (tmux-split, tmux-window) on non-macOS
- [ ] Mouse selection in transcript across very long scrollback
- [ ] Undo/redo with large session histories

### Agents
- [ ] Parallel agent execution under load
- [ ] Agent timeout and cleanup after forced termination
- [ ] Subagent session isolation and cross-session state

### Integrations
- [ ] Skills system (registration, enable/disable, install/remove)
- [ ] HTTP server mode (`ocode serve`)
- [ ] Config hot-reload while TUI is running

### Edge Cases
- [ ] Empty/null tool results
- [ ] Tool results > 1MB
- [ ] Sessions with 10,000+ turns
- [ ] Rapid permission mode toggling (`Ctrl+O` spam)
- [ ] Terminal resize during active bash execution
- [ ] Switching providers mid-session
- [ ] YOLO mode with conflicting bash prefix rules
- [ ] Config merge conflicts (global vs project)

### Performance
- [ ] Compaction latency on 50KB+ context
- [ ] TUI render time with 1000+ file tree entries
- [ ] MCP client handling 100+ concurrent tool calls

## Test Running

```bash
# Unit tests
go test ./...

# Verbose
go test -v ./...

# Specific package
go test ./internal/tui -v

# Coverage
go test -cover ./...
```

## How to Add Tests

1. Identify untested feature/provider above
2. Move to "Tested & Working" with date and notes
3. If issues found, create a GitHub issue and link it
4. Update this file and commit

Example:
```markdown
- [x] Google Gemini API (tested 2026-06-03, fully working with custom rate limits)
```
