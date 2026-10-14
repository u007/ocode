package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// verdictCriteriaKeys returns the action keys offered on the last recorded
// judge request.
func verdictCriteriaKeys(t *testing.T, h *typesafeJudgeHarness) map[string]bool {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	qs, ok := h.body["questions"].(map[string]any)
	if !ok {
		t.Fatalf("questions missing from request: %v", h.body)
	}
	verdict := qs[typesafeAutoContinueVerdictKey].(map[string]any)
	criteria := verdict["criteria"].(map[string]any)
	keys := make(map[string]bool, len(criteria))
	for k := range criteria {
		keys[k] = true
	}
	return keys
}

func confirmationReply() []Message {
	return []Message{
		{Role: "user", Content: "add caching to the user lookup"},
		{Role: "assistant", Content: "Plan: wrap lookupUser in an in-process LRU (size 1000, 5 min TTL) in internal/users/cache.go and add a unit test. Shall I proceed?"},
	}
}

// proceed answers a confirmation question on the user's behalf and sends the
// fixed "yes" prompt.
func TestAutoContinueProceedOnConfirmationResumes(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("proceed", 0.9, typesafeAutoContinueAwaitingConfirmation))
	dec, err := a.AutoContinueJudgeSync(confirmationReply(), nil, nil)
	if err != nil || !dec.Resume {
		t.Fatalf("expected resume, got %+v err=%v", dec, err)
	}
	if dec.Action != "proceed" || dec.Prompt != "Yes, proceed as described." {
		t.Fatalf("action/prompt = %q/%q", dec.Action, dec.Prompt)
	}
	if !strings.Contains(dec.Detail, "proceed") {
		t.Fatalf("detail should name the action: %q", dec.Detail)
	}
}

// An answer-class action acts for the user, so it needs the permission-grade
// floor: a 0.7 proceed passes the resume floor but not the answer floor.
func TestAutoContinueAnswerClassUsesHigherFloor(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("proceed", 0.7, typesafeAutoContinueAwaitingConfirmation))
	dec, err := a.AutoContinueJudgeSync(confirmationReply(), nil, nil)
	if err != nil || dec.Resume {
		t.Fatalf("0.7 proceed must fail closed at the answer floor, got %+v err=%v", dec, err)
	}
	if !strings.Contains(dec.Detail, "floor") || !strings.Contains(dec.Detail, "0.80") {
		t.Fatalf("detail should cite the 0.80 answer floor: %q", dec.Detail)
	}
	// Sanity: the same confidence resumes for a resume-class action.
	a2, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("continue", 0.7, "mid_task"))
	dec2, err := a2.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "Next I will"}}, nil, nil)
	if err != nil || !dec2.Resume {
		t.Fatalf("0.7 continue should resume at the resume floor, got %+v err=%v", dec2, err)
	}
}

// Nobody asked: an answer-class action without the awaiting_confirmation
// reason is vetoed, whatever the confidence.
func TestAutoContinueAnswerWithoutConfirmationQuestionVetoed(t *testing.T) {
	for _, reason := range []string{"mid_task", "finished", typesafeAutoContinueAwaitingDecision} {
		a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("proceed", 0.99, reason))
		dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "Working on it"}}, nil, nil)
		if err != nil || dec.Resume {
			t.Fatalf("reason=%s: proceed must be vetoed, got %+v err=%v", reason, dec, err)
		}
	}
}

// A confirmation question admits only an explicit typed answer: a bare
// "continue" would be an implicit yes the judge never stated.
func TestAutoContinueResumeOnConfirmationVetoed(t *testing.T) {
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("continue", 0.99, typesafeAutoContinueAwaitingConfirmation))
	dec, err := a.AutoContinueJudgeSync(confirmationReply(), nil, nil)
	if err != nil || dec.Resume {
		t.Fatalf("continue on a confirmation must be vetoed, got %+v err=%v", dec, err)
	}
	if !strings.Contains(dec.Detail, "confirmation") {
		t.Fatalf("detail should explain the confirmation veto: %q", dec.Detail)
	}
}

