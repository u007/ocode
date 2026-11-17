package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/skill"
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
	// The advisor gate resolves its judge through newClientFn. Return no client
	// so these tests never reach a live judge and take the threshold fallback.
	prevNewClient := newClientFn
	t.Cleanup(func() { newClientFn = prevNewClient })
	newClientFn = func(*config.Config, string) LLMClient { return nil }
	cfg := &config.Config{}
	cfg.Ocode.Advisor = config.AdvisorConfig{
		Enabled:     true,
		Provider:    "deepseek",
		Model:       "deepseek-v4-pro",
		Checkpoints: checkpoints,
	}
	a := newTestAgent(nil, nil, cfg, nil)
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
		t.Fatal("expected both plan and done checkpoints enabled")
	}
	if !a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected the done checkpoint enabled")
	}

	// Advisor disabled → off.
	a.advisorEnabled.Store(false)
	if a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected checkpoint disabled when advisor disabled")
	}
	a.advisorEnabled.Store(true)

	// No explicit model — checkpoints still fire because the advisor tool
	// has a built-in default fallback (AdvisorTool.resolveModel). The old
	// model gate was removed: absence of an explicit model does not disable
	// checkpoints; the advisor call itself handles resolution gracefully.
	// The triggers are the SESSION's, so drop the model from the session's
	// config rather than from the process-wide one.
	a.SetAdvisorConfig(AdvisorConfig{Checkpoints: []string{"done"}})
	if !a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected checkpoint enabled even without explicit advisor model (default fallback)")
	}
	a.SetAdvisorConfig(AdvisorConfig{Model: "deepseek/deepseek-v4-pro", Checkpoints: []string{"done"}})

	// Sub-agent (spec set) → off.
	a.spec = &AgentSpec{Name: "sub"}
	if a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected checkpoint disabled for sub-agents")
	}
	a.spec = nil

	// Checkpoint not in the session's trigger set → off. Changing the
	// PROCESS-WIDE config must not move a session's triggers.
	a.config.Ocode.Advisor.Checkpoints = []string{"done"}
	a.SetAdvisorConfig(AdvisorConfig{Model: "deepseek/deepseek-v4-pro", Checkpoints: []string{"done"}})
	if a.advisorCheckpointEnabled(checkpointPlan) {
		t.Fatal("expected plan checkpoint disabled when not in the session's triggers")
	}
	if !a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected done checkpoint still enabled")
	}
	a.config.Ocode.Advisor.Checkpoints = nil

	// No triggers on the session → off, even with a nil process-wide config:
	// the session's own trigger set is the only source of truth.
	a.SetAdvisorConfig(AdvisorConfig{Model: "deepseek/deepseek-v4-pro"})
	a.config = nil
	if a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected checkpoint disabled when the session has no triggers")
	}

	// A nil process-wide config does NOT disable a session that has its own
	// triggers — the session's pinned set governs, not cfg.
	a.SetAdvisorConfig(AdvisorConfig{Checkpoints: []string{"done"}})
	if !a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("expected session triggers to still fire with a nil process config")
	}
}

func TestAdvisorPlanCheckpoint_InjectsReviewWithoutDeferring(t *testing.T) {
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
	review := a.advisorPlanCheckpoint(st, resp)
	if review == nil {
		t.Fatal("expected the plan checkpoint to return an advisor review")
	}
	if fake.calls != 1 {
		t.Fatalf("expected 1 advisor call, got %d", fake.calls)
	}
	// The review is injected as a user message: the write batch already ran,
	// so no synthetic tool results are produced and the model never has to
	// regenerate the writes.
	if review.Role != "user" {
		t.Fatalf("expected the review to be injected as a user message, got role %q", review.Role)
	}
	if review.ToolID != "" {
		t.Fatalf("review must not impersonate a tool result, got ToolID %q", review.ToolID)
	}
	if !strings.Contains(review.Content, fake.advice) {
		t.Fatal("review must carry the advisor advice")
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
		t.Fatal("advisor failure must not inject a review")
	}
	if !st.planChecked {
		t.Fatal("failed checkpoint still counts as checked (no retry loop)")
	}
}

