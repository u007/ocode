package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscardRecentRemovesBackups(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile("file.txt", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	snapshots = nil
	redoStack = nil
	mu.Unlock()

	if err := Backup("file.txt"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	if len(snapshots) != 1 {
		mu.Unlock()
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	mu.Unlock()

	if err := DiscardRecent(1); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(snapshots) != 0 {
		t.Fatalf("expected snapshots to be cleared, got %d", len(snapshots))
	}
	entries, err := os.ReadDir(filepath.Join(tmpDir, ".opencode", "snapshots"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected snapshot files to be removed, got %d", len(entries))
	}
}
