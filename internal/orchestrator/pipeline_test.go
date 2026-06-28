// internal/orchestrator/pipeline_test.go
package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
			"orchestrator-planner":   "Fix the nil panic in auth.go.\n\nverify_mode: llm_only\nmax_iterations: 4",
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
			"orchestrator-planner":  "Add auth feature.\n\nverify_mode: llm_only\nmax_iterations: 4",
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
			"orchestrator-planner":   "Implement feature g.\n\nverify_mode: llm_only\nmax_iterations: 2",
			"orchestrator-explorer":  "snap",
			"orchestrator-developer": "### Developer Completion Report\n- **Files Changed:** [f.go]\n- **What Was Done:** done\n- **What Was NOT Done:** -\n- **Confidence:** low\n- **Suggested Validator Focus:** -",
			"orchestrator-validator": "### Validation Failure Report\n- **Issue:** still broken",
			"general":                "Try fixing X differently",
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
	if report.AdvisorNote == "" {
		t.Error("expected AdvisorNote to be populated when advisor was consulted (bug fix: was previously empty)")
	}
}

func TestPipeline_haltOnCompileFailureSurfacesReason(t *testing.T) {
	// Regression: the original pipeline produced a HALT report with NO
	// failure reason when the most recent iteration failed at compile, because
	// FinalValidatorReport was empty and the renderer had no compile-output
	// fallback. The user saw only "HALTED — Validation did not pass" with
	// no explanation. This test forces compile failure (via a temp dir
	// containing no Go module) and asserts that the report now surfaces
	// LastCompileOutput and the full iteration history.
	dir := t.TempDir()
	stub := &stubDispatcher{
		responses: map[string]string{
			"orchestrator-planner":   "Implement feature.\n\nverify_mode: llm_only\nmax_iterations: 1",
			"orchestrator-explorer":  "snap",
			"orchestrator-developer": "### Developer Completion Report\n- **Files Changed:** [f.go]\n- **What Was Done:** done\n- **What Was NOT Done:** -\n- **Confidence:** high\n- **Suggested Validator Focus:** -",
		},
	}
	p := &Pipeline{
		opts:  PipelineOptions{WorkDir: dir, MaxIterations: 1},
		runID: "test-run",
	}
	p.dispatchFn = stub.dispatch

	report, err := p.Run(context.Background(), "ship feature")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.Passed {
		t.Error("expected HALTED")
	}
	// The bug: FinalValidatorReport is empty (validator never ran), but the
	// HALT report must still carry the compile error so the user knows why.
	if report.FinalValidatorReport != "" {
		t.Errorf("FinalValidatorReport should be empty on a compile-fail halt, got %q", report.FinalValidatorReport)
	}
	if report.LastCompileOutput == "" {
		t.Error("LastCompileOutput must be populated on a compile-fail halt (silent-halt bug fix)")
	}
	if len(report.Iterations) == 0 {
		t.Error("Iterations must be populated on a HALT report (silent-halt bug fix)")
	}
	// The rendered markdown must contain a "Final Compile Output" section
	// and an "Iteration History" section so the user can see what went wrong.
	out := report.FormatMarkdown()
	if !strings.Contains(out, "Final Compile Output") {
		t.Errorf("rendered HALT markdown missing Final Compile Output section:\n%s", out)
	}
	if !strings.Contains(out, "Iteration History") {
		t.Errorf("rendered HALT markdown missing Iteration History section:\n%s", out)
	}
	if !strings.Contains(out, "compile-fail") {
		t.Errorf("rendered iteration history missing compile-fail status:\n%s", out)
	}
}

func TestPipeline_clearsStaleCompileOutputAfterLaterValidationFail(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\tmissing()\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stub := &stubDispatcher{
		responses: map[string]string{
			"orchestrator-planner":   "Implement feature.\n\nverify_mode: llm_only\nmax_iterations: 1",
			"orchestrator-explorer":  "snap",
			"orchestrator-developer": "### Developer Completion Report\n- **Files Changed:** [main.go]\n- **What Was Done:** done\n- **What Was NOT Done:** -\n- **Confidence:** high\n- **Suggested Validator Focus:** -",
			"orchestrator-validator": "### Validation Failure Report\n- **Issue:** still broken",
			"general":                "Try a different fix",
		},
	}
	p := &Pipeline{
		opts:  PipelineOptions{WorkDir: dir},
		runID: "test-run",
	}
	devCalls := 0
	originalDispatch := stub.dispatch
	p.dispatchFn = func(agentName, prompt string) (string, error) {
		if agentName == "orchestrator-developer" {
			devCalls++
			if devCalls == 2 {
				if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
					return "", err
				}
			}
		}
		return originalDispatch(agentName, prompt)
	}

	report, err := p.Run(context.Background(), "ship feature")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.Passed {
		t.Fatal("expected HALTED")
	}
	if !report.AdvisorConsulted {
		t.Fatal("expected advisor to be consulted after the first compile failure")
	}
	if report.FinalValidatorReport == "" {
		t.Fatal("expected a final validator failure report")
	}
	if report.LastCompileOutput != "" {
		t.Fatalf("expected stale compile output to be cleared after later validation failure, got %q", report.LastCompileOutput)
	}
	out := report.FormatMarkdown()
	if strings.Contains(out, "Final Compile Output") {
		t.Fatalf("rendered HALT markdown should not show compile output after a later validation failure:\n%s", out)
	}
	if !strings.Contains(out, "Final Validator Report") {
		t.Fatalf("rendered HALT markdown missing validator report:\n%s", out)
	}
}
