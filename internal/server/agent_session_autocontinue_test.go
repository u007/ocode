package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// stepLimitClient forces the agent's stepLimitHit flag on its first N Chat
// rounds, so a runTurn hits the /max-step summary path without needing 300
// real tool rounds. The caller rewrites the flag via SetMaxSteps + a tiny
// limit; simpler: the test agent gets maxSteps=1, which makes Step() hit the
// cap on i>=1 — i.e. the very first round forces the summarize branch and
// sets stepLimitHit.
type scriptedAutoContinueClient struct {
	mu             sync.Mutex
	calls          int
	bodies         []string
	handler        func(call int) string
	toolCallsUntil int // first N rounds answer with a tool_call (drives the Step loop)
	// roleSeqs records the roles of the input messages for each Chat call. A
	// resumed Step must receive the previous Step's rows, so a call that runs
	// after a resume must carry a tool row; before that regression was fixed
	// the resumed input was base + resume hint only (no assistant/tool rows).
	roleSeqs [][]string
}

func (c *scriptedAutoContinueClient) Chat(messages []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	roles := make([]string, 0, len(messages))
	for _, m := range messages {
		roles = append(roles, m.Role)
	}
	c.roleSeqs = append(c.roleSeqs, roles)
	c.mu.Unlock()
	if len(messages) > 0 {
		c.mu.Lock()
		c.bodies = append(c.bodies, messages[len(messages)-1].Content)
		c.mu.Unlock()
	}
	content := "final answer"
	if c.handler != nil {
		content = c.handler(n)
	}
	if c.toolCallsUntil > 0 && n <= c.toolCallsUntil {
		tc := agent.ToolCall{ID: "call_test", Type: "function"}
		tc.Function.Name = "bash"
		tc.Function.Arguments = "{}"
		return &agent.Message{Role: "assistant", Content: "", ToolCalls: []agent.ToolCall{tc}}, nil
	}
	return &agent.Message{Role: "assistant", Content: content}, nil
}
func (c *scriptedAutoContinueClient) GetProvider() string { return "fake" }
func (c *scriptedAutoContinueClient) GetModel() string    { return "fake-model" }

// autoContinueTestServer wires a handler whose judge model points at the
// given fake /systemone server ("" disables the judge). Callers set the
// agent's max steps and the scripted client behaviour per test.
func autoContinueTestServer(t *testing.T, sysoneURL string) (*Handler, string, *scriptedAutoContinueClient) {
	t.Helper()
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	cfg := &config.Config{}
	cfg.Ocode.MaxSteps = 1
	cfg.Ocode.AutoContinueEnabled = true
	if sysoneURL != "" {
		cfg.Ocode.AutoContinueModel = "typesafe/jev-latest"
		// Point the typesafe provider's base URL at the test stub (the same
		// override path config.Provider gives real deployments).
		cfg.Provider = map[string]interface{}{
			"typesafe": map[string]interface{}{
				"options": map[string]interface{}{"baseURL": sysoneURL},
			},
		}
	}
	h.mu.Lock()
	h.cfg = cfg
	h.mu.Unlock()

	cl := &scriptedAutoContinueClient{}
	a := agent.NewAgent(cl, nil, cfg, nil)
	a.SetWorkDir(proj)
	as := &agentSession{agent: a, model: "fake-model", credVersion: auth.CredentialVersion()}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()
	return h, id, cl
}

// newSystemoneStub spins a /systemone endpoint answering with the given
// verdict JSON and recording request bodies. It also sets a dummy
// TYPESAFE_API_KEY so the client factory builds the (stub-pointed) client.
func newSystemoneStub(t *testing.T, reply string) (string, func() []map[string]any) {
	t.Helper()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), bodies...)
	}
}

const continueVerdictJSON = `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"continue","probabilities":{"continue":0.5,"end":0.5},"confidence":0.95},"reason":{"type":"choice","choice":"truncated","probabilities":{},"confidence":0.8}},"usage":{"input_tokens":5,"output_tokens":1}}`

const endVerdictJSON = `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"end","probabilities":{"continue":0.1,"end":0.9},"confidence":0.95},"reason":{"type":"choice","choice":"finished","probabilities":{},"confidence":0.8}},"usage":{"input_tokens":5,"output_tokens":1}}`

