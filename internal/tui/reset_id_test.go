package tui

import (
	"os"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/redact"
	"github.com/u007/ocode/internal/server"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// newResetIDTestModel builds a minimal model with a persisted session, so
// handleResetIDCmd has a real transcript to re-key.
func newResetIDTestModel(t *testing.T, workDir, sessionID string) model {
	t.Helper()
	session.SetWorkDir(workDir)
	t.Cleanup(func() { session.SetWorkDir("") })
	if err := session.Save(sessionID, "Chat", []agent.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	return model{
		input:     textarea.New(),
		viewport:  fastviewport.New(80, 20),
		styles:    ApplyThemeColors("tokyonight"),
		workDir:   workDir,
		sessionID: sessionID,
	}
}

// TestHandleResetIDCmdRekeysSession is the TUI regression for /reset-id: the
// model's session id moves to a fresh value, the transcript is preserved, and
// the old id no longer resolves.
func TestHandleResetIDCmdRekeysSession(t *testing.T) {
	dir := t.TempDir()
	oldID := "ses_2026-01-02-030405-cafebabe"
	m := newResetIDTestModel(t, dir, oldID)

	m.handleResetIDCmd(nil)

	if m.sessionID == oldID || m.sessionID == "" {
		t.Fatalf("sessionID = %q, want a fresh id", m.sessionID)
	}
	if _, err := session.LoadForDir(dir, oldID); err == nil {
		t.Fatal("old id still loads after /reset-id")
	}
	moved, err := session.LoadForDir(dir, m.sessionID)
	if err != nil {
		t.Fatalf("load rekeyed session: %v", err)
	}
	if len(moved.Messages) != 2 {
		t.Fatalf("transcript count = %d, want 2", len(moved.Messages))
	}
	last := m.messages[len(m.messages)-1]
	if !strings.Contains(last.text, oldID) || !strings.Contains(last.text, m.sessionID) {
		t.Fatalf("notice does not name both ids: %q", last.text)
	}
}

// TestHandleResetIDCmdRefusesMidTurn pins the guard: a rekey must never run
// while a turn is streaming.
func TestHandleResetIDCmdRefusesMidTurn(t *testing.T) {
	dir := t.TempDir()
	oldID := "ses_2026-01-02-030405-cafebabe"
	m := newResetIDTestModel(t, dir, oldID)
	m.streaming = true

	m.handleResetIDCmd(nil)

	if m.sessionID != oldID {
		t.Fatalf("sessionID changed to %q during a turn, want %q", m.sessionID, oldID)
	}
	if _, err := session.LoadForDir(dir, oldID); err != nil {
		t.Fatalf("old session should still exist: %v", err)
	}
	last := m.messages[len(m.messages)-1]
	if !strings.Contains(last.text, "Cannot reset") {
		t.Fatalf("expected refusal notice, got %q", last.text)
	}
}

// TestHandleResetIDCmdRefusesWhileRemoteControl pins the RC guard: the bridge
// caches the session id and must not be mutated underneath its HTTP
// goroutines.
func TestHandleResetIDCmdRefusesWhileRemoteControl(t *testing.T) {
	dir := t.TempDir()
	oldID := "ses_2026-01-02-030405-cafebabe"
	m := newResetIDTestModel(t, dir, oldID)
	// A non-nil bridge is enough to trip the guard without starting a server.
	m.rcBridge = &server.RCBridge{}

	m.handleResetIDCmd(nil)

	if m.sessionID != oldID {
		t.Fatalf("sessionID changed to %q while /rc active, want %q", m.sessionID, oldID)
	}
}

// TestResetIDKeepsTokenResolution pins the vault half of /reset-id: the
// registry object is kept (same nonce), so an OCSEC token already written into
// the transcript still resolves, and the vault file moves to the new id.
func TestResetIDKeepsTokenResolution(t *testing.T) {
	dir := t.TempDir()
	oldID := "ses_2026-01-02-030405-cafebabe"
	m := newResetIDTestModel(t, dir, oldID)

	reg := redact.NewRegistry(redact.NewNonce())
	reg.GetOrAssign("hunter2-secret", "password", "session")
	m.redactionRegistry = reg
	nonce := reg.Nonce()
	tokenised := reg.Substitute("the password is hunter2-secret")
	if tokenised == "the password is hunter2-secret" {
		t.Fatal("test setup: value was not tokenised")
	}

	base, err := redact.DefaultVaultBase()
	if err != nil {
		t.Fatalf("DefaultVaultBase() error: %v", err)
	}
	slug := session.ProjectSlugForPath(dir)
	oldVault := redact.VaultPath(base, slug, oldID)
	if err := redact.SaveVault(oldVault, reg); err != nil {
		t.Fatalf("SaveVault(old) error: %v", err)
	}

	m.handleResetIDCmd(nil)

	newID := m.sessionID
	if newID == oldID || newID == "" {
		t.Fatalf("sessionID = %q, want a fresh id", newID)
	}
	if m.redactionRegistry != reg {
		t.Fatal("/reset-id replaced the in-memory registry")
	}
	if m.redactionRegistry.Nonce() != nonce {
		t.Fatalf("nonce changed across reset: %q -> %q", nonce, m.redactionRegistry.Nonce())
	}
	if got := m.redactionRegistry.Resolve(tokenised); got != "the password is hunter2-secret" {
		t.Fatalf("token no longer resolves after reset: %q", got)
	}

	if _, err := os.Stat(oldVault); !os.IsNotExist(err) {
		t.Errorf("old vault still present after reset (err=%v)", err)
	}
	loaded, err := redact.LoadVault(redact.VaultPath(base, slug, newID))
	if err != nil {
		t.Fatalf("LoadVault(new) error: %v", err)
	}
	if loaded.Nonce() != nonce {
		t.Errorf("vault nonce = %q, want %q", loaded.Nonce(), nonce)
	}
	if entry, ok := loaded.Lookup(1); !ok || entry.Value != "hunter2-secret" {
		t.Errorf("rekeyed vault entry = %+v (ok=%v), want hunter2-secret", entry, ok)
	}
}
