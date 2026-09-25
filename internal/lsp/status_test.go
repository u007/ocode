package lsp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerStatusesReportsWarmupFailure(t *testing.T) {
	original, hadOriginal := serversByExt[".go"]
	missing := filepath.Join(t.TempDir(), "missing-ocode-lsp")
	serversByExt[".go"] = serverSpec{cmd: missing, langID: "go"}
	t.Cleanup(func() {
		if hadOriginal {
			serversByExt[".go"] = original
		} else {
			delete(serversByExt, ".go")
		}
	})

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(root)
	defer mgr.Close()
	mgr.WarmUp(root)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, status := range mgr.Statuses() {
			if status.Cmd == missing && status.State == "failed" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("warmup status never reached failed state: %#v", mgr.Statuses())
}