// awaitingUserVerdictJSON is the contradictory shape the veto exists for: a
// high-confidence "continue" verdict with an awaiting_user reason (the reply
// ends by asking the user a question or requesting feedback).
const awaitingUserVerdictJSON = `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"continue","probabilities":{"continue":0.8,"end":0.2},"confidence":0.95},"reason":{"type":"choice","choice":"awaiting_decision","probabilities":{},"confidence":0.9}},"usage":{"input_tokens":5,"output_tokens":1}}`

// TestRunTurnAutoContinueStepLimitResumes covers the hard signal: with
// maxSteps=1 every Step round trips the cap, and an enabled auto-continue
// keeps resuming until the chain cap, appending the resume prompt as a real
// user message.
func TestRunTurnAutoContinueStepLimitResumes(t *testing.T) {
	h, id, cl := autoContinueTestServer(t, "")
	// Round 1 answers with a tool_call so the Step loop advances to i=1,
	// where the maxSteps=1 cap trips and forces the summarize branch
	// (stepLimitHit=true). The summary answer then completes the round.
	cl.toolCallsUntil = 1
	cl.handler = func(call int) string {
		return "summary of round"
	}

	_, err := h.runTurn(id, h.lookupAgentSession(id), "do the work", turnOptions{})
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	cl.mu.Lock()
	calls := cl.calls
	cl.mu.Unlock()
	// 1 initial + (cap-1) auto-resumed rounds, then the cap declines and the
	// loop exits (each resume consumes one chain slot; the resumed Step also
	// hits the cap, so rounds = cap... the resumed round after the last
	// resume still runs and hits the cap again but must not resume).
	if calls < 2 {
		t.Fatalf("expected auto-continue to resume at least once, got %d Chat calls", calls)
	}

	as := h.lookupAgentSession(id)
	sawResume := false
	for _, m := range as.messages {
		if m.Role == "user" && m.Content == agent.AutoContinueStepLimitPrompt {
			sawResume = true
		}
	}
	if !sawResume {
		t.Fatalf("resume prompt not found in transcript: %+v", as.messages)
	}

	// Regression: a resumed Step's LLM input must carry the rows the previous
	// Step produced. Round 1 answers with a tool_call, so any Chat call made by
	// a resumed Step must include a tool row. Before the fix the resumed slice
	// was base + resume hint only, and only the first Step's own summarize call
	// ever carried a tool row (exactly one such call).
	cl.mu.Lock()
	var withToolRow int
	for _, roles := range cl.roleSeqs {
		for _, r := range roles {
			if r == "tool" {
				withToolRow++
				break
			}
		}
	}
	seqs := append([][]string(nil), cl.roleSeqs...)
	cl.mu.Unlock()
	if withToolRow < 2 {
		t.Fatalf("resumed Step did not receive the previous step's rows: %d of %d Chat calls carried a tool row (want >=2); roleSeqs=%v", withToolRow, len(seqs), seqs)
	}
}

// TestRunTurnAutoContinueDisabledStopsImmediately covers the off toggle: a
// step-limited turn ends after the single Step with a visible end-of-turn
// notice in the transcript (not silent).
func TestRunTurnAutoContinueDisabledStopsImmediately(t *testing.T) {
	h, id, cl := autoContinueTestServer(t, "")
	h.mu.Lock()
	h.cfg.Ocode.AutoContinueEnabled = false
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	cl.toolCallsUntil = 1 // round 2 trips the maxSteps=1 cap

	_, err := h.runTurn(id, as, "do the work", turnOptions{})
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	// The step-limit end-of-turn notice must be present as a UI-only message.
	found := false
	for _, m := range as.messages {
		if m.Role == "assistant" && m.Notice != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a transcript notice, got %+v", as.messages)
	}
}

