# Plugin/Hook System — Implementation TODO

Investigation target: `~/www/opencode` (Node.js/TypeScript opencode app)
Current state: `internal/hooks/hooks.go` + `internal/hooks/pipeline.go`
Date: 2026-07-02

---

## Goal

Port opencode's plugin/hook model (JS/TypeScript, Bun runtime) into ocode (Go
binary) without importing a JS runtime. Make ocode extensible by running plugin
processes as subprocesses — language-agnostic, process-boundary isolation.

---

## What opencode Has That We Want

Opencode defines ~18 typed hook points in `packages/plugin/src/index.ts` (the
`Hooks` interface). Plugins are JS/TS modules loaded via Bun `import()` from npm
packages, local files, or a global directory. The system is built on top of an
**event bus** (`EventV2`) with durable SQLite-backed replay.

### Hook catalog (priority-ordered for porting)

| Priority | Hook | What it does |
|----------|------|-------------|
| **P0** | `tool.execute.before` | Intercept/modify/block tool calls before execution |
| **P0** | `tool.execute.after` | Transform tool output after execution |
| **P1** | `event({event})` | Subscribe to lifecycle/activity events |
| **P1** | `tool.definition` | Modify tool definitions sent to LLM (description, params) |
| **P2** | `permission.ask` | Auto-approve/deny permission prompts |
| **P2** | `chat.params` | Override temperature, topP, maxTokens before LLM call |
| **P3** | `shell.env` | Inject env vars into shell execution (already in Pipeline) |
| **P3** | `command.execute.before` | Intercept slash commands |
| **P3** | `chat.headers` | Inject HTTP headers to LLM API |
| **Hold** | `experimental.chat.messages.transform` | Transform full messages array |
| **Hold** | `experimental.chat.system.transform` | Rewrite system prompt |
| **Hold** | `experimental.session.compacting` | Customize compaction prompt |

### Events (for the event bus layer)

Opencode fires these event types (subscription via `event({event})` hook):

- `file.edited`, `file.watcher.updated`
- `session.created`, `session.compacted`, `session.deleted`, `session.error`, `session.idle`, `session.status`, `session.updated`
- `lsp.client.diagnostics`, `lsp.updated`
- `permission.asked`, `permission.replied`
- `tool.execute.before`, `tool.execute.after`
- `message.part.removed`, `message.part.updated`, `message.removed`, `message.updated`
- `installation.updated`, `server.connected`
- `todo.updated`, `command.executed`, `shell.env`

---

## Architecture Decision: Two-Tier Plugin Model

```
┌─────────────────────────────────────────────────┐
│                  Agent Loop                       │
│  ┌──────────┐  ┌──────────┐  ┌────────────────┐ │
│  │ Shell     │  │ Pipeline  │  │ Plugin Process  │ │
│  │ Hooks     │  │ (Go func)│  │ Manager (new)   │ │
│  │ (keep)    │  │ (keep)   │  │                 │ │
│  └──────────┘  └──────────┘  └────────┬───────┘ │
│                                        │         │
└────────────────────────────────────────┼─────────┘
                                         │
                              JSON-RPC over stdio
                                         │
              ┌──────────────────────────┼──────────┐
              │     Plugin Process        │          │
              │  (any language)           │          │
              │  bun / node / python / …  │          │
              └───────────────────────────┘          │
```

### Tier 1: Shell hooks (keep as-is)

**What**: `config.HookConfig` — stateless pre/post commands per tool name.
**Where**: `internal/hooks/hooks.go`
**Why keep**: Zero overhead for simple "run a script before/after tool X". Works
with any binary already installed on the system.

**No changes needed** — the existing shell-command approach already lets users
run JS via `bun script.js` or `node script.js` if they want.

### Tier 2: Plugin processes (new)

**What**: Long-lived subprocesses connected via JSON-RPC over stdin/stdout.
**Protocol**: Newline-delimited JSON (one message per line, `\n` delimited).
**Lifecycle**: One process per plugin, started at session init, kept alive for
the session duration, restarted on crash.

**Why not per-invocation shell**: Stateful plugins (event subscriptions, custom
tools, auth providers) need a persistent connection. Per-invocation shell is
too slow and has no state.

### MCP servers (already exist)

MCP servers already cover tool registration + invocation. A plugin that only
adds tools should just be an MCP server — no new protocol needed. Plugins that
need event subscription, permission interception, or chat param overrides use
the new plugin process protocol.

---

## Implementation Phases

### Phase 1: Plugin Process Protocol & Runner (~2-3 weeks)

**Files to create:**
- `internal/plugin/` — new package
  - `protocol.go` — JSON-RPC message types, connect/handshake/register
  - `manager.go` — process lifecycle (start, restart, shutdown)
  - `types.go` — Go types mirroring the hook interface

**Protocol sketch:**

```json
// Handshake (plugin → ocode at startup)
{"method": "register", "params": {"hooks": ["tool.before", "event"], "tools": [{"name": "my-tool", ...}]}}
// or
{"method": "register_tool", "params": {"name": "my-tool", "description": "..."}}

// Hook invocation (ocode → plugin)
{"method": "hook:tool.before", "params": {"tool": "read", "args": {...}}, "id": 1}

// Hook response (plugin → ocode)
{"id": 1, "result": {"args": {...}, "blocked": false}}

// Event notification (ocode → plugin, no response expected)
{"method": "event", "params": {"type": "session.compacted", "data": {...}}}

// Plugin error
{"id": 1, "error": {"message": "Don't read .env files"}}
```