// A write batch still gets its design review, and the completion checkpoint
// still fires when the model stops — both triggers are independent.
func TestAdvisorDoneCheckpoint_FiresAfterPlanCheckpointOnSameTurn(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan", "done"})
	st := &advisorCheckpointState{userGoal: "fix the bug"}

	planResp := &Message{Role: "assistant", ToolCalls: []ToolCall{writeToolCall("tc1", "a.go")}}
	// The Step loop counts the batch (it executed) before the plan checkpoint runs.
	st.countBatch(planResp.ToolCalls)
	if review := a.advisorPlanCheckpoint(st, planResp); review == nil {
		t.Fatal("expected plan checkpoint to review the first write batch")
	}

	finalResp := &Message{Role: "assistant", Content: "Done, fixed."}
	followUp := a.advisorDoneCheckpoint(st, finalResp)
	if followUp == nil {
		t.Fatal("expected done checkpoint to fire after a write batch")
	}
	if fake.calls != 2 {
		t.Fatalf("expected two advisor calls (plan + done), got %d", fake.calls)
	}
	if !strings.Contains(followUp.Content, fake.advice) {
		t.Fatal("follow-up must carry the advisor advice")
	}
}

// Only write-class tools trigger the design review; read-only tools never do.
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

	// Reaching the threshold with read-only calls does not review either: the
	// session has no change, so there is nothing for the advisor to check.
	st.countBatch(make([]ToolCall, doneCheckpointMinToolCalls))
	if f := a.advisorDoneCheckpoint(st, resp); f != nil {
		t.Fatal("a read-only session with no changed files must not be reviewed, even past the threshold")
	}
	if fake.calls != 0 {
		t.Fatal("advisor must not be called for a session with no changes")
	}
}

func TestNewAdvisorCheckpointState_ReceivesExplicitGoal(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, _ := checkpointTestAgent(t, []string{"done"})

	st := a.newAdvisorCheckpointState("explicit user goal", false, false)
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
	st2 := a.newAdvisorCheckpointState("real user request", false, false)
	if st2.userGoal != "real user request" {
		t.Fatalf("expected 'real user request', got %q (tail leaked into checkpoint)", st2.userGoal)
	}
	if got := lastHumanUserContent(messagesWithTail); got != "real user request" {
		t.Fatalf("human prompt = %q, want 'real user request' (tail/synthetic leaked)", got)
	}

	// A background-job notice is user-role but not a request: it must never be
	// read as one, and it must not hide the human prompt before it.
	jobTurn := []Message{
		{Role: "user", Content: "real user request"},
		{Role: "assistant", Content: "working"},
		{Role: "user", Content: "[ocode:event] lint passed; just commit"},
	}
	if got := lastHumanUserContent(jobTurn); got != "real user request" {
		t.Fatalf("job notice became the user request: %q", got)
	}
	if got := lastHumanUserContent(jobTurn[2:]); got != "" {
		t.Fatalf("synthetic-only transcript yielded %q, want empty", got)
	}
}

// ctxCheckpointFakeAdvisor is a stub advisor that blocks until its context is
// cancelled, so the checkpoint's cancellation wiring can be exercised.
type ctxCheckpointFakeAdvisor struct {
	checkpointFakeAdvisor
	started chan struct{}
}

func (f *ctxCheckpointFakeAdvisor) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	close(f.started)
	<-ctx.Done()
	return "", ctx.Err()
}

