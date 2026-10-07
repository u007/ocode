package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// btwRequest POSTs /btw for id with the given content and returns the recorder.
func btwRequest(h *Handler, id, content string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	body := `{"content":` + strconvQuote(content) + `}`
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/btw", strings.NewReader(body))
	h.HandleBtw(rec, req, id)
	return rec
}

// btwCancelRequest DELETEs the in-flight side query for id.
func btwCancelRequest(h *Handler, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/sessions/"+id+"/btw", nil)
	h.HandleBtwCancel(rec, req, id)
	return rec
}

// strconvQuote produces a JSON string literal (minimal, no deps).
func strconvQuote(s string) string {
	var buf bytes.Buffer
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			buf.WriteRune(r)
		}
	}
	buf.WriteByte('"')
	return buf.String()
}

// btwLoopStub replaces btwAskLoopFn so a test drives the side-query loop
// deterministically (the production loop builds a fresh client from the
// session's model, which needs a live provider). It records every invocation
// and counts cancel calls.
type btwLoopStub struct {
	mu       sync.Mutex
	invocs   int
	cancels  int
	lastOpts agent.AskLoopOptions
	lastMsgs []agent.Message
	// optss/results keep one entry per invocation so a test can drive a LATE
	// callback for a superseded run.
	optss   []agent.AskLoopOptions
	results []func(string, error)
}

func (s *btwLoopStub) snapshot() (invocs, cancels int, msgs []agent.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.invocs, s.cancels, s.lastMsgs
}

// resultAt returns the i-th invocation's onResult so a test can complete a
// superseded run late.
func (s *btwLoopStub) resultAt(i int) func(string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.results[i]
}

// optsAt returns the i-th invocation's options so a test can push a late
// activity/delta frame from a superseded run.
func (s *btwLoopStub) optsAt(i int) agent.AskLoopOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.optss[i]
}

// stubBtwLoop installs the stub for one test. run, when non-nil, is invoked
// synchronously inside the loop call with the options the handler passed.
func stubBtwLoop(t *testing.T, run func(opts agent.AskLoopOptions, onResult func(string, error))) *btwLoopStub {
	t.Helper()
	s := &btwLoopStub{}
	prev := btwAskLoopFn
	btwAskLoopFn = func(_ *agent.Agent, msgs []agent.Message, opts agent.AskLoopOptions, onResult func(string, error)) func() {
		s.mu.Lock()
		s.invocs++
		s.lastOpts = opts
		s.lastMsgs = msgs
		s.optss = append(s.optss, opts)
		s.results = append(s.results, onResult)
		s.mu.Unlock()
		if run != nil {
			run(opts, onResult)
		}
		return func() {
			s.mu.Lock()
			s.cancels++
			s.mu.Unlock()
		}
	}
	t.Cleanup(func() { btwAskLoopFn = prev })
	return s
}

// collectBtwFrames drains `btw` frames from sub until want have been seen.
func collectBtwFrames(t *testing.T, sub chan Envelope, want int) []btwFrame {
	t.Helper()
	var out []btwFrame
	deadline := time.After(3 * time.Second)
	for len(out) < want {
		select {
		case env := <-sub:
			if env.Event != "btw" {
				continue
			}
			raw, _ := json.Marshal(env.Data)
			var f btwFrame
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatalf("decode btw frame: %v", err)
			}
			out = append(out, f)
		case <-deadline:
			t.Fatalf("timed out waiting for %d btw frames, got %d (%+v)", want, len(out), out)
		}
	}
	return out
}

