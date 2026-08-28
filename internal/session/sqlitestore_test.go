package session

import (
	"path/filepath"
	"testing"
)

func TestOpenSessionDBCreatesSchema(t *testing.T) {
	dir := t.TempDir()
	path := sqliteSessionPath(dir, "ses_test1")

	db, err := openSessionDB(path)
	if err != nil {
		t.Fatalf("openSessionDB: %v", err)
	}
	defer db.Close()

	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='meta'`).Scan(&name); err != nil {
		t.Fatalf("meta table not created: %v", err)
	}
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='messages'`).Scan(&name); err != nil {
		t.Fatalf("messages table not created: %v", err)
	}
}

func TestOpenIndexDBCreatesSchema(t *testing.T) {
	dir := t.TempDir()

	db, err := openIndexDB(dir)
	if err != nil {
		t.Fatalf("openIndexDB: %v", err)
	}
	defer db.Close()

	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='sessions'`).Scan(&name); err != nil {
		t.Fatalf("sessions table not created: %v", err)
	}
}

func TestSqliteSessionPath(t *testing.T) {
	got := sqliteSessionPath("/tmp/proj/sessions", "ses_abc")
	want := filepath.Join("/tmp/proj/sessions", "ses_abc.sqlite")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestIndexDBPath(t *testing.T) {
	got := indexDBPath("/tmp/proj/sessions")
	want := filepath.Join("/tmp/proj/sessions", "index.sqlite")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
