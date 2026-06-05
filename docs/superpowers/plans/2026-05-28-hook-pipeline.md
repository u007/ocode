# In-Process Hook Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an in-process hook pipeline to the agent so plugins can intercept tool calls, modify chat parameters, and inject shell environment variables without shelling out to external processes.

**Architecture:** Define a `HookPipeline` type in `internal/hooks/pipeline.go` with four hook points: `RunToolBefore`, `RunToolAfter`, `RunChatParams`, `RunShellEnv`. The `Agent` holds an optional `*hooks.Pipeline`. `chatWithDelta` (line 146 of `agent.go`) already type-asserts to `*GenericClient` — we hook `RunChatParams` there by temporarily setting `gc.Temperature`/`gc.TopP` (already `*float64` pointer fields on `GenericClient`). `RunShellEnv` is wired into `internal/tool/process.go` via a package-level `*hooks.Pipeline` var guarded by `sync.RWMutex`.

**Tech Stack:** Go, existing `internal/agent`, `internal/tool`, `internal/config`.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/hooks/pipeline.go` | Create | Hook function types, `Pipeline` struct, `Register*`, `Run*` methods |
| `internal/hooks/pipeline_test.go` | Create | Unit tests for each hook type and chaining |
| `internal/agent/agent.go` | Modify | Add `hooks *hooks.Pipeline` field + `SetHooks`; wire `RunToolBefore/After` in `executeToolCall`; wire `RunChatParams` in `chatWithDelta` |
| `internal/tool/process.go` | Modify | Add `processHooks *hooks.Pipeline` + `sync.RWMutex`; inject env from `RunShellEnv` on spawn |
| `internal/tui/model.go` | Modify | Create `*hooks.Pipeline`, call `agent.SetHooks` + `tool.SetHookPipeline` after agent construction |

---

### Task 1: Define the `HookPipeline` type

**Files:**
- Create: `internal/hooks/pipeline.go`
- Create: `internal/hooks/pipeline_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/hooks/pipeline_test.go
package hooks

import (
	"encoding/json"
	"testing"
)

func TestToolBeforeHookModifiesArgs(t *testing.T) {
	p := New()
	p.RegisterToolBefore(func(name string, args json.RawMessage) json.RawMessage {
		if name == "read" {
			return json.RawMessage(`{"path":"/modified"}`)
		}
		return args
	})
	result := p.RunToolBefore("read", json.RawMessage(`{"path":"/original"}`))
	if string(result) != `{"path":"/modified"}` {
		t.Errorf("got %s, want modified args", string(result))
	}
}

