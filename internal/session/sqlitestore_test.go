package session

import (
	"os"
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

func TestUpsertAndQueryIndexRow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	if err := upsertIndexRow(dir, ocodeMeta{ID: "ses_idx1", Title: "first", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsertIndexRow: %v", err)
	}

	metas, err := queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	if len(metas) != 1 || metas[0].ID != "ses_idx1" || metas[0].Title != "first" {
		t.Fatalf("expected 1 indexed row, got %+v", metas)
	}

	// Upsert again with a new title — must update, not duplicate.
	later := now.Add(time.Hour)
	if err := upsertIndexRow(dir, ocodeMeta{ID: "ses_idx1", Title: "renamed", CreatedAt: now, UpdatedAt: later}); err != nil {
		t.Fatalf("upsertIndexRow (update): %v", err)
	}
	metas, err = queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	if len(metas) != 1 || metas[0].Title != "renamed" || !metas[0].UpdatedAt.Equal(later) {
		t.Fatalf("expected upsert to update in place, got %+v", metas)
	}
}

func TestQueryIndexMetasOnMissingIndexReturnsEmpty(t *testing.T) {
	dir := t.TempDir() // no index.sqlite ever written here
	metas, err := queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("expected no rows, got %+v", metas)
	}
}

func TestDeleteIndexRow(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := upsertIndexRow(dir, ocodeMeta{ID: "ses_del1", Title: "x", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("upsertIndexRow: %v", err)
	}
	if err := deleteIndexRow(dir, "ses_del1"); err != nil {
		t.Fatalf("deleteIndexRow: %v", err)
	}
	metas, err := queryIndexMetas(dir)
	if err != nil {
		t.Fatalf("queryIndexMetas: %v", err)
	}
	if len(metas) != 0 {
		t.Fatalf("expected row removed, got %+v", metas)
	}
}

func TestDeleteIndexRowOnMissingIndexIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := deleteIndexRow(dir, "ses_never_existed"); err != nil {
		t.Fatalf("deleteIndexRow on missing index.sqlite should be a no-op, got: %v", err)
	}
}

func TestMergeMetasIndexShadowsSameIDLegacyEntry(t *testing.T) {
	now := time.Now()
	legacy := []ocodeMeta{
		{ID: "ses_a", Title: "legacy a", UpdatedAt: now},
		{ID: "ses_b", Title: "legacy b (stale orphan)", UpdatedAt: now},
	}
	indexed := []ocodeMeta{
		{ID: "ses_b", Title: "migrated b", UpdatedAt: now.Add(time.Minute)},
	}

	merged := mergeMetas(legacy, indexed)

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged entries, got %d: %+v", len(merged), merged)
	}
	var gotB ocodeMeta
	found := false
	for _, m := range merged {
		if m.ID == "ses_b" {
			gotB = m
			found = true
		}
	}
	if !found || gotB.Title != "migrated b" {
		t.Fatalf("expected indexed entry to shadow legacy entry for ses_b, got %+v", merged)
	}
}

func TestMergeMetasNoIndexReturnsLegacyUnchanged(t *testing.T) {
	legacy := []ocodeMeta{{ID: "ses_a", Title: "a"}}
	merged := mergeMetas(legacy, nil)
	if len(merged) != 1 || merged[0].ID != "ses_a" {
		t.Fatalf("expected legacy passed through unchanged, got %+v", merged)
	}
}

func TestOpenDBWithSpecialCharacters(t *testing.T) {
	// Paths containing URI-significant characters must still open and create schema.
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "proj%name?with#hash")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir special dir: %v", err)
	}
	path := sqliteSessionPath(dir, "ses_special1")
	db, err := openSessionDB(path)
	if err != nil {
		t.Fatalf("openSessionDB with special chars: %v", err)
	}
	defer db.Close()
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='meta'`).Scan(&name); err != nil {
		t.Fatalf("meta table not created for special path: %v", err)
	}
	// Also verify we can write and read via helpers
	s := Session{
		ID:        "ses_special1",
		Title:     "special",
		Messages:  []agent.Message{{Role: "user", Content: "hi"}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  map[string]any{"x": float64(1)},
	}
	if err := writeSqliteSessionFull(dir, s); err != nil {
		t.Fatalf("writeSqliteSessionFull special: %v", err)
	}
	got, err := readSqliteSession(path)
	if err != nil {
		t.Fatalf("readSqliteSession special: %v", err)
	}
	if got.Title != "special" || len(got.Messages) != 1 {
		t.Fatalf("unexpected session for special path: %+v", got)
	}
}

func TestAppendPreservesMetadataWhenNil(t *testing.T) {
	dir := t.TempDir()
	id := "ses_meta_append"
	initialMeta := map[string]any{"claude_original_session_id": "orig-123", "keep": float64(1)}
	if err := writeSqliteSessionFull(dir, Session{
		ID:        id,
		Title:     "t",
		Messages:  []agent.Message{{Role: "user", Content: "hi"}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  initialMeta,
	}); err != nil {
		t.Fatalf("writeSqliteSessionFull: %v", err)
	}
	// Append with nil metadata — should preserve existing
	if err := appendSqliteSession(dir, id, "", []agent.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "there"}}, nil); err != nil {
		t.Fatalf("appendSqliteSession nil meta: %v", err)
	}
	got, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	if got.Metadata["claude_original_session_id"] != "orig-123" || got.Metadata["keep"] != float64(1) {
		t.Fatalf("expected metadata preserved when nil passed, got %+v", got.Metadata)
	}
	// Append with new metadata — should replace
	newMeta := map[string]any{"new": float64(2)}
	if err := appendSqliteSession(dir, id, "", []agent.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "there"}, {Role: "user", Content: "again"}}, newMeta); err != nil {
		t.Fatalf("appendSqliteSession new meta: %v", err)
	}
	got, err = readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		t.Fatalf("readSqliteSession: %v", err)
	}
	if got.Metadata["new"] != float64(2) {
		t.Fatalf("expected new metadata to replace, got %+v", got.Metadata)
	}
	if _, ok := got.Metadata["claude_original_session_id"]; ok {
		t.Fatalf("expected old metadata discarded when new supplied, got %+v", got.Metadata)
	}
}
