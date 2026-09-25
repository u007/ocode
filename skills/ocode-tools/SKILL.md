---
name: ocode-tools
description: The ocode tool system — the Tool interface, LoadBuiltins registration, every built-in tool at a glance, the LSP manager lifecycle, permission defaults, and the NoticedError pattern. Use this when adding a new tool, modifying tool dispatch, debugging tool execution, or understanding how tools wire into the permission system.
when_to_use: When working on internal/tool/ — adding a new tool, changing tool registration in LoadBuiltins, modifying the Tool interface, fixing tool execution bugs, or wiring tool permissions. Also triggered by: "add tool", "new tool", "tool interface", "tool permissions", "LoadBuiltins", "LSP manager".
---

# ocode Tools Field Guide

The tool system is in `internal/tool/`. Tools implement a Go interface, register in `LoadBuiltins()` (or `InitBuiltinTools()`), and are executed by the agent loop after passing through permission gates and hooks.

## 1. The `Tool` interface (`tool.go:11`)

```go
type Tool interface {
    Name() string
    Description() string
    Definition() map[string]interface{}   // OpenAI tool schema format
    Execute(args json.RawMessage) (string, error)
    Parallel() bool                       // can run concurrently with other tools
}
```

### Optional extensions

| Extension | Method | Purpose |
|-----------|--------|---------|
| `ContextualTool` | `ExecuteCtx(ctx, args)` | Tools needing snapshot store access or tool call ID |
| `StreamingTool` | `ExecuteStream(args, emit)` | Long-running tools that emit incremental output (e.g. bash) |
| `ContextualStreamingTool` | `ExecuteStreamCtx(ctx, args, emit)` | Combines context + streaming (currently only `BashTool`) |
| `ImageResultTool` | `ExecuteImage(args)` | Tools returning raw image bytes for vision embedding |
| `ImageProducingTool` | `ProducesImage(args)` | Extends `ImageResultTool`; agent checks this to decide if a call produces an image |
| `ContextualImageResultTool` | `ExecuteImageCtx(ctx, args)` | Extends `ImageResultTool`; image read needs the session workdir (currently `ReadTool`) |

The agent loop checks for these extensions at dispatch time and calls the appropriate method. `ContextualStreamingTool` is checked first (highest priority), then `StreamingTool`, then `ContextualTool`, then plain `Execute`.

**Parallel tools** (read, glob, grep, rgrep, list, lsp, ast, skill, lsp_diagnostics, GitHub tools, repo_overview, todo_read, preview_open) run in goroutines. **Sequential tools** (bash, write, edit, delete, apply_patch, webfetch, websearch, etc.) block the loop — each runs to completion before the next starts.

**`NoticedError`** — tools that encounter a recoverable problem (e.g. LSP server not installed) wrap their error with a user-facing notice:
```go
type NoticedError struct {
    Err    error
    Notice string  // Shown in transcript, NOT sent to LLM
}
```
The agent extracts `NoticedError.Notice` into `Message.Notice`; the TUI renders it as a transient non-LLM message in the transcript.

## 2. Tool registration (`tool.go:100:InitBuiltinTools`)

```go
func InitBuiltinTools(lspMgr *lsp.Manager, cfg *config.Config, svc any) []Tool
func LoadBuiltins(cfg *config.Config, svc any) ([]Tool, *lsp.Manager)
```

Called once per session from `agent.go:NewAgent()`. Creates one shared `lsp.Manager` (lives as long as the session) and registers built-in tools. `svc` is an optional `*scheduler.Service` — when non-nil, the `cron` tool is included.

### Complete tool registry

