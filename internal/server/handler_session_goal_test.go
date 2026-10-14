package server

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// goalTurnClient answers odd Chat calls with a tool call and even ones with
// text, so every Step ends on a tool round. A text-only turn three times
// running stalls the goal before the judge is ever asked.
type goalTurnClient struct {
	mu       sync.Mutex
	calls    int
	toolName string
}

func (c *goalTurnClient) Chat(_ []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.mu.Unlock()
	if n%2 == 1 {
		tc := agent.ToolCall{ID: fmt.Sprintf("call_%d", n), Type: "function"}
		tc.Function.Name = c.toolName
		tc.Function.Arguments = "{}"
		return &agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{tc}}, nil
	}
	return &agent.Message{Role: "assistant", Content: "working on it"}, nil
}
func (c *goalTurnClient) GetProvider() string { return "fake" }
func (c *goalTurnClient) GetModel() string    { return "fake-model" }

// goalAskTool answers like a tool that needs the user's permission: its result
// is the PERMISSION_ASK sentinel, so the turn pauses on a pending ask.
type goalAskTool struct{}

func (goalAskTool) Name() string        { return "goal_ask" }
func (goalAskTool) Description() string { return "test tool that asks the user for permission" }
func (goalAskTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "goal_ask",
			"description": "test tool that asks the user for permission",
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
}
func (goalAskTool) Execute(json.RawMessage) (string, error) {
	return tool.SentinelPermissionAsk + `{"tool_name":"goal_ask"}`, nil
}
func (goalAskTool) Parallel() bool { return false }

// goalVerdictReply is a /systemone answer for the goal judge's verdict and
// reason questions, in the same wire shape the typesafe client parses.
func goalVerdictReply(t *testing.T, verdict string, confidence float64, reason string) string {
	t.Helper()
	pMet := 0.1
	if verdict == "met" {
		pMet = 0.9
	}
	raw, err := json.Marshal(map[string]any{
		"model": "jev-latest",
		"answers": map[string]any{
			"verdict": map[string]any{
				"type":          "choice",
				"choice":        verdict,
				"probabilities": map[string]float64{"met": pMet, "not_met": 1 - pMet},
				"confidence":    confidence,
			},
			"reason": map[string]any{
				"type":          "choice",
				"choice":        reason,
				"probabilities": map[string]float64{},
				"confidence":    0.8,
			},
		},
		"usage": map[string]any{"input_tokens": 5, "output_tokens": 1},
	})
	if err != nil {
		t.Fatalf("marshal verdict reply: %v", err)
	}
	return string(raw)
}

// newScriptedJudgeStub serves /systemone replies in order, repeating the last
// one, and reports how many requests it has answered.
func newScriptedJudgeStub(t *testing.T, replies ...string) (string, func() int) {
	t.Helper()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	var mu sync.Mutex
	var served int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reply := replies[min(served, len(replies)-1)]
		served++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() int {
		mu.Lock()
		defer mu.Unlock()
		return served
	}
}

// newGoalTestServer builds a handler whose session has a live agent running cl
// with tools, and whose goal judge (and triage judge, when enabled) points at
// judgeURL. The session exists on disk so metadata writes have a file.
func newGoalTestServer(t *testing.T, cl agent.LLMClient, tools []tool.Tool, judgeURL string) (*Handler, string, string) {
	t.Helper()
	h := NewHandler()
	h.turnHeartbeatInterval = time.Hour
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	// Empty on disk: a seeded row would make the turn-end reconcile see a
	// diverged base and re-sync memory to disk, dropping the turn under test.
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	if err := session.Save(id, "goal session", nil, nil); err != nil {
		t.Fatalf("save empty session %s: %v", id, err)
	}

	cfg := &config.Config{}
	cfg.Ocode.Goal.JudgeModel = "typesafe/jev-latest"
	cfg.Provider = map[string]interface{}{
		"typesafe": map[string]interface{}{
			"options": map[string]interface{}{"baseURL": judgeURL},
		},
	}
	h.mu.Lock()
	h.cfg = cfg
	h.mu.Unlock()

	a := agent.NewAgent(cl, tools, cfg, nil)
	a.SetWorkDir(proj)
	a.SetMaxSteps(0)
	as := &agentSession{agent: a, model: "fake-model", credVersion: auth.CredentialVersion()}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()
	return h, id, proj
}

// persistedGoal reads the goal metadata from disk, the way a restart would.
func persistedGoal(t *testing.T, proj, id string) (agent.GoalState, bool) {
	t.Helper()
	s, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load session %s: %v", id, err)
	}
	return agent.GoalStateFromMetadata(s.Metadata)
}

