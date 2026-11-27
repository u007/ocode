package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/tool"
)

// parkedQuestionHarness is a handler with ONE session whose parked-question
// asker is installed exactly as buildAgentSession installs it. The asker is
// returned so a test can play the Claude Code bridge: it blocks until the
// user's decision arrives, as the real caller inside the turn does.
type parkedQuestionHarness struct {
	t         *testing.T
	h         *Handler
	as        *agentSession
	sessionID string
	asker     agent.QuestionAsker
	sub       chan SSEEvent
}

type parkedQuestionResult struct {
	answers   []tool.QuestionAnswerSet
	dismissed bool
}

func newParkedQuestionHarness(t *testing.T, timeout time.Duration) *parkedQuestionHarness {
	t.Helper()
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	const sessionID = "sess-parked-question"
	as := &agentSession{
		model:          "fake-model",
		credVersion:    auth.CredentialVersion(),
		childAsks:      newChildPermAsks(),
		childQuestions: newChildQuestionAsks(),
	}
	if timeout > 0 {
		as.childQuestions.timeout = timeout
	}
	as.agent = agent.NewAgent(&childAskClient{}, nil, nil, nil)
	asker := h.newServerQuestionAsker(sessionID, as)
	as.agent.SetQuestionAsker(asker)
	h.agents[sessionID] = as
	h.sessions.Register(sessionID, t.TempDir())
	h.sessions.setAgent(sessionID, as)
	sub := h.subscribeHeadless()
	t.Cleanup(func() { h.unsubscribeHeadless(sub) })
	return &parkedQuestionHarness{t: t, h: h, as: as, sessionID: sessionID, asker: asker, sub: sub}
}

// ask runs the asker on its own goroutine, the way the bridge blocks inside a
// turn. The returned channel receives the outcome once the asker returns. The
// cleanup sweeps the registry first, so a test that never resolves cannot
// block its own teardown on a parked caller.
func (harn *parkedQuestionHarness) ask(ctx context.Context) chan parkedQuestionResult {
	harn.t.Helper()
	out := make(chan parkedQuestionResult, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		answers, dismissed := harn.asker(ctx, []tool.QuestionPrompt{sampleQuestion()})
		out <- parkedQuestionResult{answers: answers, dismissed: dismissed}
	}()
	harn.t.Cleanup(func() {
		harn.as.childQuestions.denyAll()
		<-exited
	})
	waitForParkedQuestionLen(harn.t, harn.as, 1)
	return out
}

// requestID returns the id of the single parked question.
func (harn *parkedQuestionHarness) requestID() string {
	harn.t.Helper()
	list := harn.as.childQuestions.list()
	if len(list) != 1 {
		harn.t.Fatalf("parked questions = %d, want 1", len(list))
	}
	return list[0].RequestID
}

func waitForParkedQuestionLen(t *testing.T, as *agentSession, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if n := len(as.childQuestions.list()); n == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("parked question registry never reached %d entries (has %d)", want, len(as.childQuestions.list()))
}

// awaitParked returns the asker's outcome, failing if it does not return.
func awaitParked(t *testing.T, out chan parkedQuestionResult, timeout time.Duration) parkedQuestionResult {
	t.Helper()
	select {
	case res := <-out:
		return res
	case <-time.After(timeout):
		t.Fatalf("parked question did not resolve within %s", timeout)
		return parkedQuestionResult{}
	}
}

// postWithDeadline runs handler(w, r) and fails if it does not return within
// deadline. This is the non-blocking proof: the handler must complete while the
// test holds as.mu, the same lock a parked turn holds.
func postWithDeadline(t *testing.T, handler http.HandlerFunc, path, body string, deadline time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(rec, req)
	}()
	select {
	case <-done:
	case <-time.After(deadline):
		t.Fatalf("%s blocked for more than %s", path, deadline)
	}
	return rec
}

// ── tests ───────────────────────────────────────────────────────────────────

// TestParkedQuestionVisibleWhileTurnLockHeld: the question must show up in
// pending_asks and count as outstanding while the turn holds as.mu, and the
// `question` frame must carry the same request id the registry answers by.
func TestParkedQuestionVisibleWhileTurnLockHeld(t *testing.T) {
	harn := newParkedQuestionHarness(t, 0)
	as := harn.as

	as.mu.Lock()
	defer as.mu.Unlock()
	if as.mu.TryLock() {
		t.Fatal("test setup did not hold as.mu")
	}

	out := harn.ask(context.Background())
	id := harn.requestID()

	asks := harn.h.livePendingAsks(harn.sessionID)
	if asks == nil || len(asks.Questions) != 1 {
		t.Fatalf("livePendingAsks = %+v, want one parked question", asks)
	}
	if asks.Questions[0].RequestID != id {
		t.Fatalf("pending question request_id = %q, want %q", asks.Questions[0].RequestID, id)
	}
	if len(asks.Questions[0].Questions) != 1 || asks.Questions[0].Questions[0].Question != sampleQuestion().Question {
		t.Fatalf("pending question prompts = %+v", asks.Questions[0].Questions)
	}
	if !harn.h.sessionHasOutstandingAsk(as) {
		t.Fatal("sessionHasOutstandingAsk = false while a question is parked")
	}

	ev := waitForSSEEvent(t, harn.sub, "question", 3*time.Second)
	var got QuestionEvent
	eventPayload(t, ev, &got)
	if got.RequestID != id {
		t.Fatalf("question frame request_id = %q, want %q", got.RequestID, id)
	}

	// Tidy: dismiss so the asker returns before the deferred unlock.
	rec := postWithDeadline(t, harn.h.HandleDismissQuestion, "/api/questions/cancel",
		`{"request_id":"`+id+`","session_id":"`+harn.sessionID+`"}`, 3*time.Second)
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	awaitParked(t, out, 3*time.Second)
}

