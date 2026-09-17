package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// plantSession writes a session under wd's storage dir with the given messages
// (nil = an empty session) and returns its id.
func plantSession(t *testing.T, wd, id, title string, messages []agent.Message) string {
	t.Helper()
	session.SetWorkDir(wd)
	t.Cleanup(func() { session.SetWorkDir("") })
	if err := session.Save(id, title, messages, nil); err != nil {
		t.Fatalf("save session %s under %s: %v", id, wd, err)
	}
	return id
}

// TestExportEmptySessionErrors is the acceptance test for the "empty session"
// report: both export endpoints must answer 422 with a "session is empty"
// message instead of returning an empty markdown file (/export used to 200 an
// empty body) or a misleading "no messages to export".
func TestExportEmptySessionErrors(t *testing.T) {
	h := NewHandler()
	wd := t.TempDir()
	h.SetWorkDir(wd)
	id := plantSession(t, wd, session.NewSessionID(), "empty", nil)

	for _, tc := range []struct {
		name string
		call func(w http.ResponseWriter, r *http.Request, id string)
	}{
		{"markdown", h.HandleExportSession},
		{"claude", h.HandleExportClaudeSession},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.call(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/export", nil), id)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "session is empty") {
				t.Fatalf("body %q does not mention an empty session", rec.Body.String())
			}
		})
	}
}

// TestExportNonEmptySessionStillSucceeds guards the success path so the empty
// guard can't regress into a blanket rejection.
func TestExportNonEmptySessionStillSucceeds(t *testing.T) {
	h := NewHandler()
	wd := t.TempDir()
	h.SetWorkDir(wd)
	id := plantSession(t, wd, session.NewSessionID(), "hello", []agent.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	})

	rec := httptest.NewRecorder()
	h.HandleExportSession(rec, httptest.NewRequest("GET", "/api/sessions/"+id+"/export", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "## User") || !strings.Contains(body, "hi there") {
		t.Fatalf("markdown body missing transcript: %q", body)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/markdown") {
		t.Fatalf("content-type %q, want text/markdown", got)
	}
}
