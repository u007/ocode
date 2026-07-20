package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// checkpointFakeAdvisor is a stub advisor tool that records the prompt it was
// called with and returns canned advice.
type checkpointFakeAdvisor struct {
	advice     string
	err        error
	lastPrompt string
	calls      int
}

func (f *checkpointFakeAdvisor) Name() string        { return "advisor" }
func (f *checkpointFakeAdvisor) Description() string { return "fake advisor" }
func (f *checkpointFakeAdvisor) Definition() map[string]interface{} {
	return map[string]interface{}{"name": "advisor"}
}
func (f *checkpointFakeAdvisor) Parallel() bool { return false }
func (f *checkpointFakeAdvisor) Execute(args json.RawMessage) (string, error) {
	f.calls++
	var p struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	f.lastPrompt = p.Prompt
	return f.advice, f.err
}

func checkpointTestAgent(t *testing.T, checkpoints []string) (*Agent, *checkpointFakeAdvisor) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Ocode.Advisor = config.AdvisorConfig{
		Enabled:     true,
		Provider:    "deepseek",
		Model:       "deepseek-v4-pro",
		Checkpoints: checkpoints,
	}
	a := NewAgent(nil, nil, cfg, nil)
	fake := &checkpointFakeAdvisor{advice: "proceed — plan looks correct"}
	a.tools["advisor"] = fake
	return a, fake
}

func writeToolCall(id, path string) ToolCall {
	return namedToolCall(id, "write", `{"path":"`+path+`","content":"x"}`)
}

func namedToolCall(id, name, args string) ToolCall {
	tc := ToolCall{ID: id}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

func TestAdvisorCheckpointEnabled_Gating(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")

	a, _ := checkpointTestAgent(t, []string{"plan", "done"})
	if !a.advisorCheckpointEnabled(checkpointPlan) || !a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected both checkpoints enabled")
	}

	// Advisor disabled → off.
	a.advisorEnabled.Store(false)
	if a.advisorCheckpointEnabled(checkpointPlan) {
		t.Fatal("expected checkpoint disabled when advisor disabled")
	}
	a.advisorEnabled.Store(true)

	// No explicit model — checkpoints still fire because the advisor tool
	// has a built-in default fallback (AdvisorTool.resolveModel). The old
	// model gate was removed: absence of an explicit model does not disable
	// checkpoints; the advisor call itself handles resolution gracefully.
	a.config.Ocode.Advisor.Model = ""
	if !a.advisorCheckpointEnabled(checkpointPlan) {
		t.Fatal("expected checkpoint enabled even without explicit advisor model (default fallback)")
	}
	a.config.Ocode.Advisor.Model = "deepseek-v4-pro"

	// Sub-agent (spec set) → off.
	a.spec = &AgentSpec{Name: "sub"}
	if a.advisorCheckpointEnabled(checkpointPlan) {
		t.Fatal("expected checkpoint disabled for sub-agents")
	}
	a.spec = nil

	// Checkpoint not listed → off.
	a.config.Ocode.Advisor.Checkpoints = []string{"done"}
	if a.advisorCheckpointEnabled(checkpointPlan) {
		t.Fatal("expected plan checkpoint disabled when not listed")
	}
	if !a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected done checkpoint still enabled")
	}

	// Nil config → off.
	a.config = nil
	if a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected checkpoint disabled with nil config")
	}
}

func TestAdvisorPlanCheckpoint_DefersWriteBatch(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan"})
	st := &advisorCheckpointState{userGoal: "add feature X"}

	resp := &Message{
		Role:    "assistant",
		Content: "I will edit two files.",
		ToolCalls: []ToolCall{
			writeToolCall("tc1", "a.go"),
			writeToolCall("tc2", "b.go"),
		},
	}
	deferred := a.advisorPlanCheckpoint(st, resp)
	if len(deferred) != 2 {
		t.Fatalf("expected 2 deferred tool results, got %d", len(deferred))
	}
	if fake.calls != 1 {
		t.Fatalf("expected 1 advisor call, got %d", fake.calls)
	}
	if deferred[0].ToolID != "tc1" || deferred[1].ToolID != "tc2" {
		t.Fatalf("tool IDs not preserved: %+v", deferred)
	}
	if !strings.Contains(deferred[0].Content, fake.advice) {
		t.Fatal("first deferred result must carry the advisor advice")
	}
	if !strings.Contains(fake.lastPrompt, "add feature X") || !strings.Contains(fake.lastPrompt, "a.go") {
		t.Fatalf("plan prompt missing goal or file: %q", fake.lastPrompt)
	}

	// Second batch in the same Step must NOT re-fire.
	if again := a.advisorPlanCheckpoint(st, resp); again != nil {
		t.Fatal("plan checkpoint fired twice in one Step")
	}
}

