package session

import (
	"testing"

	"github.com/u007/ocode/internal/agent"
)

func TestLoadForDir(t *testing.T) {
	root := t.TempDir()
	SetWorkDir(root)
	t.Cleanup(func() { SetWorkDir("") })
	id := NewSessionID()
	if err := Save(id, "other project", []agent.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadForDir(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != id || len(loaded.Messages) != 1 {
		t.Fatalf("loaded wrong session: %#v", loaded)
	}
	if _, err := LoadForDir(root, "ses_missing"); err == nil {
		t.Fatal("expected missing session error")
	}
}
