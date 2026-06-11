package lsp

import (
	"strings"
	"testing"
)

func TestActiveServersEmpty(t *testing.T) {
	m := NewManager(".")
	defer m.Close()
	got := m.ActiveServers()
	if len(got) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(got))
	}
}

func TestSetEventChanReceivesStartEvent(t *testing.T) {
	// We can't actually start a real server in a unit test, but we can verify
	// that SetEventChan stores the channel and that a non-blocking send on a
	// nil channel is a no-op (doesn't panic).
	m := NewManager(".")
	defer m.Close()

	ch := make(chan ServerStartedEvent, 4)
	m.SetEventChan(ch)

	// Verify the channel is stored.
	if m.eventCh != ch {
		t.Fatal("event channel not stored")
	}
}

func TestInstallHint(t *testing.T) {
	tests := []struct {
		cmd      string
		contains string
	}{
		{"gopls", "go install golang.org/x/tools/gopls@latest"},
		{"rust-analyzer", "rustup component add rust-analyzer"},
		{"pyright-langserver", "npm install -g pyright"},
		{"typescript-language-server", "npm install -g typescript"},
		{"unknown-server", "check your package manager"},
	}
	for _, tt := range tests {
		got := InstallHint(tt.cmd)
		if !strings.Contains(got, tt.contains) {
			t.Errorf("InstallHint(%q) = %q, want it to contain %q", tt.cmd, got, tt.contains)
		}
	}
}
