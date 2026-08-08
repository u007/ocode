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
