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
		Plan:            Plan{Text: "add validation\n\nSuccess criteria:\n- nil safe"},
		ExploreSnapshot: "## auth.go",
	}
	p.doc.AddIteration(Iteration{
		Number:          1,
		ValidatorReport: "### Validation Failure Report\n- **Issue:** nil panic",
	})
	p.iterCount = 1 // matches the iteration we just added
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
