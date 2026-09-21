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
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
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
	as := &agentSession{agent: a, model: "fake-model"}
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
const awaitingUserVerdictJSON = `{"model":"jev-latest","answers":{"verdict":{"type":"choice","choice":"continue","probabilities":{"continue":0.8,"end":0.2},"confidence":0.95},"reason":{"type":"choice","choice":"awaiting_user","probabilities":{},"confidence":0.9}},"usage":{"input_tokens":5,"output_tokens":1}}`

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
		if m.Role == "user" && m.Content == "The step limit has been reset — you may use tools again. Continue the task from where you left off; do not just repeat any previous summary." {
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
		if m.Role == "user" && m.Content == "Continue the task from where you left off; do not just repeat any previous summary." {
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
		if m.Role == "user" && m.Content == "Continue the task from where you left off; do not just repeat any previous summary." {
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
		if m.Role == "user" && m.Content == "Continue the task from where you left off; do not just repeat any previous summary." {
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

	should, _ := h.autoContinueShouldResume(id, as, 0)
	if should {
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

	if _, ok := h.fireAutoContinue(id, as, true); !ok {
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
