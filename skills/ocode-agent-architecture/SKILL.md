---
name: ocode-agent-architecture
description: Internal architecture of the ocode agent system — agent loop, context loading, provider abstraction, sub-agents, compact/truncate, and hooks integration. Use this when modifying the agent loop, adding a new provider, changing context loading, fixing tool dispatch, or debugging sub-agent behaviour.
when_to_use: When working on the core agent loop (internal/agent/), context loading, LLM provider integration, sub-agents (task tool), compaction/truncation logic, or hook system. Also triggered by: "agent loop", "context loading", "sub-agent", "provider", "compact", "hooks pipeline".
---

# ocode Agent Architecture

This skill maps the ocode agent subsystem (`internal/agent/`, 30+ files). It is the neural centre of the application — every user message, tool call, and LLM response flows through it.

## 1. File atlas (non-test files, grouped by function)

| Group | Files | Responsibility |
|-------|-------|---------------|
| **Agent loop** | `agent.go` | Central `Agent` struct, `Step()` loop, message prep, tool dispatch, cancellation |
| **LLM client** | `client.go`, `llm_contract.go`, `websocket.go` | `LLMClient` interface, `GenericClient`, per-provider chat impls, WebSocket transport |
| **Context loading** | `context.go`, `prompt.go`, `provider_prompts.go` | Assemble system prompt chunks: env, provider, mode, AGENTS.md/CLAUDE.md, model context, skills |
| **Provider/model** | `models_registry.go`, `small_model.go`, `images.go` | Model metadata (windows, pricing), small-model resolution for cheap tasks, vision detection |
| **Sub-agents** | `subagent.go`, `agent_registry.go`, `agent_loader.go`, `child_session.go`, `agent_runs.go` | Task tool, agent definitions (built-in + markdown), run tracking |
| **Compaction** | `compact.go`, `truncate.go` | Conversation compaction via small-model summarisation; large tool-result truncation |
| **Permissions** | `permissions.go`, `permission_interpreter.go`, `agent_permissions.go`, `mode_gate.go`, `command_capabilities.go` | Permission evaluation, LLM auto-permission, mode-based tool gating (see also `ocode-permissions` skill) |
| **Hooks** | (in `internal/hooks/`) | Pre/post tool hooks, chat param overrides, shell env injection |
| **Observability** | `activity.go`, `telemetry.go`, `retry_events.go`, `retry_status.go` | Activity tracking for TUI, token usage telemetry, retry status events |
| **Other** | `title.go`, `redaction.go`, `mode.go`, `registry.go`, `advisor_tool.go`, `wait_tool.go` | Session title gen, secret redaction, agent modes (build/plan/review/debug/docs), advisor sub-agent, wait tool |

## 2. Agent loop (`agent.go:Step`)

