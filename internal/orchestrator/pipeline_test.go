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