| # | Tool | File | Name() | Parallel | Permission |
|---|------|------|--------|----------|------------|
| | **File operations** | | | | |
| 1 | `ReadTool` | `file.go` | `read` | ✅ | allow |
| 2 | `WriteTool` | `file.go` | `write` | ❌ | allow |
| 3 | `ReplaceLinesToolImpl` | `file.go` | `replace_lines` | ❌ | allow |
| 4 | `DeleteTool` | `file.go` | `delete` | ❌ | ask |
| 5 | `EditTool` | `file.go` | `edit` | ❌ | allow |
| 6 | `MultiEditTool` | `file.go` | `multiedit` | ❌ | allow |
| 7 | `MultiFileEditTool` | `file.go` | `multi_file_edit` | ❌ | allow |
| 8 | `UndoTool` | `undo.go` | `undo_file_change` | ❌ | allow |
| | **Search** | | | | |
| 9 | `GlobTool` | `search.go` | `glob` | ✅ | allow |
| 10 | `GrepTool` | `search.go` | `grep` | ✅ | allow |
| 11 | `ListTool` | `search.go` | `list` | ✅ | allow |
| | **Execution** | | | | |
| 12 | `BashTool` | `exec.go` | `bash` | ❌ | ask |
| 13 | `PatchTool` | `patch.go` | `apply_patch` | ❌ | allow |
| | **Todo** | | | | |
| 14 | `TodoWriteTool` | `todo_store.go` | `todowrite` | ❌ | allow |
| 15 | `TodoReadTool` | `todo_store.go` | `todoread` | ✅ | allow |
| 16 | `TodoUpdateTool` | `todo_store.go` | `todo_update` | ❌ | allow |
| | **Skills & prompts** | | | | |
| 17 | `SkillTool` | `misc.go` | `skill` | ✅ | allow |
| 18 | `SkillAliasTool` | `misc.go` | `load_skill` | ✅ | allow |
| | **Interaction** | | | | |
| 19 | `QuestionTool` | `misc.go` | `question` | ❌ | allow |
| | **Web** | | | | |
| 20 | `WebFetchTool` | `web.go` | `webfetch` | ❌ | ask |
| 21 | `WebSearchTool` | `web.go` | `websearch` | ✅ | ask |
| | **Repo** | | | | |
| 22 | `RepoCloneTool` | `repo.go` | `repo_clone` | ❌ | ask |
| 23 | `RepoOverviewTool` | `repo.go` | `repo_overview` | ✅ | allow |
| | **Planning** | | | | |
| 24 | `PlanEnterTool` | `plan.go` | `plan_enter` | ❌ | allow |
| 25 | `PlanExitTool` | `plan.go` | `plan_exit` | ❌ | allow |
| | **LSP** | | | | |
| 26 | `LSPTool` | `lsp.go` | `lsp` | ✅ | allow |
| 27 | `LSPDiagnosticsTool` | `diagnostics.go` | `lsp_diagnostics` | ✅ | allow |
| 28 | `FormatTool` | `formatter.go` | `format` | ❌ | allow |
| | **GitHub** | | | | |
| 29 | `GitHubPRTool` | `github.go` | `github_pr` | ✅ | ask |
| 30 | `GitHubIssueTool` | `github.go` | `github_issue` | ✅ | ask |
| 31 | `GitHubWorkflowTool` | `github.go` | `github_workflow` | ✅ | ask |
| | **Media** | | | | |
| 32 | `OcrTool` | `ocr.go` | `ocr` | ❌ | allow |
| 33 | `ImageGenTool` | `imagegen.go` | `imagegen` | ❌ | allow |
| 34 | `PreviewOpenTool` | `preview.go` | `preview_open` | ✅ | ask |
| | **Opt-in** | | | | |
| 35 | `AstTool` | `ast.go` | `ast` | ✅ | allow |
| 36 | `AstGrepTool` | `ast_grep.go` | `ast_grep` | ✅ | allow |
| | **Conditional** | | | | |
| 37 | `RgrepTool` | `rgrep.go` | `rgrep` | ✅ | allow |
| 38 | `ComputerTool` | `computer.go` | `computer` | ❌ | ask |
| | **Scheduled** | | | | |
| 39 | `CronTool` | `cron.go` | `cron` | ❌ | allow |

> Permission defaults from `permissions.go:NewPermissionManager()`. Override via `ocodeconfig.json:permissions.tools` or agent-specific permission maps.

**Tools registered outside LoadBuiltins** (added by `agent.go:NewAgent()` after LoadBuiltins):
- `task` (sub-agent spawner from `subagent.go`)
- `advisor` (strategic advisor from `advisor_tool.go`)
- `wait` (block/sleep from `wait_tool.go`)
- `bash_output` / `kill_shell` / `list_processes` (process management from `process_tools.go` — registered when `ProcessRegistry` is present)
- `agent_status` / `task_status` (run status tools)
- `knowledge_lookup` / `task_cancel` (knowledge + task lifecycle)
- MCP tools (registered from MCP server connections)

## 3. LSP manager lifecycle

```go
lspMgr := lsp.NewManager(".")   // created once per session
// shared by LSPTool, LSPDiagnosticsTool, and AstTool
```

All three tools receive `Mgr: lspMgr` at registration. The manager owns the language-server processes. When the session/agent is torn down, `lspMgr.Close()` must be called — failing to do so leaks server processes. `Manager.Statuses()` also retains `starting`/`running`/`failed` lifecycle rows for headless status consumers; `ActiveServers()` remains the running-only view used by the TUI.

## 4. How tools run (dispatch chain in `agent.go:executeToolCall`)

