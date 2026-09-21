package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// proxiedProfileRequest builds the shape of request a remote request carries
// after the local proxy has stamped it: the originating window id plus either
// the active profile or an explicit reset when the window has no profile.
func proxiedProfileRequest(windowID, profile string) *http.Request {
	r := httptest.NewRequest("POST", "/api/chat", nil)
	if windowID != "" {
		r.Header.Set("X-Window-Id", windowID)
	}
	if profile != "" {
		r.Header.Set(remoteActiveProfileHeader, profile)
		r.Header.Set(remoteProfileAuthoritativeHeader, "1")
	} else {
		r.Header.Set(remoteProfileAuthoritativeHeader, "1")
		r.Header.Set(remoteProfileResetHeader, "1")
	}
	return r
}

// TestApplyProxiedActiveProfileMatrix pins the remote-side contract that lets a
// desktop profile selection reach a remote SSH chat turn.
func TestApplyProxiedActiveProfileMatrix(t *testing.T) {
	h := NewHandler()

	// A proxied, authoritative profile binds the window.
	h.applyProxiedActiveProfile(proxiedProfileRequest("win-a", "work"), "win-a")
	if got := h.getWindowProfile("win-a"); got != "work" {
		t.Fatalf("win-a profile = %q, want work", got)
	}

	// A bare header (no authoritative marker) must be ignored: a browser can
	// set arbitrary headers, only the local proxy's marker is trusted.
	bare := httptest.NewRequest("POST", "/api/chat", nil)
	bare.Header.Set(remoteActiveProfileHeader, "sneaky")
	h.applyProxiedActiveProfile(bare, "win-b")
	if got := h.getWindowProfile("win-b"); got != "" {
		t.Fatalf("win-b profile = %q, want empty (bare header must be ignored)", got)
	}

	// A bare reset header (no authoritative marker) must not clear a binding
	// either: a client hitting the remote directly could forge it.
	bareReset := httptest.NewRequest("POST", "/api/chat", nil)
	bareReset.Header.Set(remoteProfileResetHeader, "1")
	h.applyProxiedActiveProfile(bareReset, "win-a")
	if got := h.getWindowProfile("win-a"); got != "work" {
		t.Fatalf("win-a profile after bare reset = %q, want work (bare reset must be ignored)", got)
	}

	// Switching back to Default must clear the binding, not leave the old one.
	h.applyProxiedActiveProfile(proxiedProfileRequest("win-a", ""), "win-a")
	if got := h.getWindowProfile("win-a"); got != "" {
		t.Fatalf("win-a profile after reset = %q, want empty", got)
	}

	// No window id: nothing to bind.
	h.applyProxiedActiveProfile(proxiedProfileRequest("", "work"), "")
	if got := h.getWindowProfile(""); got != "" {
		t.Fatalf("empty-window profile = %q, want empty", got)
	}
}