// TestRunTurnAutoContinueTypesafeTriageNaturalStop covers the Jev triage on a
// NATURALLY ended turn (no step limit): the judge says continue → the turn
// resumes with the plain continue prompt and the triage detail is recorded.
func TestRunTurnAutoContinueTypesafeTriageNaturalStop(t *testing.T) {
	url, bodies := newSystemoneStub(t, continueVerdictJSON)
	h, id, cl := autoContinueTestServer(t, url)
	// maxSteps=1 forces the FIRST round to hit the cap... to test the natural
	// path instead, give unlimited steps: the first Chat answers normally.
	h.mu.Lock()
	h.cfg.Ocode.MaxSteps = 0
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	as.agent.SetMaxSteps(0)
	cl.handler = func(call int) string { return "complete reply" }

	_, err := h.runTurn(id, as, "hello", turnOptions{})
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	if got := len(bodies()); got == 0 {
		t.Fatal("triage judge was never consulted")
	}
	// The resume prompt must be the plain (non-step-limit) wording.
	found := false
	for _, m := range as.messages {
		if m.Role == "user" && m.Content == agent.AutoContinueResumePrompt {
			found = true
		}
	}
	if !found {
		t.Fatal("judge-approved resume prompt not found")
	}
}

// TestRunTurnAutoContinueTypesafeEndVerdictNoResume: the judge says the reply
// finished → no resume, and the triage outcome lands in the transcript.
func TestRunTurnAutoContinueTypesafeEndVerdictNoResume(t *testing.T) {
	url, bodies := newSystemoneStub(t, endVerdictJSON)
	h, id, cl := autoContinueTestServer(t, url)
	h.mu.Lock()
	h.cfg.Ocode.MaxSteps = 0
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	as.agent.SetMaxSteps(0)
	cl.handler = func(call int) string { return "all done" }

	_, err := h.runTurn(id, as, "hello", turnOptions{})
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if len(bodies()) == 0 {
		t.Fatal("triage judge was never consulted")
	}
	sawNotice := false
	for _, m := range as.messages {
		if m.Role == "assistant" && m.Notice != "" {
			sawNotice = true
		}
	}
	if !sawNotice {
		t.Fatal("expected the triage outcome notice in the transcript")
	}
	// No resume prompt may exist.
	for _, m := range as.messages {
		if m.Role == "user" && m.Content == agent.AutoContinueResumePrompt {
			t.Fatal("resume prompt must not exist after an end verdict")
		}
	}
}

// TestRunTurnAutoContinueAwaitingUserVetoesContinue: a reply that ends by
// asking the user a question must not be auto-resumed, even when the triage
// verdict is a high-confidence "continue" — the typed awaiting_user reason
// vetoes it. The turn ends with an outcome notice and no resume prompt.
func TestRunTurnAutoContinueAwaitingUserVetoesContinue(t *testing.T) {
	url, bodies := newSystemoneStub(t, awaitingUserVerdictJSON)
	h, id, cl := autoContinueTestServer(t, url)
	h.mu.Lock()
	h.cfg.Ocode.MaxSteps = 0
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	as.agent.SetMaxSteps(0)
	cl.handler = func(call int) string { return "Should I use Redis or an in-process LRU?" }

	_, err := h.runTurn(id, as, "add caching", turnOptions{})
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if len(bodies()) == 0 {
		t.Fatal("triage judge was never consulted")
	}
	for _, m := range as.messages {
		if m.Role == "user" && m.Content == agent.AutoContinueResumePrompt {
			t.Fatalf("resume prompt must not exist when the reply is awaiting the user; messages=%v", as.messages)
		}
	}
}

// TestRunTurnAutoContinuePermissionAskBlocksResume: a step-limited turn that
// paused on an unresolved permission ask must NOT auto-continue.
func TestRunTurnAutoContinuePermissionAskBlocksResume(t *testing.T) {
	h, id, cl := autoContinueTestServer(t, "")
	// First round trips the cap AND leaves a permission sentinel... simulate
	// by having the handler return a summary, then append a sentinel to the
	// transcript before the resume check runs. Simplest deterministic setup:
	// run the loop decision directly.
	h.mu.Lock()
	h.cfg.Ocode.AutoContinueEnabled = true
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	as.agent.SetMaxSteps(0)
	cl.handler = func(call int) string { return "done" }
	as.messages = append(as.messages, agent.Message{Role: "tool", ToolID: "t1", Content: toolSentinelPermissionAsk()})

	fire, _ := h.autoContinueShouldResume(id, as, 0, nil)
	if fire.Prompt != "" {
		t.Fatal("pending permission ask must block auto-continue")
	}
}

// toolSentinelPermissionAsk builds a sentinel-prefixed tool result (the
// exact shape tailIsPermissionAsk looks for).
func toolSentinelPermissionAsk() string {
	return "PERMISSION_ASK:{\"tool\":\"bash\",\"reason\":\"test\"}"
}

