# Orchestrator Pipeline — Plan A: Core Engine

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `internal/orchestrator/` Go package — the self-healing multi-agent pipeline engine — plus four specialist agent markdown files and small-model registration. Produces `Pipeline.Run()` callable from tests and, later, entry-point wiring (Plan B).

**Architecture:** Go state machine (Planning→Exploring→Developing→Compiling→Validating→Advising→Done) dispatches specialist subagents via the existing `*agent.Agent` dispatch path. A growing `ContextDoc` is the core data artefact — it accumulates context across iterations and renders a curated prompt for each agent dispatch. Worktree isolation (default on) keeps compile/test results deterministic. Advisor escalation is a one-shot circuit breaker.

**Tech Stack:** Go 1.26, `github.com/u007/ocode/internal/agent` (existing), `os/exec` for compile commands, `os` for worktree shell commands, standard `testing` package.

## Global Constraints

- Module: `github.com/u007/ocode`
- All new Go files: `package orchestrator` (except modifications to `internal/agent/`)
- Test files: `package orchestrator` (white-box) or `package orchestrator_test` (black-box); follow existing pattern in `internal/agent/` — white-box preferred for state machine internals
- No new external dependencies — stdlib and internal packages only
- `gofmt` and `go vet ./internal/orchestrator/...` must pass after every task
- Agent markdown files: `.opencode/agents/` (create dir if absent)
- All `Delay()` / timing functions must accept a seed/rand param so tests are deterministic (no `math/rand` calls inside the function itself)

---

## File Map

**Create:**
- `internal/orchestrator/plan.go` — `Plan` struct, `ParsePlan()`
- `internal/orchestrator/plan_test.go`
- `internal/orchestrator/context_doc.go` — `ContextDoc`, `Iteration`, `FileDiff`, `Render()`
- `internal/orchestrator/context_doc_test.go`
- `internal/orchestrator/report.go` — `StructuredReport`, `DeveloperReport`
- `internal/orchestrator/report_test.go`
- `internal/orchestrator/parse.go` — `ExtractVerdict()`, `ExtractDeveloperReport()`, `ExtractPlanJSON()`
- `internal/orchestrator/parse_test.go`
- `internal/orchestrator/backoff.go` — `BackoffPolicy`, `Delay()`
- `internal/orchestrator/backoff_test.go`
- `internal/orchestrator/worktree.go` — `WorktreeManager`
- `internal/orchestrator/worktree_test.go`
- `internal/orchestrator/advisor.go` — `escalateToAdvisor()`
- `internal/orchestrator/advisor_test.go`
- `internal/orchestrator/pipeline.go` — `Pipeline`, `PipelineOptions`, `New()`, `Run()`
- `internal/orchestrator/states.go` — `state` type, state constants
- `internal/orchestrator/pipeline_test.go`
- `.opencode/agents/orchestrator-planner.md`
- `.opencode/agents/orchestrator-explorer.md`
- `.opencode/agents/orchestrator-developer.md`
- `.opencode/agents/orchestrator-validator.md`

**Modify:**
- `internal/agent/agent.go` — add `DispatchSubagent()` method
- `internal/agent/small_model.go` — add `orchestrator-planner`, `orchestrator-explorer` to eligible names

---

### Task 1: `Plan` struct and JSON parsing

**Files:**
- Create: `internal/orchestrator/plan.go`
- Create: `internal/orchestrator/plan_test.go`

**Interfaces:**
- Produces: `Plan`, `ParsePlan(s string) (*Plan, error)` — consumed by Tasks 2, 8

- [ ] **Step 1: Write the failing tests**

```go
// internal/orchestrator/plan_test.go
package orchestrator

import (
	"testing"
)

func TestParsePlan_valid(t *testing.T) {
	raw := `{
		"intent": "feature",
		"goal": "add user validation",
		"success_criteria": ["nil input returns error", "valid input passes"],
		"verify_mode": "build_test_llm",
		"max_iterations": 4
	}`
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Intent != "feature" {
		t.Errorf("intent = %q, want feature", p.Intent)
	}
	if p.Goal != "add user validation" {
		t.Errorf("goal = %q", p.Goal)
	}
	if len(p.SuccessCriteria) != 2 {
		t.Errorf("success_criteria len = %d, want 2", len(p.SuccessCriteria))
	}
	if p.VerifyMode != "build_test_llm" {
		t.Errorf("verify_mode = %q", p.VerifyMode)
	}
	if p.MaxIterations != 4 {
		t.Errorf("max_iterations = %d, want 4", p.MaxIterations)
	}
}

func TestParsePlan_defaultMaxIterations(t *testing.T) {
	raw := `{"intent":"bugfix","goal":"fix crash","success_criteria":["no panic"],"verify_mode":"build_llm"}`
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.MaxIterations != 4 {
		t.Errorf("default max_iterations = %d, want 4", p.MaxIterations)
	}
}

func TestParsePlan_missingGoal(t *testing.T) {
	raw := `{"intent":"feature","success_criteria":["x"],"verify_mode":"llm_only"}`
	_, err := ParsePlan(raw)
	if err == nil {
		t.Fatal("expected error for missing goal")
	}
}

func TestParsePlan_unknownIntent(t *testing.T) {
	raw := `{"intent":"refactor","goal":"g","success_criteria":["x"],"verify_mode":"llm_only"}`
	_, err := ParsePlan(raw)
	if err == nil {
		t.Fatal("expected error for unknown intent")
	}
}

func TestParsePlan_unknownVerifyMode(t *testing.T) {
	raw := `{"intent":"feature","goal":"g","success_criteria":["x"],"verify_mode":"everything"}`
	_, err := ParsePlan(raw)
	if err == nil {
		t.Fatal("expected error for unknown verify_mode")
	}
}

func TestParsePlan_embeddedInProse(t *testing.T) {
	// Planner may wrap JSON in prose — we extract the first JSON object
	raw := `Here is the plan:\n{"intent":"bugfix","goal":"fix nil","success_criteria":["no crash"],"verify_mode":"build_llm"}`
	p, err := ParsePlan(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Intent != "bugfix" {
		t.Errorf("intent = %q", p.Intent)
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... 2>&1 | head -20
```
Expected: `cannot find package` or `undefined: ParsePlan`

- [ ] **Step 3: Implement `plan.go`**

```go
// internal/orchestrator/plan.go
package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Plan is the immutable task contract produced by the planner agent.
// It is parsed once at the start of Planning and never mutated.
type Plan struct {
	Intent          string   `json:"intent"`           // "feature" | "bugfix"
	Goal            string   `json:"goal"`
	SuccessCriteria []string `json:"success_criteria"`
	VerifyMode      string   `json:"verify_mode"`      // "llm_only" | "build_llm" | "build_test_llm"
	MaxIterations   int      `json:"max_iterations"`
}

var validIntents = map[string]bool{"feature": true, "bugfix": true}
var validVerifyModes = map[string]bool{
	"llm_only":       true,
	"build_llm":      true,
	"build_test_llm": true,
}

// ParsePlan extracts the first JSON object from s and validates it as a Plan.
// Planner LLM output may wrap JSON in prose — we tolerate that.
func ParsePlan(s string) (*Plan, error) {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in planner output")
	}
	var p Plan
	if err := json.Unmarshal([]byte(s[start:end+1]), &p); err != nil {
		return nil, fmt.Errorf("plan JSON parse error: %w", err)
	}
	if p.Goal == "" {
		return nil, fmt.Errorf("plan missing required field: goal")
	}
	if p.Intent == "" {
		return nil, fmt.Errorf("plan missing required field: intent")
	}
	if !validIntents[p.Intent] {
		return nil, fmt.Errorf("plan intent %q must be one of: feature, bugfix", p.Intent)
	}
	if p.VerifyMode == "" {
		return nil, fmt.Errorf("plan missing required field: verify_mode")
	}
	if !validVerifyModes[p.VerifyMode] {
		return nil, fmt.Errorf("plan verify_mode %q must be one of: llm_only, build_llm, build_test_llm", p.VerifyMode)
	}
	if len(p.SuccessCriteria) == 0 {
		return nil, fmt.Errorf("plan missing required field: success_criteria (must have at least one)")
	}
	if p.MaxIterations <= 0 {
		p.MaxIterations = 4
	}
	return &p, nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestParsePlan -v
```
Expected: all `TestParsePlan_*` PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/james/www/ocode && git add internal/orchestrator/plan.go internal/orchestrator/plan_test.go
git commit -m "feat(orchestrator): Plan struct and JSON parsing"
```

---

### Task 2: `ContextDoc` and `Render()` with truncation

**Files:**
- Create: `internal/orchestrator/context_doc.go`
- Create: `internal/orchestrator/context_doc_test.go`

**Interfaces:**
- Consumes: `Plan` (Task 1)
- Produces: `ContextDoc`, `FileDiff`, `Iteration`, `(*ContextDoc).AddIteration()`, `(*ContextDoc).Render(brief string) string` — consumed by Tasks 8, 9

- [ ] **Step 1: Write the failing tests**

```go
// internal/orchestrator/context_doc_test.go
package orchestrator

