package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// A below-floor allow reaches the human as a bare verdict plus the confidence
// numbers; the concern answer is the only place a reason can come from. Jev's
// confidence is distribution-shaped, so on a genuinely opaque request (a
// command head that is an undefined variable, e.g. "$g --version") it leans
// allow well below the floor while still answering concern "none" — the banner
// then explains nothing. The residual-doubt rule is what removes that blank,
// so pin it: an edit must not quietly drop the instruction that makes a
// hesitant verdict name its doubt.
func TestTypesafeConcernInstructionsNameResidualDoubt(t *testing.T) {
	for _, want := range []string{
		`Reserve "none" for a call that gives you no pause`,
		"residual doubt",
		"undefined variable",
		`"truncated_or_unknown"`,
	} {
		if !strings.Contains(typesafeConcernInstructions, want) {
			t.Fatalf("concern instructions lost %q:\n%s", want, typesafeConcernInstructions)
		}
	}
	// The rule is advice about how to answer the concern question — the shared
	// verdict rubric carries no concern guidance and must not grow one.
	if strings.Contains(typesafeJudgeInstructions, "residual doubt") {
		t.Fatal("residual-doubt rule leaked into the shared verdict rubric")
	}
	// The concern question must still inherit the full gatekeeper rubric.
	if !strings.HasPrefix(typesafeConcernInstructions, typesafeJudgeInstructions) {
		t.Fatal("concern instructions no longer embed the shared rubric")
	}
}

// The wire request must actually carry the residual-doubt rule on the concern
// question — pinning the const alone would pass even if the question kept
// building its own instruction string.
func TestTypesafeConcernQuestionSendsResidualDoubtRule(t *testing.T) {
	a, h := newTypesafeJudge(t, typesafeChoiceReply("allow", 0.95))

	a.consultPermissionModel("bash", json.RawMessage(`{"command":"$g --version"}`), nil)

	h.mu.Lock()
	defer h.mu.Unlock()

	qs, ok := h.body["questions"].(map[string]any)
	if !ok {
		t.Fatalf("questions = %#v", h.body["questions"])
	}
	concern, ok := qs[typesafeJudgeConcernKey].(map[string]any)
	if !ok {
		t.Fatalf("concern question missing: %#v", qs)
	}
	if instr, _ := concern["instructions"].(string); !strings.Contains(instr, "residual doubt") {
		t.Fatalf("concern question instructions lack the residual-doubt rule:\n%s", instr)
	}

	verdict, ok := qs[typesafeJudgeVerdictKey].(map[string]any)
	if !ok {
		t.Fatalf("verdict question missing: %#v", qs)
	}
	if vInstr, _ := verdict["instructions"].(string); strings.Contains(vInstr, "residual doubt") {
		t.Fatal("verdict question should not carry the concern-only rule")
	}
}