// TestRunTurnReplyIsCurrentTurnOnly pins the synchronous reply contract: the
// string runTurn returns to POST /api/chat callers must be this turn's
// assistant text, not the whole session transcript. Building it from all of
// as.messages made the reply grow with every prior turn.
func TestRunTurnReplyIsCurrentTurnOnly(t *testing.T) {
	h, id, cl := autoContinueTestServer(t, "")
	h.mu.Lock()
	h.cfg.Ocode.MaxSteps = 0
	h.cfg.Ocode.AutoContinueEnabled = false
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	as.agent.SetMaxSteps(0)
	// A prior turn's assistant text already in the transcript must never leak
	// into this turn's synchronous reply.
	as.messages = append(as.messages, agent.Message{Role: "assistant", Content: "PRIOR-TURN-LEAK"})
	cl.handler = func(int) string { return "current reply" }

	reply, err := h.runTurn(id, as, "question", turnOptions{})
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if strings.Contains(reply, "PRIOR-TURN-LEAK") {
		t.Fatalf("reply leaked prior-turn text: %q", reply)
	}
	if !strings.Contains(reply, "current reply") {
		t.Fatalf("reply = %q, want it to contain the current turn's text", reply)
	}
}

// TestAutoContinueMidChainRowsReachLivePersist pins the reconcile
// precondition for issue 3: the mid-chain notice and resume prompt appended
// directly to as.messages must also be mirrored into the live-persist view.
// Without the liveAppend hook the on-disk view lacks these rows, so a
// concurrent-writer rebase at turn end treats the transcript suffix as
// non-prefix and duplicates/reorders rows.
func TestAutoContinueMidChainRowsReachLivePersist(t *testing.T) {
	h, id, cl := autoContinueTestServer(t, "")
	as := h.lookupAgentSession(id)
	as.agent.SetMaxSteps(0)
	cl.handler = func(int) string { return "ok" }

	var live []agent.Message
	as.liveAppend = func(m agent.Message) { live = append(live, m) }

	if _, ok := h.fireAutoContinue(id, as, true, autoContinueFire{Action: "continue", Prompt: agent.AutoContinueResumePrompt, Reason: "test"}); !ok {
		t.Fatal("fireAutoContinue did not fire")
	}

	if len(live) != 2 {
		t.Fatalf("live view got %d rows, want 2 (notice + resume prompt): %+v", len(live), live)
	}
	if live[0].Notice == "" {
		t.Fatalf("live view row 0 is not the notice: %+v", live[0])
	}
	if live[1].Role != "user" {
		t.Fatalf("live view row 1 is not the resume prompt: %+v", live[1])
	}
	// The live view must be the same tail as the transcript, in the same order.
	tail := as.messages[len(as.messages)-2:]
	if tail[0].Notice != live[0].Notice || tail[1].Content != live[1].Content || tail[1].UserSeq != live[1].UserSeq {
		t.Fatalf("live view diverged from transcript tail: live=%+v transcript=%+v", live, tail)
	}
}

// proceedConfirmationVerdictJSON: the judge answers a plain go-ahead question
// on the user's behalf (verdict proceed + reason awaiting_confirmation).
const proceedConfirmationVerdictJSON = `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"proceed","probabilities":{"proceed":0.9,"end":0.1},"confidence":0.92},"reason":{"type":"choice","choice":"awaiting_confirmation","probabilities":{},"confidence":0.9}},"usage":{"input_tokens":5,"output_tokens":1}}`

