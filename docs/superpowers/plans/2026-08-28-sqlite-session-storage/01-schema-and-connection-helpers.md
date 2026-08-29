# Task 1: Dependency, schema, and connection helpers

**Files:**
- Modify: `go.mod`, `go.sum` (add `modernc.org/sqlite`)
- Create: `internal/session/sqlitestore.go`
- Test: `internal/session/sqlitestore_test.go`

**Interfaces:**
- Produces: `sqliteSessionPath(dir, id string) string`, `indexDBPath(dir string) string`, `openDB(path string) (*sql.DB, error)`, `openSessionDB(path string) (*sql.DB, error)`, `openIndexDB(dir string) (*sql.DB, error)`.

- [x] **Step 1: Add the dependency**

Run:
```bash
go get modernc.org/sqlite@v1.57.0
go mod tidy
```

Expected: `go.mod` gains a `require modernc.org/sqlite v1.57.0` line (and
its transitive deps); `go.sum` is updated. `modernc.org/sqlite` is pure Go
(no CGO), verified in this plan's investigation to cross-compile clean for
`GOOS=linux`, `GOOS=windows`, and `GOOS=darwin GOARCH=arm64` with default
`CGO_ENABLED` — matches `Makefile`'s `build-all`, which has no C
cross-toolchain configured.

- [x] **Step 2: Write the failing test**

```go
// internal/session/sqlitestore_test.go
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
```

- [x] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/session/... -run 'TestOpenSessionDBCreatesSchema|TestOpenIndexDBCreatesSchema|TestSqliteSessionPath|TestIndexDBPath' -v`
Expected: FAIL — `sqliteSessionPath`, `indexDBPath`, `openSessionDB`, `openIndexDB` undefined.

- [x] **Step 4: Implement the connection and schema helpers**

```go
// internal/session/sqlitestore.go

// Package-level: this file holds all SQLite-format session storage code —
// the third on-disk format alongside the legacy .json and .ojsonl formats
// in session.go and ojsonl.go. See docs/superpowers/plans/2026-08-28-sqlite-session-storage/INDEX.md
// for the design this implements.
package session

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// sqliteSessionPath returns the .sqlite file path for a session id.
func sqliteSessionPath(dir, id string) string {
	return filepath.Join(dir, id+".sqlite")
}

// indexDBPath returns the shared per-project index database path — one
// file per project sessions directory, holding a row for every migrated
// (.sqlite-format) session in that project.
func indexDBPath(dir string) string {
	return filepath.Join(dir, "index.sqlite")
}

// openDB opens a sqlite file with WAL journaling and a busy timeout, so
// concurrent access from multiple ocode processes (TUI, desktop, web
// server) against the same session or index file waits briefly instead of
// failing with "database is locked". It does not create any schema —
// callers reading an existing file that don't want to implicitly create
// empty tables for a missing/corrupt file should use this directly;
// openSessionDB/openIndexDB below wrap it with CREATE TABLE IF NOT EXISTS.
func openDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	return sql.Open("sqlite", dsn)
}

// openSessionDB opens (creating if needed) a per-session .sqlite file with
// its schema: a single-row meta table and an ordered messages table.
func openSessionDB(path string) (*sql.DB, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS meta (
			id              TEXT PRIMARY KEY,
			title           TEXT NOT NULL DEFAULT '',
			title_generated INTEGER NOT NULL DEFAULT 0,
			created_at      DATETIME NOT NULL,
			updated_at      DATETIME NOT NULL,
			metadata_json   TEXT NOT NULL DEFAULT '{}'
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("session: create meta table: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			seq  INTEGER PRIMARY KEY,
			data TEXT NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("session: create messages table: %w", err)
	}
	return db, nil
}

// openIndexDB opens (creating if needed) the shared per-project
// index.sqlite, with one row per migrated session — see queryIndexMetas
// in Task 3 for why: it lets listing serve migrated sessions from a
// single indexed query instead of opening every session file.
func openIndexDB(dir string) (*sql.DB, error) {
	db, err := openDB(indexDBPath(dir))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT PRIMARY KEY,
			title      TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			clone_of   TEXT NOT NULL DEFAULT ''
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("session: create index table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS sessions_updated_at_idx ON sessions(updated_at DESC)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("session: create index idx: %w", err)
	}
	return db, nil
}
```

- [x] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/session/... -run 'TestOpenSessionDBCreatesSchema|TestOpenIndexDBCreatesSchema|TestSqliteSessionPath|TestIndexDBPath' -v`
Expected: PASS (all 4 tests).

- [x] **Step 6: Run the full package test suite and race detector**

Run: `go build ./... && go test ./internal/session/... -race`
Expected: PASS, no new failures.

- [x] **Step 7: Commit**

```bash
git add go.mod go.sum internal/session/sqlitestore.go internal/session/sqlitestore_test.go
git commit -m "feat: add sqlite dependency and session-store schema helpers"
```