// TestHandleBtwStartsSideQueryWithoutWritingTranscript is the core contract:
// /btw answers 202 immediately, runs a side query over the transcript plus the
// aside, and writes NOTHING to the persisted transcript.
func TestHandleBtwStartsSideQueryWithoutWritingTranscript(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	if err := session.AppendUserMessageForDir(proj, id, "hello"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	as := newTestSession(h, id, instantClient{})
	defer as.agent.Shutdown()

	before, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load before: %v", err)
	}

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	stub := stubBtwLoop(t, func(_ agent.AskLoopOptions, onResult func(string, error)) {
		onResult("the answer", nil)
	})

	rec := btwRequest(h, id, "actually use tabs")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202 (body: %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "started" {
		t.Errorf("status = %v, want started", body["status"])
	}
	if gen, _ := body["generation"].(float64); gen != 1 {
		t.Errorf("generation = %v, want 1", body["generation"])
	}

	after, err := session.LoadForDir(proj, id)
	if err != nil {
		t.Fatalf("load after: %v", err)
	}
	if len(after.Messages) != len(before.Messages) {
		t.Fatalf("transcript grew from %d to %d messages; a side query must never write the transcript",
			len(before.Messages), len(after.Messages))
	}

	_, _, msgs := stub.snapshot()
	if len(msgs) == 0 {
		t.Fatal("side query got no messages")
	}
	last := msgs[len(msgs)-1]
	if last.Role != "user" || last.Content != "actually use tabs" {
		t.Errorf("last side-query message = %q/%q, want user/actually use tabs", last.Role, last.Content)
	}

	frames := collectBtwFrames(t, sub, 2)
	if frames[0].Phase != "started" || frames[0].Question != "actually use tabs" {
		t.Errorf("frame[0] = %+v, want started with the question", frames[0])
	}
	if frames[1].Phase != "done" || frames[1].Text != "the answer" {
		t.Errorf("frame[1] = %+v, want done with the answer", frames[1])
	}
}

// TestHandleBtwEmitsBusFrames pins the activity + delta streaming shape.
func TestHandleBtwEmitsBusFrames(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, instantClient{})
	defer as.agent.Shutdown()

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	stubBtwLoop(t, func(opts agent.AskLoopOptions, onResult func(string, error)) {
		if opts.OnMessage != nil {
			m := agent.Message{Role: "assistant"}
			m.ToolCalls = []agent.ToolCall{{}}
			m.ToolCalls[0].Function.Name = "read"
			m.ToolCalls[0].Function.Arguments = `{"file":"a.go"}`
			opts.OnMessage(m)
		}
		if opts.OnDelta != nil {
			opts.OnDelta("text", "partial ")
			opts.OnDelta("reasoning", "must be ignored")
		}
		onResult("final", nil)
	})

	if rec := btwRequest(h, id, "q"); rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202", rec.Code)
	}

	frames := collectBtwFrames(t, sub, 4)
	if frames[0].Phase != "started" {
		t.Errorf("frame[0] phase = %q, want started", frames[0].Phase)
	}
	if frames[1].Phase != "activity" || !strings.Contains(frames[1].Text, "→ read") {
		t.Errorf("frame[1] = %+v, want activity containing '→ read'", frames[1])
	}
	if frames[2].Phase != "delta" || frames[2].Text != "partial " {
		t.Errorf("frame[2] = %+v, want delta 'partial '", frames[2])
	}
	if frames[3].Phase != "done" || frames[3].Text != "final" {
		t.Errorf("frame[3] = %+v, want done 'final'", frames[3])
	}
	// Exactly 4: the reasoning-kind delta must not be published.
	select {
	case env := <-sub:
		if env.Event == "btw" {
			t.Errorf("unexpected extra btw frame: %+v", env)
		}
	case <-time.After(150 * time.Millisecond):
	}
}

// TestHandleBtwCancelAndReplace: a second /btw cancels the first, DELETE cancels
// the current run, and DELETE is idempotent.
func TestHandleBtwCancelAndReplace(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, instantClient{})
	defer as.agent.Shutdown()

	stub := stubBtwLoop(t, nil) // never completes: the runs stay in flight

	if rec := btwRequest(h, id, "first"); rec.Code != http.StatusAccepted {
		t.Fatalf("first status %d, want 202", rec.Code)
	}
	if rec := btwRequest(h, id, "second"); rec.Code != http.StatusAccepted {
		t.Fatalf("second status %d, want 202", rec.Code)
	}

	invocs, cancels, _ := stub.snapshot()
	if invocs != 2 {
		t.Fatalf("loop invocations = %d, want 2", invocs)
	}
	if cancels != 1 {
		t.Fatalf("cancels after replace = %d, want 1 (the first run must be cancelled)", cancels)
	}

	if rec := btwCancelRequest(h, id); rec.Code != http.StatusOK {
		t.Fatalf("DELETE status %d, want 200", rec.Code)
	}
	_, cancels, _ = stub.snapshot()
	if cancels != 2 {
		t.Fatalf("cancels after DELETE = %d, want 2", cancels)
	}

	// Idempotent: a second DELETE cancels nothing more.
	if rec := btwCancelRequest(h, id); rec.Code != http.StatusOK {
		t.Fatalf("second DELETE status %d, want 200", rec.Code)
	}
	if _, cancels, _ = stub.snapshot(); cancels != 2 {
		t.Fatalf("cancels after second DELETE = %d, want 2 (idempotent)", cancels)
	}
}

