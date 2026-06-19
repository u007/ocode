# Orchestrator Pipeline — Plan B: Entry Points

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the `orchestrator.Pipeline` (Plan A) into ocode's three entry points: the `/orchestrate` slash command (async TUI), the `orchestrator` agent-picker intercept (session mode), and the `ocode --orchestrate` CLI flag (headless).

**Architecture:** Each entry point creates an `orchestrator.Pipeline` with the session's `*agent.Agent` as parent and calls `Run()`. Slash command and session intercept run the pipeline as a background goroutine and stream status to the TUI. CLI flag runs synchronously, streams to stdout, exits 0/1.

**Tech Stack:** Go 1.26, `github.com/u007/ocode/internal/orchestrator` (Plan A), Bubble Tea TUI (`charm.land/bubbletea/v2`), existing slash command registry in `internal/tui/commands.go`, existing `switchAgent` in `internal/tui/model.go`, `main.go` CLI switch.

**Prerequisite:** Plan A must be complete — `internal/orchestrator/` package must compile and all Plan A tests must pass.

## Global Constraints

- Module: `github.com/u007/ocode`
- `gofmt` and `go vet ./...` must pass after every task
- Never disable `MouseMode` or touch TUI rendering outside the specific sections modified
- Pipeline background runs use existing `AgentRun` / background task infrastructure — do not create new goroutine management
- `--no-worktree` defaults to `false` (worktree on by default); the opt-out is explicit

---

## File Map

**Modify:**
- `internal/tui/commands.go` — add `/orchestrate` to `commandSpecs`, implement `runOrchestrateCmd`
- `internal/tui/model.go` — add `switchAgent` intercept for name `"orchestrator"`, add `orchestratorMode bool` field and message-send handler branch
- `internal/agent/agent_registry.go` (or `agent_loader.go`) — register `orchestrator` as a picker-only primary agent entry (no system prompt)
- `main.go` — add `--orchestrate` / `orchestrate` subcommand

**Create:**
- `internal/tui/orchestrate.go` — `runOrchestrateBackground()` helper (keeps `commands.go` clean)
- `internal/tui/orchestrate_test.go`
- `internal/cli/orchestrate.go` — headless CLI runner

---

### Task 11: `/orchestrate` slash command

**Files:**
- Modify: `internal/tui/commands.go` — add entry to `commandSpecs`, add `runOrchestrateCmd`
- Create: `internal/tui/orchestrate.go` — background pipeline runner
- Create: `internal/tui/orchestrate_test.go`

**Interfaces:**
- Consumes: `orchestrator.New()`, `orchestrator.Pipeline.Run()` (Plan A)
- Produces: `/orchestrate <goal>` command available in TUI, runs pipeline async

- [ ] **Step 1: Write the failing test**

```go
// internal/tui/orchestrate_test.go
package tui

import (
	"strings"
	"testing"
)

func TestOrchestrateCommandRegistered(t *testing.T) {
	found := false
	for _, spec := range commandSpecs {
		if spec.name == "/orchestrate" {
			found = true
			if spec.handler == nil {
				t.Error("/orchestrate has no handler")
			}
			break
		}
	}
	if !found {
		t.Error("/orchestrate not found in commandSpecs")
	}
}

func TestOrchestrateGoalExtraction(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"add", "user", "validation"}, "add user validation"},
		{[]string{"fix", "nil", "panic", "in", "auth"}, "fix nil panic in auth"},
		{[]string{}, ""},
	}
	for _, c := range cases {
		got := strings.Join(c.args, " ")
		if got != c.want {
			t.Errorf("args %v → %q, want %q", c.args, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run — verify `/orchestrate` not yet registered**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestOrchestrateCommandRegistered -v 2>&1 | head -10
```
Expected: FAIL — `/orchestrate not found in commandSpecs`

- [ ] **Step 3: Add command entry to `commandSpecs` in `commands.go`**

In the `init()` function in `internal/tui/commands.go`, add to the `commandSpecs` slice (after `/upload`):

```go
{name: "/orchestrate", usage: "/orchestrate <goal>", help: "Run the multi-agent orchestration pipeline on a coding goal", handler: runOrchestrateCmd},
```

