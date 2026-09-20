package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// focusCaptureClient records the last prompt it was handed, so a test can
// assert the compaction focus actually reached the summarisation prompt rather
// than being silently dropped at the HTTP boundary.
type focusCaptureClient struct {
	mu     sync.Mutex
	prompt string
}

func (c *focusCaptureClient) Chat(msgs []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// The summary request carries the built prompt as the final user message.
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			c.prompt = msgs[i].Content
			break
		}
	}
	return &agent.Message{Role: "assistant", Content: validCompactSummaryForTest()}, nil
}
func (c *focusCaptureClient) GetProvider() string { return "mock" }
func (c *focusCaptureClient) GetModel() string    { return "mock-compact" }

func (c *focusCaptureClient) lastPrompt() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prompt
}

// TestCompactSessionHonoursFocusBody is the regression test for the web
// `/compact <focus>` dropped-argument bug: the TUI passes focus into the
// summarisation prompt, while the server's compact endpoint had no focus field
// at all and agent.Compact hardcoded "" — so `/compact <focus>` silently did a
// plain compaction. The focus is now carried in the request body.
func TestCompactSessionHonoursFocusBody(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	client := &focusCaptureClient{}
	as := &agentSession{
		agent:    agent.NewAgent(client, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: seedTranscript(),
	}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	body := strings.NewReader(`{"focus":"the auth refactor"}`)
	rec := httptest.NewRecorder()
	h.HandleCompactSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact", body), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("compact status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	got := client.lastPrompt()
	if got == "" {
		t.Fatal("compaction never called the summarisation client")
	}
	if !strings.Contains(got, "the auth refactor") {
		t.Fatalf("focus did not reach the summary prompt:\n%s", got)
	}
}

// TestCompactSessionWithoutBodyStillWorks pins that the now-optional body keeps
// the pre-existing no-focus callers (the web no-arg `/compact`, tests, and any
// external API user) working.
func TestCompactSessionWithoutBodyStillWorks(t *testing.T) {
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "gpt-4o-mini"
	}
	proj := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, proj, id)
	h.sessions.Register(id, proj)

	as := &agentSession{
		agent:    agent.NewAgent(compactSummaryClient{}, nil, autoCompactConfig(), nil),
		model:    "fake-model",
		messages: seedTranscript(),
	}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()

	rec := httptest.NewRecorder()
	h.HandleCompactSession(rec, httptest.NewRequest("POST", "/api/sessions/"+id+"/compact", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("compact without body: status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}