```
User message arrives from TUI/session
  ↓
a.PrepareMessages(messages, selection)    → prompt.go
  ├─ a.environmentPrompt()                → [ocode:environment]  (cwd, git branch, platform, date)
  ├─ modelFamilyPrompt()                  → [ocode:provider]     (model-family-specific guidance)
  ├─ a.Mode().SystemPrompt()              → [ocode:mode]         (build/plan/review/debug/docs)
  ├─ a.LoadContext(selection)             → [ocode:context]      (AGENTS.md, CLAUDE.md, .cursorrules,
  │                                                               .opencode/rules/*.md, plugins, skills)
  ├─ LoadModelContext(modelName)          → [ocode:model_context] (model-specific OCODE.md files)
  └─ selectionContext                     → [ocode:selection]    (code selection if any)
  ↓
a.injectLSPDiagnostics(messages)          → live LSP diagnostics injected
  ↓
LOOP (up to maxSteps, default ~20):
  1. a.chatWithDelta(stopCh, messages, toolDefs)
     → sets OnDelta (streaming), OnUsage (token tracking)
     → a.pipeline.RunChatParams() for hook-based param overrides
     → gc.ChatWithContext(ctx, messages, toolDefs)
       → redaction safety net
       → dispatches to provider-specific chat:
         chatOpenAI() / chatAnthropic() / chatCopilot()
       → retries on 429 / transient errors

  2. If response has NO tool calls → break (done)

  3. Sort tool calls: parallel (true in Definition()) run in goroutines;
     sequential (false) run one at a time.

  4. For each tool call:
     a. gateToolCall(a.Mode(), name, args)   → mode_gate.go
     b. a.permissions.Decide(name, args)      → permissions.go (see ocode-permissions skill)
     c. hooks.RunPreHook(name, args)          → may block tool
     d. a.pipeline.RunToolBefore(name, args)  → may transform args
     e. tool.Execute(args)                    → actually runs the tool
     f. result = TruncateToolResult(result)   → truncate.go (cap large output)
     g. hooks.RunPostHook(name, args, result) → fire-and-forget
     h. a.pipeline.RunToolAfter(name, result) → may transform result

  5. Append assistant response + tool results to messages
  6. a.MaybeCompactAsync(messages)           → compact.go (if nearing context limit)
  7. Loop back to step 1
```

## 3. Context loading order (`context.go:LoadContext`)

```go
func LoadContext(selection string, config *Config) (string, []systemContextKind) {
```

Loads in order — earlier items appear higher in the assembled context:

1. **AGENTS.md** (project root) — if git-tracked with unstaged changes, uses HEAD version
2. **CLAUDE.md** (project root) — same HEAD-vs-working-tree rule
3. **`.cursorrules`** (project root) — if present
4. **`.opencode/rules/*.md** (project root) — all files sorted alphabetically
5. **Plugin context** — `plugins.LoadPluginContext()` for installed plugins
6. **Skill catalog** — `skill.Catalog(true)` → all loaded skills' frontmatter

Model-specific context (`LoadModelContext`): searches three directories (priority order):
1. Project root (`./`)
2. `.opencode/` subdirectory
3. `~/.config/opencode/` global directory

Within each directory: exact stem match beats wildcard match (trailing `*` stem). First directory with a match wins.

**Embedded fallback:** If no file found on disk, `loadBundledModelContext` checks the compiled-in `//go:embed` FS set by `main.go`. This ensures the default model always ships with its instructions.

## 4. Provider abstraction (`client.go`)

```go
type LLMClient interface {
    Chat(ctx context.Context, messages []Message, tools []ToolDefinition) (*Message, error)
    GetProvider() string
    GetModel() string
}
```

`GenericClient` is the concrete implementation. Key fields:

| Field | Purpose |
|-------|---------|
| `APIKey` | Auth token for the provider |
| `Model` | Model identifier string (e.g. `"gpt-4o"`) |
| `BaseURL` | API endpoint override |
| `Provider` | Provider name key (e.g. `"openai"`) |
| `OnDelta` | Streaming callback (set by agent loop) |
| `OnUsage` | Token usage callback |
| `ThinkingBudget` | For reasoning models |
| `UseWebSocket` | Flag for OpenAI Responses API WebSocket transport |

Provider routing: `NewClient()` switches on `Provider` → one of `chatOpenAI()`, `chatAnthropic()`, `chatCopilot()`. Each builds the provider's native request format, calls the API, and maps the response back to the generic `Message`/`ToolCall` types.

Model metadata: `models_registry.go` provides `ModelWindow(modelName)` for context-window sizes, pricing info from an embedded `models-snapshot.json` (regenerated periodically via `make models-snapshot`).

Small model resolution: `small_model.go:ResolveSmallModel()` selects a cheaper model for compaction, title generation, and sub-agents like `explore`/`general`. Falls back to the primary model if no small model is configured.

## 5. Sub-agent system