func TestRunAdvisorCheckpoint_CancelledByAgentStop(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, _ := checkpointTestAgent(t, []string{"done"})
	fake := &ctxCheckpointFakeAdvisor{started: make(chan struct{})}
	a.tools["advisor"] = fake

	go func() {
		<-fake.started
		a.Cancel()
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if advice, ok := a.runAdvisorCheckpoint(checkpointDone, "review this"); ok {
			t.Errorf("expected cancelled advisor to report ok=false, got advice %q", advice)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runAdvisorCheckpoint did not return after the agent was cancelled")
	}
}

func TestRunAdvisorCheckpoint_ReportsProgress(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, _ := checkpointTestAgent(t, []string{"done"})

	var events []bool
	a.OnAdvisorCheckpoint = func(kind string, running bool) {
		if kind != checkpointDone {
			t.Errorf("unexpected checkpoint kind %q", kind)
		}
		events = append(events, running)
	}

	if _, ok := a.runAdvisorCheckpoint(checkpointDone, "review this"); !ok {
		t.Fatal("expected the fake advisor to return advice")
	}
	if len(events) != 2 || !events[0] || events[1] {
		t.Fatalf("expected start(true) then finish(false), got %v", events)
	}
}

// TestAdvisorToggleKeepsToolListStable locks in the cache-safety contract:
// flipping advisor on/off must not change the advertised tool set (a tools
// block change invalidates the provider prompt-cache prefix). The gate lives
// at execution time instead.
func TestAdvisorToggleKeepsToolListStable(t *testing.T) {
	a := newTestAgent(nil, nil, nil, nil)
	if !a.AdvisorEnabled() {
		t.Fatal("expected advisor enabled by default")
	}
	names := func() []string {
		var out []string
		for _, t := range a.GetTools() {
			out = append(out, t.Name())
		}
		sort.Strings(out)
		return out
	}
	before := names()
	a.SetAdvisorEnabled(false)
	after := names()
	if len(before) != len(after) {
		t.Fatalf("tool list changed on advisor toggle: before=%v after=%v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("tool list changed on advisor toggle at %d: before=%v after=%v", i, before, after)
		}
	}
	hasAdvisor := false
	for _, n := range after {
		if n == "advisor" {
			hasAdvisor = true
		}
	}
	if !hasAdvisor {
		t.Fatalf("advisor must stay advertised while disabled, got %v", after)
	}

	out, err := a.executeToolCall("advisor", []byte(`{"question":"x"}`), nil, "tc1")
	if err != nil {
		t.Fatalf("expected soft refusal, got err %v", err)
	}
	if !strings.Contains(out, "disabled") {
		t.Fatalf("expected disabled notice, got %q", out)
	}
}

func TestAdvisorPlanCheckpoint_SkillSuppressedStandsDown(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan"})
	st := &advisorCheckpointState{skillSuppressed: true}

	resp := &Message{Role: "assistant", ToolCalls: []ToolCall{writeToolCall("tc1", "a.go")}}
	if d := a.advisorPlanCheckpoint(st, resp); d != nil {
		t.Fatal("plan checkpoint must stand down when skillSuppressed")
	}
	if fake.calls != 0 {
		t.Fatal("advisor must not be called when skillSuppressed")
	}
	if st.planChecked {
		t.Fatal("suppressed checkpoint must not consume planChecked")
	}

	// Without suppression it fires.
	st2 := &advisorCheckpointState{}
	if d := a.advisorPlanCheckpoint(st2, resp); d == nil {
		t.Fatal("plan checkpoint should fire when not suppressed")
	}
}

func TestAdvisorDoneCheckpoint_SkillSuppressedStandsDown(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"done"})
	fake.advice = "gap"
	st := &advisorCheckpointState{userGoal: "fix", skillSuppressed: true}
	st.countBatch([]ToolCall{writeToolCall("tc1", "a.go")})

	resp := &Message{Role: "assistant", Content: "Done."}
	if d := a.advisorDoneCheckpoint(st, resp); d != nil {
		t.Fatal("done checkpoint must stand down when skillSuppressed")
	}
	if fake.calls != 0 {
		t.Fatal("advisor must not be called when skillSuppressed")
	}

	st2 := &advisorCheckpointState{userGoal: "fix"}
	st2.countBatch([]ToolCall{writeToolCall("tc1", "a.go")})
	if d := a.advisorDoneCheckpoint(st2, resp); d == nil {
		t.Fatal("done checkpoint should fire when not suppressed")
	}
}

