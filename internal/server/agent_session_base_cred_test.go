package server

import (
	"testing"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// Setting a provider key from the TUI /connect dialog or the web/desktop
// Connectors settings writes the BASE store (auth.json). A resident agent
// resolves its key at client construction (agent.NewClientWithProfile), so it
// keeps serving the old key unless something rebuilds it.
//
// reconcileProfileAgent already rebuilds on a credential-version mismatch —
// its comment says the version is "global, not per-profile: an in-place edit
// must invalidate the cached client" — but the base store never bumped that
// version, so only profile edits ever triggered the rebuild. This pins the
// base-store half of that promise.
func TestReconcileProfileAgentRebuildsAfterBaseCredentialEdit(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	// Both stores are process-global; restore them so nothing leaks into
	// sibling tests that build agents for the same provider.
	credVersion := auth.CredentialVersion()
	t.Cleanup(func() { auth.SetCredentialVersionForTest(credVersion) })
	prevCred, hadPrev := auth.Get("opencode-go")
	t.Cleanup(func() {
		if hadPrev {
			_ = auth.Set("opencode-go", prevCred)
		} else {
			_ = auth.Remove("opencode-go")
		}
	})
	if err := auth.Set("opencode-go", auth.Credential{Kind: auth.KindAPIKey, Key: "base-key-1"}); err != nil {
		t.Fatalf("set base credential: %v", err)
	}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	if got := clientAPIKey(t, as); got != "base-key-1" {
		t.Fatalf("initial client key = %q, want base-key-1", got)
	}
	h.replaceAgentSession(id, as)

	// The user replaces the key in Settings → Connectors.
	if err := auth.Set("opencode-go", auth.Credential{Kind: auth.KindAPIKey, Key: "base-key-2"}); err != nil {
		t.Fatalf("replace base credential: %v", err)
	}

	reb, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reb == as {
		t.Fatalf("resident agent was not rebuilt after a base-store credential edit; " +
			"it will keep sending the old key (see auth.CredentialVersion)")
	}
	defer reb.agent.Shutdown()
	if got := clientAPIKey(t, reb); got != "base-key-2" {
		t.Fatalf("rebuilt client key = %q, want base-key-2", got)
	}
}

// Removing a key must invalidate too: the next turn has to rebuild rather than
// keep authenticating with a credential the user deleted.
func TestReconcileProfileAgentRebuildsAfterBaseCredentialRemoval(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	credVersion := auth.CredentialVersion()
	t.Cleanup(func() { auth.SetCredentialVersionForTest(credVersion) })
	prevCred, hadPrev := auth.Get("opencode-go")
	t.Cleanup(func() {
		if hadPrev {
			_ = auth.Set("opencode-go", prevCred)
		} else {
			_ = auth.Remove("opencode-go")
		}
	})
	if err := auth.Set("opencode-go", auth.Credential{Kind: auth.KindAPIKey, Key: "base-key-1"}); err != nil {
		t.Fatalf("set base credential: %v", err)
	}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	h.replaceAgentSession(id, as)

	if err := auth.Remove("opencode-go"); err != nil {
		t.Fatalf("remove base credential: %v", err)
	}

	reb, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reb == as {
		t.Fatalf("resident agent was not rebuilt after the base credential was removed")
	}
	defer reb.agent.Shutdown()
}
