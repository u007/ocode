# Small Model Priority + TUI Model Indicator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `SmallModelPriority` constant with free/cheap model candidates, auto-resolve the small model on startup (with a chat hint), wire it into the explore/subagent dispatch path, and show the active model+provider in the TUI both in the agent strip and status bar.

**Architecture:** New `internal/agent/small_model.go` owns the priority list and resolution logic. `AgentRun` gains a `ModelLabel()` helper. `subagent.go` wires the small model into explore/lightweight agent dispatch when `spec.Model` is unset. The TUI's `renderStatus()` and `renderAgentStrip()` are patched to show model tags. A transient chat hint surfaces the resolved small model on startup. `~/.config/opencode/opencode.json` gets `"model": "opencode-go/deepseek-v4-flash"`.

**Tech Stack:** Go, Bubble Tea TUI (`internal/tui/model.go`), existing `agent.NewClient`, `config.SaveSmallModel`.

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/agent/small_model.go` | Priority list constant + `ResolveSmallModel()` |
| Create | `internal/agent/small_model_test.go` | Tests for resolution logic |
| Modify | `internal/agent/agent_runs.go` | Add `ModelLabel()` to `AgentRun` |
| Modify | `internal/agent/subagent.go` | Wire small model into explore/lightweight agent dispatch |
| Modify | `internal/tui/model.go` | Startup chat hint + `renderStatus` + `renderAgentStrip` model badges |
| Edit   | `~/.config/opencode/opencode.json` | Set default model |

---

### Task 1: `SmallModelPriority` constant + `ResolveSmallModel()`

**Files:**
- Create: `internal/agent/small_model.go`
- Create: `internal/agent/small_model_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/small_model_test.go
package agent

import (
	"testing"

	"github.com/jamesmercstudio/ocode/internal/config"
)

func TestResolveSmallModel_ReturnsFirstViable(t *testing.T) {
	// Stub NewClient to return non-nil only for the second candidate.
	orig := newClientFn
	defer func() { newClientFn = orig }()
	newClientFn = func(cfg *config.Config, model string) LLMClient {
		if model == SmallModelPriority[1] {
			return &GenericClient{}
		}
		return nil
	}

	got := ResolveSmallModel(nil)
	if got != SmallModelPriority[1] {
		t.Fatalf("ResolveSmallModel = %q, want %q", got, SmallModelPriority[1])
	}
}

func TestResolveSmallModel_ReturnsEmptyWhenNoneViable(t *testing.T) {
	orig := newClientFn
	defer func() { newClientFn = orig }()
	newClientFn = func(_ *config.Config, _ string) LLMClient { return nil }

	got := ResolveSmallModel(nil)
	if got != "" {
		t.Fatalf("ResolveSmallModel = %q, want empty", got)
	}
}

