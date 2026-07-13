package rc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetBaseDirForTest(dir)
	t.Cleanup(func() { SetBaseDirForTest("") })
	return dir
}

func TestRegisterListUnregister(t *testing.T) {
	dir := withTempDir(t)
	e := Entry{
		InstanceID: "abc123",
		SessionID:  "sess-xyz",
		Model:      "claude",
		CWD:        "/tmp/proj",
		Addr:       "localhost:4096",
		Token:      "secret",
		PID:        1234,
		StartedAt:  time.Now().Unix(),
		LastSeen:   time.Now().Unix(),
	}
	if err := Register(e); err != nil {
		t.Fatalf("Register: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, FilePattern))
	if len(matches) != 1 {
		t.Fatalf("expected 1 instance file, got %d", len(matches))
	}

	list, err := List(DefaultTTL)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].InstanceID != "abc123" {
		t.Fatalf("unexpected list: %+v", list)
	}

	found, ok := Find("abc123")
	if !ok || found.SessionID != "sess-xyz" {
		t.Fatalf("Find by instance id failed: %+v", found)
	}
	if _, ok := Find("sess-xyz"); !ok {
		t.Fatalf("Find by session id failed")
	}
	if _, ok := Find("xyz"); !ok {
		t.Fatalf("Find by session prefix failed")
	}

	if err := Unregister("abc123"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	list, _ = List(DefaultTTL)
	if len(list) != 0 {
		t.Fatalf("expected empty list after unregister, got %d", len(list))
	}
}

func TestListPrunesStale(t *testing.T) {
	dir := withTempDir(t)
	// Write the stale entry directly (bypassing Register, which stamps
	// LastSeen=now) so we can exercise List's TTL pruning.
	old := Entry{
		InstanceID: "stale",
		SessionID:  "old",
		Addr:       "localhost:1",
		StartedAt:  time.Now().Add(-10 * time.Minute).Unix(),
		LastSeen:   time.Now().Add(-10 * time.Minute).Unix(),
	}
	fresh := Entry{
		InstanceID: "fresh",
		SessionID:  "new",
		Addr:       "localhost:2",
		StartedAt:  time.Now().Unix(),
		LastSeen:   time.Now().Unix(),
	}
	writeEntry(t, dir, old)
	writeEntry(t, dir, fresh)

	list, err := List(DefaultTTL)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].InstanceID != "fresh" {
		t.Fatalf("stale entry not pruned: %+v", list)
	}
}

func writeEntry(t *testing.T, dir string, e Entry) {
	t.Helper()
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance-"+e.InstanceID+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}
