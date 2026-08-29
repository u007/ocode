# Task 2: Per-session read/write core

**Files:**
- Modify: `internal/session/sqlitestore.go`
- Test: `internal/session/sqlitestore_test.go`

**Interfaces:**
- Consumes: `openSessionDB(path string) (*sql.DB, error)`, `sqliteSessionPath(dir, id string) string` (Task 1); `Session{ID, Title, TitleGenerated, Messages, CreatedAt, UpdatedAt, Metadata}` (existing, `session.go:24`); `agent.Message` (existing, `internal/agent/client.go:81`).
- Produces: `writeSqliteSessionFull(dir string, s Session) error`, `appendSqliteSession(dir, id, title string, messages []agent.Message, metadata map[string]any) error`, `readSqliteSession(path string) (*Session, error)`.

- [x] **Step 1: Write the failing test**

```go
// internal/session/sqlitestore_test.go — add to existing file

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
```

Add the needed imports to the test file: `"time"` and `"github.com/u007/ocode/internal/agent"`.

- [x] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/session/... -run 'TestWriteSqliteSessionFullThenRead|TestAppendSqliteSessionOnlyInsertsNewMessages|TestAppendSqliteSessionExplicitTitleOverrides|TestAppendSqliteSessionShrinkingMessagesReplacesAll' -v`
Expected: FAIL — `writeSqliteSessionFull`, `appendSqliteSession`, `readSqliteSession` undefined.

- [x] **Step 3: Implement the read/write core**

Add to `internal/session/sqlitestore.go` (add `"encoding/json"` and
`"time"` to the existing import block, plus
`"github.com/u007/ocode/internal/agent"`):

```go
// writeSqliteSessionFull creates path fresh (it must not already exist —
// callers that might be re-creating an existing session should use
// appendSqliteSession instead) and writes the full session, meta plus
// every message, in one transaction. Used for brand-new sessions and for
// one-time migration of an existing .json/.ojsonl session (see
// migrateToSqlite in Task 4).
func writeSqliteSessionFull(dir string, s Session) error {
	path := sqliteSessionPath(dir, s.ID)
	db, err := openSessionDB(path)
	if err != nil {
		return fmt.Errorf("session: open sqlite %s: %w", s.ID, err)
	}
	defer db.Close()

	metaJSON, err := json.Marshal(s.Metadata)
	if err != nil {
		return fmt.Errorf("session: marshal metadata %s: %w", s.ID, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("session: begin tx %s: %w", s.ID, err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`INSERT INTO meta (id, title, title_generated, created_at, updated_at, metadata_json) VALUES (?, ?, ?, ?, ?, ?)`,
		s.ID, s.Title, s.TitleGenerated, s.CreatedAt, s.UpdatedAt, string(metaJSON),
	); err != nil {
		return fmt.Errorf("session: insert meta %s: %w", s.ID, err)
	}

	stmt, err := tx.Prepare(`INSERT INTO messages (seq, data) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("session: prepare insert %s: %w", s.ID, err)
	}
	defer stmt.Close()
	for i, m := range s.Messages {
		data, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("session: marshal message %d of %s: %w", i, s.ID, err)
		}
		if _, err := stmt.Exec(i, string(data)); err != nil {
			return fmt.Errorf("session: insert message %d of %s: %w", i, s.ID, err)
		}
	}

	return tx.Commit()
}

// appendSqliteSession updates an already-.sqlite session's meta row and
// inserts only the messages appended since the last save — the sqlite
// counterpart to saveOjsonl's incremental-append design in ojsonl.go
// (state.count), so a long session doesn't pay to rewrite its whole
// history on every turn. title is the caller's explicit-title-this-save
// signal exactly as in saveOjsonl/saveJSON: "" means keep the existing
// title, non-empty always wins and marks the title as explicitly set.
func appendSqliteSession(dir, id, title string, messages []agent.Message, metadata map[string]any) error {
	path := sqliteSessionPath(dir, id)
	db, err := openSessionDB(path)
	if err != nil {
		return fmt.Errorf("session: open sqlite %s: %w", id, err)
	}
	defer db.Close()

	var existingTitle string
	var existingTitleGenerated bool
	if err := db.QueryRow(`SELECT title, title_generated FROM meta WHERE id = ?`, id).
		Scan(&existingTitle, &existingTitleGenerated); err != nil {
		return fmt.Errorf("session: read meta %s: %w", id, err)
	}
	var existingCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&existingCount); err != nil {
		return fmt.Errorf("session: count messages %s: %w", id, err)
	}

	resolvedTitle := existingTitle
	titleGenerated := existingTitleGenerated
	if title != "" {
		resolvedTitle = title
		titleGenerated = true
	}

	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("session: marshal metadata %s: %w", id, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("session: begin tx %s: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`UPDATE meta SET title = ?, title_generated = ?, updated_at = ?, metadata_json = ? WHERE id = ?`,
		resolvedTitle, titleGenerated, time.Now(), string(metaJSON), id,
	); err != nil {
		return fmt.Errorf("session: update meta %s: %w", id, err)
	}

	if existingCount > len(messages) {
		// Message count shrank (e.g. /compact) — the append-only path
		// can't represent that, so replace the message set wholesale.
		// Mirrors saveOjsonl's identical handling in ojsonl.go.
		if _, err := tx.Exec(`DELETE FROM messages`); err != nil {
			return fmt.Errorf("session: clear messages %s: %w", id, err)
		}
		existingCount = 0
	}

	stmt, err := tx.Prepare(`INSERT INTO messages (seq, data) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("session: prepare insert %s: %w", id, err)
	}
	defer stmt.Close()
	for i := existingCount; i < len(messages); i++ {
		data, err := json.Marshal(messages[i])
		if err != nil {
			return fmt.Errorf("session: marshal message %d of %s: %w", i, id, err)
		}
		if _, err := stmt.Exec(i, string(data)); err != nil {
			return fmt.Errorf("session: insert message %d of %s: %w", i, id, err)
		}
	}

	return tx.Commit()
}

// readSqliteSession loads the full session (meta + all messages) from a
// .sqlite file.
func readSqliteSession(path string) (*Session, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var s Session
	var metaJSON string
	row := db.QueryRow(`SELECT id, title, title_generated, created_at, updated_at, metadata_json FROM meta LIMIT 1`)
	if err := row.Scan(&s.ID, &s.Title, &s.TitleGenerated, &s.CreatedAt, &s.UpdatedAt, &metaJSON); err != nil {
		return nil, fmt.Errorf("session: read meta: %w", err)
	}
	if metaJSON != "" && metaJSON != "null" {
		if err := json.Unmarshal([]byte(metaJSON), &s.Metadata); err != nil {
			return nil, fmt.Errorf("session: unmarshal metadata: %w", err)
		}
	}

	rows, err := db.Query(`SELECT data FROM messages ORDER BY seq ASC`)
	if err != nil {
		return nil, fmt.Errorf("session: read messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("session: scan message: %w", err)
		}
		var m agent.Message
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return nil, fmt.Errorf("session: unmarshal message: %w", err)
		}
		s.Messages = append(s.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate messages: %w", err)
	}

	return &s, nil
}
```

- [x] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/session/... -run 'TestWriteSqliteSessionFullThenRead|TestAppendSqliteSessionOnlyInsertsNewMessages|TestAppendSqliteSessionExplicitTitleOverrides|TestAppendSqliteSessionShrinkingMessagesReplacesAll' -v`
Expected: PASS (all 4 tests).

- [x] **Step 5: Run the full package test suite and race detector**

Run: `go build ./... && go test ./internal/session/... -race`
Expected: PASS, no new failures.

- [x] **Step 6: Commit**

```bash
git add internal/session/sqlitestore.go internal/session/sqlitestore_test.go
git commit -m "feat: sqlite per-session read/write core with incremental append"
```