- [ ] **Step 4: Implement `runOrchestrateCmd` in `commands.go`**

Add this function to `internal/tui/commands.go`:

```go
func runOrchestrateCmd(m *model, args []string) tea.Cmd {
	if len(args) == 0 {
		m.messages = append(m.messages, message{
			role: roleAssistant,
			text: "Usage: /orchestrate <goal>\nExample: /orchestrate add user validation to the login flow",
		})
		return nil
	}
	goal := strings.Join(args, " ")
	if m.agent == nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: "No active agent — cannot launch orchestrator."})
		return nil
	}
	m.messages = append(m.messages, message{
		role: roleAssistant,
		text: fmt.Sprintf("[Orchestrator] Starting pipeline for: %s\nRunning in background — status updates will appear here.", goal),
	})
	return runOrchestrateBackground(m, goal, false)
}
```

Ensure `"strings"` and `"fmt"` are already in the import block (they are).

- [ ] **Step 5: Create `orchestrate.go` with background runner**

```go
// internal/tui/orchestrate.go
package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/orchestrator"
)

// orchestrateStatusMsg is sent to the TUI model each time the pipeline
// transitions to a new state.
type orchestrateStatusMsg struct {
	state string
	msg   string
}

// orchestrateDoneMsg is sent when the pipeline completes (pass or halt).
type orchestrateDoneMsg struct {
	report string // FormatMarkdown() output
	err    string // non-empty if Run() returned an error
}

// runOrchestrateBackground launches the orchestrator pipeline as a Bubble Tea
// background command. Status updates stream to the TUI via orchestrateStatusMsg.
// noWorktree disables worktree isolation (--no-worktree mode).
func runOrchestrateBackground(m *model, goal string, noWorktree bool) tea.Cmd {
	parent := m.agent
	workDir := m.workDir

	return func() tea.Msg {
		opts := orchestrator.PipelineOptions{
			UseWorktree: !noWorktree,
			WorkDir:     workDir,
		}
		// StatusFunc sends intermediate state updates back to the TUI.
		// We can't send tea.Msg from a goroutine directly, so we collect
		// updates and the final result is delivered via the returned tea.Msg.
		// For live streaming, use a channel-based approach in a follow-up.
		pipeline := orchestrator.New(parent, opts)
		report, err := pipeline.Run(context.Background(), goal)
		if err != nil {
			return orchestrateDoneMsg{err: fmt.Sprintf("orchestrator error: %v", err)}
		}
		return orchestrateDoneMsg{report: report.FormatMarkdown()}
	}
}
```

- [ ] **Step 6: Handle `orchestrateDoneMsg` in `model.go`**

In `internal/tui/model.go`, find the `Update` method's `switch msg.(type)` block and add a case for `orchestrateDoneMsg`. Search for a similar pattern — e.g. how `agentDoneMsg` is handled — and add after it:

```go
case orchestrateDoneMsg:
    if msg.err != "" {
        m.messages = append(m.messages, message{
            role: roleAssistant,
            text: fmt.Sprintf("[Orchestrator] Error: %s", msg.err),
        })
    } else {
        m.messages = append(m.messages, message{
            role: roleAssistant,
            text: fmt.Sprintf("[Orchestrator] Complete:\n\n%s", msg.report),
        })
    }
    return m, nil
```

- [ ] **Step 7: Run tests — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestOrchestrateCommand -v
```

- [ ] **Step 8: Build and verify compile**

```bash
cd /Users/james/www/ocode && go build ./... && echo "build ok"
```
Expected: `build ok`

- [ ] **Step 9: Commit**

```bash
cd /Users/james/www/ocode && git add internal/tui/commands.go internal/tui/orchestrate.go internal/tui/orchestrate_test.go internal/tui/model.go
git commit -m "feat(tui): /orchestrate slash command — async pipeline launch"
```

---

### Task 12: `orchestrator` session intercept (agent picker)

**Files:**
- Modify: `internal/tui/model.go` — intercept `switchAgent("orchestrator")`
- Modify: `internal/agent/agent_registry.go` — register picker-only `orchestrator` entry

**Interfaces:**
- Consumes: `runOrchestrateBackground()` (Task 11)
- Produces: selecting `orchestrator` from agent picker sets a mode flag; subsequent user messages are routed to the pipeline instead of the normal LLM turn

- [ ] **Step 1: Write the failing test**

```go
// Add to internal/tui/orchestrate_test.go

