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
	"time"

	"github.com/u007/ocode/internal/auth"
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

// A sync write of auth.json must wait for the lock auth.Set holds, so its
// read-compare-write cannot interleave with a Set or Remove. The temp home
// mirrors setHomeTree in internal/auth: every XDG variable points under it, so
// the path resolves the same on darwin, Linux and Windows.
func TestWriteMergedLocalAuthWaitsForStoreFileLock(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)

	authPath, err := auth.AuthPath()
	if err != nil {
		t.Fatalf("AuthPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(authPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("seed auth.json: %v", err)
	}

	// Hold the store's file lock, as a concurrent Set would.
	held := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- auth.WithStoreFileLock(func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	type result struct {
		wrote bool
		err   error
	}
	resCh := make(chan result, 1)
	go func() {
		wrote, err := writeMergedLocal(BlobTypeAuth, authPath, json.RawMessage(`{}`), []byte(`{"a":1}`))
		resCh <- result{wrote, err}
	}()

	select {
	case r := <-resCh:
		t.Fatalf("writeMergedLocal finished while the auth file lock was held (wrote=%v, err=%v)", r.wrote, r.err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	if err := <-holderDone; err != nil {
		t.Fatalf("WithStoreFileLock: %v", err)
	}
	r := <-resCh
	if r.err != nil || !r.wrote {
		t.Fatalf("writeMergedLocal after release: wrote=%v err=%v", r.wrote, r.err)
	}
	got, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Errorf("auth.json after sync write = %q, want the merged blob", got)
	}
}