```
agent.go:Step()
  → finds tool by name in a.tools[name] (map, not slice)
  → gateToolCall(mode, name, args)      — mode-gating (mode.go)
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
- **Always allow** (no prompt): read, glob, grep, rgrep, list, lsp, lsp_diagnostics, skill, load_skill, question, todoread, todowrite, todo_update, advisor, task, task_status, agent_status, repo_overview, plan_enter, plan_exit, wait, bash_output, kill_shell, list_processes, ocr, cron
- **Default allow**: write, edit, multiedit, multi_file_edit, replace_lines, apply_patch, format, imagegen
- **Default ask**: delete, bash, webfetch, websearch, repo_clone, mcp_*, computer

Tools not in any list default to `PermissionAsk` (e.g. `github_pr`, `github_issue`, `github_workflow`).

## 5. Extra utilities

| File | Purpose |
|------|---------|
| `formatter.go` | `FormatTool` — delegates to `goimports`, `rustfmt`, `prettier`, etc. via `lsp_format.go` |
| `lsp_format.go` | Format-via-LSP logic |
| `ignore.go` | Path ignore patterns (`.gitignore`, `watcher.ignore`, sensitive paths) for tool path safety |
| `diff.go` | `DiffStrings()` helper used by edit/multiedit tools |
| `process.go` | `ProcessRegistry` — tracks background shell processes, output buffering, state management |
| `process_supervisor.go` | `ProcessSupervisor` — supervises process groups, timeout enforcement, cleanup |
| `process_tools.go` | `BashOutputTool` + `KillShellTool` + `ListProcessesTool` — expose process registry to the LLM |
| `custom.go` | `CustomTool` — wraps user-defined tools from config (name, description, shell command) |
| `ast.go` | `AstTool` — LSP-backed semantic code query (registered when LSP server available on PATH) |
| `ast_grep.go` | `AstGrepTool` — structural search/rewrite via ast-grep CLI (opt-in via `plugins.ast`) |
| `rgrep.go` | `RgrepTool` — ripgrep-backed content search (registered when `rg` resolves on PATH) |
| `bash_backup.go` | Bash undo command whitelist used by `UndoTool` |
| `undo.go` | `UndoTool` — file change undo via snapshot store |
| `cron.go` | `CronTool` — scheduled job management (requires scheduler service) |
| `diagnostics.go` | `LSPDiagnosticsTool` — LSP diagnostics reader |
| `imagegen.go` | `ImageGenTool` — image generation via Gemini/OpenAI/etc. |
| `misc.go` | `SkillTool`, `SkillAliasTool` (`load_skill`), `QuestionTool` |
| `ocr.go` | `OcrTool` — optical character recognition |
| `patch.go` | `PatchTool` — apply unified diff patches |
| `plan.go` | `PlanEnterTool`, `PlanExitTool` — planning phase management |
| `preview.go` | `PreviewOpenTool` — sidebar preview activation |
| `repo.go` | `RepoCloneTool`, `RepoOverviewTool` — external repo research |
| `computer.go` | `ComputerTool` — host desktop control (opt-in via `computer_use.enabled`) |
| `computer_driver.go` | `ComputerDriver` interface — platform abstraction for desktop control |
| `todo_store.go` | `TodoWriteTool`, `TodoReadTool`, `TodoUpdateTool` — persistent todo plan |
| `readpath.go` | `NormalizeUnicodeSpaces` + `ResolveReadTarget` — Unicode-space-tolerant read-target resolution and not-found hints (see below) |

### Read-path resolution (`file.go` + `readpath.go`)

`confinedPath(ctx, p)` resolves tilde, strips `:L` line refs, resolves symlinks,
and enforces workdir/extra-root containment. It is shared by ~18 call sites, so
**write/edit keep the literal path**. `ReadTool.Execute` and
`ReadTool.ExecuteImage` instead use `confinedReadPath`, which retries through
`ResolveReadTarget` when the literal path is missing: macOS screenshot
filenames contain U+202F (NARROW NO-BREAK SPACE) before AM/PM and models
routinely re-emit it as an ASCII space. Recovery fires only when exactly one
sibling matches after `NormalizeUnicodeSpaces` (case preserved), and the
recovered sibling is re-confined so a symlinked sibling cannot escape the
allowed roots. `ResolveReadTarget(...).Hint` feeds the permission layer's
not-found message (`resolved to <abs>; similar names in <dir>: "…"`, with
non-ASCII spaces escaped).

`ReadTool` implements `ContextualTool` (`ExecuteCtx`) and
`ContextualImageResultTool` (`ExecuteImageCtx`), so the agent's dispatch passes
the session project root (`WithWorkDir`, set in `agent.go` from `a.workDir`).
This matters because the desktop app is launched from Finder with cwd `/`:
without it a relative read resolved against `/` and missed every project file.
The search tools already anchored this way (`TestSearchToolsUseContextWorkDir`);
`ReadTool` was the outlier. `Execute`/`ExecuteImage` remain as `context.Background()`
wrappers for non-agent callers (tests, TUI direct calls, orphan recovery,
permission checks), which still fall back to the process cwd.

## 6. Adding a new tool (checklist)

1. **Choose the right file** — file operations go in `file.go`, search in `search.go`, web in `web.go`, process in `process_tools.go`, etc. If none fit, create a new file.
2. **Implement `Tool` interface** — all 5 methods: `Name()`, `Description()`, `Definition()`, `Execute(json.RawMessage)`, `Parallel()`. Optionally implement `ContextualTool`, `StreamingTool`, `ContextualStreamingTool`, `ImageResultTool`, or `ImageProducingTool` extensions.
3. **Register in `InitBuiltinTools()`** — add to the `builtins` slice in `tool.go:126`.
4. **Set permission default** — add to the default rules table in `permissions.go:NewPermissionManager()`.
5. **Add to the skill catalog** if it's a tool the LLM should discover via the skill tool.
6. **Update this skill's table** above.
