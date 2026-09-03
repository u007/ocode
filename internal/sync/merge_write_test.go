package sync

// Regression test for the HIGH finding: a local config/auth edit made while
// a push/pull is in flight must not be overwritten by a merge computed from
// a stale snapshot. writeMergedLocal compares before replacing; callers
// re-merge against the fresh file instead of losing the edit.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteMergedLocalAbortsOnMovedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")

	if err := os.WriteFile(path, []byte(`{"a":1}`), 0600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Merge computed from a stale snapshot must NOT overwrite the newer file.
	wrote, err := writeMergedLocal(BlobTypeConfig, path, json.RawMessage(`{"stale":0}`), []byte(`{"merged":1}`))
	if err != nil {
		t.Fatalf("writeMergedLocal: %v", err)
	}
	if wrote {
		t.Fatalf("writeMergedLocal overwrote a newer local edit")
	}
	if data, _ := os.ReadFile(path); string(data) != `{"a":1}` {
		t.Fatalf("newer local edit was damaged: %s", data)
	}

	// Merge computed from the current content writes normally.
	wrote, err = writeMergedLocal(BlobTypeConfig, path, json.RawMessage(`{"a":1}`), []byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatalf("writeMergedLocal: %v", err)
	}
	if !wrote {
		t.Fatalf("writeMergedLocal refused a write based on current content")
	}
	if data, _ := os.ReadFile(path); string(data) != `{"a":1,"b":2}` {
		t.Fatalf("merged content not written: %s", data)
	}
}