**What it replaces from opencode's Hooks interface:**
- `tool.execute.before` → `hook:tool.before` method
- `tool.execute.after` → `hook:tool.after` method
- `tool.definition` → `register_tool` + `hook:tool.definition` method
- `event({event})` → `event` notification
- `config({config})` → `config` notification
- `dispose()` → process exit

### Phase 2: Event Bus (~1 week)

**What**: Simple in-process pub/sub.

**Files to create/modify:**
- `internal/events/` — new package
  - `bus.go` — `type Bus struct { subscribers map[string][]chan Event }`
  - `types.go` — event type definitions

**Why not durable**: Opencode needs SQLite-backed replay because its web UI,
CLI, and background processes are separate OS processes. Ocode is a single
process with a TUI. If ocode ever adds a web UI or daemon mode, upgrade then.

**Integration points (agent loop):**
- `agent.go:Step()` — fire `session.status` on each step
- `agent.go¹` tool dispatch — fire `tool.execute.before`/`after`
- `compact.go` — fire `session.compacted`
- `permissions.go` — fire `permission.asked`, `permission.replied`

### Phase 3: Wire Plugin Hooks into Agent Loop (~1 week)

**Modify** `internal/agent/agent.go` around lines 2801-2876:

```go
// Current flow:
//   1. hooks.RunPreHook(name, argsStr, hooksCfg)        ← shell
//   2. pipeline.RunToolBefore(name, args)                 ← Go funcs
//   3. tool.Execute(args)
//   4. pipeline.RunToolAfter(name, result)                ← Go funcs
//   5. hooks.RunPostHook(name, argsStr, resultStr, ...)   ← shell

// New flow:
//   0. bus.Publish("tool.execute.before", {tool, args})
//   1. hooks.RunPreHook(name, argsStr, hooksCfg)
//   2. pipeline.RunToolBefore(name, args)
//   2b. pluginManager.RunHook("tool.before", {tool, args}, &args)
//   3. tool.Execute(args)
//   4. pipeline.RunToolAfter(name, result)
//   4b. pluginManager.RunHook("tool.after", {tool, args, result}, &result)
//   5. hooks.RunPostHook(name, argsStr, resultStr, ...)
//   6. bus.Publish("tool.execute.after", {tool, args, result})
```

**Hook manager integration** — `pluginManager.RunHook(name, input, output)`
iterates all connected plugin processes and calls the matching method. If any
plugin returns `blocked: true`, the tool is skipped (for `before` hooks).

### Phase 4: Permission Interception (P2) (~1 week)

**Where it hooks in:**

Currently `internal/agent/permissions.go:Decide()` returns a decision
(allow/deny/ask). Add a plugin hook point between the permission logic and the
TUI prompt:

```go
// After Decide() returns "ask":
pluginDecision := pluginManager.RunHook("permission.ask", permission, &output)
if pluginDecision == "allow" { /* skip TUI prompt */ }
if pluginDecision == "deny"  { /* block tool */ }
// else fall through to TUI prompt
```

### Phase 5: Chat Param Overrides (P2) (~1 week)

**Where it hooks in:**

`internal/agent/client.go` — before building the provider chat request, call:

```go
pluginManager.RunHook("chat.params", {model, provider, message}, &params)
```

This feeds into the existing `pipeline.RunChatParams()` call.

---

## Plugin Configuration

**Config schema** (in `internal/config/config.go`, `PluginConfig`):

```json
{
  "plugins": {
    "my-plugin": {
      "source": "local",
      "command": ["bun", "run", ".opencode/plugins/my-plugin.ts"],
      "enabled": true
    },
    "npm-plugin": {
      "source": "npm",
      "package": "opencode-my-plugin",
      "enabled": true
    }
  }
}
```

Add a `PluginManager` struct that:
- Resolves npm packages (downloads to cache dir, resolves command)
- Starts the process with stdin/stdout pipes
- Reads the JSON-RPC handshake
- Keeps the process alive, restarts on crash
- Drains pending hooks on shutdown

---

## Testing Strategy

| Layer | Testing approach |
|-------|-----------------|
| **Protocol** | Unit tests with mock processes (pipe stdin/stdout to a Go test helper that speaks JSON-RPC) |
| **Manager** | Start/stop lifecycle, crash recovery, duplicate registration rejection |
| **Agent integration** | Unit tests with a mock plugin that blocks/modifies tools, verifies agent loop respects the result |
| **Event bus** | Subscribe/unsubscribe, fan-out, panic isolation |
| **End-to-end** | Write a real plugin file in a temp dir, start ocode in headless mode, verify hooks fire |

Example test helper:

```go
func newMockPlugin(t *testing.T, hooks []string, handler func(method string, params json.RawMessage) json.RawMessage) (*PluginManager, *PluginProcess) {
    // Returns a connected process that responds to hook methods
}
```

---

## What NOT to Do

1. **Don't embed a JS runtime** (goja, v8go, etc.) — cannot run real npm
   packages, adds complexity, ocode doesn't need to be a JS host.

2. **Don't use per-invocation shell for rich plugins** — no state, no event
   subscription, no custom tools, no streaming. Keep shell for the simple
   pre/post case only.

3. **Don't build a durable event store** — ocode is single-process. If a
   web UI or daemon mode materializes, upgrade then.

4. **Don't over-generic the protocol** — use a simple JSON-RPC (or even a
   fixed set of message types). Don't design for "every conceivable plugin
   model". Design for the hooks above.

---

## How to Use This Document

Each phase is a self-contained chunk. Start with Phase 1 (protocol + runner)
and Phase 2 (event bus) in parallel. Then Phase 3 (agent loop wiring). Leave
Phase 4 and Phase 5 for later if the core isn't proven yet.

---

[1] `agent.go` tool dispatch is around line 2795-2920 in the current codebase.