func TestAdvisorPlanCheckpoint_SkipsReadOnlyBatch(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan"})
	st := &advisorCheckpointState{}

	resp := &Message{
		Role: "assistant",
		ToolCalls: []ToolCall{
			namedToolCall("tc1", "read", `{"path":"a.go"}`),
		},
	}
	if d := a.advisorPlanCheckpoint(st, resp); d != nil {
		t.Fatal("plan checkpoint must not fire on read-only batches")
	}
	if fake.calls != 0 {
		t.Fatal("advisor must not be called for read-only batches")
	}
	if st.planChecked {
		t.Fatal("read-only batch must not consume the plan checkpoint")
	}
}

func TestAdvisorPlanCheckpoint_AdvisorFailureExecutesNormally(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan"})
	fake.err = assertAnError("advisor down")
	st := &advisorCheckpointState{}

	resp := &Message{Role: "assistant", ToolCalls: []ToolCall{writeToolCall("tc1", "a.go")}}
	if d := a.advisorPlanCheckpoint(st, resp); d != nil {
		t.Fatal("advisor failure must not defer the batch")
	}
	if !st.planChecked {
		t.Fatal("failed checkpoint still counts as checked (no retry loop)")
	}
}

func TestAdvisorDoneCheckpoint_FiresOnNonTrivialTurn(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"done"})
	fake.advice = "gap: tests not run"
	st := &advisorCheckpointState{userGoal: "fix the bug"}
	st.countBatch([]ToolCall{writeToolCall("tc1", "a.go")})

	resp := &Message{Role: "assistant", Content: "Done, bug fixed."}
	followUp := a.advisorDoneCheckpoint(st, resp)
	if followUp == nil {
		t.Fatal("expected done checkpoint to fire after a write")
	}
	if followUp.Role != "user" {
		t.Fatalf("expected user role, got %q", followUp.Role)
	}
	if !strings.Contains(followUp.Content, fake.advice) {
		t.Fatal("follow-up must carry the advisor advice")
	}
	if !strings.Contains(fake.lastPrompt, "fix the bug") || !strings.Contains(fake.lastPrompt, "Done, bug fixed.") {
		t.Fatalf("done prompt missing goal or final report: %q", fake.lastPrompt)
	}

	// Must not fire twice in one Step.
	if again := a.advisorDoneCheckpoint(st, resp); again != nil {
		t.Fatal("done checkpoint fired twice in one Step")
	}
}

func TestAdvisorDoneCheckpoint_SkipsTrivialTurn(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"done"})
	st := &advisorCheckpointState{}
	st.countBatch([]ToolCall{
		namedToolCall("tc1", "read", `{}`),
		namedToolCall("tc2", "grep", `{}`),
	})

	resp := &Message{Role: "assistant", Content: "The answer is 42."}
	if f := a.advisorDoneCheckpoint(st, resp); f != nil {
		t.Fatal("done checkpoint must not fire on trivial turns")
	}
	if fake.calls != 0 {
		t.Fatal("advisor must not be called on trivial turns")
	}

	// Threshold of read-only calls also counts as non-trivial.
	st.countBatch(make([]ToolCall, doneCheckpointMinToolCalls))
	if f := a.advisorDoneCheckpoint(st, resp); f == nil {
		t.Fatal("done checkpoint must fire once the tool-call threshold is reached")
	}
}

// TestAdvisorDoneCheckpoint_FiresAfterDeferredPlanWithoutReissue exercises the
// bypass case: the plan checkpoint defers the first write batch, the model then
// stops without re-issuing tools, and the done checkpoint must still run.
func TestAdvisorDoneCheckpoint_FiresAfterDeferredPlanWithoutReissue(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})
	st := &advisorCheckpointState{userGoal: "fix the bug"}

	planResp := &Message{Role: "assistant", ToolCalls: []ToolCall{writeToolCall("tc1", "a.go")}}
	deferred := a.advisorPlanCheckpoint(st, planResp)
	if deferred == nil {
		t.Fatal("expected plan checkpoint to defer the first write batch")
	}
	if !st.deferredWriteIntent {
		t.Fatal("expected deferredWriteIntent to be recorded")
	}

	finalResp := &Message{Role: "assistant", Content: "Done, fixed."}
	followUp := a.advisorDoneCheckpoint(st, finalResp)
	if followUp == nil {
		t.Fatal("expected done checkpoint to fire after deferred writes even without reissue")
	}
	if fake.calls != 2 {
		t.Fatalf("expected two advisor calls (plan + done), got %d", fake.calls)
	}
	if !strings.Contains(followUp.Content, fake.advice) {
		t.Fatal("follow-up must carry the advisor advice")
	}
}