// awaiting_decision stays decisive for every non-end action, including the
// answer class.
func TestAutoContinueAwaitingDecisionVetoesEverything(t *testing.T) {
	for _, key := range []string{"continue", "proceed", "skip", "reject", "fix_errors"} {
		a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply(key, 0.99, typesafeAutoContinueAwaitingDecision))
		dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "Redis or in-process LRU — which do you prefer?"}}, nil, nil)
		if err != nil || dec.Resume {
			t.Fatalf("%s: awaiting_decision must veto, got %+v err=%v", key, dec, err)
		}
	}
	// await_user itself is an end action and reports cleanly.
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("await_user", 0.99, typesafeAutoContinueAwaitingDecision))
	dec, err := a.AutoContinueJudgeSync(nil, nil, nil)
	if err != nil || dec.Resume || dec.Action != "await_user" {
		t.Fatalf("await_user: got %+v err=%v", dec, err)
	}
	if !strings.Contains(dec.Detail, "waiting on the user") {
		t.Fatalf("detail: %q", dec.Detail)
	}
}

// proceed never auto-approves a plan whose own quoted commands the permission
// layer calls harmful.
func TestAutoContinueProceedVetoedOnHarmfulCommand(t *testing.T) {
	cases := []string{
		"I will clean up the branch:\n```\ngit reset --hard origin/main\n```\nShall I proceed?",
		"Next I run `git push --force origin main` to publish. OK to go ahead?",
		"Steps:\n```sh\n$ git stash\n$ go test ./...\n```\nProceed?",
	}
	for _, reply := range cases {
		a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("proceed", 0.95, typesafeAutoContinueAwaitingConfirmation))
		dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: reply}}, nil, nil)
		if err != nil || dec.Resume {
			t.Fatalf("proceed must be vetoed for %q, got %+v err=%v", reply, dec, err)
		}
		if !strings.Contains(dec.Detail, "destructive") {
			t.Fatalf("detail should explain the destructive veto: %q", dec.Detail)
		}
	}
	// reject on the same reply is the safe answer and is allowed through.
	a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("reject", 0.95, typesafeAutoContinueAwaitingConfirmation))
	dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: cases[0]}}, nil, nil)
	if err != nil || !dec.Resume || dec.Action != "reject" {
		t.Fatalf("reject should resume with the refusal prompt, got %+v err=%v", dec, err)
	}
}

func TestQuotedCommandCandidates(t *testing.T) {
	got := quotedCommandCandidates("Run `go build ./...` then:\n```\n$ git status\nmake test\n```\nand `ls` too")
	want := []string{"go build ./...", "git status", "make test", "ls"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("candidates = %q, want %q", got, want)
	}
	if replyProposesHarmfulCommand([]Message{{Role: "assistant", Content: "plain prose mentioning git reset --hard with no code span"}}) {
		t.Fatal("prose outside code spans must not count as a proposed command")
	}
}

// ask_advisor is offered only when the advisor is switched on; a judge that
// picks an unoffered key fails closed.
func TestAutoContinueAskAdvisorOfferedOnlyWhenAvailable(t *testing.T) {
	a, h := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("ask_advisor", 0.9, "mid_task"))
	a.SetAdvisorEnabled(false)
	dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "I am not sure whether to use A or B."}}, nil, nil)
	if err != nil || dec.Resume {
		t.Fatalf("ask_advisor without an advisor must not resume, got %+v err=%v", dec, err)
	}
	if !strings.Contains(dec.Detail, "unknown choice") {
		t.Fatalf("detail: %q", dec.Detail)
	}
	if verdictCriteriaKeys(t, h)["ask_advisor"] {
		t.Fatal("ask_advisor must not be offered while the advisor is off")
	}

	a.SetAdvisorEnabled(true)
	if _, ok := a.tools[advisorAgentName]; !ok {
		t.Fatal("test agent should have the advisor tool installed")
	}
	dec, err = a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "I am not sure whether to use A or B."}}, nil, nil)
	if err != nil || !dec.Resume || dec.Action != "ask_advisor" {
		t.Fatalf("ask_advisor with an advisor should resume, got %+v err=%v", dec, err)
	}
	if !strings.Contains(dec.Prompt, "advisor") {
		t.Fatalf("prompt should direct the model to the advisor tool: %q", dec.Prompt)
	}
	if !verdictCriteriaKeys(t, h)["ask_advisor"] {
		t.Fatal("ask_advisor must be offered while the advisor is on")
	}
}

