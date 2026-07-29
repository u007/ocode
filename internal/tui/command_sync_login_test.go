package tui

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/sync"
)

func TestRunSyncLogoutCmdCancelsPendingLoginWithoutSavedToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	m := &model{
		syncClient:     sync.NewClient("http://127.0.0.1:65535"),
		syncDeviceCode: "pending-device-code",
	}
	stopped := false
	m.syncStop = func() { stopped = true }

	cmd := runSyncLogoutCmd(m, nil)
	if cmd == nil {
		t.Fatal("expected logout command")
	}

	msg := cmd()
	status, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("expected statusMsg, got %T", msg)
	}
	if !strings.Contains(status.text, "Logged out of sync") {
		t.Fatalf("expected logout status, got %q", status.text)
	}
	if !stopped {
		t.Fatal("expected pending watcher to be stopped")
	}
	if m.syncStop != nil {
		t.Fatal("expected syncStop to be cleared")
	}
	if m.syncClient != nil || m.syncDeviceCode != "" {
		t.Fatalf("expected pending login state to be cleared, got client=%v deviceCode=%q", m.syncClient, m.syncDeviceCode)
	}
}
