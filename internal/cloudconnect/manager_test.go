package cloudconnect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagerRegisterAndList(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir(), "")
	err := m.Register(Config{ID: "g1", Provider: "google-drive"}, SecretBundle{AccessKey: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 1 {
		t.Fatalf("expected 1, got %d", len(m.List()))
	}
	c, ok := m.Get("g1")
	if !ok || c.Provider() != "google-drive" {
		t.Fatal("get failed")
	}
}

func TestManagerBrowse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"files":[{"id":"f1","name":"a.txt","mimeType":"text/plain","size":"10","modifiedTime":"2026-01-01T00:00:00Z"}]}`))
	}))
	defer srv.Close()
	old := driveAPIBase
	driveAPIBase = srv.URL
	defer func() { driveAPIBase = old }()

	m := NewManager(t.TempDir(), t.TempDir(), "")
	_ = m.Register(Config{ID: "g1", Provider: "google-drive"}, SecretBundle{AccessKey: "tok"})
	entries, err := m.Browse(context.Background(), "g1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "a.txt" {
		t.Fatalf("bad: %+v", entries)
	}
	// unknown connector
	_, err = m.Browse(context.Background(), "nope", "")
	if err == nil {
		t.Fatal("expected error")
	}
	_ = strings.HasPrefix
}

func TestManagerRemove(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir(), "")
	_ = m.Register(Config{ID: "g1", Provider: "google-drive"}, SecretBundle{AccessKey: "tok"})
	if err := m.Remove("g1"); err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 0 {
		t.Fatal("expected empty")
	}
}

func TestManagerUnsupportedProvider(t *testing.T) {
	m := NewManager(t.TempDir(), t.TempDir(), "")
	err := m.Register(Config{ID: "x", Provider: "box"}, SecretBundle{})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}
