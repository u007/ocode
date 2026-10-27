package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// stubCommitMessageGenerator replaces the agent-side generator for one test.
func stubCommitMessageGenerator(t *testing.T, fn func(ctx context.Context, cfg *config.Config, profile, diff string) (*agent.CommitMessageResult, error)) {
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
	stubCommitMessageGenerator(t, func(_ context.Context, _ *config.Config, _ /*profile*/, diff string) (*agent.CommitMessageResult, error) {
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
	stubCommitMessageGenerator(t, func(_ context.Context, _ *config.Config, profile, _ string) (*agent.CommitMessageResult, error) {
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
	stubCommitMessageGenerator(t, func(context.Context, *config.Config, string, string) (*agent.CommitMessageResult, error) {
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

// A provider failure is a 500 whose body carries the provider error as one line.
func TestGitCommitMessageGenerationFailureIs500WithDetail(t *testing.T) {
	stubCommitMessageGenerator(t, func(context.Context, *config.Config, string, string) (*agent.CommitMessageResult, error) {
		return nil, errors.New("small/model: 429\n  rate limited,   retry later")
	})
	srv := newCommitMessageServer(t)

	rec := postCommitMessage(t, srv.serveHandler(), `{"diff":"DIFF"}`, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
	want := `{"error":"commit message generation failed: small/model: 429 rate limited, retry later"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

// Secrets in the provider error never reach the client: a key in a request URL
// and a bearer token are masked, and the result stays on one line.
func TestGitCommitMessageGenerationFailureRedactsSecrets(t *testing.T) {
	const key = "sk-abcdefghijklmnopqrstuvwxyz0123456789"
	stubCommitMessageGenerator(t, func(context.Context, *config.Config, string, string) (*agent.CommitMessageResult, error) {
		return nil, errors.New("small/model: Post \"https://api.example.com/v1?key=" + key + "\":\n Authorization: Bearer " + key)
	})
	srv := newCommitMessageServer(t)

	rec := postCommitMessage(t, srv.serveHandler(), `{"diff":"DIFF"}`, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), key) {
		t.Fatalf("body leaks the key: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "[REDACTED:") {
		t.Fatalf("body = %s, want the key masked", rec.Body.String())
	}
}

func TestCommitMessageErrorDetailCapsLength(t *testing.T) {
	got := commitMessageErrorDetail(errors.New(strings.Repeat("é", 300)))
	if want := strings.Repeat("é", commitMessageDetailMaxRunes) + "..."; got != want {
		t.Fatalf("detail = %q (%d runes), want %d runes plus ellipsis", got, len([]rune(got)), commitMessageDetailMaxRunes)
	}
}

// No configured model is a precondition the user can fix: 409, not a failure.
func TestGitCommitMessageNotConfiguredIs409(t *testing.T) {
	stubCommitMessageGenerator(t, func(context.Context, *config.Config, string, string) (*agent.CommitMessageResult, error) {
		return nil, agent.ErrCommitMessageNotConfigured
	})
	srv := newCommitMessageServer(t)

	rec := postCommitMessage(t, srv.serveHandler(), `{"diff":"DIFF"}`, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
}

// The provider call runs under a deadline derived from the request.
func TestGitCommitMessageBoundsGenerationWithDeadline(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	stubCommitMessageGenerator(t, func(ctx context.Context, _ *config.Config, _, _ string) (*agent.CommitMessageResult, error) {
		deadline, hasDeadline = ctx.Deadline()
		return &agent.CommitMessageResult{Text: "fix: bounded", Model: "small/model"}, nil
	})
	srv := newCommitMessageServer(t)

	rec := postCommitMessage(t, srv.serveHandler(), `{"diff":"DIFF"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !hasDeadline {
		t.Fatal("generator context has no deadline")
	}
	if left := time.Until(deadline); left <= 0 || left > commitMessageTimeout {
		t.Fatalf("deadline in %v, want within %v", left, commitMessageTimeout)
	}
}

// A client that disconnects cancels the generation: the context is derived from
// the request, not from context.Background.
func TestGitCommitMessageCancelledRequestCancelsGeneration(t *testing.T) {
	var genErr error
	stubCommitMessageGenerator(t, func(ctx context.Context, _ *config.Config, _, _ string) (*agent.CommitMessageResult, error) {
		genErr = ctx.Err()
		return nil, ctx.Err()
	})
	srv := newCommitMessageServer(t)

	reqCtx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/git/commit-message", strings.NewReader(`{"diff":"DIFF"}`)).WithContext(reqCtx)
	rec := httptest.NewRecorder()
	srv.handler.HandleGitCommitMessage(rec, req)

	if !errors.Is(genErr, context.Canceled) {
		t.Fatalf("generator ctx err = %v, want context.Canceled from the request", genErr)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}
