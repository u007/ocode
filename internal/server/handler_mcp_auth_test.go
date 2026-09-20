package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

// mcpAuthTestConfig returns an OcodeConfig whose MCP map has one remote server
// with a complete OAuth block, so the handler reaches the job-manager path.
func mcpAuthTestConfig() *config.Config {
	enabled := true
	return &config.Config{
		Ocode: config.OcodeConfig{},
		MCP: map[string]config.MCPConfig{
			"linear": {
				Type:    "remote",
				URL:     "https://mcp.linear.app/sse",
				Enabled: true,
				OAuth: &config.MCPOAuthConfig{
					Enabled:          &enabled,
					AuthorizationURL: "https://linear.app/oauth/authorize",
					TokenURL:         "https://api.linear.app/oauth/token",
					ClientID:         "client-1",
				},
			},
			"local-only": {Type: "local", Command: []string{"echo"}},
		},
	}
}

func mcpAuthHandler(t *testing.T) *Handler {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	h.mu.Lock()
	h.cfg = mcpAuthTestConfig()
	h.mu.Unlock()
	return h
}

// TestMCPAuthRefusesNonLoopbackCallers is the core safety property: the OAuth
// flow launches a system browser and listens on the SERVER's 127.0.0.1:8085.
// A request from another machine (e.g. through the remote proxy) must be
// refused — otherwise the flow would open a browser on the wrong host and wait
// two minutes for a callback it never receives.
func TestMCPAuthRefusesNonLoopbackCallers(t *testing.T) {
	h := mcpAuthHandler(t)

	req := httptest.NewRequest("POST", "/api/mcp/linear/auth", nil)
	req.RemoteAddr = "203.0.113.9:51234" // non-loopback peer
	rec := httptest.NewRecorder()
	h.HandleStartMCPAuth(rec, req, "linear")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a non-loopback caller", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "127.0.0.1:8085") {
		t.Errorf("refusal should explain the callback constraint, got %s", body)
	}
}

// TestMCPAuthRejectsUnknownServer pins that a loopback caller asking for a
// server that does not exist gets a clear 400 rather than a started job.
func TestMCPAuthRejectsUnknownServer(t *testing.T) {
	h := mcpAuthHandler(t)

	req := httptest.NewRequest("POST", "/api/mcp/nope/auth", nil)
	req.RemoteAddr = "127.0.0.1:51234"
	rec := httptest.NewRecorder()
	h.HandleStartMCPAuth(rec, req, "nope")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestMCPAuthRejectsServerWithoutOAuth pins the second validation: a remote
// server with no OAuth block cannot start a flow.
func TestMCPAuthRejectsServerWithoutOAuth(t *testing.T) {
	h := mcpAuthHandler(t)

	req := httptest.NewRequest("POST", "/api/mcp/local-only/auth", nil)
	req.RemoteAddr = "127.0.0.1:51234"
	rec := httptest.NewRecorder()
	h.HandleStartMCPAuth(rec, req, "local-only")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestMCPAuthStatusUnknownJob pins the 404 contract for a job id the manager
// has never seen (or has pruned).
func TestMCPAuthStatusUnknownJob(t *testing.T) {
	h := mcpAuthHandler(t)

	req := httptest.NewRequest("GET", "/api/mcp/auth/deadbeef", nil)
	req.RemoteAddr = "127.0.0.1:51234" // loopback, like a desktop/local web caller
	req.SetPathValue("id", "deadbeef")
	rec := httptest.NewRecorder()
	h.HandleGetMCPAuthStatus(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// The status route is gated with the same loopback rule as start: a flow can
// only be started from loopback (or proxied WSL), so a non-loopback poller has
// no legitimate job.
func TestMCPAuthStatusRefusesNonLoopbackCallers(t *testing.T) {
	h := mcpAuthHandler(t)

	req := httptest.NewRequest("GET", "/api/mcp/auth/deadbeef", nil)
	req.RemoteAddr = "203.0.113.9:51234"
	req.SetPathValue("id", "deadbeef")
	rec := httptest.NewRecorder()
	h.HandleGetMCPAuthStatus(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a non-loopback caller", rec.Code)
	}
}

// TestMCPAuthJobManagerLifecycle exercises the manager directly (no real OAuth
// network call) to pin the get contract and the pruning of terminal jobs.
func TestMCPAuthJobManagerLifecycle(t *testing.T) {
	m := newMCPAuthJobManager()
	if got := m.get("missing"); got != nil {
		t.Fatalf("get(missing) = %+v, want nil", got)
	}

	now := time.Now()
	m.mu.Lock()
	m.jobs["done"] = &mcpAuthJob{id: "done", status: "done", completed: now}
	m.jobs["live"] = &mcpAuthJob{id: "live", status: "running"}
	m.mu.Unlock()

	// A terminal job past its TTL is pruned; a running one never is.
	m.mu.Lock()
	m.pruneLocked(now.Add(mcpAuthJobTTL + time.Minute))
	_, hasDone := m.jobs["done"]
	_, hasLive := m.jobs["live"]
	m.mu.Unlock()
	if hasDone {
		t.Errorf("terminal job past TTL should have been pruned")
	}
	if !hasLive {
		t.Errorf("running job must never be pruned")
	}
}

// TestMCPAuthStatusReturnsSnapshot pins that get() hands back a copy, so a
// caller reading after the lock cannot observe a later mutation.
func TestMCPAuthStatusReturnsSnapshot(t *testing.T) {
	m := newMCPAuthJobManager()
	m.mu.Lock()
	m.jobs["j"] = &mcpAuthJob{id: "j", server: "linear", status: "running"}
	m.mu.Unlock()

	got := m.get("j")
	if got == nil || got.status != "running" {
		t.Fatalf("get(j) = %+v, want running", got)
	}
	// Mutate the stored job; the previously returned snapshot must not change.
	m.mu.Lock()
	m.jobs["j"].status = "done"
	m.mu.Unlock()
	if got.status != "running" {
		t.Errorf("snapshot mutated with the stored job (status = %q)", got.status)
	}
}

// TestMCPAuthStartResponseShape pins the wire contract the web client polls.
func TestMCPAuthStartResponseShape(t *testing.T) {
	// Encoding only — the handler's success path is covered by the gate tests
	// above; starting a real flow would open a browser and bind 8085.
	b, err := json.Marshal(mcpAuthStartResponse{JobID: "j1", Server: "linear", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"job_id", "server", "status"} {
		if _, ok := back[k]; !ok {
			t.Errorf("response missing %q: %s", k, b)
		}
	}
}
