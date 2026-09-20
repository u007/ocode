package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// clientAPIKey digs the raw API key out of a session's resident agent client so
// a test can prove which credential the next turn will authenticate with.
func clientAPIKey(t *testing.T, as *agentSession) string {
	t.Helper()
	if as == nil || as.agent == nil {
		t.Fatalf("no agent session")
	}
	client := as.agent.Client()
	if client == nil {
		t.Fatalf("no client on agent")
	}
	gc, ok := client.(*agent.GenericClient)
	if !ok {
		t.Fatalf("client is %T, want *GenericClient", client)
	}
	return gc.APIKey
}

// pickWindowProfile drives the real PUT /api/window/{id}/activeProfile handler,
// so the test covers the exact request the web ProfileSwitcher issues.
func pickWindowProfile(t *testing.T, h *Handler, windowID, profile string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"profile": profile})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/window/"+windowID+"/activeProfile", bytes.NewReader(body))
	req.SetPathValue("id", windowID)
	h.handleSetWindowActiveProfile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set active profile: %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestReconcileProfileAgentAppliesProfileCredential is the regression test for
// the reported bug: switching the top-right profile on an already-open chat
// left the session authenticating with the base key. A profile that only
// overrides credentials (no model delta) must still rebuild the resident client
// on that profile's key at the next turn boundary.
//
// The session is window-bound exactly as HandleSendMessage binds it, and the
// profile is selected through the real endpoint the web pill calls.
func TestReconcileProfileAgentAppliesProfileCredential(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	windowID := "win-cred"
	h.sessions.RegisterWithWindow(id, proj, windowID)

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	// The profile must exist in config for the handler to accept it (a keys-only
	// profile has an empty delta). Both the auth store and the config profile map
	// are process-global, so restore them afterwards — otherwise these entries
	// leak into sibling tests that build agents for the same provider.
	credVersion := auth.ProfileCredentialVersion()
	t.Cleanup(func() { auth.SetProfileCredentialVersionForTest(credVersion) })
	if err := config.SaveProfile("credprof", config.ProfileDelta{}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	t.Cleanup(func() { _ = config.DeleteProfile("credprof") })
	// Base credential for opencode-go, and a DIFFERENT key on the profile.
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
	if err := auth.SetProfileCredential("credprof", "opencode-go", auth.Credential{Kind: auth.KindAPIKey, Key: "profile-key"}); err != nil {
		t.Fatalf("set profile credential: %v", err)
	}
	t.Cleanup(func() { _ = auth.DeleteProfileCredentials("credprof") })

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build base agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	if got := clientAPIKey(t, as); got != "base-key" {
		t.Fatalf("base client key = %q, want base-key", got)
	}
	h.replaceAgentSession(id, as)

	// The user picks the credentials-only profile in the pill.
	pickWindowProfile(t, h, windowID, "credprof")

	reb, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reb == as {
		t.Fatalf("expected the resident agent to be rebuilt for the credential-only profile")
	}
	if got := clientAPIKey(t, reb); got != "profile-key" {
		t.Fatalf("rebuilt client key = %q, want profile-key", got)
	}
}