// Once-per-chain actions already fired are not offered again, and the chain
// history rides the state.
func TestAutoContinueOncePerChainActionsFiltered(t *testing.T) {
	a, h := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("write_plan", 0.9, "mid_task"))
	prior := []string{"write_plan", "retry", "continue"}
	dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "Let me think about the approach."}}, nil, prior)
	if err != nil || dec.Resume {
		t.Fatalf("a repeated write_plan must not resume, got %+v err=%v", dec, err)
	}
	keys := verdictCriteriaKeys(t, h)
	for _, k := range []string{"write_plan", "retry"} {
		if keys[k] {
			t.Fatalf("%s already fired in this chain and must not be offered", k)
		}
	}
	// continue is not once-per-chain and stays available.
	if !keys["continue"] {
		t.Fatal("continue must stay offered")
	}
	h.mu.Lock()
	state := h.body["state"].(map[string]any)
	h.mu.Unlock()
	fired, _ := state["chain_actions_fired"].([]any)
	if len(fired) != 3 {
		t.Fatalf("state.chain_actions_fired = %v, want the three prior keys", state["chain_actions_fired"])
	}
	if _, ok := state["todo_has_open_items"]; !ok {
		t.Fatal("state should report whether the todo plan has open items")
	}
}

// next needs an open todo item; the test session has none, so it is not
// offered.
func TestAutoContinueNextRequiresOpenTodo(t *testing.T) {
	a, h := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("next", 0.9, "finished"))
	a.SetWorkDir(t.TempDir())
	a.SetSessionID("ses-no-plan")
	dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "Item 1 done."}}, nil, nil)
	if err != nil || dec.Resume {
		t.Fatalf("next without an open todo must not resume, got %+v err=%v", dec, err)
	}
	if verdictCriteriaKeys(t, h)["next"] {
		t.Fatal("next must not be offered without an open todo item")
	}
}

// writeSessionTodo drops a todo file for sessionID under root/.ocode/todo in
// the store's on-disk grammar, bypassing the process-global current session.
func writeSessionTodo(t *testing.T, root, sessionID, body string) {
	t.Helper()
	dir := filepath.Join(root, ".ocode", "todo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".md"), []byte("# Todo (revision 1)\n\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The next gate reads the judged agent's own plan by (WorkDir, session id),
// not the process-global current todo session: a server never sets the global,
// and in the TUI the global may be another session.
func TestAutoContinueNextGateIsSessionScoped(t *testing.T) {
	root := t.TempDir()
	writeSessionTodo(t, root, "ses-open", "- [✓] t1 one\n- [ ] t2 two\n")
	writeSessionTodo(t, root, "ses-done", "- [✓] t1 one\n")

	a, h := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("next", 0.9, "finished"))
	a.SetWorkDir(root)
	a.SetSessionID("ses-open")
	dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "Item 1 done."}}, nil, nil)
	if err != nil || !dec.Resume || dec.Action != "next" {
		t.Fatalf("next with this session's open item must resume, got %+v err=%v", dec, err)
	}
	if !verdictCriteriaKeys(t, h)["next"] {
		t.Fatal("next must be offered when this session's plan has an open item")
	}
	h.mu.Lock()
	open := h.body["state"].(map[string]any)["todo_has_open_items"]
	h.mu.Unlock()
	if open != true {
		t.Fatalf("state.todo_has_open_items = %v, want true", open)
	}

	// Same root, a session whose plan is finished: not offered, even though a
	// sibling session's file in the same directory has an open item.
	a, h = newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("next", 0.9, "finished"))
	a.SetWorkDir(root)
	a.SetSessionID("ses-done")
	dec, err = a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "Item 1 done."}}, nil, nil)
	if err != nil || dec.Resume {
		t.Fatalf("next for a finished plan must not resume, got %+v err=%v", dec, err)
	}
	if verdictCriteriaKeys(t, h)["next"] {
		t.Fatal("next must not be offered for a session whose own plan is finished")
	}
}