func TestResolveSmallModel_SkipsWhenAlreadySet(t *testing.T) {
	called := false
	orig := newClientFn
	defer func() { newClientFn = orig }()
	newClientFn = func(_ *config.Config, _ string) LLMClient {
		called = true
		return &GenericClient{}
	}

	cfg := &config.Config{}
	cfg.Ocode.SmallModel = "existing/model"
	got := ResolveSmallModel(cfg)
	if got != "existing/model" {
		t.Fatalf("ResolveSmallModel should return existing, got %q", got)
	}
	if called {
		t.Fatal("should not probe NewClient when SmallModel already set")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/ -run "TestResolveSmallModel" -v 2>&1 | tail -20
```

Expected: compile error — `SmallModelPriority`, `ResolveSmallModel`, `newClientFn` not defined.

- [ ] **Step 3: Create `internal/agent/small_model.go`**

```go
package agent

import "github.com/jamesmercstudio/ocode/internal/config"

// SmallModelPriority is the ordered list of cheap/fast models tried when
// auto-selecting a small model for lightweight tasks (title generation, etc.).
// First candidate whose provider has a usable API key wins.
// "opencode/mimo-v2.5-free" is keyless and serves as a reliable fallback.
var SmallModelPriority = []string{
	"opencode-go/deepseek-v4-flash",
	"opencode/mimo-v2.5-free",
	"opencode-go/qwen-3.5-plus",
	"deepseek/deepseek-chat",
	"xiaomi-token-plan-sgp/MiMo-V2.5",
}

// newClientFn is the production factory; tests override it.
var newClientFn = func(cfg *config.Config, model string) LLMClient {
	return NewClient(cfg, model)
}

// ResolveSmallModel returns the first candidate in SmallModelPriority for which
// a client can be constructed (i.e. its provider key is available). Returns the
// already-configured value unchanged if cfg.Ocode.SmallModel is non-empty.
func ResolveSmallModel(cfg *config.Config) string {
	if cfg != nil && cfg.Ocode.SmallModel != "" {
		return cfg.Ocode.SmallModel
	}
	for _, candidate := range SmallModelPriority {
		if c := newClientFn(cfg, candidate); c != nil {
			return candidate
		}
	}
	return ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/ -run "TestResolveSmallModel" -v 2>&1 | tail -15
```

Expected: all three `TestResolveSmallModel_*` PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/james/www/ocode && git add internal/agent/small_model.go internal/agent/small_model_test.go && git commit -m "feat(agent): add SmallModelPriority constant and ResolveSmallModel"
```

---

### Task 2: Auto-resolve small model on TUI startup

**Files:**
- Modify: `internal/tui/model.go` — `newModel()` at line ~922

- [ ] **Step 1: Write the failing test**

The existing `model_test.go` doesn't test startup auto-resolution. Add this test at the end of `internal/tui/model_test.go`:

```go
func TestNewModel_AutoResolvesSmallModel(t *testing.T) {
	// Arrange: config with no SmallModel set, but a viable candidate exists.
	orig := agent.NewClientFnForTest()
	defer agent.RestoreNewClientFn(orig)
	agent.SetNewClientFn(func(cfg *config.Config, model string) agent.LLMClient {
		if model == agent.SmallModelPriority[0] {
			return &agent.GenericClientForTest{}
		}
		return nil
	})

	// newModel doesn't save; just verify cfg.Ocode.SmallModel is populated.
	// We test the helper directly since newModel has heavy TUI side-effects.
	cfg := &config.Config{}
	cfg.Ocode.SmallModel = ""
	resolved := agent.ResolveSmallModel(cfg)
	if resolved != agent.SmallModelPriority[0] {
		t.Fatalf("expected %q, got %q", agent.SmallModelPriority[0], resolved)
	}
}
```

> **Note:** This test verifies `ResolveSmallModel` integration with config rather than the full TUI init path (which requires bubbletea plumbing). The actual wiring in `newModel` is confirmed by the build compiling.

- [ ] **Step 2: Add exported test helpers to `small_model.go`** (needed by the test above)

Append to `internal/agent/small_model.go`:

```go
// NewClientFnForTest / SetNewClientFn / RestoreNewClientFn expose the
// newClientFn var for test packages that need to stub it.
func NewClientFnForTest() func(*config.Config, string) LLMClient { return newClientFn }
func SetNewClientFn(fn func(*config.Config, string) LLMClient)   { newClientFn = fn }
func RestoreNewClientFn(fn func(*config.Config, string) LLMClient) { newClientFn = fn }

// GenericClientForTest is a minimal LLMClient stub for tests.
type GenericClientForTest = GenericClient
```

- [ ] **Step 3: Wire `ResolveSmallModel` into `newModel()` with startup chat hint**

In `internal/tui/model.go`, inside `newModel()`, after `cfg, _ := config.Load()` (around line 923), add:

```go
// Auto-select a small model from the priority list if none is configured.
var resolvedSmallModel string
if cfg != nil && cfg.Ocode.SmallModel == "" {
    if small := agent.ResolveSmallModel(cfg); small != "" {
        cfg.Ocode.SmallModel = small
        resolvedSmallModel = small
        _ = config.SaveSmallModel(small) // persist for next session; ignore error
    }
}
```

Then, at the point where `m.messages` is first initialised (inside the `model{...}` literal or just after it, around line 995), append:

```go
if resolvedSmallModel != "" {
    m.messages = append(m.messages, message{
        role:      roleAssistant,
        text:      hintStyle.Render("⚡ small model: " + resolvedSmallModel),
        transient: true,
    })
}
```

- [ ] **Step 4: Build to confirm it compiles**

```bash
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
cd /Users/james/www/ocode && git add internal/tui/model.go internal/agent/small_model.go && git commit -m "feat(tui): auto-resolve small model on startup with chat hint"
```

---

### Task 3: `AgentRun.ModelLabel()` helper

**Files:**
- Modify: `internal/agent/agent_runs.go`
- Modify: `internal/agent/agent_runs_test.go` (or the existing `agent_registry_test.go`)

- [ ] **Step 1: Write the failing test**

In `internal/agent/agent_runs_test.go`, add:

```go
func TestAgentRun_ModelLabel(t *testing.T) {
	t.Run("returns provider/model when subagent has both", func(t *testing.T) {
		run := &AgentRun{
			Sub: &Agent{client: &GenericClient{provider: "opencode-go", model: "deepseek-v4-flash"}},
		}
		got := run.ModelLabel()
		if got != "opencode-go/deepseek-v4-flash" {
			t.Fatalf("ModelLabel = %q, want %q", got, "opencode-go/deepseek-v4-flash")
		}
	})

	t.Run("returns model only when no provider", func(t *testing.T) {
		run := &AgentRun{
			Sub: &Agent{client: &GenericClient{provider: "", model: "gpt-4o"}},
		}
		got := run.ModelLabel()
		if got != "gpt-4o" {
			t.Fatalf("ModelLabel = %q, want %q", got, "gpt-4o")
		}
	})

	t.Run("returns empty string when Sub is nil", func(t *testing.T) {
		run := &AgentRun{Sub: nil}
		if got := run.ModelLabel(); got != "" {
			t.Fatalf("ModelLabel = %q, want empty", got)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/ -run "TestAgentRun_ModelLabel" -v 2>&1 | tail -15
```

Expected: compile error — `ModelLabel` undefined.

- [ ] **Step 3: Add `ModelLabel()` to `AgentRun` in `agent_runs.go`**

After the `Done()` method (around line 79), add:

```go
// ModelLabel returns "provider/model" (or just "model" when no provider) for
// the subagent backing this run. Returns "" when Sub is nil.
func (r *AgentRun) ModelLabel() string {
	if r.Sub == nil {
		return ""
	}
	p := r.Sub.GetProvider()
	m := r.Sub.Client().GetModel()
	if p != "" {
		return p + "/" + m
	}
	return m
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/ -run "TestAgentRun_ModelLabel" -v 2>&1 | tail -15
```

Expected: all three sub-tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/james/www/ocode && git add internal/agent/agent_runs.go internal/agent/agent_runs_test.go && git commit -m "feat(agent): add ModelLabel() to AgentRun for TUI display"
```

---

### Task 4: Show model label in agent strip card header

**Files:**
- Modify: `internal/tui/model.go` — `renderAgentStrip()`, around line 6413

- [ ] **Step 1: Locate the card header line in `renderAgentStrip`**

The current head format is (around line 6413):

```go
head := fmt.Sprintf("▸ %-10s %s %s · %s", ri.Name, icon, status, formatRunElapsed(ri))
if summary := formatChildSummary(agentRunChildren(ri)); summary != "" {
    head += " · " + summary
}
```

- [ ] **Step 2: Append model label to the card header**

Replace those lines with:

```go
head := fmt.Sprintf("▸ %-10s %s %s · %s", ri.Name, icon, status, formatRunElapsed(ri))
if summary := formatChildSummary(agentRunChildren(ri)); summary != "" {
    head += " · " + summary
}
if lbl := ri.ModelLabel(); lbl != "" {
    head += " [" + lbl + "]"
}
```

- [ ] **Step 3: Build to confirm it compiles**

```bash
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/james/www/ocode && git add internal/tui/model.go && git commit -m "feat(tui): show model label in agent strip card header"
```

---

### Task 5: Show active subagent model in status bar

**Files:**
- Modify: `internal/tui/model.go` — `renderStatus()`, around line 7075

- [ ] **Step 1: Understand current leftStatus line**

```go
// line 7075
leftStatus := fmt.Sprintf(" LLM: %s · Agent: %s · Model: %s%s%s%s%s",
    llmState, displayAgentName, m.currentModelName(),
    reasoningState, permissionMode, compactState, jobState)
```

When subagents are running, `Model:` shows only the main agent's model. We want to append the model of the most recently started running subagent.

- [ ] **Step 2: Add `activeSubagentModel()` helper method on `model`**

Add this method anywhere near `currentModelName()` (around line 863 in `model.go`):

```go
// activeSubagentModel returns "provider/model" of the most recently started
// running subagent, or "" when no subagents are active.
func (m model) activeSubagentModel() string {
	if m.agent == nil || m.agent.Runs() == nil {
		return ""
	}
	runs := m.agent.Runs().Snapshot()
	// Walk in reverse — most recently started run is last.
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == agent.RunRunning {
			if lbl := runs[i].ModelLabel(); lbl != "" {
				return lbl
			}
		}
	}
	return ""
}
```

- [ ] **Step 3: Splice active subagent model into `renderStatus()`**

In `renderStatus()`, just before the `leftStatus` assignment (line 7075), add:

```go
subagentModel := m.activeSubagentModel()
```

Then update the `leftStatus` line to append it:

```go
leftStatus := fmt.Sprintf(" LLM: %s · Agent: %s · Model: %s%s%s%s%s",
    llmState, displayAgentName, m.currentModelName(),
    reasoningState, permissionMode, compactState, jobState)
if subagentModel != "" {
    leftStatus += fmt.Sprintf(" · subagent: %s", subagentModel)
}
```

- [ ] **Step 4: Build to confirm it compiles**

```bash
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 5: Run full test suite**

```bash
cd /Users/james/www/ocode && go test ./... 2>&1 | tail -20
```

Expected: all pass (or only pre-existing failures).

- [ ] **Step 6: Commit**

```bash
cd /Users/james/www/ocode && git add internal/tui/model.go && git commit -m "feat(tui): show active subagent model in status bar"
```

---

### Task 6: Set opencode default model to `opencode-go/deepseek-v4-flash`

**Files:**
- Edit: `~/.config/opencode/opencode.json`

- [ ] **Step 1: Update the `model` field**

In `~/.config/opencode/opencode.json`, change:

```json
"model": "",
```

to:

```json
"model": "opencode-go/deepseek-v4-flash",
```

This is a user-level config change, not committed to the repo.

- [ ] **Step 2: Verify opencode picks it up**

```bash
cd /Users/james/www/opencode && node -e "const f = require('fs'); const c = JSON.parse(f.readFileSync(process.env.HOME+'/.config/opencode/opencode.json','utf8')); console.log('model:', c.model)"
```

Expected output:
```
model: opencode-go/deepseek-v4-flash
```

---

---

### Task 7: Wire small model into explore/lightweight subagent dispatch

**Files:**
- Modify: `internal/agent/subagent.go` — around line 225 (after `NewAgent`, before `SetSpec`)

**Context:** `subagent.go:225` creates a subagent with `NewAgent(t.mainAgent.client, ...)`. Then `SetSpec` is called, which calls `applySpecModel` — this swaps the client only when `spec.Model != ""`. For agents like `explore`, `general`, and `compaction` the spec has no Model set, so they inherit the expensive main client. We inject the small model here.

The agents eligible for the small model are those whose spec has no Model set AND whose name is in the lightweight set: `explore`, `general`, `compaction`. The `build` (primary coding) agent must never get demoted.

- [ ] **Step 1: Write the failing test**

In `internal/agent/subagent_permissions_test.go` or a new `internal/agent/subagent_small_model_test.go`:

```go
package agent

import (
    "testing"
    "github.com/jamesmercstudio/ocode/internal/config"
)

func TestSmallModelEligible(t *testing.T) {
    cases := []struct {
        name  string
        want  bool
    }{
        {"explore", true},
        {"general", true},
        {"compaction", true},
        {"build", false},
        {"plan", false},
        {"", false},
    }
    for _, c := range cases {
        got := smallModelEligible(c.name)
        if got != c.want {
            t.Errorf("smallModelEligible(%q) = %v, want %v", c.name, got, c.want)
        }
    }
}

func TestTaskTool_injectsSmallModelForExplore(t *testing.T) {
    // Arrange
    orig := newClientFn
    defer func() { newClientFn = orig }()
    var capturedModel string
    newClientFn = func(cfg *config.Config, model string) LLMClient {
        capturedModel = model
        return &GenericClient{provider: "opencode-go", model: "deepseek-v4-flash"}
    }

    cfg := &config.Config{}
    cfg.Ocode.SmallModel = "opencode-go/deepseek-v4-flash"
    mainClient := &GenericClient{provider: "anthropic", model: "claude-sonnet-4"}
    mainAgent := NewAgent(mainClient, nil, cfg)

    task := TaskTool{mainAgent: mainAgent}
    spec := task.findAgent("explore")
    if spec == nil {
        t.Fatal("explore agent spec not found")
    }

    subAgent := NewAgent(mainClient, nil, cfg)
    subSpec := AgentSpec{Name: spec.Name, Model: spec.Model}
    injectSmallModelIfEligible(subAgent, &subSpec, cfg)

    if subSpec.Model != "opencode-go/deepseek-v4-flash" {
        t.Fatalf("expected small model injected, got %q", subSpec.Model)
    }
    _ = capturedModel
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/ -run "TestSmallModelEligible|TestTaskTool_injectsSmallModel" -v 2>&1 | tail -15
```

Expected: compile error — `smallModelEligible` and `injectSmallModelIfEligible` undefined.

- [ ] **Step 3: Add helpers to `small_model.go`**

Append to `internal/agent/small_model.go`:

```go
// smallModelEligibleNames is the set of agent names that may use the small
// model. Primary coding agents (build, plan) are excluded to avoid downgrading
// the main coding loop.
var smallModelEligibleNames = map[string]bool{
    "explore":    true,
    "general":    true,
    "compaction": true,
}

// smallModelEligible reports whether the named agent is a candidate for the
// small model. Empty name returns false.
func smallModelEligible(name string) bool {
    return name != "" && smallModelEligibleNames[name]
}

// injectSmallModelIfEligible sets spec.Model to the configured small model
// when the spec has no explicit model and the agent name is eligible.
// No-op if cfg is nil, cfg.Ocode.SmallModel is empty, or spec already has a
// Model set (explicit registry override takes precedence).
func injectSmallModelIfEligible(a *Agent, spec *AgentSpec, cfg *config.Config) {
    if cfg == nil || cfg.Ocode.SmallModel == "" {
        return
    }
    if spec == nil || !smallModelEligible(spec.Name) {
        return
    }
    if strings.TrimSpace(spec.Model) != "" {
        return // explicit override in agent definition wins
    }
    spec.Model = cfg.Ocode.SmallModel
    emitDebug("AGENT", fmt.Sprintf("spec %q: injecting small model %s", spec.Name, spec.Model))
}
```

Also add `"fmt"` and `"strings"` to the import block of `small_model.go` (they are needed by the new helpers):

```go
import (
    "fmt"
    "strings"

    "github.com/jamesmercstudio/ocode/internal/config"
)
```

- [ ] **Step 4: Call `injectSmallModelIfEligible` in `subagent.go`**

In `internal/agent/subagent.go`, after building `subSpec` (around line 244, just before `subAgent.SetSpec(&subSpec)`):

```go
// Inject the small model for lightweight agents (explore, general, compaction)
// when no explicit model override is present on the spec.
injectSmallModelIfEligible(subAgent, &subSpec, t.mainAgent.config)
subAgent.SetSpec(&subSpec)
```

(Replace the bare `subAgent.SetSpec(&subSpec)` call with the two lines above.)

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/ -run "TestSmallModelEligible|TestTaskTool_injectsSmallModel" -v 2>&1 | tail -15
```

Expected: all tests PASS.

- [ ] **Step 6: Run full test suite**

```bash
cd /Users/james/www/ocode && go test ./... 2>&1 | tail -20
```

Expected: all pass (or only pre-existing failures).

- [ ] **Step 7: Commit**

```bash
cd /Users/james/www/ocode && git add internal/agent/small_model.go internal/agent/subagent.go internal/agent/subagent_small_model_test.go && git commit -m "feat(agent): wire small model into explore/general/compaction subagent dispatch"
```

---

## Summary

| Task | Deliverable |
|------|------------|
| 1 | `SmallModelPriority` + `ResolveSmallModel()` with tests |
| 2 | Auto-resolution wired into TUI startup + chat hint |
| 3 | `AgentRun.ModelLabel()` with tests |
| 4 | Model badge in agent strip card header |
| 5 | Active subagent model in status bar |
| 6 | opencode default set to `opencode-go/deepseek-v4-flash` |
| 7 | Small model wired into explore/general/compaction agent dispatch |
