package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jamesmercstudio/ocode/internal/agent"
)

// newRunsHandler builds a Handler with one session whose agent owns a "worker"
// run that has a nested "helper" sub-agent run.
func newRunsHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	a := agent.NewAgent(nil, nil, nil)
	worker := a.Runs().New("worker")

	// Sub-agents always have a real client in production; give the test one so
	// ModelLabel() (which reads Sub.Client().GetModel()) behaves realistically.
	sub := agent.NewAgent(&agent.GenericClient{Model: "test-model"}, nil, nil)
	sub.Runs().New("helper")
	worker.Sub = sub

	h := NewHandler()
	const sessionID = "sess-1"
	h.agents[sessionID] = &agentSession{agent: a}
	return h, sessionID
}

func TestHandleListRunsReturnsNestedTree(t *testing.T) {
	h, sessionID := newRunsHandler(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/agents/runs?session="+sessionID, nil)
	h.HandleListRuns(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var runs []agentRunDTO
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 top-level run, got %d", len(runs))
	}
	if runs[0].Name != "worker" || runs[0].Status != "running" {
		t.Fatalf("unexpected top run: %+v", runs[0])
	}
	if len(runs[0].Children) != 1 || runs[0].Children[0].Name != "helper" {
		t.Fatalf("expected nested helper child, got %+v", runs[0].Children)
	}
}

func TestHandleListRunsEmptyWhenNoSession(t *testing.T) {
	h := NewHandler()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/agents/runs", nil)
	h.HandleListRuns(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Must be an empty JSON array, never null, so the web can map over it.
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Fatalf("expected empty array for unknown session, got %q", got)
	}
}

func TestHandleRunsStreamEmitsInitialFrame(t *testing.T) {
	h, sessionID := newRunsHandler(t)

	ctx, cancel := context.WithCancel(context.Background())
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/agents/runs/stream?session="+sessionID, nil).WithContext(ctx)

	done := make(chan struct{})
	go func() {
		h.HandleRunsStream(w, r)
		close(done)
	}()

	// Give the handler time to write the initial snapshot, then close it.
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not return after context cancel")
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: runs") {
		t.Fatalf("expected an initial runs frame, got: %q", body)
	}
	if !strings.Contains(body, "\"name\":\"worker\"") {
		t.Fatalf("expected worker run in stream frame, got: %q", body)
	}
}