func TestToolBeforeHookChaining(t *testing.T) {
	p := New()
	calls := 0
	p.RegisterToolBefore(func(_ string, args json.RawMessage) json.RawMessage { calls++; return args })
	p.RegisterToolBefore(func(_ string, args json.RawMessage) json.RawMessage { calls++; return args })
	p.RunToolBefore("any", json.RawMessage(`{}`))
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestToolAfterHookModifiesResult(t *testing.T) {
	p := New()
	p.RegisterToolAfter(func(name, result string) string { return "[modified] " + result })
	got := p.RunToolAfter("bash", "hello")
	if got != "[modified] hello" {
		t.Errorf("got %q", got)
	}
}

func TestChatParamsHookOverridesTemperature(t *testing.T) {
	p := New()
	p.RegisterChatParams(func(model string, cp ChatParams) ChatParams {
		v := 0.1
		cp.Temperature = &v
		return cp
	})
	got := p.RunChatParams("gpt-4", ChatParams{})
	if got.Temperature == nil || *got.Temperature != 0.1 {
		t.Errorf("expected temperature 0.1, got %v", got.Temperature)
	}
}

func TestShellEnvHookInjectsVars(t *testing.T) {
	p := New()
	p.RegisterShellEnv(func(cwd string) map[string]string {
		return map[string]string{"MY_VAR": "injected"}
	})
	env := p.RunShellEnv("/some/dir")
	if env["MY_VAR"] != "injected" {
		t.Errorf("expected MY_VAR=injected, got %q", env["MY_VAR"])
	}
}

func TestShellEnvHookMergesMultiple(t *testing.T) {
	p := New()
	p.RegisterShellEnv(func(_ string) map[string]string { return map[string]string{"A": "1"} })
	p.RegisterShellEnv(func(_ string) map[string]string { return map[string]string{"B": "2"} })
	env := p.RunShellEnv("/dir")
	if env["A"] != "1" || env["B"] != "2" {
		t.Errorf("expected merged env, got %v", env)
	}
}

func TestEmptyPipelineIsNoOp(t *testing.T) {
	p := New()
	args := json.RawMessage(`{"x":1}`)
	if got := p.RunToolBefore("any", args); string(got) != string(args) {
		t.Errorf("empty pipeline changed args: %s", got)
	}
	if got := p.RunToolAfter("any", "result"); got != "result" {
		t.Errorf("empty pipeline changed result: %s", got)
	}
	if got := p.RunChatParams("m", ChatParams{}); got.Temperature != nil {
		t.Errorf("empty pipeline set temperature")
	}
	if env := p.RunShellEnv("/"); len(env) != 0 {
		t.Errorf("empty pipeline returned env vars")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd /Users/james/www/ocode && go test ./internal/hooks/... -v 2>&1 | head -10
```

Expected: `no Go files in .../internal/hooks`.

- [ ] **Step 3: Create `internal/hooks/pipeline.go`**

```go
package hooks

import "encoding/json"

// ToolBeforeFunc is called before a tool executes. Returns (potentially modified) args.
type ToolBeforeFunc func(name string, args json.RawMessage) json.RawMessage

// ToolAfterFunc is called after a tool executes. Returns (potentially modified) result.
type ToolAfterFunc func(name, result string) string

// ChatParams holds optional LLM request parameter overrides. Pointer fields
// distinguish "not set" from an explicit zero.
type ChatParams struct {
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
}

// ChatParamsFunc is called before each LLM request. It receives the current
// params and returns (potentially modified) params.
type ChatParamsFunc func(model string, params ChatParams) ChatParams

// ShellEnvFunc is called before spawning a bash subprocess. Returns extra
// environment variables to inject (key=value). Later registrations win on conflicts.
type ShellEnvFunc func(cwd string) map[string]string

// Pipeline holds registered hook functions for all four hook points.
type Pipeline struct {
	toolBefore []ToolBeforeFunc
	toolAfter  []ToolAfterFunc
	chatParams []ChatParamsFunc
	shellEnv   []ShellEnvFunc
}

// New returns an empty Pipeline.
func New() *Pipeline { return &Pipeline{} }

func (p *Pipeline) RegisterToolBefore(fn ToolBeforeFunc)  { p.toolBefore = append(p.toolBefore, fn) }
func (p *Pipeline) RegisterToolAfter(fn ToolAfterFunc)    { p.toolAfter = append(p.toolAfter, fn) }
func (p *Pipeline) RegisterChatParams(fn ChatParamsFunc)  { p.chatParams = append(p.chatParams, fn) }
func (p *Pipeline) RegisterShellEnv(fn ShellEnvFunc)      { p.shellEnv = append(p.shellEnv, fn) }

// RunToolBefore threads args through all registered ToolBeforeFuncs in order.
func (p *Pipeline) RunToolBefore(name string, args json.RawMessage) json.RawMessage {
	for _, fn := range p.toolBefore {
		args = fn(name, args)
	}
	return args
}

// RunToolAfter threads result through all registered ToolAfterFuncs in order.
func (p *Pipeline) RunToolAfter(name, result string) string {
	for _, fn := range p.toolAfter {
		result = fn(name, result)
	}
	return result
}

// RunChatParams threads params through all registered ChatParamsFuncs in order.
func (p *Pipeline) RunChatParams(model string, params ChatParams) ChatParams {
	for _, fn := range p.chatParams {
		params = fn(model, params)
	}
	return params
}

// RunShellEnv calls all registered ShellEnvFuncs and merges the results.
func (p *Pipeline) RunShellEnv(cwd string) map[string]string {
	merged := map[string]string{}
	for _, fn := range p.shellEnv {
		for k, v := range fn(cwd) {
			merged[k] = v
		}
	}
	return merged
}
```

- [ ] **Step 4: Run tests**

```
cd /Users/james/www/ocode && go test ./internal/hooks/... -v 2>&1
```

Expected: all 7 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/hooks/pipeline.go internal/hooks/pipeline_test.go
git commit -m "feat(hooks): add in-process hook pipeline (ToolBefore/After, ChatParams, ShellEnv)"
```

---

### Task 2: Wire `RunToolBefore`/`RunToolAfter` into `Agent.executeToolCall`

**Files:**
- Modify: `internal/agent/agent.go`

- [ ] **Step 1: Add `hooks` field and `SetHooks` to `Agent`**

Find `type Agent struct` in `agent.go`. Add field:

```go
hooks *hooks.Pipeline
```

Add import:

```go
"github.com/u007/ocode/internal/hooks"
```

After `NewAgent` (line ~208), add:

```go
// SetHooks wires an in-process hook pipeline into this agent.
func (a *Agent) SetHooks(p *hooks.Pipeline) { a.hooks = p }
```

- [ ] **Step 2: Wire hooks in `executeToolCall` (line ~763)**

`executeToolCall` calls `t.Execute(args)` and returns `(string, error)`. Add hook calls around it:

```go
func (a *Agent) executeToolCall(name string, args json.RawMessage) (string, error) {
    emitDebug("TOOL", fmt.Sprintf("→ %s %s", name, truncateDebugArgs(args, 120)))
    if !a.isToolAllowed(name) {
        return fmt.Sprintf("denied: tool %q is not allowed for this agent", name), nil
    }

    t, ok := a.tools[name]
    if !ok {
        return "", fmt.Errorf("tool %s not found", name)
    }

    // ... existing watcher/ignore injection block stays here ...

    // RunToolBefore hooks — may modify args.
    if a.hooks != nil {
        args = a.hooks.RunToolBefore(name, args)
    }

    result, err := t.Execute(args)
    if err != nil {
        return result, err
    }

    // RunToolAfter hooks — may modify result.
    if a.hooks != nil {
        result = a.hooks.RunToolAfter(name, result)
    }
    return result, nil
}
```

Place the `RunToolBefore` call immediately before `t.Execute(args)`. Place `RunToolAfter` immediately after `t.Execute` returns with `err == nil`.

- [ ] **Step 3: Build**

```
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): wire OnToolBefore/After hooks into executeToolCall"
```

---

### Task 3: Wire `RunChatParams` in `chatWithDelta`

**Files:**
- Modify: `internal/agent/agent.go`

`chatWithDelta` lives at line 146 of `agent.go`. It type-asserts to `*GenericClient`:

```go
func (a *Agent) chatWithDelta(stopCh <-chan struct{}, messages []Message, toolDefs []map[string]interface{}) (*Message, error) {
    gc, ok := a.client.(*GenericClient)
    if ok {
        if a.OnDelta != nil { ... }
        if a.OnUsage != nil { ... }
        ctx, ctxCancel := stopChContext(stopCh)
        defer ctxCancel()
        return gc.ChatWithContext(ctx, messages, toolDefs)
    }
    return a.client.Chat(messages, toolDefs)
}
```

`gc.Temperature` and `gc.TopP` are already `*float64` fields (lines 75–78 of `client.go`). `applyGenerationParams` (line 141) reads them automatically when building the request payload. We temporarily override them for the duration of the call.

- [ ] **Step 1: Add `RunChatParams` application inside `chatWithDelta`**

After the `SetOnUsage` block and before `ctx, ctxCancel := stopChContext(stopCh)`, add:

```go
// Apply ChatParams hooks. Save and restore original values so the client
// is not permanently mutated between calls.
origTemp, origTopP := gc.Temperature, gc.TopP
if a.hooks != nil {
    cp := a.hooks.RunChatParams(gc.Model, hooks.ChatParams{
        Temperature: gc.Temperature,
        TopP:        gc.TopP,
    })
    gc.Temperature = cp.Temperature
    gc.TopP = cp.TopP
}
defer func() {
    gc.Temperature = origTemp
    gc.TopP = origTopP
}()
```

The `defer` ensures the client is restored even if `ChatWithContext` panics.

- [ ] **Step 2: Build and test**

```
cd /Users/james/www/ocode && go build ./... && go test ./internal/agent/... 2>&1
```

Expected: pass.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/agent.go
git commit -m "feat(agent): wire RunChatParams hook into chatWithDelta"
```

---

### Task 4: Wire `RunShellEnv` into bash subprocess env injection

**Files:**
- Modify: `internal/tool/process.go`

- [ ] **Step 1: Find where subprocesses are spawned**

```
grep -n "exec.Command\|exec.CommandContext\|cmd\.Env\|cmd\.Start\|cmd\.Run" /Users/james/www/ocode/internal/tool/process.go | head -20
```

- [ ] **Step 2: Add mutex-guarded package-level pipeline variable**

At the top of `process.go`, after the existing `var` blocks:

```go
import (
    // ... existing imports ...
    "sync"
    "github.com/u007/ocode/internal/hooks"
)

var (
    processHooksMu sync.RWMutex
    processHooks   *hooks.Pipeline
)

// SetHookPipeline wires the hook pipeline into the process/tool layer.
// Safe to call concurrently.
func SetHookPipeline(p *hooks.Pipeline) {
    processHooksMu.Lock()
    processHooks = p
    processHooksMu.Unlock()
}
```

- [ ] **Step 3: Inject env vars from `RunShellEnv` when building subprocess command**

At each `exec.Command` / `exec.CommandContext` call site in `process.go`, after `cmd` is created and before `cmd.Start()` or `cmd.Run()`, add:

```go
processHooksMu.RLock()
ph := processHooks
processHooksMu.RUnlock()
if ph != nil {
    cwd, _ := os.Getwd()
    extra := ph.RunShellEnv(cwd)
    if len(extra) > 0 {
        base := os.Environ()
        for k, v := range extra {
            base = append(base, k+"="+v)
        }
        cmd.Env = base
    }
}
```

- [ ] **Step 4: Build and test**

```
cd /Users/james/www/ocode && go build ./... && go test ./internal/tool/... ./internal/hooks/... 2>&1
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tool/process.go
git commit -m "feat(tool): wire RunShellEnv hook into bash subprocess env injection"
```

---

### Task 5: Wire shared `Pipeline` from TUI

**Files:**
- Modify: `internal/tui/model.go`

- [ ] **Step 1: Add `hookPipeline` field to `model` struct**

```go
hookPipeline *hooks.Pipeline
```

Import:

```go
"github.com/u007/ocode/internal/hooks"
"github.com/u007/ocode/internal/tool"
```

- [ ] **Step 2: Initialise in `newModel` (or wherever the model is first constructed)**

```go
m.hookPipeline = hooks.New()
```

- [ ] **Step 3: Wire to agent and tool layer after agent construction**

Find `rebuildAgentWithExternalTools` (line ~3291 of `model.go`). After `next := agent.NewAgent(...)`, add:

```go
if m.hookPipeline != nil {
    next.SetHooks(m.hookPipeline)
    tool.SetHookPipeline(m.hookPipeline)
}
```

- [ ] **Step 4: Build**

```
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 5: Run all tests**

```
cd /Users/james/www/ocode && go test ./... 2>&1 | tail -20
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go
git commit -m "feat(tui): wire shared hooks.Pipeline to agent and tool layer"
```

---

## Self-Review

**All advisor issues addressed:**
- `sync.RWMutex` on `processHooks` (Task 4) — write-locked in `SetHookPipeline`, read-locked at each spawn site ✓
- No placeholder steps — all concrete: `chatWithDelta` at line 146, `gc.Temperature`/`gc.TopP` as `*float64` fields at lines 75–78, `applyGenerationParams` at line 141 reads them automatically ✓
- `ChatParams` pointer fields distinguish "not set" from zero — hooks that don't override a param return the original pointer unchanged ✓

**Type consistency:**
- `hooks.Pipeline` — defined Task 1, used Tasks 2, 3, 4, 5 ✓
- `hooks.ChatParams{Temperature, TopP *float64, MaxTokens *int}` — defined Task 1, populated Task 3 ✓
- `agent.SetHooks(*hooks.Pipeline)` — defined Task 2, called Task 5 ✓
- `tool.SetHookPipeline(*hooks.Pipeline)` — defined Task 4, called Task 5 ✓

**Known limit (documented):** `tool.processHooks` is process-global — concurrent web/serve sessions share the same pipeline. Acceptable for v1; add per-session pipeline threading if multi-session isolation becomes a requirement.
