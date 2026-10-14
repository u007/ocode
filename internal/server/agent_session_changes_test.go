package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/snapshot"
	"github.com/u007/ocode/internal/tool"
)

// TestBuildAgentSessionRehydratesChangesFromProjectJournal covers the desktop
// case where the process cwd is not the project (the .app launches with cwd
// "/"). The session's snapshots are journaled under the project's snapshot
// dir; a rebuilt agent must still list them on its Changes tab.
func TestBuildAgentSessionRehydratesChangesFromProjectJournal(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	const id = "ses_changes_rehydrate"
	h.sessions.Register(id, proj)

	base, err := paths.GlobalDataDir()
	if err != nil {
		t.Fatal(err)
	}
	snapDir := filepath.Join(base, "project", paths.ProjectSlug(proj), "snapshots")
	filePath := filepath.Join(proj, "a.txt")
	if err := os.WriteFile(filePath, []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	seed := snapshot.NewStore(snapshot.NewAgentID(), snapDir)
	seed.SetSessionID(id)
	if err := seed.Backup(filePath, "tc-1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Desktop-like process cwd: a directory whose snapshot journal is empty.
	t.Chdir(t.TempDir())

	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()

	got := as.agent.Changes().List()
	if len(got) != 1 {
		t.Fatalf("expected 1 change rehydrated from the project journal, got %d", len(got))
	}
	if got[0].OriginalPath != filePath {
		t.Fatalf("expected change for %s, got %s", filePath, got[0].OriginalPath)
	}
}
