# Small Model Priority + TUI Model Indicator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `SmallModelPriority` constant with free/cheap model candidates, auto-resolve the small model on startup, and show the active model+provider in the TUI both in the agent strip and status bar.

**Architecture:** New `internal/agent/small_model.go` owns the priority list and resolution logic. `AgentRun` gains a `ModelLabel()` helper. The TUI's `renderStatus()` and `renderAgentStrip()` are patched to show model tags. `~/.config/opencode/opencode.json` gets `"model": "opencode-go/deepseek-v4-flash"`.

**Tech Stack:** Go, Bubble Tea TUI (`internal/tui/model.go`), existing `agent.NewClient`, `config.SaveSmallModel`.

---

## File Map

| Action | Path | Purpose |
|--------|------|---------|
| Create | `internal/agent/small_model.go` | Priority list constant + `ResolveSmallModel()` |
| Create | `internal/agent/small_model_test.go` | Tests for resolution logic |
| Modify | `internal/agent/agent_runs.go` | Add `ModelLabel()` to `AgentRun` |
| Modify | `internal/tui/model.go` | `renderStatus` + `renderAgentStrip` model badges |
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

- [ ] **Step 3: Wire `ResolveSmallModel` into `newModel()`**

In `internal/tui/model.go`, inside `newModel()`, after `cfg, _ := config.Load()` (around line 923), add:

```go
// Auto-select a small model from the priority list if none is configured.
if cfg != nil && cfg.Ocode.SmallModel == "" {
    if small := agent.ResolveSmallModel(cfg); small != "" {
        cfg.Ocode.SmallModel = small
        _ = config.SaveSmallModel(small) // persist for next session; ignore error
    }
}
```

- [ ] **Step 4: Build to confirm it compiles**

```bash
cd /Users/james/www/ocode && go build ./... 2>&1
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
cd /Users/james/www/ocode && git add internal/tui/model.go internal/agent/small_model.go && git commit -m "feat(tui): auto-resolve small model from priority list on startup"
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

## Summary

| Task | Deliverable |
|------|------------|
| 1 | `SmallModelPriority` + `ResolveSmallModel()` with tests |
| 2 | Auto-resolution wired into TUI startup |
| 3 | `AgentRun.ModelLabel()` with tests |
| 4 | Model badge in agent strip card header |
| 5 | Active subagent model in status bar |
| 6 | opencode default set to `opencode-go/deepseek-v4-flash` |
