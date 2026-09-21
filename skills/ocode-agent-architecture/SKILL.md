---
name: ocode-agent-architecture
description: Internal architecture of the ocode agent system — agent loop, context loading, provider abstraction, sub-agents, compact/truncate, and hooks integration. Use this when modifying the agent loop, adding a new provider, changing context loading, fixing tool dispatch, or debugging sub-agent behaviour.
when_to_use: When working on the core agent loop (internal/agent/), context loading, LLM provider integration, sub-agents (task tool), compaction/truncation logic, or hook system. Also triggered by: "agent loop", "context loading", "sub-agent", "provider", "compact", "hooks pipeline".
---

# ocode Agent Architecture

This skill maps the ocode agent subsystem (`internal/agent/`). It is the neural centre of the application — every user message, tool call, and LLM response flows through it.

## 1. File atlas (non-test files, grouped by function)

| Group | Files | Responsibility |
|-------|-------|---------------|
| **Agent loop** | `agent.go` | Central `Agent` struct, `Step()` loop, message prep, tool dispatch, cancellation, tail injectors |
| **LLM client** | `client.go`, `llm_contract.go`, `websocket.go` | `LLMClient`/`StreamingLLMClient` interfaces, `GenericClient`, per-provider chat impls (`chatAnthropic`, `chatCopilot`, `chatGoogle`, `chatOpenAI`, `chatOpenAIResponses`, `chatOpenAIWebSocket`, `chatOpenAIHTTP`), WebSocket transport, stream idle watchdog |
| **Context loading** | `context.go`, `prompt.go`, `provider_prompts.go` | Assemble system prompt chunks: env, provider, mode, AGENTS.md/CLAUDE.md, model context, skills |
| **Provider/model** | `models_registry.go`, `small_model.go`, `images.go` | Model metadata (windows, pricing), small-model resolution for cheap tasks, vision detection |
| **Sub-agents** | `subagent.go`, `agent_registry.go`, `agent_loader.go`, `child_session.go`, `agent_runs.go`, `task_cancel.go`, `task_contract.go`, `task_dag.go` | Task tool, agent definitions (built-in + markdown), run tracking, cancellation, output contracts, in-batch DAG scheduling |
| **Compaction** | `compact.go`, `truncate.go` | Conversation compaction via small-model summarisation; large tool-result truncation |
| **Permissions** | `permissions.go`, `permission_interpreter.go`, `agent_permissions.go`, `command_capabilities.go`, `permission_paths.go` | Permission evaluation, LLM auto-permission, mode-based tool gating, out-of-scope path classification (see also `ocode-permissions` skill) |
| **Hooks** | (in `internal/hooks/`) | Pre/post tool hooks (`hooks.go`), chat param overrides, shell env injection (`pipeline.go`) |
| **Observability** | `activity.go`, `telemetry.go`, `retry_events.go`, `retry_status.go` | Activity tracking for TUI, token usage telemetry, retry status events |
| **Discovery** | `discovery_glue.go`, `discovery_opt.go`, `dir_docs.go`, `md_discovery.go` | Markdown doc discovery, project doc indexing, post-task discovery optimization |
| **LSP** | `lsp_inject.go` | LSP diagnostic injection: `appendEditDiagnostics` (per-tool result) + `injectLSPDelta` (per-turn user-role tail) |
| **Knowledge** | `knowledge_lookup.go`, `doc_tools.go`, `doc_maintenance.go` | Knowledge bundle tooling: `KnowledgeLookupTool`, doc CRUD, post-job maintenance |
| **Advisor** | `advisor_tool.go`, `advisor_checkpoint.go` | Advisor sub-agent, checkpoint/resume for advisor runs |
| **Harness** | `harness.go` | Outbound fingerprint presets (User-Agent, Referer, X-Title) for provider-specific harness identity |
| **Todo** | `todo_inject.go` | Re-anchor injection of persistent todo plan into user-role volatile tail |
| **Runtime paths** | `runtime_paths.go` | Detect JS/Bun/React/Python projects and enumerate tool paths in `<env>` block |
| **Other** | `title.go`, `redaction.go`, `redaction_helpers.go`, `mode.go`, `registry.go`, `wait_tool.go`, `brief_seeding.go`, `child_emit.go`, `delta_inject.go`, `append_stable.go`, `ask.go`, `group_bus.go`, `group_tracker.go`, `group_reconcile_handoff.go`, `group_bus_handoff.go`, `local_model_limiter.go`, `local_models.go`, `agent_dedup.go`, `claude_settings.go`, `script_detection.go`, `memory_maintenance.go`, `llm_stream_idle.go`, `registry_lock_unix.go`, `registry_lock_windows.go` | Session title gen, secret redaction + helpers, agent modes, wait tool, brief seeding for review, child agent emit, delta injection, stable message append, ask handler, group bus (parallel sub-agent collaboration), local model rate limiting, tool-call dedup, Claude settings import, bash script detection, memory maintenance, LLM stream idle watchdog, registry file locking (platform-specific) |