// A corrupt plan file gates next closed instead of panicking or resuming.
func TestAutoContinueNextGateCorruptPlanClosed(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".ocode", "todo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ses-bad.md"), []byte("- [ ] no header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, h := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply("next", 0.9, "finished"))
	a.SetWorkDir(root)
	a.SetSessionID("ses-bad")
	dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "Item 1 done."}}, nil, nil)
	if err != nil || dec.Resume {
		t.Fatalf("next over a corrupt plan must not resume, got %+v err=%v", dec, err)
	}
	if verdictCriteriaKeys(t, h)["next"] {
		t.Fatal("next must not be offered over a corrupt plan")
	}
}

// Every resume-class action carries its fixed prompt through the decision.
func TestAutoContinueResumeActionsCarryPrompt(t *testing.T) {
	for _, key := range []string{"continue", "retry", "fix_errors", "verify", "review_first", "write_plan", "summarize"} {
		a, _ := newAutoContinueTypesafeJudge(t, autoContinueVerdictReply(key, 0.9, "mid_task"))
		dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "tests failed, stopping here"}}, nil, nil)
		if err != nil || !dec.Resume {
			t.Fatalf("%s should resume, got %+v err=%v", key, dec, err)
		}
		act, _ := autoContinueActionByKey(key)
		if dec.Prompt != act.Prompt || dec.Prompt == "" {
			t.Fatalf("%s prompt = %q, want %q", key, dec.Prompt, act.Prompt)
		}
	}
}

// The chat judge returns an action key; punctuation and trailing prose are
// tolerated, the answer class is never offered, and an unknown word ends.
func TestAutoContinueChatJudgeActionKeyword(t *testing.T) {
	run := func(response string, prior []string) (AutoContinueDecision, string) {
		prev := newClientFn
		t.Cleanup(func() { newClientFn = prev })
		cap := &scriptedCaptureClient{Responses: []string{response}}
		newClientFn = func(_ *config.Config, _ string) LLMClient { return cap }
		cfg := &config.Config{}
		cfg.Ocode.AutoContinueModel = "opencode-go/gpt-x"
		cfg.Ocode.AutoContinueEnabled = true
		a := newTestAgent(nil, nil, cfg, nil)
		dec, err := a.AutoContinueJudgeSync([]Message{{Role: "assistant", Content: "FAIL: TestX ... exit status 1"}}, nil, prior)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return dec, cap.Prompts[0]
	}

	dec, prompt := run("fix_errors. The tests are red.", nil)
	if !dec.Resume || dec.Action != "fix_errors" {
		t.Fatalf("expected fix_errors resume, got %+v", dec)
	}
	act, _ := autoContinueActionByKey("fix_errors")
	if dec.Prompt != act.Prompt {
		t.Fatalf("prompt = %q", dec.Prompt)
	}
	for _, forbidden := range []string{"- proceed:", "- skip:", "- reject:"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("chat judge must not be offered the answer class, prompt has %q", forbidden)
		}
	}
	if !strings.Contains(prompt, "- await_user:") || !strings.Contains(prompt, "- continue:") {
		t.Fatalf("chat judge prompt should list the resume/end actions:\n%s", prompt)
	}

	dec, _ = run("YES", nil)
	if dec.Resume {
		t.Fatalf("a bare YES is not an action key and must not resume: %+v", dec)
	}
	if !strings.Contains(dec.Detail, "unknown action") {
		t.Fatalf("detail: %q", dec.Detail)
	}

	dec, _ = run("await_user", nil)
	if dec.Resume || dec.Action != "await_user" {
		t.Fatalf("await_user should end: %+v", dec)
	}

	dec, prompt = run("verify", []string{"verify"})
	if dec.Resume {
		t.Fatalf("a once-per-chain action already fired must not resume again: %+v", dec)
	}
	if !strings.Contains(prompt, "already fired in this chain: verify") {
		t.Fatalf("chat judge should be told what already fired:\n%s", prompt)
	}
}
