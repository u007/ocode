package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/contextbudget"
	"github.com/u007/ocode/internal/session"
)

// The web/desktop /context must render the same token-budget breakdown as the
// TUI. GET /api/sessions/:id/context now carries a `report` built by the shared
// internal/contextbudget package whenever a live, quiescent agent exists.
func TestHandleSessionContextIncludesSharedReport(t *testing.T) {
	h := NewHandler()
	dir := t.TempDir()
	h.SetWorkDir(dir)

	id := session.NewSessionID()
	saveSessionToDir(t, dir, id)
	h.agents[id] = &agentSession{
		agent:    agent.NewAgent(nil, nil, nil, nil),
		messages: []agent.Message{{Role: "user", Content: "hello world"}},
	}

	rec := httptest.NewRecorder()
	h.HandleSessionContext(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/context", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		MessageCount int                   `json:"message_count"`
		Report       *contextbudget.Report `json:"report"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Report == nil {
		t.Fatal("expected a report in the response")
	}
	if len(resp.Report.Sections) == 0 {
		t.Fatal("report has no sections")
	}
	// The shared builder always emits these sections, so their presence is a
	// cheap assertion that we are serialising the full Report, not a stub.
	want := map[string]bool{"Base Prompt": false, "Tools (injected every request)": false, "Session Messages": false}
	for _, s := range resp.Report.Sections {
		if _, ok := want[s.Title]; ok {
			want[s.Title] = true
		}
	}
	for title, found := range want {
		if !found {
			t.Errorf("report missing section %q (got %d sections)", title, len(resp.Report.Sections))
		}
	}
}

// A session that exists on disk but has no live agent must still answer with the
// summary fields and must NOT carry a report — the breakdown needs agent
// internals we do not have.
func TestHandleSessionContextOmitsReportWithoutLiveAgent(t *testing.T) {
	h := NewHandler()
	dir := t.TempDir()
	h.SetWorkDir(dir)

	id := session.NewSessionID()
	saveSessionToDir(t, dir, id)

	rec := httptest.NewRecorder()
	h.HandleSessionContext(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/context", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp struct {
		MessageCount int                   `json:"message_count"`
		Report       *contextbudget.Report `json:"report"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Report != nil {
		t.Fatal("report must be omitted when no live agent is available")
	}
}
