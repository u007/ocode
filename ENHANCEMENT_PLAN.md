# Ocode Enhancement Plan

**Goal:** Close feature gaps between ocode (clone) and opencode (upstream).

**Current state:** Core TUI, MCP client (local+remote), multi-provider auth, OAuth for major providers, session persistence, undo/redo, LSP, 3 agent modes, 2 themes.

---

## Phase 1: Quick Wins + Visual Fixes ✅ COMPLETE

**Goal:** Immediate UX improvements, low effort.

### 1.1 Project Path in Sidebar ✅
- Add "Project" section at top of sidebar
- Display shortened cwd (e.g., `~/www/ocode` → `ocode` or `www/ocode`)
- Use `shortenSidebarPath` logic or extract basename

### 1.2 Config Layer Expansion ✅
- Add `.opencode/` directory support (agents/, commands/, plugins/, skills/, tools/, themes/)
- Add `OPENCODE_CONFIG_DIR` env override
- Add `OPENCODE_CONFIG_CONTENT` env override
- Support singular dir names for backwards compat

### 1.3 Theme System Expansion ✅
- Move themes from hardcoded to `~/.config/opencode/themes/` and `.opencode/themes/`
- Load theme JSON files dynamically

**Review fixes applied:**
- Fixed `OPENCODE_MODEL` env override priority (moved before TUI config)
- Deduplicated `findProjectRoot` → exported as `config.FindProjectRoot()`
- Added empty string guard to `shortenWorkingDir`
- Fixed Windows APPDATA relative path in `themeSearchPaths`
- Added 6 new config tests (`loadFromString`, `OPENCODE_CONFIG_DIR`, `OPENCODE_CONFIG_CONTENT`, `.opencode/`)

---

## Phase 2: MCP Enhancement ✅ COMPLETE

**Goal:** Full MCP parity with opencode — CLI management, OAuth, timeouts.

### 2.1 MCP CLI Commands ✅
- `opencode mcp add` — interactive wizard (local vs remote)
- `opencode mcp list` / `opencode mcp ls` — show servers + status
- `opencode mcp auth <name>` — trigger OAuth flow
- `opencode mcp auth list` — list OAuth-capable servers + status
- `opencode mcp logout <name>` — clear stored tokens
- `opencode mcp debug <name>` — diagnose connection issues
- Write to config file (global or project)

### 2.2 MCP OAuth Support ✅
- Auto-detect OAuth requirement from remote MCP server
- Store tokens in `~/.local/share/opencode/mcp-auth.json`
- Auto token refresh
- Manual trigger via `/mcp-auth` or CLI
- `oauth: false` config to disable

### 2.3 MCP Timeout Config ✅
- Add `timeout` field to `MCPConfig` (default 5000ms)
- Pass timeout to MCP client initialization
- Surface timeout errors in sidebar

---

## Phase 3: CLI Execution Modes ✅ COMPLETE

**Goal:** Headless, scripting, and web access.

### 3.1 `opencode run` ✅
- Non-interactive prompt execution
- Flags: `--prompt`, `--model`, `--agent`, `--session`, `--continue`, `--fork`, `--file`, `--format`, `--title`, `--attach`, `--port`
- Supports attaching to running `serve` instance
- JSON event output format

### 3.2 `opencode serve` ✅
- Headless HTTP API server
- Reuse MCP connections across requests
- `OPENCODE_SERVER_PASSWORD` / `OPENCODE_SERVER_USERNAME` basic auth
- Port/hostname config

### 3.3 `opencode web` ✅
- Same as serve + opens browser

### 3.4 `opencode acp` ✅
- Agent Client Protocol via stdin/stdout (ND-JSON)
- For advanced tooling integrations

### 3.5 `opencode models [provider]`
- List available models per provider
- Use models.dev registry

---

## Phase 4: Extensibility System

**Goal:** User-defined skills, plugins, and commands.

### 4.1 Skills System
- `.opencode/skills/<name>/SKILL.md` directory structure
- Load skills at session start
- Skill metadata: name, description, trigger patterns
- `skill` tool already exists — wire up directory loading
- `~/.config/opencode/skills/` for global skills

### 4.2 Plugins System
- `.opencode/plugins/` directory
- Plugin definition: name, description, commands, tools
- Load plugins at startup
- Plugin isolation (separate agent context?)

### 4.3 Custom Commands
- `.opencode/commands/` directory
- User-defined slash commands with prompts
- Command metadata: name, description, prompt template
- Tab completion for custom commands

### 4.4 Custom Tools
- `~/.config/opencode/tools/*.json` — already partially implemented
- Complete the loading pipeline
- Tool definition: name, description, command, parameters

---

## Phase 5: Agents & Permissions

**Goal:** Multi-agent system with granular access control.

### 5.1 Primary Agents Expansion
- Add **Review** agent (read-only + write REVIEW.md)
- Add **Debug** agent (bash + read tools only)
- Add **Docs** agent (file ops, no system commands)
- Agent switching via Tab (already exists, expand to 5 agents)
- Custom agents via config + `.opencode/agents/` markdown files

### 5.2 Subagent System
- **General** subagent — multi-step tasks, parallel work
- **Explore** subagent — fast read-only codebase exploration
- **Scout** subagent — external docs, dependency research
- `Task` tool for subagent delegation
- `@agent` mention syntax for manual invocation

### 5.3 Permission System
- Granular `allow` / `deny` / `ask` per tool
- Wildcard patterns (e.g., `mymcp_*: "ask"`)
- Config: `permission` field in opencode.json
- TUI approval dialog for `ask` permissions
- Per-agent permission overrides
- `task_permission` for subagent gating

### 5.4 Agent Mode Refinement
- Current 3 modes → map to 5 agents
- Mode-specific tool gating (already partially done)
- System prompt per agent

---

## Phase 6: Polish & Advanced Features

**Goal:** Complete the feature set, improve DX.

### 6.1 Image Support
- Drag/drop images into terminal
- Paste images from clipboard
- Send to vision-capable models
- File reference syntax: `@image.png`

### 6.2 apply_patch Improvements
- Line range targeting
- Better error recovery
- Hunk-based editing

### 6.3 Formatters
- Code formatter configuration in opencode.json
- Auto-format on file write
- Per-language formatter rules

### 6.4 Hooks
- Pre-action hooks (before tool execution)
- Post-action hooks (after tool execution)
- Hook definition in config

### 6.5 Remote Config
- `.well-known/opencode` endpoint
- Organizational defaults
- Merge with local config

### 6.6 GitHub Integration
- GitHub Actions support
- PR review mode
- Issue triage agent

---

## Execution Notes

- Each phase is independent enough to ship separately
- Phase 1 is prerequisite for nothing — can be done in parallel with anything
- Phase 2 (MCP) has no dependencies on other phases
- Phase 3 (CLI modes) requires Phase 2 MCP to be stable for `serve` attachment
- Phase 4 (extensibility) is standalone
- Phase 5 (agents) is the largest — consider splitting into 5.1+5.2 and 5.3+5.4
- Phase 6 is all polish — can be done incrementally

## Risk Assessment

| Phase | Risk | Effort |
|-------|------|--------|
| 1 | Low | 1-2 days |
| 2 | Medium (OAuth complexity) | 3-5 days |
| 3 | High (HTTP server, web UI) | 5-7 days |
| 4 | Medium | 3-5 days |
| 5 | High (permission system, subagent orchestration) | 7-10 days |
| 6 | Low-Medium (per feature) | 5-8 days |
