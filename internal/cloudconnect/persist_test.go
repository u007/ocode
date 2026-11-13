package cloudconnect

import (
	"testing"
)

func TestManagerPersistence(t *testing.T) {
	dataDir := t.TempDir()
	projectRoot := t.TempDir()

	m := NewManager(dataDir, projectRoot, "secret")
	err := m.Register(Config{ID: "g1", Provider: "google-drive", RootPath: "root123"}, SecretBundle{AccessKey: "tok"})
	if err != nil {
		t.Fatal(err)
	}

	// New manager with same master password loads connector.
	m2 := NewManager(dataDir, projectRoot, "secret")
	c, ok := m2.Get("g1")
	if !ok {
		t.Fatal("connector not loaded on restart")
	}
	if c.ID() != "g1" {
		t.Fatal("bad id")
	}
}