// TestRunTurnAutoContinueConfirmationProceeds: a reply that asks "shall I
// proceed?" on an already-described plan is answered with the fixed proceed
// prompt, the chain records the action, and the transcript hint names it.
func TestRunTurnAutoContinueConfirmationProceeds(t *testing.T) {
	url, bodies := newSystemoneStub(t, proceedConfirmationVerdictJSON)
	h, id, cl := autoContinueTestServer(t, url)
	h.mu.Lock()
	h.cfg.Ocode.MaxSteps = 0
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	as.agent.SetMaxSteps(0)
	cl.handler = func(call int) string {
		if call == 1 {
			return "Plan: add an LRU in internal/users/cache.go with a unit test. Shall I proceed?"
		}
		return "Done — cache added, tests pass."
	}

	_, err := h.runTurn(id, as, "add caching", turnOptions{})
	if err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if len(bodies()) == 0 {
		t.Fatal("triage judge was never consulted")
	}
	proceeds, hints := 0, 0
	for _, m := range as.messages {
		if m.Role == "user" && m.Content == "Yes, proceed as described." {
			proceeds++
		}
		if m.Notice != "" && strings.Contains(m.Notice, "chose proceed") {
			hints++
		}
		if m.Role == "user" && m.Content == agent.AutoContinueResumePrompt {
			t.Fatalf("a proceed verdict must send the proceed prompt, not the generic continue: %v", as.messages)
		}
	}
	if proceeds == 0 || hints == 0 {
		t.Fatalf("expected the proceed prompt and its hint in the transcript (proceeds=%d hints=%d): %+v", proceeds, hints, as.messages)
	}
	// Later judge calls are told what already fired in the chain.
	last := bodies()[len(bodies())-1]
	state, _ := last["state"].(map[string]any)
	fired, _ := state["chain_actions_fired"].([]any)
	if len(bodies()) > 1 && len(fired) == 0 {
		t.Fatalf("state.chain_actions_fired should list the earlier proceed, got %v", state["chain_actions_fired"])
	}
}

// questionAskThenReplyClient raises one structured question (Postgres or SQLite)
// on its first round and replies in plain text after that. The question tool
// result it leaves behind is the ask the auto-continue question judge answers.
type questionAskThenReplyClient struct {
	mu    sync.Mutex
	calls int
}

func (c *questionAskThenReplyClient) Chat(_ []agent.Message, tools []map[string]interface{}) (*agent.Message, error) {
	if len(tools) == 0 {
		// The session title generator calls the same client without a tool list.
		// Only the turn's own calls carry the tools, so these are not counted.
		return &agent.Message{Role: "assistant", Content: "Database for the store"}, nil
	}
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.mu.Unlock()
	if n == 1 {
		tc := agent.ToolCall{ID: "q-call-db", Type: "function"}
		tc.Function.Name = "question"
		tc.Function.Arguments = `{"questions":[{"header":"Database","question":"Which database?","options":[{"label":"Postgres","description":"Postgres server"},{"label":"SQLite","description":"Embedded file"}]}]}`
		return &agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}}, nil
	}
	return &agent.Message{Role: "assistant", Content: "Using Postgres for the store."}, nil
}
func (c *questionAskThenReplyClient) GetProvider() string { return "fake" }
func (c *questionAskThenReplyClient) GetModel() string    { return "fake-model" }

