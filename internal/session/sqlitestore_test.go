package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
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

func TestWriteSqliteSessionFullThenRead(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)

	s := Session{
		ID:             "ses_full1",
		Title:          "hello world",
		TitleGenerated: true,
		Messages: []agent.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{"total_tokens": float64(42)},
	}

	if err := writeSqliteSessionFull(dir, s); err != nil {
		t.Fatalf("writeSqliteSessionFull: %v", err)
	}

	got, err := readSqliteSession(sqliteSessionPath(dir, "ses_full1"))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	if got.ID != s.ID || got.Title != s.Title || !got.TitleGenerated {
		t.Fatalf("meta mismatch: %+v", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("created_at mismatch: got %v want %v", got.CreatedAt, now)
	}
	if len(got.Messages) != 2 || got.Messages[0].Content != "hi" || got.Messages[1].Content != "hello" {
		t.Fatalf("messages mismatch: %+v", got.Messages)
	}
	if got.Metadata["total_tokens"] != float64(42) {
		t.Fatalf("metadata mismatch: %+v", got.Metadata)
	}
}

func TestAppendSqliteSessionOnlyInsertsNewMessages(t *testing.T) {
	dir := t.TempDir()
	id := "ses_append1"

	if err := writeSqliteSessionFull(dir, Session{
		ID:        id,
		Messages:  []agent.Message{{Role: "user", Content: "one"}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("writeSqliteSessionFull: %v", err)
	}

	full := []agent.Message{
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "two"},
		{Role: "user", Content: "three"},
	}
	if err := appendSqliteSession(dir, id, "", full, nil); err != nil {
		t.Fatalf("appendSqliteSession: %v", err)
	}

	got, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[2].Content != "three" {
		t.Fatalf("expected third message 'three', got %q", got.Messages[2].Content)
	}
}

func TestAppendSqliteSessionExplicitTitleOverrides(t *testing.T) {
	dir := t.TempDir()
	id := "ses_title1"

	if err := writeSqliteSessionFull(dir, Session{
		ID:        id,
		Title:     "auto title",
		Messages:  []agent.Message{{Role: "user", Content: "hi"}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("writeSqliteSessionFull: %v", err)
	}

	if err := appendSqliteSession(dir, id, "Explicit Title", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("appendSqliteSession: %v", err)
	}

	got, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	if got.Title != "Explicit Title" || !got.TitleGenerated {
		t.Fatalf("expected explicit title to win, got %+v", got)
	}
}

func TestAppendSqliteSessionShrinkingMessagesReplacesAll(t *testing.T) {
	dir := t.TempDir()
	id := "ses_shrink1"

	if err := writeSqliteSessionFull(dir, Session{
		ID: id,
		Messages: []agent.Message{
			{Role: "user", Content: "one"},
			{Role: "assistant", Content: "two"},
			{Role: "user", Content: "three"},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("writeSqliteSessionFull: %v", err)
	}

	// Simulates /compact splicing out earlier messages.
	shrunk := []agent.Message{{Role: "user", Content: "compacted summary"}}
	if err := appendSqliteSession(dir, id, "", shrunk, nil); err != nil {
		t.Fatalf("appendSqliteSession: %v", err)
	}

	got, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "compacted summary" {
		t.Fatalf("expected message set replaced wholesale, got %+v", got.Messages)
	}
}