// TestProxiedActiveProfileRebuildsAgentCredential is the regression test for
// the reported bug: a desktop profile with a custom opencode-go key was not
// used by chat on a remote SSH project. The remote server has the profile's
// credentials (synced on connect) but no window-state.json, so a proxied
// request must carry the active profile and the resident agent must rebuild on
// that profile's key at the next turn boundary.
func TestProxiedActiveProfileRebuildsAgentCredential(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	windowID := "win-remote-cred"
	h.sessions.RegisterWithWindow(id, proj, windowID)

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	// Profiles and auth stores are process-global; restore them afterwards so
	// these entries do not leak into sibling tests.
	credVersion := auth.ProfileCredentialVersion()
	t.Cleanup(func() { auth.SetProfileCredentialVersionForTest(credVersion) })
	if err := config.SaveProfile("remoteprof", config.ProfileDelta{}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	t.Cleanup(func() { _ = config.DeleteProfile("remoteprof") })
	prevCred, hadPrev := auth.Get("opencode-go")
	t.Cleanup(func() {
		if hadPrev {
			_ = auth.Set("opencode-go", prevCred)
		} else {
			_ = auth.Remove("opencode-go")
		}
	})
	if err := auth.Set("opencode-go", auth.Credential{Kind: auth.KindAPIKey, Key: "base-key"}); err != nil {
		t.Fatalf("set base credential: %v", err)
	}
	if err := auth.SetProfileCredential("remoteprof", "opencode-go", auth.Credential{Kind: auth.KindAPIKey, Key: "profile-key"}); err != nil {
		t.Fatalf("set profile credential: %v", err)
	}
	t.Cleanup(func() { _ = auth.DeleteProfileCredentials("remoteprof") })

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build base agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	if got := clientAPIKey(t, as); got != "base-key" {
		t.Fatalf("base client key = %q, want base-key", got)
	}
	h.replaceAgentSession(id, as)

	// The local proxy forwards the desktop window's active profile.
	h.applyProxiedActiveProfile(proxiedProfileRequest(windowID, "remoteprof"), windowID)

	reb, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reb == as {
		t.Fatalf("expected the resident agent to be rebuilt for the proxied profile")
	}
	if got := clientAPIKey(t, reb); got != "profile-key" {
		t.Fatalf("rebuilt client key = %q, want profile-key", got)
	}
}

// TestHandleSendMessageAppliesProxiedActiveProfile pins the handler wiring: the
// remote-chat path (POST /api/sessions/{id}/message, which is what the web
// client uses for an existing remote session) must apply the proxied profile
// before it reconciles the resident agent.
func TestHandleSendMessageAppliesProxiedActiveProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	h := NewHandler()
	if h.cfg != nil {
		h.cfg.Model = "fake-model"
	}
	proj := t.TempDir()
	id := session.NewSessionID()
	windowID := "win-send"
	h.sessions.RegisterWithWindow(id, proj, windowID)
	as := newTestSession(h, id, instantClient{})
	// Pre-set the agent's profile so the reconcile below is a no-op and the
	// harmless fake client stays in place (the credential rebuild itself is
	// covered by TestProxiedActiveProfileRebuildsAgentCredential).
	as.profile = "remoteprof"
	as.credVersion = auth.ProfileCredentialVersion()
	h.tryClaimTitleGen(id)

	body, _ := json.Marshal(map[string]any{"content": "hi", "windowId": windowID, "async": true})
	r := httptest.NewRequest("POST", "/api/sessions/"+id+"/message", bytes.NewReader(body))
	r.Header.Set("X-Window-Id", windowID)
	r.Header.Set(remoteActiveProfileHeader, "remoteprof")
	r.Header.Set(remoteProfileAuthoritativeHeader, "1")
	rec := httptest.NewRecorder()
	h.HandleSendMessage(rec, r, id)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if got := h.getWindowProfile(windowID); got != "remoteprof" {
		t.Fatalf("window profile = %q, want remoteprof (handler did not apply the proxied profile)", got)
	}
}

// TestRemoteProxyForwardsWindowActiveProfile covers the local half of the
// contract: the proxy must stamp the originating window's active profile (or an
// explicit reset) onto the forwarded request, and must strip a client-forged
// profile header that does not come with a window id.
func TestRemoteProxyForwardsWindowActiveProfile(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]http.Header{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	ws := &fakeTestWorkspace{apiURL: srv.URL, token: "remote-tok"}
	h := newTestProxyHandler(t, "user@realhost", "/home/user/project", ws)
	injectProxy(t, h.remoteHosts, "user@realhost", ws)

	// The desktop window's active profile is what the proxy must forward.
	h.windowProfilesMu.Lock()
	h.windowProfiles["win-9"] = "work"
	h.windowProfilesMu.Unlock()

	do := func(windowID string, forged map[string]string) http.Header {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/remote/user@realhost/api/sessions?token=local", nil)
		r.Header.Set("X-Ocode-Project", "/home/user/project")
		if windowID != "" {
			r.Header.Set("X-Window-Id", windowID)
		}
		for k, v := range forged {
			r.Header.Set(k, v)
		}
		setPathValues(r, map[string]string{"host": "user@realhost", "rest": "sessions"})
		h.HandleRemoteProxy(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("proxy status = %d; body: %s", w.Code, w.Body.String())
		}
		mu.Lock()
		defer mu.Unlock()
		return seen["/api/sessions"]
	}

	hdrs := do("win-9", nil)
	if got := hdrs.Get(remoteActiveProfileHeader); got != "work" {
		t.Fatalf("forwarded profile = %q, want work", got)
	}
	if got := hdrs.Get(remoteProfileAuthoritativeHeader); got != "1" {
		t.Fatalf("authoritative marker = %q, want 1", got)
	}

	// A window with no profile forwards an explicit reset, not a stale value.
	resetHdrs := do("win-none", nil)
	if got := resetHdrs.Get(remoteProfileResetHeader); got != "1" {
		t.Fatalf("reset header = %q, want 1", got)
	}
	if got := resetHdrs.Get(remoteProfileAuthoritativeHeader); got != "1" {
		t.Fatalf("reset authoritative marker = %q, want 1", got)
	}

	// A forged profile header is stripped when no window id travels with it.
	forged := do("", map[string]string{
		remoteActiveProfileHeader:        "evil",
		remoteProfileAuthoritativeHeader: "1",
	})
	if got := forged.Get(remoteActiveProfileHeader); got != "" {
		t.Fatalf("forged profile leaked through: %q", got)
	}
	if got := forged.Get(remoteProfileAuthoritativeHeader); got != "" {
		t.Fatalf("forged authoritative marker leaked through: %q", got)
	}
}