func (c *questionAskThenReplyClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// questionDeterminedVerdictJSON is Jev's reply when the transcript settles the
// database choice. The same reply carries a triage verdict of end, which the
// triage judge reads after the answered question resumes the turn.
const questionDeterminedVerdictJSON = `{"model":"jev-latest","answers":{"q0_owner":{"type":"choice","choice":"determined_by_transcript","probabilities":{"determined_by_transcript":0.95,"user_preference":0.05},"confidence":0.95},"q0_answer":{"type":"choice","choice":"opt0","probabilities":{"opt0":0.92,"opt1":0.05,"defer_to_user":0.03},"confidence":0.92},"verdict":{"type":"choice","choice":"end","probabilities":{"continue":0.1,"end":0.9},"confidence":0.95},"reason":{"type":"choice","choice":"finished","probabilities":{},"confidence":0.8}},"usage":{"input_tokens":5,"output_tokens":1}}`

// questionPreferenceVerdictJSON is Jev's reply when the database choice is a
// preference the user never stated, so the question must go back to the user.
const questionPreferenceVerdictJSON = `{"model":"jev-latest","answers":{"q0_owner":{"type":"choice","choice":"user_preference","probabilities":{"determined_by_transcript":0.05,"user_preference":0.95},"confidence":0.95},"q0_answer":{"type":"choice","choice":"defer_to_user","probabilities":{"postgres":0.04,"sqlite":0.04,"defer_to_user":0.92},"confidence":0.92},"verdict":{"type":"choice","choice":"end","probabilities":{"continue":0.1,"end":0.9},"confidence":0.95},"reason":{"type":"choice","choice":"finished","probabilities":{},"confidence":0.8}},"usage":{"input_tokens":5,"output_tokens":1}}`

// swapToQuestionAgent replaces the agent autoContinueTestServer built with one
// whose client can raise a question tool call. It keeps the judge config and
// the session's project root, and sets the auto-continue switch explicitly.
func swapToQuestionAgent(t *testing.T, h *Handler, id string, client agent.LLMClient, autoContinue bool) *agentSession {
	t.Helper()
	entry, ok := h.sessions.SnapshotEntry(id)
	if !ok {
		t.Fatalf("session %s is not registered", id)
	}
	h.mu.Lock()
	cfg := h.cfg
	cfg.Ocode.MaxSteps = 0
	h.mu.Unlock()
	ag := agent.NewAgent(client, []tool.Tool{&tool.QuestionTool{}}, cfg, nil)
	t.Cleanup(func() { ag.Shutdown() })
	ag.SetWorkDir(entry.ProjectRoot)
	ag.SetMaxSteps(0)
	ag.SetAutoContinueEnabled(autoContinue)
	as := h.lookupAgentSession(id)
	as.agent = ag
	return as
}

// questionToolRow returns the tool row that answers toolID, or nil.
func questionToolRow(msgs []agent.Message, toolID string) *agent.Message {
	for i := range msgs {
		if msgs[i].Role == "tool" && msgs[i].ToolID == toolID {
			return &msgs[i]
		}
	}
	return nil
}

// The transcript settles the database choice, so the question judge answers the
// pending question, the turn resumes from the answered tool result, and the
// answer is mirrored to disk.
func TestRunTurnAutoContinueAnswersDeterminedQuestion(t *testing.T) {
	url, bodies := newSystemoneStub(t, questionDeterminedVerdictJSON)
	h, id, _ := autoContinueTestServer(t, url)
	qc := &questionAskThenReplyClient{}
	as := swapToQuestionAgent(t, h, id, qc, true)
	entry, _ := h.sessions.SnapshotEntry(id)

	if _, err := h.runTurn(id, as, "use Postgres for the store", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	row := questionToolRow(as.messages, "q-call-db")
	if row == nil {
		t.Fatalf("question tool row missing from transcript: %+v", as.messages)
	}
	var sets []tool.QuestionAnswerSet
	if err := json.Unmarshal([]byte(row.Content), &sets); err != nil {
		t.Fatalf("question row is not an answer set: %v (content %q)", err, row.Content)
	}
	if len(sets) != 1 || len(sets[0].Answers) != 1 || sets[0].Answers[0].Label != "Postgres" {
		t.Fatalf("answer = %+v, want one answer labelled Postgres", sets)
	}
	if tailIsQuestionAsk(as.messages) {
		t.Fatal("question is still pending after the auto-answer")
	}
	announced := false
	for _, m := range as.messages {
		if strings.Contains(m.Notice, "answered on your behalf") {
			announced = true
		}
	}
	if !announced {
		t.Fatalf("no answered-on-your-behalf notice in transcript: %+v", as.messages)
	}
	if got := qc.callCount(); got != 2 {
		t.Fatalf("model calls = %d, want 2 (the ask, then the resumed step)", got)
	}

	if err := session.FlushForDir(entry.ProjectRoot, id, 10*time.Second); err != nil {
		t.Fatalf("flush session: %v", err)
	}
	saved, err := session.LoadForDir(entry.ProjectRoot, id)
	if err != nil {
		t.Fatalf("load persisted session: %v", err)
	}
	savedRow := questionToolRow(saved.Messages, "q-call-db")
	if savedRow == nil || savedRow.Content != row.Content {
		t.Fatalf("persisted question row = %+v, want the answer %q", savedRow, row.Content)
	}

	// The question judge was the first Jev call; the triage judge that followed
	// the resume is the second and must see the answer in the chain history.
	bs := bodies()
	if len(bs) < 2 {
		t.Fatalf("expected the question judge then the triage judge, got %d Jev calls", len(bs))
	}
	state, _ := bs[1]["state"].(map[string]any)
	fired, _ := state["chain_actions_fired"].([]any)
	if len(fired) != 1 || fired[0] != agent.AutoContinueAnswerQuestionAction {
		t.Fatalf("state.chain_actions_fired = %v, want [%s]", state["chain_actions_fired"], agent.AutoContinueAnswerQuestionAction)
	}
}

// A preference the user never stated goes back to the user. The turn stays on
// the open dialog: no second model call, and no transcript row appended after
// the ask, because the answer endpoint needs the ask as the last row.
func TestRunTurnAutoContinueDefersPreferenceQuestion(t *testing.T) {
	url, bodies := newSystemoneStub(t, questionPreferenceVerdictJSON)
	h, id, _ := autoContinueTestServer(t, url)
	qc := &questionAskThenReplyClient{}
	as := swapToQuestionAgent(t, h, id, qc, true)

	if _, err := h.runTurn(id, as, "pick a database for the store", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	if got := qc.callCount(); got != 1 {
		t.Fatalf("model calls = %d, want 1: a deferred question must not resume", got)
	}
	if got := len(bodies()); got != 1 {
		t.Fatalf("Jev calls = %d, want 1 (the question judge only)", got)
	}
	last := as.messages[len(as.messages)-1]
	if !isQuestionAsk(last.Content) || last.ToolID != "q-call-db" {
		t.Fatalf("the open ask must stay the last row, got role=%s notice=%q content=%q", last.Role, last.Notice, last.Content)
	}
	if !tailIsQuestionAsk(as.messages) {
		t.Fatalf("deferred question should stay open for the user: %+v", as.messages)
	}
}

// With auto-continue off but a /goal active, the question judge still runs: a
// goal loop stuck on a question is the case it exists for.
func TestRunTurnAutoContinueQuestionJudgeRunsUnderGoal(t *testing.T) {
	url, bodies := newSystemoneStub(t, questionPreferenceVerdictJSON)
	h, id, _ := autoContinueTestServer(t, url)
	qc := &questionAskThenReplyClient{}
	as := swapToQuestionAgent(t, h, id, qc, false)
	if err := as.agent.SetGoal("the store uses the chosen database"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}

	if _, err := h.runTurn(id, as, "pick a database for the store", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}
	bs := bodies()
	if len(bs) != 1 {
		t.Fatalf("Jev calls = %d, want 1 (the question judge under the goal)", len(bs))
	}
	questions, _ := bs[0]["questions"].(map[string]any)
	if _, ok := questions["q0_owner"]; !ok {
		t.Fatalf("the Jev call is not the question judge: questions = %v", bs[0]["questions"])
	}
	if !tailIsQuestionAsk(as.messages) {
		t.Fatal("deferred question should stay open under the goal too")
	}
}

// An auto-answered question must reach watchers as a messages frame carrying
// the rewritten tool row, sent right after question_resolved. Without it the
// web transcript keeps the unanswered question card until turn end.
func TestRunTurnAutoContinueAnswerBroadcastsRewrittenRow(t *testing.T) {
	url, _ := newSystemoneStub(t, questionDeterminedVerdictJSON)
	h, id, _ := autoContinueTestServer(t, url)
	qc := &questionAskThenReplyClient{}
	as := swapToQuestionAgent(t, h, id, qc, true)

	sub := h.subscribeHeadless()
	defer h.unsubscribeHeadless(sub)

	if _, err := h.runTurn(id, as, "use Postgres for the store", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	var events []SSEEvent
	for drained := false; !drained; {
		select {
		case ev := <-sub:
			events = append(events, ev)
		default:
			drained = true
		}
	}
	resolved := -1
	for i, ev := range events {
		if ev.Event == "question_resolved" {
			resolved = i
			break
		}
	}
	if resolved < 0 {
		t.Fatalf("no question_resolved event; events = %+v", events)
	}
	var frame []agent.Message
	for _, ev := range events[resolved+1:] {
		if ev.Event != "messages" {
			continue
		}
		msgs, ok := ev.Data.([]agent.Message)
		if !ok {
			t.Fatalf("messages event data is %T, want []agent.Message", ev.Data)
		}
		frame = msgs
		break
	}
	if len(frame) == 0 {
		t.Fatalf("no messages event after question_resolved; events = %+v", events)
	}
	tail := frame[len(frame)-1]
	if tail.Role != "tool" || tail.ToolID != "q-call-db" {
		t.Fatalf("messages frame tail = role %s tool id %s, want the question tool row", tail.Role, tail.ToolID)
	}
	if isQuestionAsk(tail.Content) {
		t.Fatalf("messages frame tail still carries the unanswered question: %q", tail.Content)
	}
	if tail.Content != questionToolRow(as.messages, "q-call-db").Content {
		t.Fatalf("messages frame tail = %q, want the answered row", tail.Content)
	}
}