func TestTurnDisablesAdvisor_Detection(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".claude", "skills", "commit-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: commit-skill\nskip_advisor: true\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	a := newTestAgent(nil, nil, cfg, nil)
	a.SetWorkDir(root)
	st := a.newAdvisorCheckpointState("", false, false)

	// skill tool call
	tc := ToolCall{}
	tc.Function.Name = "skill"
	tc.Function.Arguments = `{"name":"commit-skill"}`
	msgs := []Message{{Role: "assistant", ToolCalls: []ToolCall{tc}}}
	if !a.turnDisablesAdvisor(st, msgs) {
		t.Fatal("expected detection via skill tool call")
	}

	// auto-inject marker
	msgs2 := []Message{{Role: "user", Content: "auto-loaded skill \"commit-skill\" (Jev relevance 0.90)"}}
	if !a.turnDisablesAdvisor(st, msgs2) {
		t.Fatal("expected detection via auto-inject marker")
	}

	// slash command prompt
	msgs3 := []Message{{Role: "user", Content: "Run the **commit-skill** skill."}}
	if !a.turnDisablesAdvisor(st, msgs3) {
		t.Fatal("expected detection via slash prompt")
	}

	// unrelated skill (no flag) → false
	otherDir := filepath.Join(root, ".claude", "skills", "other-skill")
	os.MkdirAll(otherDir, 0o755)
	os.WriteFile(filepath.Join(otherDir, "SKILL.md"), []byte("---\nname: other-skill\n---\nbody\n"), 0o644)
	msgs4 := []Message{{Role: "user", Content: "Run the **other-skill** skill."}}
	if a.turnDisablesAdvisor(st, msgs4) {
		t.Fatal("skill without flag must not suppress")
	}

	// missing skill fails open → false
	msgs5 := []Message{{Role: "user", Content: "Run the **nope** skill."}}
	if a.turnDisablesAdvisor(st, msgs5) {
		t.Fatal("missing skill must fail open (not suppress)")
	}

	// turn boundary: a suppressed skill in an earlier turn must not suppress a later turn
	msgs6 := []Message{
		{Role: "user", Content: "Run the **commit-skill** skill."},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "now do unrelated work"},
	}
	if a.turnDisablesAdvisor(st, msgs6) {
		t.Fatal("suppression must not leak across turns")
	}
}