func goalResumeCount(msgs []agent.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "user" && strings.HasPrefix(m.Content, "[ocode:goal]") {
			n++
		}
	}
	return n
}

// TestRunTurnGoalLoopResumesUntilMet: a not_met verdict resumes the turn with a
// goal message, and the met verdict ends it. Each verdict is a notice and the
// goal is persisted as met.
func TestRunTurnGoalLoopResumesUntilMet(t *testing.T) {
	url, judged := newScriptedJudgeStub(t,
		goalVerdictReply(t, "not_met", 0.9, "work_remains"),
		goalVerdictReply(t, "not_met", 0.9, "work_remains"),
		goalVerdictReply(t, "met", 0.9, "all_satisfied"),
	)
	cl := &goalTurnClient{toolName: "bash"}
	h, id, proj := newGoalTestServer(t, cl, nil, url)
	as := h.lookupAgentSession(id)
	if err := as.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}

	if _, err := h.runTurn(id, as, "make the tests pass", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	if got := judged(); got != 3 {
		t.Fatalf("goal judge consulted %d times, want 3", got)
	}
	if got := goalResumeCount(as.messages); got != 2 {
		t.Fatalf("[ocode:goal] resume messages = %d, want 2", got)
	}
	cl.mu.Lock()
	steps := cl.calls / 2
	cl.mu.Unlock()
	if steps != 3 {
		t.Fatalf("Step rounds = %d, want 3", steps)
	}
	lastNotice := ""
	for _, m := range as.messages {
		if m.Role == "assistant" && m.Notice != "" {
			lastNotice = m.Notice
		}
	}
	if !strings.Contains(lastNotice, "met") {
		t.Fatalf("final notice = %q, want it to report met", lastNotice)
	}
	gs, ok := persistedGoal(t, proj, id)
	if !ok || gs.Status != agent.GoalMet {
		t.Fatalf("persisted goal = %+v ok=%v, want status met", gs, ok)
	}
}

// TestRunTurnGoalPausedOnPendingAsk: a turn that ends on a permission ask must
// not reach the goal judge and must not resume.
func TestRunTurnGoalPausedOnPendingAsk(t *testing.T) {
	url, judged := newScriptedJudgeStub(t, goalVerdictReply(t, "not_met", 0.9, "work_remains"))
	cl := &goalTurnClient{toolName: "goal_ask"}
	h, id, _ := newGoalTestServer(t, cl, []tool.Tool{goalAskTool{}}, url)
	as := h.lookupAgentSession(id)
	if err := as.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}

	if _, err := h.runTurn(id, as, "run the migration", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	if !tailIsPermissionAsk(as.messages) {
		t.Fatalf("turn did not end on a permission ask; tail=%+v", as.messages[len(as.messages)-1])
	}
	if got := judged(); got != 0 {
		t.Fatalf("goal judge consulted %d times behind a pending ask, want 0", got)
	}
	if got := goalResumeCount(as.messages); got != 0 {
		t.Fatalf("goal resumed behind a pending ask: %d resume messages", got)
	}
}

// TestRunTurnGoalSkipsAutoContinueJudge: with a live goal the triage judge
// stays silent for the turn. Only the goal judge is consulted.
func TestRunTurnGoalSkipsAutoContinueJudge(t *testing.T) {
	url, judged := newScriptedJudgeStub(t, goalVerdictReply(t, "met", 0.9, "all_satisfied"))
	cl := &goalTurnClient{toolName: "bash"}
	h, id, _ := newGoalTestServer(t, cl, nil, url)
	h.mu.Lock()
	h.cfg.Ocode.AutoContinueEnabled = true
	h.cfg.Ocode.AutoContinueModel = "typesafe/jev-latest"
	h.mu.Unlock()
	as := h.lookupAgentSession(id)
	if err := as.agent.SetGoal("all tests pass"); err != nil {
		t.Fatalf("SetGoal: %v", err)
	}

	if _, err := h.runTurn(id, as, "make the tests pass", turnOptions{}); err != nil {
		t.Fatalf("runTurn: %v", err)
	}

	if got := judged(); got != 1 {
		t.Fatalf("judge consulted %d times, want exactly 1 (the goal judge)", got)
	}
	gs, ok := as.agent.Goal()
	if !ok || gs.Status != agent.GoalMet {
		t.Fatalf("goal = %+v ok=%v, want met from the goal judge", gs, ok)
	}
}

func goalEndpointBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return body
}

func putSessionGoal(t *testing.T, h *Handler, id, condition string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"condition": condition})
	rec := httptest.NewRecorder()
	h.HandleSetSessionGoal(rec, httptest.NewRequest("PUT", "/api/sessions/"+id+"/goal", bytes.NewReader(raw)), id)
	return rec
}