// TestHandleBtwSupersededRunPublishesNothing: once a second /btw replaces the
// first, the superseded run's late callbacks must not surface frames — a stale
// "done" would overwrite the panel's answer for the new run.
func TestHandleBtwSupersededRunPublishesNothing(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, instantClient{})
	defer as.agent.Shutdown()

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	stub := stubBtwLoop(t, nil)
	if rec := btwRequest(h, id, "first"); rec.Code != http.StatusAccepted {
		t.Fatalf("first status %d, want 202", rec.Code)
	}
	if rec := btwRequest(h, id, "second"); rec.Code != http.StatusAccepted {
		t.Fatalf("second status %d, want 202", rec.Code)
	}

	// Complete the FIRST (superseded) run late.
	stub.resultAt(0)("stale answer", nil)
	// Also try to push a late activity frame from it.
	if opts := stub.optsAt(0); opts.OnMessage != nil {
		m := agent.Message{Role: "assistant"}
		m.ToolCalls = []agent.ToolCall{{}}
		m.ToolCalls[0].Function.Name = "read"
		opts.OnMessage(m)
	}

	// None of the superseded run's frames may appear; only the two `started`
	// frames are expected, and no terminal frame at all.
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case env := <-sub:
			if env.Event != "btw" {
				continue
			}
			raw, _ := json.Marshal(env.Data)
			var f btwFrame
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if f.Phase != "started" {
				t.Fatalf("superseded run published %q frame: %+v", f.Phase, f)
			}
			if f.Generation != 1 && f.Generation != 2 {
				t.Fatalf("unexpected generation: %+v", f)
			}
		case <-deadline:
			return
		}
	}
}

// TestHandleBtwUnknownSessionStillNotFound: no registry entry -> 404.
func TestHandleBtwUnknownSessionStillNotFound(t *testing.T) {
	h := NewHandler()
	rec := httptest.NewRecorder()
	body := `{"content":"orphan aside"}`
	req := httptest.NewRequest("POST", "/api/sessions/nope/btw", strings.NewReader(body))
	h.HandleBtw(rec, req, "nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestHandleBtwEmptyContentBadRequest: an empty aside is a 400.
func TestHandleBtwEmptyContentBadRequest(t *testing.T) {
	h := NewHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/sessions/x/btw", strings.NewReader(`{"content":""}`))
	h.HandleBtw(rec, req, "x")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestHandleBtwBuildsAgentWhenNotResident: an idle/evicted session (registry
// entry, no resident agent) must build an agent and answer, not 404 or hang.
func TestHandleBtwBuildsAgentWhenNotResident(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded; cannot build a client")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	h.cfg.Model = "opencode-go/deepseek-v4-flash"

	// Completed MCP cache so bootstrap does not stall waiting on MCP.
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}}

	id := session.NewSessionID()
	if err := session.Save(id, "idle", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.BindNewOrVerify(id, proj, ""); err != nil {
		t.Fatal(err)
	}

	stub := stubBtwLoop(t, func(_ agent.AskLoopOptions, onResult func(string, error)) {
		onResult("ok", nil)
	})

	defer func() {
		if as := h.lookupAgentSession(id); as != nil && as.agent != nil {
			as.agent.Shutdown()
		}
	}()

	rec := btwRequest(h, id, "are we there yet")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202 (body: %s)", rec.Code, rec.Body.String())
	}
	if h.lookupAgentSession(id) == nil {
		t.Fatal("handler did not build/register an agent for the non-resident session")
	}
	if invocs, _, _ := stub.snapshot(); invocs != 1 {
		t.Fatalf("loop invocations = %d, want 1", invocs)
	}
}

// TestHandleBtwStartupErrorEmitsErrorFrame: a loop that fails to start surfaces
// an error frame instead of hanging.
func TestHandleBtwStartupErrorEmitsErrorFrame(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)
	as := newTestSession(h, id, instantClient{})
	defer as.agent.Shutdown()

	sub := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(sub)

	stubBtwLoop(t, func(_ agent.AskLoopOptions, onResult func(string, error)) {
		onResult("", errors.New("could not build a side-query client"))
	})

	if rec := btwRequest(h, id, "q"); rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202", rec.Code)
	}

	frames := collectBtwFrames(t, sub, 2)
	if frames[0].Phase != "started" {
		t.Errorf("frame[0] phase = %q, want started", frames[0].Phase)
	}
	if frames[1].Phase != "error" || !strings.Contains(frames[1].Error, "could not build") {
		t.Errorf("frame[1] = %+v, want error carrying the startup failure", frames[1])
	}
}

// TestHandleBtwResetIdCancelsRun: /reset-id cancels the in-flight side query
// and strands no btwRuns entry under the deleted id.
func TestHandleBtwResetIdCancelsRun(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })

	id := session.NewSessionID()
	if err := session.Save(id, "Keep me", []agent.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
	}, map[string]any{"spend": 0.5}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.BindNewOrVerify(id, proj, ""); err != nil {
		t.Fatal(err)
	}

	// Seed an in-flight side query directly (no live agent needed for this
	// wiring; the assertion is that reset-id cancels + clears it).
	gen, _ := h.registerBtwRun(id)
	cancelled := false
	h.recordBtwCancel(id, gen, func() { cancelled = true })

	rec := httptest.NewRecorder()
	h.HandleResetSessionID(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/reset-id", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !cancelled {
		t.Fatal("reset-id did not cancel the in-flight btw run")
	}
	if _, ok := h.btwRuns[id]; ok {
		t.Fatal("btwRuns entry stranded under the deleted id")
	}
}