import (
	"strings"
	"testing"
)

func baseDoc() *ContextDoc {
	return &ContextDoc{
		Plan: Plan{
			Goal:            "add user validation",
			SuccessCriteria: []string{"nil input returns error", "valid input passes"},
		},
		ExploreSnapshot: "## auth.go\nfunc Validate(u *User) error { ... }",
	}
}

func TestRender_noIterations(t *testing.T) {
	doc := baseDoc()
	out := doc.Render("implement validation")
	if !strings.Contains(out, "[GOAL]") {
		t.Error("missing [GOAL]")
	}
	if !strings.Contains(out, "add user validation") {
		t.Error("missing goal text")
	}
	if !strings.Contains(out, "[SUCCESS CRITERIA]") {
		t.Error("missing [SUCCESS CRITERIA]")
	}
	if !strings.Contains(out, "nil input returns error") {
		t.Error("missing criterion")
	}
	if !strings.Contains(out, "[CODEBASE CONTEXT]") {
		t.Error("missing [CODEBASE CONTEXT]")
	}
	if !strings.Contains(out, "func Validate") {
		t.Error("missing explore snapshot")
	}
	if strings.Contains(out, "[PRIOR ATTEMPTS") {
		t.Error("should not have prior attempts section with 0 iterations")
	}
	if strings.Contains(out, "[RECENT ATTEMPTS]") {
		t.Error("should not have recent attempts with 0 iterations")
	}
	if !strings.Contains(out, "implement validation") {
		t.Error("missing brief")
	}
}

func TestRender_twoIterationsVerbatim(t *testing.T) {
	doc := baseDoc()
	doc.AddIteration(Iteration{
		Number:          1,
		FilesChanged:    []FileDiff{{Path: "auth.go", Summary: "added guard"}},
		CompilerOutput:  "ok",
		ValidatorReport: "### Validation Failure Report\n- **Issue:** missing nil check",
	})
	doc.AddIteration(Iteration{
		Number:          2,
		FilesChanged:    []FileDiff{{Path: "auth.go", Summary: "fixed nil"}},
		CompilerOutput:  "ok",
		ValidatorReport: "### Validation Failure Report\n- **Issue:** empty string not handled",
	})
	out := doc.Render("fix empty string")
	if strings.Contains(out, "[PRIOR ATTEMPTS SUMMARY]") {
		t.Error("should not summarise when ≤2 iterations")
	}
	if !strings.Contains(out, "[RECENT ATTEMPTS]") {
		t.Error("missing [RECENT ATTEMPTS]")
	}
	if !strings.Contains(out, "Attempt 1") {
		t.Error("missing attempt 1")
	}
	if !strings.Contains(out, "missing nil check") {
		t.Error("missing validator report for attempt 1")
	}
}