func getSessionGoal(t *testing.T, h *Handler, id string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HandleGetSessionGoal(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/goal", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET goal: %d (%s)", rec.Code, rec.Body.String())
	}
	return goalEndpointBody(t, rec)
}

// TestSessionGoalEndpoints: PUT sets the goal and returns the kickoff prompt,
// GET reports it, DELETE clears it, and an overlong condition is a 400.
func TestSessionGoalEndpoints(t *testing.T) {
	h, id, _ := newGoalTestServer(t, &goalTurnClient{toolName: "bash"}, nil, "")

	rec := putSessionGoal(t, h, id, "all tests pass")
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT goal: %d (%s)", rec.Code, rec.Body.String())
	}
	put := goalEndpointBody(t, rec)
	if kick, _ := put["kickoff_prompt"].(string); !strings.Contains(kick, "all tests pass") {
		t.Fatalf("kickoff_prompt = %q, want it to carry the condition", kick)
	}

	got := getSessionGoal(t, h, id)
	if got["active"] != true || got["condition"] != "all tests pass" {
		t.Fatalf("GET after PUT = %v, want active with the condition", got)
	}

	del := httptest.NewRecorder()
	h.HandleClearSessionGoal(del, httptest.NewRequest("DELETE", "/api/sessions/"+id+"/goal", nil), id)
	if del.Code != http.StatusNoContent {
		t.Fatalf("DELETE goal: %d, want 204 (%s)", del.Code, del.Body.String())
	}
	if got := getSessionGoal(t, h, id); got["active"] != false || got["condition"] != "" {
		t.Fatalf("GET after DELETE = %v, want active=false and empty fields", got)
	}

	tooLong := putSessionGoal(t, h, id, strings.Repeat("x", agent.GoalMaxConditionLen+1))
	if tooLong.Code != http.StatusBadRequest {
		t.Fatalf("PUT 4001 chars: %d, want 400 (%s)", tooLong.Code, tooLong.Body.String())
	}
}

// TestSessionGoalPutReplacesPrior: a second PUT replaces the goal and starts it
// over (turns back to zero), and the persisted metadata carries the new condition.
func TestSessionGoalPutReplacesPrior(t *testing.T) {
	h, id, proj := newGoalTestServer(t, &goalTurnClient{toolName: "bash"}, nil, "")
	if rec := putSessionGoal(t, h, id, "first condition"); rec.Code != http.StatusOK {
		t.Fatalf("PUT first: %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := putSessionGoal(t, h, id, "second condition"); rec.Code != http.StatusOK {
		t.Fatalf("PUT second: %d (%s)", rec.Code, rec.Body.String())
	}

	got := getSessionGoal(t, h, id)
	if got["turns"] != float64(0) || got["condition"] != "second condition" {
		t.Fatalf("GET after replace = %v, want turns=0 and the new condition", got)
	}
	gs, ok := persistedGoal(t, proj, id)
	if !ok || gs.Condition != "second condition" || gs.Turns != 0 {
		t.Fatalf("persisted goal = %+v ok=%v, want the replacement", gs, ok)
	}
}

// TestBuildAgentSessionRestoresGoal: a goal persisted in session metadata comes
// back on the agent when the session is rebuilt (restart or profile reconcile).
func TestBuildAgentSessionRestoresGoal(t *testing.T) {
	credVersion := auth.CredentialVersion()
	t.Cleanup(func() { auth.SetCredentialVersionForTest(credVersion) })
	prevCred, hadPrev := auth.Get("opencode-go")
	t.Cleanup(func() {
		if hadPrev {
			_ = auth.Set("opencode-go", prevCred)
		} else {
			_ = auth.Remove("opencode-go")
		}
	})
	if err := auth.Set("opencode-go", auth.Credential{Kind: auth.KindAPIKey, Key: "goal-restore-key"}); err != nil {
		t.Fatalf("set credential: %v", err)
	}

	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	saveSessionToDir(t, proj, id)
	saved := agent.GoalState{Condition: "ship the release", Status: agent.GoalPaused, Turns: 4, NoToolTurns: 1, LastReason: "awaiting input", SetAt: time.Now().UTC().Truncate(time.Second)}
	if err := session.UpdateMetadataForDir(proj, id, func(md map[string]any) {
		for k, v := range agent.GoalStateToMetadata(saved) {
			md[k] = v
		}
	}); err != nil {
		t.Fatalf("persist goal: %v", err)
	}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	gs, ok := as.agent.Goal()
	if !ok || gs.Condition != "ship the release" || gs.Status != agent.GoalPaused || gs.Turns != 4 {
		t.Fatalf("restored goal = %+v ok=%v, want the persisted goal", gs, ok)
	}
}