// TestAdvisorPlanCheckpoint_NonWriteTools pass through without deferral.
// This validates that the expanded isWriteTool list is consistent: read-only
// tool names (read, glob, grep, list, lsp, bash, webfetch, websearch) should
// never trigger the plan checkpoint.
func TestAdvisorPlanCheckpoint_OnlyWriteToolsTrigger(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan"})

	readOnlyTools := []string{"read", "glob", "grep", "list", "lsp", "bash", "webfetch", "websearch"}
	for _, toolName := range readOnlyTools {
		st := &advisorCheckpointState{userGoal: "read-only check"}
		resp := &Message{Role: "assistant", ToolCalls: []ToolCall{
			namedToolCall("tc1", toolName, `{}`),
		}}
		if d := a.advisorPlanCheckpoint(st, resp); d != nil {
			t.Fatalf("plan checkpoint fired for read-only tool %q", toolName)
		}
		if fake.calls != 0 {
			t.Fatalf("advisor called for read-only tool %q", toolName)
		}
		if st.planChecked {
			t.Fatalf("planChecked set by read-only tool %q", toolName)
		}
	}

	// Every write-class tool should trigger the plan checkpoint.
	writeTools := []string{"write", "edit", "apply_patch", "replace_lines",
		"multiedit", "multi_file_edit", "delete", "format"}
	for _, toolName := range writeTools {
		fake.calls = 0
		fake.lastPrompt = ""
		st := &advisorCheckpointState{userGoal: "write check for " + toolName}
		// Use a stub path for each tool type. delete/format accept 'path';
		// multiedit uses 'file_path'; multi_file_edit uses 'edits'.
		args := `{"path":"test.go"}`
		if toolName == "multiedit" {
			args = `{"file_path":"test.go","edits":[]}`
		} else if toolName == "multi_file_edit" {
			args = `{"edits":[{"path":"a.go","search":"x","replace":"y"}]}`
		}
		resp := &Message{Role: "assistant", ToolCalls: []ToolCall{
			namedToolCall("tc1", toolName, args),
		}}
		if d := a.advisorPlanCheckpoint(st, resp); d == nil {
			t.Fatalf("plan checkpoint did NOT fire for write tool %q", toolName)
		}
		if fake.calls != 1 {
			t.Fatalf("advisor called %d times (expected 1) for write tool %q", fake.calls, toolName)
		}
		if !st.planChecked {
			t.Fatalf("planChecked NOT set by write tool %q", toolName)
		}
		// Verify the goal reached the advisor prompt.
		if !strings.Contains(fake.lastPrompt, "write check for "+toolName) {
			t.Fatalf("advisor prompt missing goal for %q: %q", toolName, fake.lastPrompt)
		}
	}
}

// TestNewAdvisorCheckpointState_ReceivesExplicitGoal verifies that the
// checkpoint state is constructed with the user goal passed explicitly (not
// derived from messages), so tail-injected user-role content cannot mask it.
func TestNewAdvisorCheckpointState_ReceivesExplicitGoal(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, _ := checkpointTestAgent(t, []string{"plan"})

	st := a.newAdvisorCheckpointState("explicit user goal")
	if st.userGoal != "explicit user goal" {
		t.Fatalf("expected 'explicit user goal', got %q", st.userGoal)
	}

	// With an explicit goal passed, tail-injected user messages in the message
	// list must not leak into the checkpoint state.
	messagesWithTail := []Message{
		{Role: "system", Content: "you are helpful"},
		{Role: "user", Content: "real user request"},
		{Role: "assistant", Content: "thinking..."},
		{Role: "user", Content: "[ocode:discovery] attached doc content"},
	}
	st2 := a.newAdvisorCheckpointState("real user request")
	if st2.userGoal != "real user request" {
		t.Fatalf("expected 'real user request', got %q (tail leaked into checkpoint)", st2.userGoal)
	}
	if st2.userGoal == lastUserContent(messagesWithTail) {
		t.Fatal("checkpoint goal must NOT be derived from tail-injected messages")
	}
}