## 2. Agent loop (`agent.go:Step`)

```
User message arrives from TUI/session
  ↓
a.PrepareMessages(messages, selection)    → prompt.go
  ├─ a.BasePromptMessages()              → system-role base (prompt.go)
  │   ├─ a.environmentPrompt()           → [ocode:environment]  (cwd, git branch, platform, date)
  │   ├─ modelFamilyPrompt()             → [ocode:provider]     (model-family-specific guidance)
  │   ├─ a.Mode().SystemPrompt()         → [ocode:mode]         (build/plan/review/debug/docs)
  │   ├─ LoadContext()                   → [ocode:context]      (AGENTS.md, CLAUDE.md, .cursorrules,
  │   │                                                            .opencode/rules/*.md, plugins, skills)
  │   └─ preloadedModelContext            → [ocode:model_context] (model-specific OCODE.md files)
  └─ selectionContext                     → [ocode:selection]    (code selection if any, user-role)
  ↓
  Volatile user-role tail injectors (appended after stable prefix for cache stability):
  ├─ injectNotesTail()                   → [ocode:notes]         (group bus delta)
  ├─ injectDiscoveryContext()            → [ocode:discovery]     (MCP name index + discover_more contract)
  ├─ injectDirMDTail()                   → subdirectory CLAUDE.md/AGENTS.md/OCODE.md (lazily surfaced)
  ├─ injectTodoTail()                    → [ocode:todo]          (persistent todo plan re-anchor)
  └─ injectLSPDelta()                    → live LSP diagnostics (user-role, only when changed)
  ↓
LOOP (unbounded; maxSteps configurable via config):
  1. a.chatWithDelta(stopCh, messages, toolDefs)
      → a.pipeline.RunChatParams() for hook-based param overrides
      → gc.ChatWithContext(ctx, messages, toolDefs)
        → redaction safety net
        → dispatches to provider-specific chat:
          chatAnthropic() / chatCopilot() / chatGoogle() / chatOpenAI()
          (chatOpenAI internally routes to chatOpenAIResponses, chatOpenAIWebSocket, chatOpenAIHTTP)
        → retries on 429 / transient errors; stream idle watchdog (llm_stream_idle.go)
  2. Process response → append assistant message
  3. If tool calls:
     → a.handleToolCall(name, args, b, toolCallID)
       → isToolAllowed() gate
       → a.tools[name] lookup (map, not slice)
        → hooks.RunPreHook(name, args)        — user-configured pre-tool shell hooks
        → redaction registry Resolve()        — restore OCSEC tokens to real values
        → a.pipeline.RunToolBefore(name, args) — in-process transform
        → tool.Execute(args)                  — actual implementation
         (ContextualStreamingTool → ExecuteStreamCtx)
         (StreamingTool → ExecuteStream)
         (ContextualTool → ExecuteCtx)
        → appendEditDiagnostics()            — LSP diagnostics appended to tool result
        → TruncateToolResult(result)          — truncate.go (cap large output)
        → hooks.RunPostHook(name, args, result)
        → a.pipeline.RunToolAfter(name, result)
        → append to messages, continue loop
  4. If no tool calls → break (turn complete)
  ↓
a.MaybeCompactAsync()                     → compact.go (async context compaction)
```

