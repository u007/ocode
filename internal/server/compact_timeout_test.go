package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

type compactTimeoutClient struct {
	called  chan struct{}
	release chan struct{}
}

type compactCancelClient struct{}

func (compactCancelClient) Chat(_ []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	return nil, context.Canceled
}

func (compactCancelClient) GetProvider() string { return "mock" }
func (compactCancelClient) GetModel() string    { return "mock-compact" }

func (c *compactTimeoutClient) Chat(_ []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	close(c.called)
	<-c.release
	return &agent.Message{Role: "assistant", Content: validCompactSummaryForTest()}, nil
}

func (c *compactTimeoutClient) GetProvider() string { return "mock" }
func (c *compactTimeoutClient) GetModel() string    { return "mock-compact" }

func TestCompactSessionTimeoutReturns504AndLeavesTranscriptUnchanged(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	cfg := autoCompactConfig()
	cfg.Ocode.Compact.SummaryTimeoutSeconds = 1
	cfg.Ocode.Compact.SummaryFirstTokenTimeoutSeconds = 1
	cfg.Ocode.Compact.MaxSummaryInputTokens = 50000
	client := &compactTimeoutClient{
		called:  make(chan struct{}),
		release: make(chan struct{}),
	}
	before := seedTranscript()
	as := &agentSession{
		agent:    agent.NewAgent(client, nil, cfg, nil),
		model:    "fake-model",
		messages: append([]agent.Message(nil), before...),
	}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	rec := httptest.NewRecorder()
	h.HandleCompactSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact", nil), id)
	close(client.release)
	select {
	case <-client.called:
	case <-time.After(time.Second):
		t.Fatal("compaction summary client was not called")
	}

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("compact timeout status = %d, want 504 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "transcript unchanged") {
		t.Fatalf("timeout response lacks retry guidance: %s", rec.Body.String())
	}
	as.mu.Lock()
	got := append([]agent.Message(nil), as.messages...)
	as.mu.Unlock()
	if len(got) != len(before) {
		t.Fatalf("timeout changed transcript length: got %d, want %d", len(got), len(before))
	}
	for i := range before {
		if got[i].Content != before[i].Content {
			t.Fatalf("timeout changed transcript message %d", i)
		}
	}
}

func TestCompactSessionBareCancellationRemains500(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	as := &agentSession{
		agent:    agent.NewAgent(compactCancelClient{}, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: seedTranscript(),
	}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	rec := httptest.NewRecorder()
	h.HandleCompactSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact", nil), id)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("bare cancellation status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCompactSessionCancelledRequestSkipsResponseWrite(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	as := &agentSession{
		agent:    agent.NewAgent(compactCancelClient{}, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: seedTranscript(),
	}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/compact", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.HandleCompactSession(rec, req, id)
	if rec.Body.Len() != 0 {
		t.Fatalf("cancelled request received a response body: %s", rec.Body.String())
	}
}
