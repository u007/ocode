---
name: ocode-tools
description: The ocode tool system — the Tool interface, LoadBuiltins registration, every built-in tool at a glance, the LSP manager lifecycle, permission defaults, and the NoticedError pattern. Use this when adding a new tool, modifying tool dispatch, debugging tool execution, or understanding how tools wire into the permission system.
when_to_use: When working on internal/tool/ — adding a new tool, changing tool registration in LoadBuiltins, modifying the Tool interface, fixing tool execution bugs, or wiring tool permissions. Also triggered by: "add tool", "new tool", "tool interface", "tool permissions", "LoadBuiltins", "LSP manager".
---

# ocode Tools Field Guide

The tool system is in `internal/tool/` (22 files). Tools implement a simple Go interface, register in `LoadBuiltins()`, and are executed by the agent loop after passing through permission gates and hooks.

## 1. The `Tool` interface (`tool.go:10`)

```go
type Tool interface {
    Name() string
    Description() string
    Definition() map[string]interface{}   // OpenAI tool schema format
    Execute(args json.RawMessage) (string, error)
    Parallel() bool                       // can run concurrently with other tools
}
```

**Parallelism matters:** The agent loop sorts tool calls into parallel-capable (`Parallel() == true`) and sequential (`false`). Parallel tools (read, glob, grep, lsp, ast, skill, lsp_diagnostics, GitHub tools) run in goroutines. Sequential tools (bash, write, edit, delete, webfetch, apply_patch) block the loop — each runs to completion before the next starts.

**`NoticedError`** — tools that encounter a recoverable problem (e.g. LSP server not installed) wrap their error with a user-facing notice:
```go
type NoticedError struct {
    Err    error
    Notice string  // Shown in transcript, NOT sent to LLM
}
```
The TUI strips the `NOTICE:` prefix and renders the remainder as a transient message.

## 2. Tool registration (`tool.go:40:LoadBuiltins`)

```go
func LoadBuiltins(cfg *config.Config) ([]Tool, *lsp.Manager)
```

Called once per session from `agent.go:NewAgent()`. Creates one shared `lsp.Manager` (lives as long as the session) and registers 26+ tools:

| # | Tool | File | Name() | Parallel | Permission default |
|---|------|------|--------|----------|-------------------|
| 1 | `ReadTool` | `file.go` | `read` | ✅ yes | allow |
| 2 | `WriteTool` | `file.go` | `write` | ❌ no | allow |
| 3 | `ReplaceLinesToolImpl` | `file.go` | `replace_lines` | ❌ no | allow |
| 4 | `DeleteTool` | `file.go` | `delete` | ❌ no | ask |
| 5 | `GlobTool` | `search.go` | `glob` | ✅ yes | allow |
| 6 | `GrepTool` | `search.go` | `grep` | ? | allow |
| 7 | `BashTool` | `exec.go` | `bash` | ❌ no | ask |
| 8 | `EditTool` | `file.go` | `edit` | ❌ no | allow |
| 9 | `MultiEditTool` | `file.go` | `multiedit` | ❌ no | allow |
| 10 | `MultiFileEditTool` | `file.go` | `multi_file_edit` | ❌ no | allow |
| 11 | `PatchTool` | `patch.go` | `apply_patch` | ❌ no | allow |
| 12 | `TodoWriteTool` | `patch.go` | `todowrite` | ❌ no | allow |
| 13 | `TodoReadTool` | `patch.go` | `todoread` | ? | allow |
| 14 | `SkillTool` | `misc.go` | `skill` | ✅ yes | allow |
| 15 | `QuestionTool` | `misc.go` | `question` | ❌ no | allow |
| 16 | `WebFetchTool` | `web.go` | `webfetch` | ❌ no | ask |
| 17 | `WebSearchTool` | `web.go` | `websearch` | ? | ask |
| 18 | `RepoCloneTool` | `repo.go` | `repo_clone` | ❌ no | ask |
| 19 | `RepoOverviewTool` | `repo.go` | `repo_overview` | ✅ yes | allow |
| 20 | `PlanEnterTool` | `plan.go` | `plan_enter` | ❌ no | allow |
| 21 | `PlanExitTool` | `plan.go` | `plan_exit` | ❌ no | allow |
| 22 | `ListTool` | `search.go` | `list` | ✅ yes | allow |
| 23 | `LSPTool` | `lsp.go` | `lsp` | ✅ yes | allow |
| 24 | `LSPDiagnosticsTool` | `diagnostics.go` | `lsp_diagnostics` | ✅ yes | allow |
| 25 | `FormatTool` | `formatter.go` | `format` | ❌ no | allow |
| 26 | `GitHubPRTool` | `github.go` | `github_pr` | ✅ yes | ask (implied) |
| 27 | `GitHubIssueTool` | `github.go` | `github_issue` | ✅ yes | ask (implied) |
| 28 | `GitHubWorkflowTool` | `github.go` | `github_workflow` | ✅ yes | ask (implied) |
| 29 | `AstTool` (opt-in) | `ast.go` | `ast` | ✅ yes | allow |