// writeSkipAdvisorSkill writes <root>/.claude/skills/<name>/SKILL.md with the
// given extra frontmatter lines and returns the SKILL.md path.
func writeSkipAdvisorSkill(t *testing.T, root, name, extraFrontmatter string) string {
	t.Helper()
	dir := filepath.Join(root, ".claude", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	content := "---\nname: " + name + "\n" + extraFrontmatter + "---\nbody\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The skill prompt is the human's message, but the tail injectors append
// user-role blocks after it. The pre-injection snapshot must still see it.
func TestTurnDisablesAdvisor_PreInjectionSnapshotSeesSkillPrompt(t *testing.T) {
	root := t.TempDir()
	writeSkipAdvisorSkill(t, root, "commit-skill", "skip_advisor: true\n")
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.SetWorkDir(root)

	input := []Message{{Role: "user", Content: "Run the **commit-skill** skill."}}
	injected := append(append([]Message(nil), input...), Message{Role: "user", Content: "[ocode:plan]\n- step one"})

	// Precondition: scanned AFTER injection, the current-turn boundary lands on
	// the injected block, so the skill prompt is out of view.
	if a.turnDisablesAdvisor(a.newAdvisorCheckpointState("", false, false), injected) {
		t.Fatal("precondition failed: the post-injection slice should hide the skill prompt")
	}
	// Step scans the snapshot taken before injection, which still holds it.
	if !a.turnDisablesAdvisor(a.newAdvisorCheckpointState("", false, false), input) {
		t.Fatal("pre-injection snapshot must detect the human's skill prompt")
	}
}

func TestToolCallsDisableAdvisor_SkillCallNamingSkipAdvisorSkill(t *testing.T) {
	root := t.TempDir()
	writeSkipAdvisorSkill(t, root, "commit-skill", "skip_advisor: true\n")
	writeSkipAdvisorSkill(t, root, "other-skill", "")
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.SetWorkDir(root)

	if !a.toolCallsDisableAdvisor(a.newAdvisorCheckpointState("", false, false),
		[]ToolCall{namedToolCall("tc1", "skill", `{"name":"commit-skill"}`)}) {
		t.Fatal("a skill tool call naming a SkipAdvisor skill must disable the advisor")
	}
	if !a.toolCallsDisableAdvisor(a.newAdvisorCheckpointState("", false, false),
		[]ToolCall{namedToolCall("tc2", "load_skill", `{"name":"commit-skill"}`)}) {
		t.Fatal("load_skill must be honoured the same as skill")
	}
	if a.toolCallsDisableAdvisor(a.newAdvisorCheckpointState("", false, false),
		[]ToolCall{namedToolCall("tc3", "skill", `{"name":"other-skill"}`), writeToolCall("tc4", "a.go")}) {
		t.Fatal("a skill without skip_advisor, or a non-skill call, must not disable the advisor")
	}
}

// A skill loaded by the response in round N is not in the pre-loop transcript.
// The per-response check must still stand the done checkpoint down for that turn.
func TestAdvisorDoneCheckpoint_SkillLoadedInResponseStandsDown(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	root := t.TempDir()
	writeSkipAdvisorSkill(t, root, "commit-skill", "skip_advisor: true\n")
	a, fake := checkpointTestAgent(t, []string{"done"})
	a.SetWorkDir(root)

	st := a.newAdvisorCheckpointState("commit it", false, false)
	calls := []ToolCall{
		namedToolCall("tc1", "skill", `{"name":"commit-skill"}`),
		writeToolCall("tc2", "a.go"),
	}
	st.skillSuppressed = st.skillSuppressed || a.toolCallsDisableAdvisor(st, calls)
	st.countBatch(calls)
	if d := a.advisorDoneCheckpoint(st, &Message{Role: "assistant", Content: "Committed."}); d != nil {
		t.Fatal("done checkpoint must stand down for a turn that loads a SkipAdvisor skill")
	}
	if fake.calls != 0 {
		t.Fatal("advisor must not be called when the turn loads a SkipAdvisor skill")
	}
}

// Each skill name is resolved from disk at most once per Step. Removing the
// file mid-Step must not change the memoized verdict; a new Step re-resolves.
func TestSkillDisablesAdvisor_ResolvesEachNameOncePerStep(t *testing.T) {
	root := t.TempDir()
	path := writeSkipAdvisorSkill(t, root, "commit-skill", "skip_advisor: true\n")
	a := newTestAgent(nil, nil, &config.Config{}, nil)
	a.SetWorkDir(root)

	st := a.newAdvisorCheckpointState("", false, false)
	if !a.skillDisablesAdvisor(st, "commit-skill") {
		t.Fatal("first resolution should see skip_advisor")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// The loader caches its scan for a few seconds; drop it so the removal is
	// visible to a fresh lookup.
	skill.InvalidateSkillCache()
	if !a.skillDisablesAdvisor(st, "commit-skill") {
		t.Fatal("second lookup in the same Step must reuse the memoized verdict, not rescan disk")
	}
	if a.skillDisablesAdvisor(a.newAdvisorCheckpointState("", false, false), "commit-skill") {
		t.Fatal("a new Step must re-resolve, and the removed skill must now fail open")
	}
}

// A stored "plan" value stays a valid design trigger; it is no longer folded
// into "done". A session pinned to it must still get its first-write review.
func TestAdvisorCheckpoint_PlanValueIsValid(t *testing.T) {
	t.Setenv("OPENCODE_ADVISOR_MODEL", "")
	a, fake := checkpointTestAgent(t, []string{"plan"})
	if !a.advisorCheckpointEnabled(checkpointPlan) {
		t.Fatal("a stored plan checkpoint must still enable the design review")
	}
	if got := a.AdvisorCheckpoints(); len(got) != 1 || got[0] != checkpointPlan {
		t.Fatalf("checkpoints = %v, want [plan]", got)
	}
	st := &advisorCheckpointState{userGoal: "fix it"}
	st.countBatch([]ToolCall{writeToolCall("tc1", "a.go")})
	if f := a.advisorPlanCheckpoint(st, &Message{Role: "assistant", ToolCalls: []ToolCall{writeToolCall("tc1", "a.go")}}); f == nil {
		t.Fatal("plan should fire on the first write batch")
	}
	if fake.calls != 1 {
		t.Fatalf("expected one advisor call, got %d", fake.calls)
	}

	a.SetAdvisorConfig(AdvisorConfig{Checkpoints: []string{"plan", "done"}})
	if !a.advisorCheckpointEnabled(checkpointPlan) || !a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("both plan and done must be enabled when configured")
	}
	if !a.advisorCheckpointEnabled(checkpointDone) {
		t.Fatal("a configured plan value must not disable done")
	}
}
