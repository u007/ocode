package server

import (
	"testing"

	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// TestReconcileProfileAgentRebuildsOnProfileChange verifies the core fix for
// "the chat still uses the base config after switching profiles": when the
// window's active profile changes, the next turn rebuilds the resident agent
// on the new profile instead of reusing the stale base-config agent.
func TestReconcileProfileAgentRebuildsOnProfileChange(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	windowID := "win-test"
	h.sessions.RegisterWithWindow(id, proj, windowID)

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	// Build the agent on the base profile (no window profile set yet).
	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build base agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	if as.profile != "" {
		t.Fatalf("expected base profile '', got %q", as.profile)
	}
	h.replaceAgentSession(id, as)

	// Simulate the window's active profile switching (the server sets this in
	// handleSetWindowActiveProfile after the user picks a profile).
	h.windowProfilesMu.Lock()
	h.windowProfiles[windowID] = "switched"
	h.windowProfilesMu.Unlock()

	reb, err := h.reconcileProfileAgent(id, h.lookupAgentSession(id), as.model)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reb.profile != "switched" {
		t.Fatalf("expected rebuilt agent profile 'switched', got %q", reb.profile)
	}
	if reb == as {
		t.Fatalf("expected a new agent instance after rebuild")
	}
	if got := h.lookupAgentSession(id); got != reb {
		t.Fatalf("registry should hold the rebuilt agent")
	}
}

// TestReconcileProfileAgentNoChangeKeepsAgent ensures an unchanged profile
// does not tear down and rebuild the running agent (avoiding needless work
// and any disruption on every turn).
func TestReconcileProfileAgentNoChangeKeepsAgent(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build base agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	h.replaceAgentSession(id, as)

	reb, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if reb != as {
		t.Fatalf("expected the same agent instance when nothing changed")
	}
}

// TestReconcileProfileAgentRebuildsOnSuggestedResponsesToggle proves a
// suggested-responses toggle reaches a live session. The fragment is baked into
// the cached system prompt at build time, and a toggle made mid-turn skips the
// rebuild, so the next turn's reconcile must catch the mismatch. A second
// reconcile must then keep the rebuilt agent, or every turn would rebuild.
func TestReconcileProfileAgentRebuildsOnSuggestedResponsesToggle(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	h.sessions.Register(id, proj)

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	h.mu.Lock()
	h.cfg.Ocode.SuggestedResponsesEnabled = false
	h.mu.Unlock()
	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	if as.agent.SuggestedResponsesEnabled() {
		t.Fatalf("agent built with suggestions off reports enabled")
	}
	h.replaceAgentSession(id, as)

	h.mu.Lock()
	h.cfg.Ocode.SuggestedResponsesEnabled = true
	h.mu.Unlock()

	reb, err := h.reconcileProfileAgent(id, as, as.model)
	if err != nil {
		t.Fatalf("reconcile (after toggle on): %v", err)
	}
	if reb == as {
		t.Fatalf("expected a rebuilt agent after the suggestions toggle")
	}
	defer reb.agent.Shutdown()
	if !reb.agent.SuggestedResponsesEnabled() {
		t.Fatalf("rebuilt agent still has suggestions off")
	}

	again, err := h.reconcileProfileAgent(id, reb, reb.model)
	if err != nil {
		t.Fatalf("reconcile (no further change): %v", err)
	}
	if again != reb {
		t.Fatalf("reconcile rebuilt again with no suggestions change")
	}
}

// TestResolveSessionProfileOrdering checks the precedence env > window profile
// > global fallback used by both buildAgentSession and reconcileProfileAgent.
func TestResolveSessionProfileOrdering(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	id := session.NewSessionID()
	windowID := "win-order"
	h.sessions.RegisterWithWindow(id, proj, windowID)

	h.windowProfilesMu.Lock()
	h.windowProfiles[windowID] = "windowProfile"
	h.windowProfilesMu.Unlock()

	entry := h.sessions.Lookup(id)

	// window profile wins in the absence of env override
	if got := h.resolveSessionProfile(entry); got != "windowProfile" {
		t.Fatalf("expected windowProfile, got %q", got)
	}

	// env override wins everywhere
	t.Setenv("OCODE_PROFILE", "envProfile")
	if got := h.resolveSessionProfile(entry); got != "envProfile" {
		t.Fatalf("expected envProfile, got %q", got)
	}
}