## 3. LLM client (`client.go`)

```
LLMClient interface (client.go:155):
    Chat(messages, tools) → (Message, error)
    GetProvider() string
    GetModel() string

StreamingLLMClient interface (llm_contract.go):
    LLMClient
    Stream(messages, tools, emit) → (Message, error)
```

`GenericClient` (client.go:161) is the concrete implementation. Key fields:

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
| `Temperature` | Optional temperature override (`*float64`, nil = unset) |
| `TopP` / `TopK` | Optional sampling params |

Provider routing: `NewClient()` (client.go:4292) constructs the client; `ChatWithContext()` (client.go:690) dispatches by provider type → `chatAnthropic()` (client.go:3638) if Anthropic Messages API, `chatCopilot()` (client.go:997) if copilot, `chatGoogle()` (client.go:1293) if Google provider, else `chatOpenAI()` (client.go:1176). `chatOpenAI` internally routes to `chatOpenAIResponses()` (client.go:2803) for OAuth/Responses-only models, `chatOpenAIWebSocket()` (client.go:4767) when WebSocket enabled, or `chatOpenAIHTTP()` (client.go:4821) for the regular HTTP path. Each builds the provider's native request format, calls the API, and maps the response back to the generic `Message`/`ToolCall` types.

Model metadata: `models_registry.go` provides `ModelWindow(modelID)` (line 763) for context-window sizes, pricing info from an embedded `models-snapshot.json` (regenerated periodically via `make models-snapshot`).

Small model resolution: `small_model.go:ResolveSmallModel()` (line 29) selects a cheaper model for compaction, title generation, and sub-agents like `explore`/`general`. Falls back to the primary model if no small model is configured.

## 4. Tool interface extensions (`internal/tool/tool.go`)

The base `Tool` interface (`internal/tool/tool.go:11`) has five optional extensions:

| Extension | Purpose |
|-----------|---------|
| `ContextualTool` | Adds `ExecuteCtx(ctx, args)` for tools needing snapshot store access or tool call ID |
| `StreamingTool` | Adds `ExecuteStream(args, emit)` for long-running tools that emit incremental output |
| `ContextualStreamingTool` | Combines `ContextualTool` + `StreamingTool` (e.g. BashTool for snapshot + streaming) |
| `ImageResultTool` | Adds `ExecuteImage(args)` → `(raw, mimeType, err)` for tools returning image bytes |
| `ImageProducingTool` | Extends `ImageResultTool` with `ProducesImage(args) bool` for calls without a file path |
| `NoticedError` | Error wrapper with user-facing notice shown in transcript but not sent to LLM |

The agent loop checks for these extensions at dispatch time and calls the appropriate method.

## 5. Sub-agent system

Built-in primary agents (from `registry.go`): `build`, `plan`, `review`, `debug`, `docs`.
Built-in sub-agents (from `subagent.go:101-120`): `general`, `explore`, `scout`, `context`.
Custom agents loaded from `.opencode/agents/*.md` or `~/.config/opencode/agents/*.md` via `agent_loader.go`.

**TaskTool** (`subagent.go:162`) — registered as the `"task"` tool in `agent.go:NewAgent()`:

```
TaskTool.Execute(args):
  1. Parse agent name + prompt from args
  2. Find agent spec via registry (t.findAgent, subagent.go:1085)
  3. Check dispatch guard (anti-runaway prevention, subagentDispatchLimit = 3)
  4. Check duplicate-active dispatch guard (acquireActiveDispatch)
  5. Get tools for that agent type from spec
  6. Create child Agent via NewAgent() sharing parent's client/config/lspMgr
  7. Apply spec: SetSpec() with mode, system prompt, tool list, model overrides
  8. Wire permission asker → parent's subAgentPermAsker (shares permission state)
  9. Background mode → create AgentRun, go child.Step(), return run_id
  10. Synchronous mode → child.Step(messages) → return result text
  11. Persist child session via childSessionID() + childSessionMetadata()
  12. Verify result against expected_output contract (task_contract.go) if set
```

