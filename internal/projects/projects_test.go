package projects

import (
	"path/filepath"
	"testing"
)

// NewStoreAt must persist additions across instances (same JSON path).
func TestNewStoreAtRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.json")

	store, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/a/b"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	reloaded, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt reload: %v", err)
	}
	list := reloaded.List()
	if len(list) != 1 || list[0].Path != "/a/b" {
		t.Fatalf("reloaded list = %+v, want [/a/b]", list)
	}
}

// Add is idempotent: re-adding an existing path keeps a single entry.
func TestAddIdempotent(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/x/y"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add("/x/y"); err != nil {
		t.Fatal(err)
	}
	if got := len(store.List()); got != 1 {
		t.Fatalf("list length = %d, want 1", got)
	}
}

// AddRemote scopes identity to (host, path): the same path on two different
// hosts is two distinct entries, and remote entries never collide with a
// local project at the same path string.
func TestAddRemoteScopedByHost(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.Add("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("otherhost", "/home/user/app"); err != nil {
		t.Fatal(err)
	}

	list := store.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 distinct entries (local + 2 hosts), got %d: %+v", len(list), list)
	}

	// Re-adding the same (host, path) upserts rather than duplicating.
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if got := len(store.List()); got != 3 {
		t.Fatalf("expected AddRemote re-add to upsert, list length = %d, want 3", got)
	}
}

// Local Touch/Rename must never match a remote entry that happens to share
// the same Path string.
func TestLocalOpsNeverMatchRemoteEntry(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	// Touch on a local path identical to the remote one should be a no-op
	// (nothing local exists yet), not silently touch the remote entry.
	if err := store.Touch("/home/user/app"); err != nil {
		t.Fatal(err)
	}
	list := store.List()
	if len(list) != 1 || list[0].Host != "devbox" {
		t.Fatalf("local Touch mutated or matched the remote entry: %+v", list)
	}

	if err := store.Rename("/home/user/app", "should not apply"); err == nil {
		t.Fatal("expected Rename to report not-found rather than renaming the remote entry")
	}
}

// Reorder (like Touch/Rename/SetGroup) must never touch a remote entry
// that happens to share a Path string with a local project.
func TestReorderNeverMatchesRemoteEntry(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("devbox", "/home/user/app"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add("/home/user/other"); err != nil {
		t.Fatal(err)
	}

	// Reorder local paths only — but include the shared-path string as if
	// it were meant for the local project. The remote entry's Order must
	// stay untouched (its zero value), never silently reassigned.
	if err := store.Reorder([]string{"/home/user/app", "/home/user/other"}); err != nil {
		t.Fatal(err)
	}

	list := store.List()
	for _, p := range list {
		if p.Host == "devbox" && p.Order != 0 {
			t.Errorf("Reorder mutated remote entry %+v (Order should stay 0)", p)
		}
	}
}

func TestFindLastRemotePicksMostRecent(t *testing.T) {
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if err := store.AddRemote("devbox", "/proj/a"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddRemote("devbox", "/proj/b"); err != nil {
		t.Fatal(err)
	}
	if err := store.TouchRemote("devbox", "/proj/a"); err != nil {
		t.Fatal(err)
	}

	got, ok := store.FindLastRemote("devbox")
	if !ok || got.Path != "/proj/a" {
		t.Fatalf("FindLastRemote = %+v, ok=%v, want /proj/a", got, ok)
	}

	if _, ok := store.FindLastRemote("no-such-host"); ok {
		t.Fatal("expected no match for an unknown host")
	}
}