func TestRender_threeIterationsTruncates(t *testing.T) {
	doc := baseDoc()
	for i := 1; i <= 3; i++ {
		doc.AddIteration(Iteration{
			Number:          i,
			FilesChanged:    []FileDiff{{Path: "auth.go", Summary: "change"}},
			CompilerOutput:  "ok",
			ValidatorReport: fmt.Sprintf("### Validation Failure Report\n- **Issue:** problem %d", i),
		})
	}
	out := doc.Render("fix problem 3")
	if !strings.Contains(out, "[PRIOR ATTEMPTS SUMMARY]") {
		t.Error("missing prior attempts summary for iteration 1")
	}
	if !strings.Contains(out, "Attempt 1:") {
		t.Error("attempt 1 should appear in summary")
	}
	// attempt 1 full diff should NOT appear (it's summarised)
	if strings.Count(out, "problem 1") > 1 {
		t.Error("attempt 1 full report should not appear verbatim")
	}
	// attempts 2 and 3 should appear verbatim
	if !strings.Contains(out, "problem 2") {
		t.Error("attempt 2 full report missing")
	}
	if !strings.Contains(out, "problem 3") {
		t.Error("attempt 3 full report missing")
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestRender 2>&1 | head -10
```

- [ ] **Step 3: Implement `context_doc.go`**

```go
// internal/orchestrator/context_doc.go
package orchestrator

import (
	"fmt"
	"strings"
)

const recentIterationsCount = 2

// FileDiff records what a developer changed in one file during one iteration.
type FileDiff struct {
	Path    string
	Summary string // one-line description of what changed
	Diff    string // full unified diff text (may be empty)
}

// Iteration records one full developer round.
type Iteration struct {
	Number          int
	DeveloperBrief  string
	FilesChanged    []FileDiff
	CompilerOutput  string
	ValidatorReport string // empty if validator passed
	AdvisorNote     string // empty unless advisor was consulted
}

// ContextDoc is the pipeline's memory. It grows across iterations.
// Render() produces a curated prompt for each agent dispatch.
type ContextDoc struct {
	Plan            Plan
	ExploreSnapshot string
	Iterations      []Iteration
	ReExploreHints  []string
}

// AddIteration appends a completed iteration record.
func (c *ContextDoc) AddIteration(iter Iteration) {
	c.Iterations = append(c.Iterations, iter)
}

// Render produces the prompt string for the next agent dispatch.
// brief is the "[YOUR TASK THIS ROUND]" instruction — caller provides it
// so the prompt is specific to the current state (first attempt vs retry vs post-advisor).
func (c *ContextDoc) Render(brief string) string {
	var b strings.Builder

	// Goal
	b.WriteString("[GOAL]\n")
	b.WriteString(c.Plan.Goal)
	b.WriteString("\n\n")

	// Success criteria
	b.WriteString("[SUCCESS CRITERIA]\n")
	for _, sc := range c.Plan.SuccessCriteria {
		b.WriteString("- ")
		b.WriteString(sc)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Codebase context
	b.WriteString("[CODEBASE CONTEXT]\n")
	b.WriteString(c.ExploreSnapshot)
	b.WriteString("\n\n")

	if len(c.Iterations) == 0 {
		b.WriteString("[YOUR TASK THIS ROUND]\n")
		b.WriteString(brief)
		return b.String()
	}

	// Split iterations: older (summarised) vs recent (verbatim)
	cutoff := len(c.Iterations) - recentIterationsCount
	if cutoff < 0 {
		cutoff = 0
	}

	if cutoff > 0 {
		b.WriteString("[PRIOR ATTEMPTS SUMMARY]\n")
		for _, iter := range c.Iterations[:cutoff] {
			files := make([]string, len(iter.FilesChanged))
			for i, f := range iter.FilesChanged {
				files[i] = f.Path
			}
			issue := "(no validator report)"
			if iter.ValidatorReport != "" {
				// Extract first Issue line for the summary
				for _, line := range strings.Split(iter.ValidatorReport, "\n") {
					if strings.HasPrefix(line, "- **Issue:**") {
						issue = strings.TrimPrefix(line, "- **Issue:** ")
						break
					}
				}
			}
			b.WriteString(fmt.Sprintf("Attempt %d: changed [%s] — validator rejected: %s\n",
				iter.Number, strings.Join(files, ", "), issue))
		}
		b.WriteString("\n")
	}

	// Recent iterations verbatim
	b.WriteString("[RECENT ATTEMPTS]\n")
	for _, iter := range c.Iterations[cutoff:] {
		b.WriteString(fmt.Sprintf("Attempt %d:\n", iter.Number))
		if len(iter.FilesChanged) > 0 {
			b.WriteString("  Changed:\n")
			for _, f := range iter.FilesChanged {
				b.WriteString(fmt.Sprintf("    %s — %s\n", f.Path, f.Summary))
			}
		}
		if iter.CompilerOutput != "" {
			b.WriteString("  Compiler:\n")
			b.WriteString(indent(iter.CompilerOutput, "    "))
			b.WriteString("\n")
		}
		if iter.ValidatorReport != "" {
			b.WriteString("  Validator rejection:\n")
			b.WriteString(indent(iter.ValidatorReport, "    "))
			b.WriteString("\n")
		}
		if iter.AdvisorNote != "" {
			b.WriteString("  Advisor note:\n")
			b.WriteString(indent(iter.AdvisorNote, "    "))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("[YOUR TASK THIS ROUND]\n")
	b.WriteString(brief)
	return b.String()
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Add missing `fmt` import to test file**

The test uses `fmt.Sprintf` — add `"fmt"` to the import block in `context_doc_test.go`.

- [ ] **Step 5: Run tests — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestRender -v
```
Expected: all `TestRender_*` PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/james/www/ocode && git add internal/orchestrator/context_doc.go internal/orchestrator/context_doc_test.go
git commit -m "feat(orchestrator): ContextDoc with Render() and truncation"
```

---

### Task 3: `StructuredReport` and `DeveloperReport`

**Files:**
- Create: `internal/orchestrator/report.go`
- Create: `internal/orchestrator/report_test.go`

**Interfaces:**
- Consumes: `FileDiff` (Task 2)
- Produces: `StructuredReport`, `DeveloperReport`, `(*StructuredReport).FormatMarkdown() string` — consumed by Task 8

- [ ] **Step 1: Write the failing tests**

```go
// internal/orchestrator/report_test.go
package orchestrator

import (
	"strings"
	"testing"
)

func TestStructuredReport_FormatMarkdown_passed(t *testing.T) {
	r := &StructuredReport{
		Goal:             "add validation",
		TotalIterations:  2,
		AdvisorConsulted: false,
		Passed:           true,
	}
	out := r.FormatMarkdown()
	if !strings.Contains(out, "VALIDATION_PASSED") {
		t.Error("missing VALIDATION_PASSED")
	}
	if !strings.Contains(out, "add validation") {
		t.Error("missing goal")
	}
	if !strings.Contains(out, "2") {
		t.Error("missing iteration count")
	}
}

func TestStructuredReport_FormatMarkdown_halted(t *testing.T) {
	r := &StructuredReport{
		Goal:                "add validation",
		TotalIterations:     4,
		AdvisorConsulted:    true,
		AdvisorNote:         "Try adding a nil guard at line 12",
		Passed:              false,
		FinalValidatorReport: "### Validation Failure Report\n- **Issue:** nil panic",
		RecommendedNextStep: "Add nil guard before calling Validate()",
		FilesChanged:        []FileDiff{{Path: "auth.go", Summary: "added validate func"}},
	}
	out := r.FormatMarkdown()
	if !strings.Contains(out, "HALTED") {
		t.Error("missing HALTED")
	}
	if !strings.Contains(out, "nil panic") {
		t.Error("missing validator report")
	}
	if !strings.Contains(out, "Add nil guard") {
		t.Error("missing recommended next step")
	}
	if !strings.Contains(out, "auth.go") {
		t.Error("missing changed file")
	}
	if !strings.Contains(out, "advisor") {
		t.Error("missing advisor mention")
	}
}

func TestDeveloperReport_defaults(t *testing.T) {
	dr := &DeveloperReport{Confidence: "low"}
	if dr.Confidence != "low" {
		t.Error("confidence not set")
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestStructuredReport -v 2>&1 | head -10
```

- [ ] **Step 3: Implement `report.go`**

```go
// internal/orchestrator/report.go
package orchestrator

import (
	"fmt"
	"strings"
)

// StructuredReport is the final output of the pipeline — either a success or
// a HALTED failure with full context for human intervention.
type StructuredReport struct {
	Goal                string
	TotalIterations     int
	AdvisorConsulted    bool
	AdvisorNote         string
	Passed              bool
	FinalValidatorReport string   // last rejection (empty if Passed)
	FilesChanged        []FileDiff
	RecommendedNextStep string   // advisor's best guess (empty if Passed)
}

// DeveloperReport is parsed from the developer agent's completion output.
type DeveloperReport struct {
	FilesChanged         []string
	WhatWasDone          string
	WhatWasNotDone       string
	Confidence           string // "high" | "medium" | "low"
	SuggestedValidatorFocus string
}

// FormatMarkdown renders the report as a human-readable markdown string.
func (r *StructuredReport) FormatMarkdown() string {
	var b strings.Builder
	if r.Passed {
		b.WriteString("## VALIDATION_PASSED\n\n")
		b.WriteString(fmt.Sprintf("**Goal:** %s\n\n", r.Goal))
		b.WriteString(fmt.Sprintf("**Iterations:** %d\n", r.TotalIterations))
		if r.AdvisorConsulted {
			b.WriteString("**Advisor consulted:** yes\n")
		}
		return b.String()
	}

	b.WriteString("## HALTED — Validation did not pass\n\n")
	b.WriteString(fmt.Sprintf("**Goal:** %s\n\n", r.Goal))
	b.WriteString(fmt.Sprintf("**Total iterations:** %d\n", r.TotalIterations))
	if r.AdvisorConsulted {
		b.WriteString("**Advisor consulted:** yes\n")
		if r.AdvisorNote != "" {
			b.WriteString(fmt.Sprintf("**Advisor note:** %s\n", r.AdvisorNote))
		}
	}
	b.WriteString("\n")

	if r.FinalValidatorReport != "" {
		b.WriteString("### Final Validator Report\n\n")
		b.WriteString(r.FinalValidatorReport)
		b.WriteString("\n\n")
	}

	if len(r.FilesChanged) > 0 {
		b.WriteString("### Files in working state (unvalidated)\n\n")
		for _, f := range r.FilesChanged {
			b.WriteString(fmt.Sprintf("- `%s` — %s\n", f.Path, f.Summary))
		}
		b.WriteString("\n")
	}

	if r.RecommendedNextStep != "" {
		b.WriteString("### Recommended next step\n\n")
		b.WriteString(r.RecommendedNextStep)
		b.WriteString("\n")
	}

	return b.String()
}
```

- [ ] **Step 4: Run tests — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestStructuredReport -v
```

- [ ] **Step 5: Commit**

```bash
cd /Users/james/www/ocode && git add internal/orchestrator/report.go internal/orchestrator/report_test.go
git commit -m "feat(orchestrator): StructuredReport and DeveloperReport"
```

---

### Task 4: LLM output parsing

**Files:**
- Create: `internal/orchestrator/parse.go`
- Create: `internal/orchestrator/parse_test.go`

**Interfaces:**
- Produces: `Verdict`, `VerdictPassed`, `VerdictFailed`, `VerdictMalformed`, `ExtractVerdict(s string) (Verdict, string)`, `ExtractDeveloperReport(s string) *DeveloperReport`, `ExtractPlanJSON(s string) (*Plan, error)` — consumed by Task 8

- [ ] **Step 1: Write the failing tests**

```go
// internal/orchestrator/parse_test.go
package orchestrator

import (
	"testing"
)

func TestExtractVerdict_passed(t *testing.T) {
	v, _ := ExtractVerdict("VALIDATION_PASSED")
	if v != VerdictPassed {
		t.Errorf("got %v, want VerdictPassed", v)
	}
}

func TestExtractVerdict_passedInProse(t *testing.T) {
	v, _ := ExtractVerdict("After careful review:\nVALIDATION_PASSED\nAll checks complete.")
	if v != VerdictPassed {
		t.Errorf("got %v, want VerdictPassed", v)
	}
}

func TestExtractVerdict_failed(t *testing.T) {
	report := "### Validation Failure Report\n- **Issue:** nil panic\n- **Target File:** `auth.go`"
	v, body := ExtractVerdict(report)
	if v != VerdictFailed {
		t.Errorf("got %v, want VerdictFailed", v)
	}
	if body == "" {
		t.Error("body should not be empty for failed verdict")
	}
}

func TestExtractVerdict_malformed(t *testing.T) {
	v, _ := ExtractVerdict("I looked at the code and it seems fine to me.")
	if v != VerdictMalformed {
		t.Errorf("got %v, want VerdictMalformed", v)
	}
}

func TestExtractDeveloperReport_withSentinel(t *testing.T) {
	raw := `I've made the changes.

### Developer Completion Report
- **Files Changed:** [auth.go, auth_test.go]
- **What Was Done:** Added nil guard
- **What Was NOT Done:** Did not add logging
- **Confidence:** high
- **Suggested Validator Focus:** line 42 nil dereference`

	dr := ExtractDeveloperReport(raw)
	if dr.Confidence != "high" {
		t.Errorf("confidence = %q, want high", dr.Confidence)
	}
	if dr.WhatWasDone != "Added nil guard" {
		t.Errorf("WhatWasDone = %q", dr.WhatWasDone)
	}
	if dr.SuggestedValidatorFocus != "line 42 nil dereference" {
		t.Errorf("SuggestedValidatorFocus = %q", dr.SuggestedValidatorFocus)
	}
}

func TestExtractDeveloperReport_noSentinel(t *testing.T) {
	dr := ExtractDeveloperReport("I changed some stuff.")
	if dr.Confidence != "low" {
		t.Errorf("no-sentinel confidence = %q, want low", dr.Confidence)
	}
}

func TestExtractPlanJSON_valid(t *testing.T) {
	raw := `{"intent":"feature","goal":"add auth","success_criteria":["works"],"verify_mode":"build_llm"}`
	p, err := ExtractPlanJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Intent != "feature" {
		t.Errorf("intent = %q", p.Intent)
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestExtract -v 2>&1 | head -10
```

- [ ] **Step 3: Implement `parse.go`**

```go
// internal/orchestrator/parse.go
package orchestrator

import (
	"strings"
)

// Verdict is the result of parsing a validator agent response.
type Verdict int

const (
	VerdictPassed    Verdict = iota // response contains "VALIDATION_PASSED"
	VerdictFailed                   // response contains "### Validation Failure Report"
	VerdictMalformed                // neither sentinel found
)

// ExtractVerdict searches s for the validator output sentinels.
// Returns the verdict and, for VerdictFailed, the failure report body
// (from the sentinel to end of string).
func ExtractVerdict(s string) (Verdict, string) {
	if strings.Contains(s, "VALIDATION_PASSED") {
		return VerdictPassed, ""
	}
	const failSentinel = "### Validation Failure Report"
	if i := strings.Index(s, failSentinel); i >= 0 {
		return VerdictFailed, s[i:]
	}
	return VerdictMalformed, s
}

// ExtractDeveloperReport parses the developer agent's completion report.
// If the sentinel is absent, returns a low-confidence stub (pipeline continues).
func ExtractDeveloperReport(s string) *DeveloperReport {
	const sentinel = "### Developer Completion Report"
	dr := &DeveloperReport{Confidence: "low"}
	i := strings.Index(s, sentinel)
	if i < 0 {
		return dr
	}
	body := s[i+len(sentinel):]
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := extractField(line, "**Files Changed:**"); ok {
			dr.FilesChanged = parseFileList(v)
		} else if v, ok := extractField(line, "**What Was Done:**"); ok {
			dr.WhatWasDone = v
		} else if v, ok := extractField(line, "**What Was NOT Done:**"); ok {
			dr.WhatWasNotDone = v
		} else if v, ok := extractField(line, "**Confidence:**"); ok {
			dr.Confidence = strings.TrimSpace(v)
		} else if v, ok := extractField(line, "**Suggested Validator Focus:**"); ok {
			dr.SuggestedValidatorFocus = v
		}
	}
	return dr
}

// ExtractPlanJSON delegates to ParsePlan — convenience wrapper used by the pipeline.
func ExtractPlanJSON(s string) (*Plan, error) {
	return ParsePlan(s)
}

func extractField(line, key string) (string, bool) {
	prefix := "- " + key + " "
	if strings.HasPrefix(line, prefix) {
		return strings.TrimSpace(line[len(prefix):]), true
	}
	prefix2 := "- " + key
	if strings.HasPrefix(line, prefix2) {
		return strings.TrimSpace(line[len(prefix2):]), true
	}
	return "", false
}

func parseFileList(s string) []string {
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestExtract -v
```

- [ ] **Step 5: Commit**

```bash
cd /Users/james/www/ocode && git add internal/orchestrator/parse.go internal/orchestrator/parse_test.go
git commit -m "feat(orchestrator): LLM output extraction with sentinel matching"
```

---

### Task 5: `BackoffPolicy`

**Files:**
- Create: `internal/orchestrator/backoff.go`
- Create: `internal/orchestrator/backoff_test.go`

**Interfaces:**
- Produces: `BackoffPolicy`, `DefaultBackoff`, `(BackoffPolicy).Delay(attempt int, seed float64) time.Duration` — consumed by Task 8

- [ ] **Step 1: Write the failing tests**

```go
// internal/orchestrator/backoff_test.go
package orchestrator

import (
	"testing"
	"time"
)

func TestBackoffDelay_firstAttempt(t *testing.T) {
	b := DefaultBackoff
	d := b.Delay(0, 0.5) // seed 0.5 = no jitter (middle)
	// 20s * 2^0 * (1 + 0.3*(0.5*2-1)) = 20s * 1 * 1.0 = 20s
	if d < 18*time.Second || d > 22*time.Second {
		t.Errorf("first attempt delay = %v, want ~20s", d)
	}
}

func TestBackoffDelay_clampsToMax(t *testing.T) {
	b := DefaultBackoff
	d := b.Delay(10, 1.0) // very high attempt — should clamp to MaxDelay
	if d != b.MaxDelay {
		t.Errorf("delay = %v, want %v (MaxDelay)", d, b.MaxDelay)
	}
}

func TestBackoffDelay_jitterRange(t *testing.T) {
	b := DefaultBackoff
	low := b.Delay(0, 0.0)
	high := b.Delay(0, 1.0)
	if low >= high {
		t.Errorf("jitter not applied: low=%v high=%v", low, high)
	}
	// Range: 20s * (1 ± 0.3) = [14s, 26s]
	if low < 13*time.Second || high > 27*time.Second {
		t.Errorf("jitter out of expected range: low=%v high=%v", low, high)
	}
}

func TestBackoffPolicy_maxAttempts(t *testing.T) {
	b := DefaultBackoff
	if b.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", b.MaxAttempts)
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestBackoff -v 2>&1 | head -10
```

- [ ] **Step 3: Implement `backoff.go`**

```go
// internal/orchestrator/backoff.go
package orchestrator

import (
	"math"
	"time"
)

// BackoffPolicy defines exponential backoff with jitter for compile retries.
// Used only in --no-worktree mode where another agent may transiently break the build.
type BackoffPolicy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
	JitterFactor float64
}

// DefaultBackoff is the policy applied in --no-worktree mode.
var DefaultBackoff = BackoffPolicy{
	InitialDelay: 20 * time.Second,
	MaxDelay:     120 * time.Second,
	MaxAttempts:  5,
	JitterFactor: 0.3,
}

// Delay returns the sleep duration for the given attempt (0-indexed).
// seed must be in [0,1] — callers provide math/rand.Float64() or a fixed value
// for tests. Delay never sleeps itself so callers control timing.
func (b BackoffPolicy) Delay(attempt int, seed float64) time.Duration {
	base := float64(b.InitialDelay) * math.Pow(2, float64(attempt))
	jitter := 1 + b.JitterFactor*(seed*2-1)
	d := time.Duration(base * jitter)
	if d > b.MaxDelay {
		return b.MaxDelay
	}
	return d
}
```

- [ ] **Step 4: Run tests — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestBackoff -v
```

- [ ] **Step 5: Commit**

```bash
cd /Users/james/www/ocode && git add internal/orchestrator/backoff.go internal/orchestrator/backoff_test.go
git commit -m "feat(orchestrator): BackoffPolicy with deterministic Delay()"
```

---

### Task 6: Worktree management

**Files:**
- Create: `internal/orchestrator/worktree.go`
- Create: `internal/orchestrator/worktree_test.go`

**Interfaces:**
- Produces: `WorktreeManager`, `NewWorktreeManager(repoRoot string) *WorktreeManager`, `(*WorktreeManager).Setup(runID string) (path string, err error)`, `(*WorktreeManager).Teardown(preserve bool) error`, `(*WorktreeManager).Path() string` — consumed by Task 8

- [ ] **Step 1: Write the failing tests**

```go
// internal/orchestrator/worktree_test.go
package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

// These are integration tests — they run real git commands.
// Skip if not in a git repo.
func repoRoot(t *testing.T) string {
	t.Helper()
	// Walk up from the test file's location to find .git
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not in a git repo")
		}
		dir = parent
	}
}

func TestWorktreeSetupTeardown(t *testing.T) {
	root := repoRoot(t)
	wm := NewWorktreeManager(root)
	path, err := wm.Setup("test-run-001")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if path == "" {
		t.Fatal("Setup returned empty path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree path does not exist: %v", err)
	}
	if err := wm.Teardown(false); err != nil {
		t.Fatalf("Teardown failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree should be removed after Teardown(preserve=false)")
	}
}

func TestWorktreePreserveOnHalt(t *testing.T) {
	root := repoRoot(t)
	wm := NewWorktreeManager(root)
	path, err := wm.Setup("test-run-002")
	if err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	t.Cleanup(func() { wm.Teardown(false) }) // always clean up
	if err := wm.Teardown(true); err != nil {
		t.Fatalf("Teardown(preserve=true) failed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("worktree should be preserved: %v", err)
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestWorktree -v 2>&1 | head -10
```

- [ ] **Step 3: Implement `worktree.go`**

```go
// internal/orchestrator/worktree.go
package orchestrator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// WorktreeManager creates and destroys a dedicated git worktree for the pipeline run.
// In worktree mode, all developer writes and compile commands execute inside the
// worktree so other concurrent agents cannot corrupt the pipeline's build state.
type WorktreeManager struct {
	repoRoot string
	path     string // set after Setup
}

// NewWorktreeManager returns a manager rooted at the given git repository root.
func NewWorktreeManager(repoRoot string) *WorktreeManager {
	return &WorktreeManager{repoRoot: repoRoot}
}

// Path returns the worktree path, or "" if Setup has not been called.
func (w *WorktreeManager) Path() string { return w.path }

// Setup creates a new git worktree at .worktrees/orchestrator-<runID>/.
// Returns the absolute path to the worktree.
func (w *WorktreeManager) Setup(runID string) (string, error) {
	if w.path != "" {
		return "", fmt.Errorf("worktree already set up at %s", w.path)
	}
	dest := filepath.Join(w.repoRoot, ".worktrees", "orchestrator-"+runID)
	cmd := exec.Command("git", "worktree", "add", "--detach", dest, "HEAD")
	cmd.Dir = w.repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %w\n%s", err, out)
	}
	w.path = dest
	return dest, nil
}

// Teardown removes the worktree. When preserve is true (HALTED state), the
// worktree directory is left in place so the user can inspect the work.
func (w *WorktreeManager) Teardown(preserve bool) error {
	if w.path == "" {
		return nil
	}
	if !preserve {
		cmd := exec.Command("git", "worktree", "remove", "--force", w.path)
		cmd.Dir = w.repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			// Best-effort remove the directory if git command fails
			_ = os.RemoveAll(w.path)
			return fmt.Errorf("git worktree remove failed: %w\n%s", err, out)
		}
	}
	w.path = ""
	return nil
}
```

- [ ] **Step 4: Run tests — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestWorktree -v
```

- [ ] **Step 5: Commit**

```bash
cd /Users/james/www/ocode && git add internal/orchestrator/worktree.go internal/orchestrator/worktree_test.go
git commit -m "feat(orchestrator): WorktreeManager for isolated build environment"
```

---

### Task 7: `DispatchSubagent` on `*Agent`

**Files:**
- Modify: `internal/agent/agent.go` (add method)
- Create: `internal/agent/dispatch_test.go`

**Interfaces:**
- Produces: `(*Agent).DispatchSubagent(agentName, prompt string) (string, error)` — consumed by Tasks 8, 9

- [ ] **Step 1: Write the failing test**

```go
// internal/agent/dispatch_test.go
package agent

import (
	"encoding/json"
	"testing"
)

func TestDispatchSubagent_callsTaskTool(t *testing.T) {
	// DispatchSubagent must be defined on *Agent
	// This test verifies the method signature compiles.
	// Functional dispatch testing requires a live client — see integration tests.
	var a *Agent
	_ = func() {
		// Should compile:
		_, _ = a.DispatchSubagent("explore", "find auth files")
	}
	// Verify JSON marshalling of the args matches what TaskTool.Execute expects
	args, err := json.Marshal(map[string]any{
		"agent":  "explore",
		"prompt": "find auth files",
	})
	if err != nil {
		t.Fatalf("failed to marshal dispatch args: %v", err)
	}
	var params struct {
		Prompt string `json:"prompt"`
		Agent  string `json:"agent"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if params.Agent != "explore" || params.Prompt != "find auth files" {
		t.Errorf("unexpected params: %+v", params)
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/... -run TestDispatchSubagent -v 2>&1 | head -10
```

- [ ] **Step 3: Add `DispatchSubagent` to `agent.go`**

Find the end of the `Agent` method block in `internal/agent/agent.go` (after the last method, before the package-level functions). Add:

```go
// DispatchSubagent runs the named subagent synchronously with the given prompt
// and returns its final text output. It uses the agent's configured task tool
// so all existing subagent wiring (permissions, small model, activity tracking)
// is applied automatically.
func (a *Agent) DispatchSubagent(agentName, prompt string) (string, error) {
	t, ok := a.tools["task"].(*TaskTool)
	if !ok {
		return "", fmt.Errorf("orchestrator: task tool not available on agent")
	}
	args, err := json.Marshal(map[string]any{
		"agent":  agentName,
		"prompt": prompt,
	})
	if err != nil {
		return "", fmt.Errorf("orchestrator: failed to marshal dispatch args: %w", err)
	}
	return t.Execute(args)
}
```

Ensure `encoding/json` is already imported in `agent.go` (it is — check the import block).

- [ ] **Step 4: Run test — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/... -run TestDispatchSubagent -v
```

- [ ] **Step 5: Run full agent tests — verify no regression**

```bash
cd /Users/james/www/ocode && go test ./internal/agent/... 2>&1 | tail -5
```
Expected: `ok github.com/u007/ocode/internal/agent`

- [ ] **Step 6: Commit**

```bash
cd /Users/james/www/ocode && git add internal/agent/agent.go internal/agent/dispatch_test.go
git commit -m "feat(agent): DispatchSubagent for programmatic subagent dispatch"
```

---

### Task 8: Pipeline state machine

**Files:**
- Create: `internal/orchestrator/states.go`
- Create: `internal/orchestrator/pipeline.go`
- Create: `internal/orchestrator/pipeline_test.go`

**Interfaces:**
- Consumes: `Plan` (T1), `ContextDoc` (T2), `StructuredReport`/`DeveloperReport` (T3), `ExtractVerdict`/`ExtractDeveloperReport`/`ExtractPlanJSON` (T4), `BackoffPolicy` (T5), `WorktreeManager` (T6), `(*Agent).DispatchSubagent` (T7)
- Produces: `PipelineOptions`, `Pipeline`, `New(parent *agent.Agent, opts PipelineOptions) *Pipeline`, `(*Pipeline).Run(ctx context.Context, goal string) (*StructuredReport, error)` — consumed by Task 9, Plan B

- [ ] **Step 1: Write `states.go`**

```go
// internal/orchestrator/states.go
package orchestrator

// state is the current step of the pipeline state machine.
type state string

const (
	statePlanning   state = "Planning"
	stateExploring  state = "Exploring"
	stateDeveloping state = "Developing"
	stateCompiling  state = "Compiling"
	stateValidating state = "Validating"
	stateAdvising   state = "Advising"
	stateDone       state = "Done"
)
```

- [ ] **Step 2: Write the failing pipeline tests**

```go
// internal/orchestrator/pipeline_test.go
package orchestrator

import (
	"context"
	"testing"
)

// stubDispatcher replaces (*agent.Agent).DispatchSubagent for pipeline unit tests.
type stubDispatcher struct {
	responses map[string]string // agentName -> response
	calls     []stubCall
}
type stubCall struct{ agent, prompt string }

func (s *stubDispatcher) dispatch(agentName, prompt string) (string, error) {
	s.calls = append(s.calls, stubCall{agentName, prompt})
	if r, ok := s.responses[agentName]; ok {
		return r, nil
	}
	return "", nil
}

// newTestPipeline creates a Pipeline with a stub dispatcher.
// Lives in the test file because stubDispatcher is test-only — production
// pipeline.go must not reference types defined in _test.go files.
func newTestPipeline(stub *stubDispatcher) *Pipeline {
	p := &Pipeline{
		opts:  PipelineOptions{WorkDir: "."},
		runID: "test-run",
	}
	p.dispatchFn = stub.dispatch
	return p
}

func TestPipeline_successOnFirstIteration(t *testing.T) {
	stub := &stubDispatcher{
		responses: map[string]string{
			"orchestrator-planner":   `{"intent":"bugfix","goal":"fix nil panic","success_criteria":["no panic"],"verify_mode":"llm_only","max_iterations":4}`,
			"orchestrator-explorer":  "## auth.go\nfunc Validate() {}",
			"orchestrator-developer": "### Developer Completion Report\n- **Files Changed:** [auth.go]\n- **What Was Done:** Added nil check\n- **What Was NOT Done:** nothing\n- **Confidence:** high\n- **Suggested Validator Focus:** line 12",
			"orchestrator-validator": "VALIDATION_PASSED",
		},
	}
	p := newTestPipeline(stub)
	report, err := p.Run(context.Background(), "fix nil panic")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !report.Passed {
		t.Errorf("expected Passed, got HALTED. Validator report: %s", report.FinalValidatorReport)
	}
	if report.TotalIterations != 1 {
		t.Errorf("TotalIterations = %d, want 1", report.TotalIterations)
	}
}

func TestPipeline_retryOnValidationFail(t *testing.T) {
	callCount := 0
	stub := &stubDispatcher{
		responses: map[string]string{
			"orchestrator-planner":  `{"intent":"feature","goal":"add auth","success_criteria":["works"],"verify_mode":"llm_only","max_iterations":4}`,
			"orchestrator-explorer": "snapshot",
		},
	}
	stub.responses["orchestrator-developer"] = "### Developer Completion Report\n- **Files Changed:** [auth.go]\n- **What Was Done:** done\n- **What Was NOT Done:** -\n- **Confidence:** high\n- **Suggested Validator Focus:** -"
	// Validator fails first time, passes second
	validatorResponses := []string{
		"### Validation Failure Report\n- **Issue:** nil panic\n- **Target File:** `auth.go`",
		"VALIDATION_PASSED",
	}
	originalDispatch := stub.dispatch
	stub.responses["orchestrator-validator"] = "" // override per-call below
	p := newTestPipeline(stub)
	p.dispatchFn = func(agentName, prompt string) (string, error) {
		if agentName == "orchestrator-validator" {
			r := validatorResponses[callCount]
			callCount++
			return r, nil
		}
		return originalDispatch(agentName, prompt)
	}
	report, err := p.Run(context.Background(), "add auth")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !report.Passed {
		t.Errorf("expected Passed after retry")
	}
	if report.TotalIterations != 2 {
		t.Errorf("TotalIterations = %d, want 2", report.TotalIterations)
	}
}

func TestPipeline_haltAfterMaxIterations(t *testing.T) {
	stub := &stubDispatcher{
		responses: map[string]string{
			"orchestrator-planner":   `{"intent":"feature","goal":"g","success_criteria":["x"],"verify_mode":"llm_only","max_iterations":2}`,
			"orchestrator-explorer":  "snap",
			"orchestrator-developer": "### Developer Completion Report\n- **Files Changed:** [f.go]\n- **What Was Done:** done\n- **What Was NOT Done:** -\n- **Confidence:** low\n- **Suggested Validator Focus:** -",
			"orchestrator-validator": "### Validation Failure Report\n- **Issue:** still broken",
			"orchestrator-advisor":   "Try fixing X differently",
		},
	}
	p := newTestPipeline(stub)
	report, err := p.Run(context.Background(), "g")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.Passed {
		t.Error("expected HALTED")
	}
	if !report.AdvisorConsulted {
		t.Error("expected advisor to be consulted")
	}
}
```

- [ ] **Step 3: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestPipeline -v 2>&1 | head -15
```

- [ ] **Step 4: Implement `pipeline.go`**

```go
// internal/orchestrator/pipeline.go
package orchestrator

import (
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// PipelineOptions configures a Pipeline run. All fields are optional.
type PipelineOptions struct {
	VerifyMode    string                 // overrides Plan.VerifyMode if non-empty
	MaxIterations int                    // overrides Plan.MaxIterations if > 0
	UseWorktree   bool                   // default true — set false for --no-worktree mode
	WorkDir       string                 // repo root; defaults to "."
	Backoff       BackoffPolicy          // used only when UseWorktree=false
	StatusFunc    func(s state, msg string) // called on each state transition (may be nil)
}

// Pipeline is the orchestration engine. Create with New(), run with Run().
type Pipeline struct {
	parent     *agent.Agent
	opts       PipelineOptions
	dispatchFn func(agentName, prompt string) (string, error) // swappable for tests
	doc        *ContextDoc
	iterCount  int
	runID      string
}

// New creates a Pipeline ready to run. parent must have its task tool configured.
func New(parent *agent.Agent, opts PipelineOptions) *Pipeline {
	if opts.WorkDir == "" {
		opts.WorkDir = "."
	}
	if opts.Backoff.MaxAttempts == 0 {
		opts.Backoff = DefaultBackoff
	}
	p := &Pipeline{parent: parent, opts: opts}
	p.dispatchFn = parent.DispatchSubagent
	p.runID = fmt.Sprintf("%d", time.Now().UnixNano())
	return p
}

// Run executes the pipeline for the given goal and returns a StructuredReport.
// The context controls cancellation.
func (p *Pipeline) Run(ctx context.Context, goal string) (*StructuredReport, error) {
	p.doc = &ContextDoc{}
	p.iterCount = 0

	// Planning
	p.emit(statePlanning, "analysing goal")
	planRaw, err := p.dispatchFn("orchestrator-planner", goal)
	if err != nil {
		return nil, fmt.Errorf("planning failed: %w", err)
	}
	plan, err := ExtractPlanJSON(planRaw)
	if err != nil {
		return nil, fmt.Errorf("planning failed: %w", err)
	}
	// Apply option overrides
	if p.opts.VerifyMode != "" {
		plan.VerifyMode = p.opts.VerifyMode
	}
	if p.opts.MaxIterations > 0 {
		plan.MaxIterations = p.opts.MaxIterations
	}
	p.doc.Plan = *plan

	// Worktree setup
	var wm *WorktreeManager
	if p.opts.UseWorktree {
		wm = NewWorktreeManager(p.opts.WorkDir)
		if _, err := wm.Setup(p.runID); err != nil {
			return nil, fmt.Errorf("worktree setup failed: %w", err)
		}
	}

	// Exploring (iteration 0)
	p.emit(stateExploring, "gathering codebase context")
	explorePrompt := fmt.Sprintf("Goal: %s\n\nGather codebase context for a developer who will implement this.", goal)
	if len(p.doc.ReExploreHints) > 0 {
		explorePrompt += "\n\nRe-explore hints (files missing from prior snapshot):\n- " + strings.Join(p.doc.ReExploreHints, "\n- ")
	}
	snapshot, err := p.dispatchFn("orchestrator-explorer", explorePrompt)
	if err != nil {
		return nil, fmt.Errorf("exploring failed: %w", err)
	}
	p.doc.ExploreSnapshot = snapshot

	// Main loop
	advisorConsulted := false
	postAdvisorAttempt := false
	var lastValidatorReport string
	var lastFilesChanged []FileDiff

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Circuit breaker
		if p.iterCount >= plan.MaxIterations {
			if postAdvisorAttempt {
				// Already tried post-advisor — halt
				break
			}
			// Escalate to advisor
			p.emit(stateAdvising, fmt.Sprintf("escalating after %d iterations", p.iterCount))
			advisorNote, err := p.escalateToAdvisor(ctx, lastValidatorReport)
			if err != nil {
				advisorNote = fmt.Sprintf("advisor unavailable: %v", err)
			}
			advisorConsulted = true
			postAdvisorAttempt = true
			// Add advisor note to next iteration context
			if p.iterCount > 0 {
				p.doc.Iterations[len(p.doc.Iterations)-1].AdvisorNote = advisorNote
			}
		}

		// Developing
		p.iterCount++
		p.emit(stateDeveloping, fmt.Sprintf("iteration %d", p.iterCount))
		brief := p.buildDeveloperBrief(lastValidatorReport)
		devPrompt := p.doc.Render(brief)
		devOut, err := p.dispatchFn("orchestrator-developer", devPrompt)
		if err != nil {
			return nil, fmt.Errorf("developer dispatch failed: %w", err)
		}
		devReport := ExtractDeveloperReport(devOut)

		// Build current iteration record
		iter := Iteration{
			Number:         p.iterCount,
			DeveloperBrief: brief,
		}

		// Compiling
		compileOut, compileErr := p.runCompile(ctx, plan.VerifyMode, wm)
		iter.CompilerOutput = compileOut
		if compileErr != nil {
			// Compile failed — add iteration and loop back to developer
			p.doc.AddIteration(iter)
			lastValidatorReport = ""
			lastFilesChanged = nil
			continue
		}

		// Validating
		p.emit(stateValidating, fmt.Sprintf("iteration %d", p.iterCount))
		valPrompt := p.buildValidatorPrompt(devReport)
		valOut, err := p.dispatchFn("orchestrator-validator", valPrompt)
		// Re-ask on malformed output (once)
		if err == nil {
			verdict, body := ExtractVerdict(valOut)
			if verdict == VerdictMalformed {
				valOut, err = p.dispatchFn("orchestrator-validator",
					"Your previous response did not contain a verdict. Output only VALIDATION_PASSED or a Validation Failure Report.")
				if err == nil {
					verdict, body = ExtractVerdict(valOut)
					if verdict == VerdictMalformed {
						body = "### Validation Failure Report\n- **Issue:** validator produced unreadable output — human review required"
						verdict = VerdictFailed
					}
				}
			}
			iter.ValidatorReport = body

			// Check for context gap hints
			if verdict == VerdictFailed {
				if gap := extractContextGap(body); gap != "" {
					p.doc.ReExploreHints = append(p.doc.ReExploreHints, gap)
					// Re-explore before next developer dispatch
					snapshot, _ := p.dispatchFn("orchestrator-explorer", fmt.Sprintf(
						"Re-explore these specific files missing from prior snapshot:\n- %s",
						strings.Join(p.doc.ReExploreHints, "\n- ")))
					if snapshot != "" {
						p.doc.ExploreSnapshot += "\n\n## Re-explored\n" + snapshot
						p.doc.ReExploreHints = nil
					}
				}
			}

			p.doc.AddIteration(iter)
			lastValidatorReport = body
			lastFilesChanged = fileChangesFromReport(devReport)

			if verdict == VerdictPassed {
				p.emit(stateDone, "validation passed")
				if wm != nil {
					_ = wm.Teardown(false)
				}
				return &StructuredReport{
					Goal:             goal,
					TotalIterations:  p.iterCount,
					AdvisorConsulted: advisorConsulted,
					Passed:           true,
				}, nil
			}
		} else {
			p.doc.AddIteration(iter)
		}
	}

	// HALTED
	p.emit(stateDone, "halted")
	if wm != nil {
		_ = wm.Teardown(true) // preserve worktree on halt
	}
	return &StructuredReport{
		Goal:                goal,
		TotalIterations:     p.iterCount,
		AdvisorConsulted:    advisorConsulted,
		Passed:              false,
		FinalValidatorReport: lastValidatorReport,
		FilesChanged:        lastFilesChanged,
	}, nil
}

func (p *Pipeline) emit(s state, msg string) {
	if p.opts.StatusFunc != nil {
		p.opts.StatusFunc(s, msg)
	}
}

func (p *Pipeline) buildDeveloperBrief(lastValidatorReport string) string {
	if lastValidatorReport == "" {
		return "Implement the goal described above. Use the codebase context provided."
	}
	return fmt.Sprintf("The previous attempt was rejected. Fix the following issues:\n\n%s", lastValidatorReport)
}

func (p *Pipeline) buildValidatorPrompt(dr *DeveloperReport) string {
	var b strings.Builder
	b.WriteString(p.doc.Render(""))
	if dr.SuggestedValidatorFocus != "" {
		b.WriteString("\n\n[DEVELOPER FOCUS HINT]\n")
		b.WriteString(dr.SuggestedValidatorFocus)
	}
	return b.String()
}

func (p *Pipeline) runCompile(ctx context.Context, verifyMode string, wm *WorktreeManager) (string, error) {
	workDir := p.opts.WorkDir
	if wm != nil && wm.Path() != "" {
		workDir = wm.Path()
	}

	cmds := [][]string{{"go", "build", "./..."}, {"go", "vet", "./..."}}
	if verifyMode == "build_test_llm" {
		cmds = append(cmds, []string{"go", "test", "./..."})
	}

	if p.opts.UseWorktree || wm != nil {
		// Worktree mode: deterministic, no backoff
		return runCommands(ctx, workDir, cmds)
	}

	// No-worktree mode: retry with backoff
	for attempt := 0; attempt < p.opts.Backoff.MaxAttempts; attempt++ {
		out, err := runCommands(ctx, workDir, cmds)
		if err == nil {
			return out, nil
		}
		if attempt+1 < p.opts.Backoff.MaxAttempts {
			p.emit(stateCompiling, fmt.Sprintf("compile failed (attempt %d/%d), retrying after backoff", attempt+1, p.opts.Backoff.MaxAttempts))
			delay := p.opts.Backoff.Delay(attempt, rand.Float64())
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return out, ctx.Err()
			}
		} else {
			return out, err
		}
	}
	return "", fmt.Errorf("unreachable")
}

func runCommands(ctx context.Context, dir string, cmds [][]string) (string, error) {
	var out strings.Builder
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = dir
		b, err := cmd.CombinedOutput()
		out.Write(b)
		if err != nil {
			return out.String(), fmt.Errorf("%s failed: %w", args[0], err)
		}
	}
	return out.String(), nil
}

func extractContextGap(validatorReport string) string {
	for _, line := range strings.Split(validatorReport, "\n") {
		if v, ok := extractField(strings.TrimSpace(line), "**Context Gap:**"); ok && v != "" {
			return v
		}
	}
	return ""
}

func fileChangesFromReport(dr *DeveloperReport) []FileDiff {
	diffs := make([]FileDiff, len(dr.FilesChanged))
	for i, f := range dr.FilesChanged {
		diffs[i] = FileDiff{Path: f, Summary: dr.WhatWasDone}
	}
	return diffs
}
```

- [ ] **Step 5: Run pipeline tests — verify pass**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestPipeline -v
```

- [ ] **Step 6: Run `go vet`**

```bash
cd /Users/james/www/ocode && go vet ./internal/orchestrator/...
```
Expected: no output (no errors)

- [ ] **Step 7: Commit**

```bash
cd /Users/james/www/ocode && git add internal/orchestrator/states.go internal/orchestrator/pipeline.go internal/orchestrator/pipeline_test.go
git commit -m "feat(orchestrator): Pipeline state machine with compile/validate loop"
```

---

### Task 9: Advisor escalation

**Files:**
- Create: `internal/orchestrator/advisor.go`
- Create: `internal/orchestrator/advisor_test.go`

**Interfaces:**
- Consumes: `ContextDoc` (T2), `dispatchFn` from `Pipeline` (T8)
- Produces: `(*Pipeline).escalateToAdvisor(ctx context.Context, lastReport string) (string, error)` — used internally by `Run()`

- [ ] **Step 1: Write the failing test**

```go
// internal/orchestrator/advisor_test.go
package orchestrator

import (
	"context"
	"strings"
	"testing"
)

func TestEscalateToAdvisor_promptContainsContext(t *testing.T) {
	var capturedPrompt string
	stub := &stubDispatcher{responses: map[string]string{}}
	p := newTestPipeline(stub)
	p.doc = &ContextDoc{
		Plan: Plan{Goal: "add validation", SuccessCriteria: []string{"nil safe"}},
		ExploreSnapshot: "## auth.go",
	}
	p.doc.AddIteration(Iteration{
		Number:          1,
		ValidatorReport: "### Validation Failure Report\n- **Issue:** nil panic",
	})
	p.dispatchFn = func(agentName, prompt string) (string, error) {
		capturedPrompt = prompt
		return "Try adding a nil guard at the entry point", nil
	}
	note, err := p.escalateToAdvisor(context.Background(), "### Validation Failure Report\n- **Issue:** nil panic")
	if err != nil {
		t.Fatalf("escalate failed: %v", err)
	}
	if !strings.Contains(capturedPrompt, "add validation") {
		t.Error("advisor prompt missing goal")
	}
	if !strings.Contains(capturedPrompt, "nil panic") {
		t.Error("advisor prompt missing validator report")
	}
	if !strings.Contains(capturedPrompt, "1 times") {
		t.Error("advisor prompt missing iteration count")
	}
	if note == "" {
		t.Error("advisor note should not be empty")
	}
}
```

- [ ] **Step 2: Run — verify fail**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -run TestEscalate -v 2>&1 | head -10
```

- [ ] **Step 3: Implement `advisor.go`**

```go
// internal/orchestrator/advisor.go
package orchestrator

import (
	"context"
	"fmt"
)

// escalateToAdvisor sends the full pipeline context to the advisor agent and
// returns its resolution note. The advisor is dispatched as a subagent named
// "orchestrator-advisor" — this name does not need a markdown file; the prompt
// below is passed directly as the user message so the subagent uses its system
// prompt (general) plus the specific advisor framing below.
//
// In practice the advisor subagent will use the parent agent's advisor tool
// (if advisor is enabled). Using "orchestrator-advisor" as the agent name
// with the prompt below is the correct pattern — the pipeline passes the full
// context in the prompt, not as a system prompt override.
func (p *Pipeline) escalateToAdvisor(ctx context.Context, lastReport string) (string, error) {
	prompt := fmt.Sprintf(`You are reviewing a failed multi-agent coding pipeline.
The developer has attempted %d times without satisfying the validator.

%s

The final validator report was:
%s

Diagnose WHY convergence failed. Is the goal underspecified? Is the validator
applying an unreasonable standard? Is there a missing dependency or architectural
constraint the developer was not told about? Provide one specific, actionable
resolution strategy the developer can act on in a single attempt.`,
		p.iterCount,
		p.doc.Render(""),
		lastReport,
	)

	note, err := p.dispatchFn("general", prompt)
	if err != nil {
		return "", fmt.Errorf("advisor dispatch failed: %w", err)
	}
	return note, nil
}
```

- [ ] **Step 4: Run test — verify pass**

```bash
cd /Uses/james/www/ocode && go test ./internal/orchestrator/... -run TestEscalate -v
```

- [ ] **Step 5: Run all orchestrator tests**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... -v 2>&1 | tail -20
```
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/james/www/ocode && git add internal/orchestrator/advisor.go internal/orchestrator/advisor_test.go
git commit -m "feat(orchestrator): advisor escalation as one-shot circuit breaker"
```

---

### Task 10: Agent files and small model registration

**Files:**
- Create: `.opencode/agents/orchestrator-planner.md`
- Create: `.opencode/agents/orchestrator-explorer.md`
- Create: `.opencode/agents/orchestrator-developer.md`
- Create: `.opencode/agents/orchestrator-validator.md`
- Modify: `internal/agent/small_model.go`

**Interfaces:**
- Produces: four registered subagents accessible to `DispatchSubagent()` by name

- [ ] **Step 1: Create `.opencode/agents/` directory**

```bash
mkdir -p /Users/james/www/ocode/.opencode/agents
```

- [ ] **Step 2: Create `orchestrator-planner.md`**

```markdown
---
name: orchestrator-planner
description: Task planner for the orchestrator pipeline
mode: subagent
hidden: true
max_steps: 10
permission:
  read: allow
  write: deny
  execute: deny
---

You are the planner agent in an automated coding pipeline. Your job is to
analyse the user's goal and produce a structured plan that the pipeline will
execute.

You will receive the user's goal. You may read files to understand the
codebase well enough to classify the task, but keep exploration minimal —
the explorer agent handles deep context gathering.

Classify the task:
- "feature" — new behaviour, new API, new capability
- "bugfix" — correcting broken or incorrect existing behaviour

Select verify mode:
- "llm_only" — tiny change, no public interface touched
- "build_llm" — bugfix, internal change
- "build_test_llm" — new feature, public API change, any data-path change

Write 3–5 success criteria: specific, testable conditions the validator can
check. Do not list file names — the developer and explorer determine those.

Output exactly this JSON and nothing else:

{
  "intent": "feature" | "bugfix",
  "goal": "<user's original request verbatim>",
  "success_criteria": ["<criterion>", ...],
  "verify_mode": "llm_only" | "build_llm" | "build_test_llm",
  "max_iterations": 4
}
```

- [ ] **Step 3: Create `orchestrator-explorer.md`**

```markdown
---
name: orchestrator-explorer
description: Codebase context gatherer for the orchestrator pipeline
mode: subagent
hidden: true
max_steps: 20
permission:
  read: allow
  write: deny
  execute: deny
---

You are the explorer agent in an automated coding pipeline. Your job is to
gather codebase context for a developer who will implement a change.

You will receive a goal and optionally re-explore hints (specific files the
validator said were missing from a prior snapshot).

Your internal loop:
1. Glob broadly to map the relevant area
2. Grep for key symbols, types, and callsites
3. Read the smallest relevant excerpts — not whole files
4. Follow imports and references one level deep for key types
5. If re-explore hints are provided, read those files and merge into snapshot
6. Re-examine your snapshot: is there anything a developer touching this area
   MUST know that you have not captured yet?
7. Only return when your snapshot is complete

Output a single structured markdown snapshot: file paths, relevant excerpts
with line numbers, key types and interfaces, call relationships. No prose.
No suggestions. No fix proposals.
```

- [ ] **Step 4: Create `orchestrator-developer.md`**

```markdown
---
name: orchestrator-developer
description: Implementation agent for the orchestrator pipeline
mode: subagent
hidden: true
max_steps: 30
permission:
  read: allow
  write: allow
  execute: allow
---

You are the developer agent in an automated coding pipeline. You receive a
fully prepared context bundle — do not re-discover what is already there.

Your internal loop:
1. Read the ContextDoc — understand the goal, prior attempts, and what failed
2. Determine which files to change based on the goal and codebase context
3. Plan your changes before writing (one sentence per file)
4. Write the changes
5. Run: go build ./... and go vet ./... — fix any errors before continuing
6. Read back what you wrote — confirm edits landed correctly and completely
7. Self-review: missing imports? broken references? incomplete stubs? Fix them.
8. Re-examine your completion report — is your confidence honest?
9. Only return when you are satisfied the code compiles and is correct

Allowed shell commands: go build ./... and go vet ./... only.
Do not run tests, install tools, or touch the network.

Rules:
- Do not argue with validator reports. Treat them as ground truth.
- Do not repeat changes that already failed in a prior iteration.
- If you must change a file not obviously related to the goal, explain why.
- If you cannot implement something, say so explicitly — do not fake it.

Output exactly this format and nothing else after it:

### Developer Completion Report
- **Files Changed:** [list]
- **What Was Done:** [summary]
- **What Was NOT Done:** [anything deferred or out of scope]
- **Confidence:** high | medium | low
- **Suggested Validator Focus:** [where to look hardest for edge cases]
```

- [ ] **Step 5: Create `orchestrator-validator.md`**

```markdown
---
name: orchestrator-validator
description: Adversarial QA agent for the orchestrator pipeline
mode: subagent
hidden: true
max_steps: 20
permission:
  read: allow
  write: deny
  execute: deny
---

You are the validator agent in an automated coding pipeline. You are
adversarial by design. Your job is to find what is wrong, not to encourage.

You receive: the goal, success criteria, files changed this iteration, the
developer's suggested focus area, and full codebase context.

Your internal loop:
1. Read each changed file fully
2. Cross-reference against every success criterion — check each one explicitly
3. Chase imports, callers, and dependents of changed code — bugs hide there
4. Check the developer's suggested validator focus area
5. Generate a draft failure report
6. Re-examine your draft — are these issues real? Would they fail in production?
   Remove false positives. Add issues you missed.
7. Check: is there a file you need that is NOT in your context? If yes, read it
   now, update your report, and add it to Context Gap.
8. Only output your final verdict when you have exhausted your checks

Output rules — your response must contain EXACTLY ONE of the following.
No prose before or after. The pipeline extracts your verdict by substring match.

If everything passes:
VALIDATION_PASSED

If there are issues:
### Validation Failure Report
- **Issue:** [describe the bug]
- **Target File:** [`path/to/file.go`]
- **Target Line:** [line number if known]
- **Expected Behavior:** [what should happen]
- **Observed Risk:** [what can fail or go wrong]
- **Context Gap:** [optional — file path missing from explore snapshot]
```

- [ ] **Step 6: Register planner and explorer for small model**

In `internal/agent/small_model.go`, add the two new agents to `smallModelEligibleNames`:

```go
var smallModelEligibleNames = map[string]bool{
    "explore":                true,
    "general":                true,
    "compaction":             true,
    "orchestrator-planner":   true,
    "orchestrator-explorer":  true,
}
```

- [ ] **Step 7: Verify agents load**

```bash
cd /Users/james/www/ocode && go build ./... && echo "build ok"
```
Expected: `build ok`

- [ ] **Step 8: Run all tests**

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... ./internal/agent/... 2>&1 | tail -10
```
Expected: all PASS

- [ ] **Step 9: Commit**

```bash
cd /Users/james/www/ocode && git add .opencode/agents/ internal/agent/small_model.go
git commit -m "feat(orchestrator): agent markdown files and small model registration"
```

---

## Plan A Complete

Run the full test suite to confirm:

```bash
cd /Users/james/www/ocode && go test ./internal/orchestrator/... ./internal/agent/... -v 2>&1 | grep -E "^(ok|FAIL|---)"
```

**Plan B** (entry points: `/orchestrate` slash command, session intercept, CLI flag) is the next step and depends on `Pipeline.Run()` from this plan.