**AdvisorTool** (`advisor_tool.go:99`) — separate sub-agent using its own LLM client (a different model) for exploratory codebase analysis. Used by the `/advisor` command.

**AgentRunRegistry** (`agent_runs.go:531`) — tracks all async sub-agent runs. Polled by `WaitTool` and the TUI for status.
**TaskCancelTool** (`task_cancel.go`) — cancels a background task by run ID.
**Task DAG** (`task_dag.go`) — in-batch dependency scheduling when `task` calls declare `id`/`depends_on`.

**Dispatch-guard lifecycle (step 3 above) — wire the reset into EVERY user-input entry point.** `subagentDispatchCount` only clears via `Agent.ResetSubagentDispatch()`; the counter increments per consecutive same-name dispatch and refuses past `subagentDispatchLimit` (3). It also resets implicitly when a *different* agent name is dispatched. The reset was originally wired into the TUI send paths only, so the headless server accumulated the count across turns and permanently locked an agent type out of the web/desktop UI. The three server entry points that accept user input are `Handler.runTurn` (HandleChat/HandleSendMessage), `HandleChatStream` (`handler_sse.go`, the legacy SSE endpoint still used by Telegram), and `tryEnqueueInjection` (a message spliced into the running turn) — all three must reset. Reset once per user turn, NOT per `Step`, or an auto-continue chain inside one turn would defeat the cap. Cron needs no reset: `scheduler_runner` builds a fresh agent per firing (counter starts at zero). Regression: `internal/server/agent_session_dispatch_guard_test.go`.

## 6. Compact / truncate

**Compact** (`compact.go`): When the message list approaches the model's context window, `MaybeCompactAsync()` splices older turns, summarises them via a small-model LLM call, and replaces them in the message list. The spliced structure is: prefix (system + first user turn) + compacted middle + suffix (recent turns). Runs async to avoid blocking the main loop.

**Truncate** (`truncate.go:40`): `TruncateToolResult()` caps each tool result at 100 lines / 12000 chars (`maxToolResultLines`, `maxToolResultChars`). Larger output is written to a cache file in `~/.local/state/opencode/tool-results/` and the truncated version includes a notice telling the model to use the `read` tool on that path.

## 7. Hooks integration

Two hook systems coexist — see `internal/hooks/pipeline.go` + `internal/hooks/hooks.go`:

| Hook point | Mechanism | Source |
|------------|-----------|--------|
| Pre-tool (blocking) | `hooks.RunPreHook()` via shell commands | Config `hooks` in `ocodeconfig.json` (`internal/hooks/hooks.go:14`) |
| Post-tool (fire-and-forget) | `hooks.RunPostHook()` via shell commands | Config `hooks` in `ocodeconfig.json` (`internal/hooks/hooks.go:19`) |
| Tool arg transformation | `pipeline.RunToolBefore(name, args)` → new args | In-process `Pipeline` from `session.SetToolHooks()` (`internal/hooks/pipeline.go:34`) |
| Tool result transformation | `pipeline.RunToolAfter(name, result)` → new result | In-process `Pipeline` (`internal/hooks/pipeline.go:41`) |
| Chat param override | `pipeline.RunChatParams(model, params)` → new params | In-process `Pipeline` (`internal/hooks/pipeline.go:48`) |
| Shell env injection | `pipeline.ShellEnvFunc` | In-process `Pipeline` (`internal/hooks/pipeline.go:17`) |

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
  ├─ internal/knowledge/  (OKF bundle lock, doc CRUD backing)
  ├─ internal/plugins/    (plugin context injection)
  └─ internal/snapshot/   (file write snapshots for undo/changes-tab)
```

**Key data flow boundary:** `agent.go` owns the `map[string]tool.Tool` (builtins + MCP tools + custom tools). When `Step()` executes a tool call, it looks up the matching `Tool` by name in the map, then calls the appropriate interface method. The `PermissionManager.Decide()` gate (permissions.go:1399) happens before execution. Permission asks are routed via `OnPermissionAsk` (agent.go:503) which connects to the TUI's synchronous permission dialog.