Built-in primary agents (from `registry.go`): `build`, `plan`, `review`, `debug`, `docs`.
Built-in sub-agents (from `subagent.go`): `general`, `explore`, `scout`.
Custom agents loaded from `.opencode/agents/*.md` or `~/.config/opencode/agents/*.md` via `agent_loader.go`.

**TaskTool** (`subagent.go`) — registered as the `"task"` tool in `agent.go:NewAgent()`:

```
TaskTool.Execute(args):
  1. Parse agent name + prompt from args
  2. Find agent spec via registry (t.findAgent)
  3. Check dispatch guard (anti-runaway prevention)
  4. Get tools for that agent type from spec
  5. Create child Agent via NewAgent() sharing parent's client/config/lspMgr
  6. Apply spec: SetSpec() with mode, system prompt, tool list, model overrides
  7. Wire permission asker → parent's subAgentPermAsker (shares permission state)
  8. Background mode → create AgentRun, go child.Step(), return run_id
  9. Synchronous mode → child.Step(messages) → return result text
  10. Persist child session via childSessionID() + childSessionMetadata()
```

**AdvisorTool** (`advisor_tool.go`) — separate sub-agent using its own LLM client (a different model) for exploratory codebase analysis. Used by the `/advisor` command.

**AgentRunRegistry** (`agent_runs.go`) — tracks all async sub-agent runs. Polled by `WaitTool` and the TUI for status.

## 6. Compact / truncate

**Compact** (`compact.go`): When the message list approaches the model's context window, `MaybeCompactAsync()` splices older turns, summarises them via a small-model LLM call, and replaces them in the message list. The spliced structure is: prefix (system + first user turn) + compacted middle + suffix (recent turns). Runs async to avoid blocking the main loop.

**Truncate** (`truncate.go`): `TruncateToolResult()` caps each tool result at 100 lines / 12000 chars. Larger output is written to a cache file in `~/.local/state/opencode/tool-results/` and the truncated version includes a notice telling the model to use the `read` tool on that path.

## 7. Hooks integration

Two hook systems coexist — see `internal/hooks/pipeline.go` + `internal/hooks/hooks.go`:

| Hook point | Mechanism | Source |
|------------|-----------|--------|
| Pre-tool (blocking) | `hooks.RunPreHook()` via shell commands | Config `hooks` in `ocodeconfig.json` |
| Post-tool (fire-and-forget) | `hooks.RunPostHook()` via shell commands | Config `hooks` in `ocodeconfig.json` |
| Tool arg transformation | `pipeline.RunToolBefore(name, args)` → new args | In-process `Pipeline` from `session.SetToolHooks()` |
| Tool result transformation | `pipeline.RunToolAfter(name, result)` → new result | In-process `Pipeline` |
| Chat param override | `pipeline.RunChatParams(model, params)` → new params | In-process `Pipeline` |
| Shell env injection | `pipeline.ShellEnvFunc` | In-process `Pipeline` (used by bash tool) |

## 8. Relationships with other packages

```
main.go
  ├─ internal/agent/      (← this skill)
  ├─ internal/tool/       (tool implementations — see ocode-tools skill)
  ├─ internal/tui/        (calls agent.Step(), reads ActivityTracker)
  ├─ internal/session/    (persists agent state, owns Pipeline hooks)
  ├─ internal/config/     (Config structs consumed by Agent, tool dispatching)
  ├─ internal/hooks/      (hook pipeline, pre/post hooks)
  ├─ internal/lsp/        (LSP manager, shared via agent's lspMgr field)
  ├─ internal/mcp/        (MCP server tools)
  └─ internal/plugins/    (plugin context injection)
```

**Key data flow boundary:** `agent.go` owns the `[]tool.Tool` slice (builtins + MCP tools + custom tools). When `Step()` needs to execute a tool call, it iterates this slice to find the matching `Tool` by name, then calls `t.Execute()`. The `PermissionManager.Decide()` gate happens before execution.