> Permission defaults shown above are the static defaults from `permissions.go:NewPermissionManager()`. Override via `ocodeconfig.json:permissions.tools` or agent-specific permission maps. The `ask (implied)` label means the tool isn't in the static defaults; it inherits `ask` from the generic tool-default rule. `?` marks tools not explicitly listed in the default table — check `permissions.go` for current classification.

**Tools registered outside LoadBuiltins** (added by `agent.go:NewAgent()` after LoadBuiltins):
- `task` (sub-agent spawner from `subagent.go`)
- `advisor` (strategic advisor from `advisor_tool.go`)
- `wait` (block/sleep from `wait_tool.go`)
- `bash_output` / `kill_shell` (process management from `process_tools.go` — registered when `ProcessRegistry` is present)
- MCP tools (registered from MCP server connections)

## 3. LSP manager lifecycle

```go
lspMgr := lsp.NewManager(".")   // created once per session
// shared by LSPTool, LSPDiagnosticsTool, and AstTool
```

All three tools receive `Mgr: lspMgr` at registration. The manager owns the language-server processes. When the session/agent is torn down, `lspMgr.Close()` must be called — failing to do so leaks server processes.

## 4. How tools run (dispatch chain in `agent.go:executeToolCall`)

```
agent.go:Step()
  → finds tool by name in a.tools[]
  → gateToolCall(mode, name, args)      — mode-gating (mode_gate.go)
  → a.permissions.Decide(name, args)    — permission check (permissions.go)
  → hooks.RunPreHook(name, args)        — user-configured pre-tool shell hooks
  → a.pipeline.RunToolBefore(name, args) — in-process transform
  → tool.Execute(args)                  — actual implementation
  → TruncateToolResult(result)          — truncate.go (cap large output)
  → hooks.RunPostHook(name, args, result)
  → a.pipeline.RunToolAfter(name, result)
  → append to messages, continue loop
```

Permission defaults are defined in `permissions.go:NewPermissionManager()`:
- **Always allow** (no prompt): read, glob, grep, list, lsp, skill, question, todoread, todowrite, advisor, task, task_status, agent_status, repo_overview, plan_enter, plan_exit, wait, bash_output, kill_shell
- **Default allow**: write, edit, multiedit, multi_file_edit, replace_lines, apply_patch, format
- **Default ask**: delete, bash, webfetch, websearch, repo_clone, mcp_*

## 5. Extra utilities

| File | Purpose |
|------|---------|
| `formatter.go` | `FormatTool` — delegates to `goimports`, `rustfmt`, `prettier`, etc. via `lsp_format.go` |
| `lsp_format.go` | Format-via-LSP logic |
| `ignore.go` | Path ignore patterns (`.gitignore`, `watcher.ignore`, sensitive paths) for tool path safety |
| `diff.go` | `DiffStrings()` helper used by edit/multiedit tools |
| `process.go` | `ProcessRegistry` — tracks background shell processes, output buffering, state management |
| `process_supervisor.go` | `ProcessSupervisor` — supervises process groups, timeout enforcement, cleanup |
| `process_tools.go` | `BashOutputTool` + `KillShellTool` — expose process registry to the LLM |
| `custom.go` | `CustomTool` — wraps user-defined tools from config (name, description, shell command) |
| `ast.go` | `AstTool` — opt-in semantic code query tool (disabled by default, enabled via `ocodeconfig.json:plugins.ast`) |

## 6. Adding a new tool (checklist)

1. **Choose the right file** — file operations go in `file.go`, search in `search.go`, web in `web.go`, process in `process_tools.go`, etc. If none fit, create a new file.
2. **Implement `Tool` interface** — all 5 methods: `Name()`, `Description()`, `Definition()`, `Execute(json.RawMessage)`, `Parallel()`.
3. **Register in `LoadBuiltins()`** — add to the `builtins` slice in `tool.go:49`.
4. **Set permission default** — add to the default rules table in `permissions.go:NewPermissionManager()`.
5. **Add to the skill catalog** if it's a tool the LLM should discover via the skill tool.
6. **Update this skill's table** above.