// TestParkedQuestionAnswerDeliveredWhileTurnLockHeld: an answer posted while the
// turn holds as.mu must complete without blocking, reach the parked caller with
// every selected option (including custom text), and close the dialog.
func TestParkedQuestionAnswerDeliveredWhileTurnLockHeld(t *testing.T) {
	harn := newParkedQuestionHarness(t, 0)
	as := harn.as

	as.mu.Lock()
	defer as.mu.Unlock()

	out := harn.ask(context.Background())
	id := harn.requestID()

	body := `{"request_id":"` + id + `","session_id":"` + harn.sessionID + `","answers":[` +
		`{"header":"Pick","question":"Which?","answers":[{"label":"Alpha"},{"label":"Other","text":"my own","custom":true}]}]}`
	rec := postWithDeadline(t, harn.h.HandleAnswerQuestion, "/api/questions", body, 3*time.Second)
	if rec.Code != http.StatusOK {
		t.Fatalf("answer status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	res := awaitParked(t, out, 3*time.Second)
	if res.dismissed {
		t.Fatal("answered question reported dismissed=true")
	}
	want := []tool.QuestionAnswerSet{{
		Header:   "Pick",
		Question: "Which?",
		Answers: []tool.QuestionAnswer{
			{Label: "Alpha"},
			{Label: "Other", Text: "my own", Custom: true},
		},
	}}
	gotJSON, _ := json.Marshal(res.answers)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("answers = %s, want %s", gotJSON, wantJSON)
	}

	if len(harn.as.childQuestions.list()) != 0 {
		t.Fatal("answered question is still parked")
	}
	waitForSSEEvent(t, harn.sub, "question_resolved", 3*time.Second)

	// A second answer for the same id is stale: it must 404 without blocking,
	// even though as.mu is still held.
	rec = postWithDeadline(t, harn.h.HandleAnswerQuestion, "/api/questions", body, 3*time.Second)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second answer status = %d, want 404", rec.Code)
	}
}

// TestParkedQuestionDismissDeliversDismissed: dismissing a parked question
// resumes the caller with dismissed=true and no answers.
func TestParkedQuestionDismissDeliversDismissed(t *testing.T) {
	harn := newParkedQuestionHarness(t, 0)
	as := harn.as

	as.mu.Lock()
	defer as.mu.Unlock()

	out := harn.ask(context.Background())
	id := harn.requestID()

	rec := postWithDeadline(t, harn.h.HandleDismissQuestion, "/api/questions/cancel",
		`{"request_id":"`+id+`","session_id":"`+harn.sessionID+`"}`, 3*time.Second)
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	res := awaitParked(t, out, 3*time.Second)
	if !res.dismissed {
		t.Fatal("dismissed question reported dismissed=false")
	}
	if len(res.answers) != 0 {
		t.Fatalf("dismissed question carried answers: %+v", res.answers)
	}
	waitForSSEEvent(t, harn.sub, "question_resolved", 3*time.Second)
}

// TestParkedQuestionParentStopDismisses: cancelling the parent turn (Stop, idle
// eviction, rebuild) must dismiss the park, not leave the caller blocked.
func TestParkedQuestionParentStopDismisses(t *testing.T) {
	harn := newParkedQuestionHarness(t, 0)

	out := harn.ask(context.Background())
	harn.as.agent.Cancel()

	res := awaitParked(t, out, 3*time.Second)
	if !res.dismissed {
		t.Fatal("parent stop returned dismissed=false")
	}
	if len(harn.as.childQuestions.list()) != 0 {
		t.Fatal("parent stop left the question parked")
	}
	waitForSSEEvent(t, harn.sub, "question_resolved", 3*time.Second)
}

// TestParkedQuestionTimeoutDismisses: an abandoned dialog must not hold the turn
// past the park bound.
func TestParkedQuestionTimeoutDismisses(t *testing.T) {
	harn := newParkedQuestionHarness(t, 50*time.Millisecond)

	out := harn.ask(context.Background())
	res := awaitParked(t, out, 3*time.Second)
	if !res.dismissed {
		t.Fatal("timed-out question returned dismissed=false")
	}
	if len(harn.as.childQuestions.list()) != 0 {
		t.Fatal("timed-out question left the registry")
	}
	waitForSSEEvent(t, harn.sub, "question_resolved", 3*time.Second)
}

// TestParkedQuestionContextCancelDismisses: a cancelled caller context ends the
// wait as a dismissal.
func TestParkedQuestionContextCancelDismisses(t *testing.T) {
	harn := newParkedQuestionHarness(t, 0)

	ctx, cancel := context.WithCancel(context.Background())
	out := harn.ask(ctx)
	cancel()

	res := awaitParked(t, out, 3*time.Second)
	if !res.dismissed {
		t.Fatal("cancelled context returned dismissed=false")
	}
	if len(harn.as.childQuestions.list()) != 0 {
		t.Fatal("cancelled context left the question parked")
	}
}

// TestChildQuestionAsksDenyAllDismisses: the sweep used on idle eviction and on
// a rebuild must release every parked caller as dismissed.
func TestChildQuestionAsksDenyAllDismisses(t *testing.T) {
	harn := newParkedQuestionHarness(t, 0)

	out := harn.ask(context.Background())
	harn.as.childQuestions.denyAll()

	res := awaitParked(t, out, 3*time.Second)
	if !res.dismissed {
		t.Fatal("denyAll returned dismissed=false")
	}
	if len(harn.as.childQuestions.list()) != 0 {
		t.Fatal("denyAll left the registry populated")
	}
}