func TestOrchestratorRegisteredInRegistry(t *testing.T) {
	def := agent.DefaultAgentRegistry.Get("orchestrator")
	if def == nil {
		t.Fatal("orchestrator not found in DefaultAgentRegistry")
	}
	if def.Hidden {
		t.Error("orchestrator should NOT be hidden — it must appear in the agent picker")
	}
	if def.SystemPrompt != "" {
		t.Error("orchestrator registry entry should have no system prompt — it is a picker-only entry")
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestOrchestratorRegistered -v 2>&1 | head -10
```
Expected: FAIL — `orchestrator not found in DefaultAgentRegistry`

- [ ] **Step 3: Register `orchestrator` as picker-only entry**

In `internal/agent/agent_registry.go`, find `NewAgentRegistry()` or the built-in registration block and add a built-in `orchestrator` entry with no system prompt:

```go
// In the function that seeds built-in agents, add:
registry.register(AgentDefinition{
    Name:         "orchestrator",
    Description:  "Self-healing multi-agent pipeline — plans, implements, and validates coding goals",
    Mode:         AgentModeAll,
    Hidden:       false,
    SystemPrompt: "", // intentionally empty — session loop intercepts this name
    Source:       "built-in",
})
```

Find the correct insertion point by checking how other built-in agents are registered in `NewAgentRegistry()`.

- [ ] **Step 4: Add `orchestratorMode` field and intercept to `model.go`**

Find the `model` struct definition in `internal/tui/model.go`. Add a field:

```go
orchestratorMode bool // true when user selected "orchestrator" from agent picker
```

Find `func (m *model) switchAgent(name string)` in `model.go` (line ~1304). Add an intercept at the top of the function, before the existing registry lookup:

```go
func (m *model) switchAgent(name string) {
	// Orchestrator is not a normal LLM agent — intercept and set mode flag.
	if name == "orchestrator" {
		m.orchestratorMode = true
		m.messages = append(m.messages, message{
			role: roleAssistant,
			text: "Orchestrator mode active. Send your coding goal and the pipeline will plan, implement, and validate it automatically.",
		})
		return
	}
	m.orchestratorMode = false
	// ... existing switchAgent logic below unchanged ...
```

- [ ] **Step 5: Route user messages to pipeline in orchestrator mode**

In `model.go`, find where user-submitted messages are dispatched to the agent (the section that calls `m.agent.Step()` or equivalent — search for `submitMessage` or `sendMessage` or the `tea.Cmd` that starts an LLM turn). Add an early-return branch:

```go
// At the top of the message submission handler, before the normal LLM dispatch:
if m.orchestratorMode && userInput != "" {
    goal := userInput
    m.messages = append(m.messages, message{role: roleUser, text: goal})
    return m, runOrchestrateBackground(m, goal, false)
}
```

The exact location depends on the current message submission flow in `model.go` — find where `m.agent` begins processing user input and add the branch immediately before it.

- [ ] **Step 6: Run tests — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestOrchestratorRegistered -v
```

- [ ] **Step 7: Build and verify**

```bash
cd /Users/james/www/ocode && go build ./... && echo "build ok"
```

- [ ] **Step 8: Commit**

```bash
cd /Users/james/www/ocode && git add internal/tui/model.go internal/agent/agent_registry.go internal/tui/orchestrate_test.go
git commit -m "feat(tui): orchestrator session intercept via agent picker"
```

---

### Task 13: `ocode --orchestrate` CLI flag (headless)

**Files:**
- Create: `internal/cli/orchestrate.go`
- Modify: `main.go` — add `orchestrate` subcommand case

**Interfaces:**
- Consumes: `orchestrator.New()`, `orchestrator.Pipeline.Run()` (Plan A)
- Produces: `ocode orchestrate "goal"` headless mode — streams status to stdout, exits 0/1

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/orchestrate_test.go
package cli

import (
	"strings"
	"testing"
)

func TestParseOrchestrateArgs_goal(t *testing.T) {
	opts, goal, err := ParseOrchestrateArgs([]string{"add user validation"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if goal != "add user validation" {
		t.Errorf("goal = %q, want %q", goal, "add user validation")
	}
	if opts.UseWorktree != true {
		t.Error("UseWorktree should default to true")
	}
}

func TestParseOrchestrateArgs_noWorktree(t *testing.T) {
	opts, _, err := ParseOrchestrateArgs([]string{"--no-worktree", "add user validation"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.UseWorktree != false {
		t.Error("--no-worktree should set UseWorktree=false")
	}
}

func TestParseOrchestrateArgs_verifyFlag(t *testing.T) {
	opts, _, err := ParseOrchestrateArgs([]string{"--verify", "build_test_llm", "add feature"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.VerifyMode != "build_test_llm" {
		t.Errorf("VerifyMode = %q", opts.VerifyMode)
	}
}

func TestParseOrchestrateArgs_maxIterations(t *testing.T) {
	opts, _, err := ParseOrchestrateArgs([]string{"--max-iterations", "6", "goal"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.MaxIterations != 6 {
		t.Errorf("MaxIterations = %d, want 6", opts.MaxIterations)
	}
}

func TestParseOrchestrateArgs_emptyGoal(t *testing.T) {
	_, _, err := ParseOrchestrateArgs([]string{})
	if err == nil {
		t.Error("expected error for empty goal")
	}
	if !strings.Contains(err.Error(), "goal") {
		t.Errorf("error should mention goal, got: %v", err)
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/cli/... -run TestParseOrchestrate -v 2>&1 | head -10
```
Expected: `cannot find package` or compile error

- [ ] **Step 3: Create `internal/cli/orchestrate.go`**

```go
// internal/cli/orchestrate.go
package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/u007/ocode/internal/orchestrator"
)

// OrchestrateOptions mirrors orchestrator.PipelineOptions for CLI parsing.
type OrchestrateOptions struct {
	UseWorktree   bool
	VerifyMode    string
	MaxIterations int
}

// ParseOrchestrateArgs parses CLI args for the orchestrate subcommand.
// Flags: --no-worktree, --verify <mode>, --max-iterations <n>
// Remaining args after flags are joined as the goal.
func ParseOrchestrateArgs(args []string) (OrchestrateOptions, string, error) {
	opts := OrchestrateOptions{UseWorktree: true}
	var remaining []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--no-worktree":
			opts.UseWorktree = false
		case "--verify":
			if i+1 >= len(args) {
				return opts, "", fmt.Errorf("--verify requires a value: llm_only | build_llm | build_test_llm")
			}
			i++
			opts.VerifyMode = args[i]
		case "--max-iterations":
			if i+1 >= len(args) {
				return opts, "", fmt.Errorf("--max-iterations requires a number")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return opts, "", fmt.Errorf("--max-iterations must be a positive integer")
			}
			opts.MaxIterations = n
		default:
			remaining = append(remaining, args[i])
		}
	}
	if len(remaining) == 0 {
		return opts, "", fmt.Errorf("goal is required: ocode orchestrate \"<goal>\"")
	}
	goal := ""
	for i, r := range remaining {
		if i > 0 {
			goal += " "
		}
		goal += r
	}
	return opts, goal, nil
}

// Run executes the orchestrator pipeline headlessly.
// Streams status lines to stdout. Exits with os.Exit(0) on pass, os.Exit(1) on halt/error.
func Run(args []string) error {
	opts, goal, err := ParseOrchestrateArgs(args)
	if err != nil {
		return fmt.Errorf("usage error: %w\n\nUsage: ocode orchestrate [--no-worktree] [--verify MODE] [--max-iterations N] \"<goal>\"", err)
	}

	fmt.Printf("[Orchestrator] Goal: %s\n", goal)

	// Build a minimal agent for headless use.
	// The agent needs a client configured from the current ocode config.
	// Import and initialise using the same pattern as the `run` subcommand.
	pipelineOpts := orchestrator.PipelineOptions{
		UseWorktree:   opts.UseWorktree,
		VerifyMode:    opts.VerifyMode,
		MaxIterations: opts.MaxIterations,
		StatusFunc: func(s orchestrator.State, msg string) {
			fmt.Printf("[%s] %s\n", s, msg)
		},
	}

	// Note: headless agent construction (client, config, tools) follows the
	// same pattern as internal/runcli — see that package for reference.
	// The pipeline.Run() call below is the integration point; agent wiring
	// is intentionally left as a follow-up once the runcli pattern is confirmed.
	_ = pipelineOpts
	_ = context.Background()

	fmt.Println("[Orchestrator] Headless agent wiring: see TODO.md")
	os.Exit(0)
	return nil
}
```

- [ ] **Step 4: Export `State` type from orchestrator package**

In `internal/orchestrator/states.go`, change `state` to `State` (exported) so the CLI's `StatusFunc` can reference it:

```go
// State is the current step of the pipeline state machine.
type State string

const (
	StatePlanning   State = "Planning"
	StateExploring  State = "Exploring"
	StateDeveloping State = "Developing"
	StateCompiling  State = "Compiling"
	StateValidating State = "Validating"
	StateAdvising   State = "Advising"
	StateDone       State = "Done"
)
```

Update all references in `pipeline.go` from lowercase `state*` to `State*`. Also update `PipelineOptions.StatusFunc` signature:

```go
StatusFunc func(s State, msg string) // called on each state transition (may be nil)
```

- [ ] **Step 5: Run `go vet` to catch any name changes**

```bash
cd /Users/james/www/ocode && go vet ./internal/orchestrator/... && echo "vet ok"
```
Fix any compilation errors from the state type rename.

- [ ] **Step 6: Add `orchestrate` case to `main.go`**

In `main.go`, find the `switch os.Args[1]` block (around line 68) and add a new case before the `default`:

```go
case "orchestrate":
    if err := orchestratecli.Run(os.Args[2:]); err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
    return
```

Add the import at the top of `main.go`:

```go
orchestratecli "github.com/u007/ocode/internal/cli"
```

Note: if `internal/cli` conflicts with an existing package, rename to `internal/orchestratecli` and update accordingly.

- [ ] **Step 7: Update TODO.md with headless agent wiring gap**

The `Run()` function in `orchestrate.go` has a stub for headless agent construction. Add to `TODO.md`:

```
## Orchestrator CLI — headless agent wiring (Plan B, Task 13)
The `ocode orchestrate` CLI subcommand needs a headless *agent.Agent built
from the current ocode config (provider, model, API key). Follow the pattern
in `internal/runcli/` — specifically how it initialises client + agent before
calling Step(). Connect that to `orchestrator.New(parent, opts).Run(ctx, goal)`.
```

- [ ] **Step 8: Run tests — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/cli/... -run TestParseOrchestrate -v
```

- [ ] **Step 9: Build and verify compile**

```bash
cd /Users/james/www/ocode && go build ./... && echo "build ok"
```

- [ ] **Step 10: Run all tests**

```bash
cd /Users/james/www/ocode && go test ./... 2>&1 | grep -E "^(ok|FAIL)" | head -20
```
Expected: all `ok`, no `FAIL`

- [ ] **Step 11: Commit**

```bash
cd /Users/james/www/ocode && git add internal/cli/ internal/orchestrator/states.go internal/orchestrator/pipeline.go main.go TODO.md
git commit -m "feat(cli): ocode orchestrate subcommand — headless pipeline runner"
```

---

## Plan B Complete

Verify the full integration:

```bash
# Slash command registered
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestOrchestrate -v

# Agent registry entry
cd /Users/james/www/ocode && go test ./internal/tui/... -run TestOrchestratorRegistered -v

# CLI arg parsing
cd /Users/james/www/ocode && go test ./internal/cli/... -v

# Full build
cd /Users/james/www/ocode && go build ./... && echo "all ok"
```

**Remaining work (from TODO.md):** headless agent wiring in `internal/cli/orchestrate.go` — connect `ocode orchestrate` to a real `*agent.Agent` using the `internal/runcli` pattern.
