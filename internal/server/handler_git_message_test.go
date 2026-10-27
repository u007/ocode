package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// stubCommitMessageGenerator replaces the agent-side generator for one test.
func stubCommitMessageGenerator(t *testing.T, fn func(cfg *config.Config, profile, diff string) (*agent.CommitMessageResult, error)) {
	t.Helper()
	prev := commitMessageGenerator
	commitMessageGenerator = fn
	t.Cleanup(func() { commitMessageGenerator = prev })
}

func newCommitMessageServer(t *testing.T) *Server {
	t.Helper()
	setHomeTree(t, t.TempDir())
	t.Setenv("OCODE_PROFILE", "")
	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.cfg = &config.Config{
		Model: "main/model",
		Ocode: config.OcodeConfig{SmallModel: "small/model"},
	}
	return srv
}

func postCommitMessage(t *testing.T, h http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/git/commit-message", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The handler test calls the handler directly elsewhere; here drive the real
// mux, because a missing route answers with the SPA fallback (the failure mode
// that matters for a new endpoint).
func TestGitCommitMessageRouteGeneratesFromDiff(t *testing.T) {
	var gotDiff string
	stubCommitMessageGenerator(t, func(_ *config.Config, _ /*profile*/, diff string) (*agent.CommitMessageResult, error) {
		gotDiff = diff
		return &agent.CommitMessageResult{Text: "feat: add a thing", Model: "small/model"}, nil
	})
	srv := newCommitMessageServer(t)

	rec := postCommitMessage(t, srv.serveHandler(), `{"diff":"  diff --git a/x b/x\n  ","project":"/proj"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/git/commit-message = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Message string `json:"message"`
		Model   string `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Message != "feat: add a thing" || got.Model != "small/model" {
		t.Fatalf("response = %+v", got)
	}
	if gotDiff != "diff --git a/x b/x" {
		t.Fatalf("generator diff = %q, want the trimmed request diff", gotDiff)
	}
}

func TestGitCommitMessageUsesWindowProfile(t *testing.T) {
	var gotProfile string
	stubCommitMessageGenerator(t, func(_ *config.Config, profile, _ string) (*agent.CommitMessageResult, error) {
		gotProfile = profile
		return &agent.CommitMessageResult{Text: "fix: profile"}, nil
	})
	srv := newCommitMessageServer(t)
	srv.handler.windowProfiles = map[string]string{"win-7": "work"}

	rec := postCommitMessage(t, srv.serveHandler(), `{"diff":"DIFF"}`, map[string]string{"X-Window-Id": "win-7"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotProfile != "work" {
		t.Fatalf("profile = %q, want the window's active profile", gotProfile)
	}
}

func TestGitCommitMessageRequiresDiff(t *testing.T) {
	called := false
	stubCommitMessageGenerator(t, func(*config.Config, string, string) (*agent.CommitMessageResult, error) {
		called = true
		return &agent.CommitMessageResult{Text: "unexpected"}, nil
	})
	srv := newCommitMessageServer(t)

	rec := postCommitMessage(t, srv.serveHandler(), `{"diff":"   "}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("an empty diff must not reach the model")
	}
}

func TestGitCommitMessageRejectsBadJSON(t *testing.T) {
	srv := newCommitMessageServer(t)
	rec := postCommitMessage(t, srv.serveHandler(), `{`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestGitCommitMessageSurfacesGeneratorError(t *testing.T) {
	stubCommitMessageGenerator(t, func(*config.Config, string, string) (*agent.CommitMessageResult, error) {
		return nil, errors.New("small/model: rate limited")
	})
	srv := newCommitMessageServer(t)

	rec := postCommitMessage(t, srv.serveHandler(), `{"diff":"DIFF"}`, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rate limited") {
		t.Fatalf("body = %s, want the generator error", rec.Body.String())
	}
}
