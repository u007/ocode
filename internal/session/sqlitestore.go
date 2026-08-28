// Package-level: this file holds all SQLite-format session storage code —
// the third on-disk format alongside the legacy .json and .ojsonl formats
// in session.go and ojsonl.go. See docs/superpowers/plans/2026-08-28-sqlite-session-storage/INDEX.md
// for the design this implements.
package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
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
	// Escape URI-significant characters in the filesystem path so a dir
	// containing "?" or "#" does not break the query string. Session ids
	// themselves are safe (generated hex), but project paths are user
	// controlled.
	escaped := strings.ReplaceAll(path, "%", "%25")
	escaped = strings.ReplaceAll(escaped, "?", "%3F")
	escaped = strings.ReplaceAll(escaped, "#", "%23")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", escaped)
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

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("session: begin tx %s: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck

	var existingTitle string
	var existingTitleGenerated bool
	var existingMetaJSON string
	if err := tx.QueryRow(`SELECT title, title_generated, metadata_json FROM meta WHERE id = ?`, id).
		Scan(&existingTitle, &existingTitleGenerated, &existingMetaJSON); err != nil {
		return fmt.Errorf("session: read meta %s: %w", id, err)
	}
	var existingCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&existingCount); err != nil {
		return fmt.Errorf("session: count messages %s: %w", id, err)
	}

	resolvedTitle := existingTitle
	titleGenerated := existingTitleGenerated
	if title != "" {
		resolvedTitle = title
		titleGenerated = true
	}

	var metaJSON string
	if metadata == nil {
		metaJSON = existingMetaJSON
		if metaJSON == "" {
			metaJSON = "{}"
		}
	} else {
		b, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("session: marshal metadata %s: %w", id, err)
		}
		metaJSON = string(b)
	}

	if _, err := tx.Exec(
		`UPDATE meta SET title = ?, title_generated = ?, updated_at = ?, metadata_json = ? WHERE id = ?`,
		resolvedTitle, titleGenerated, time.Now(), metaJSON, id,
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

// upsertIndexRow writes/updates a migrated session's row in the project's
// shared index.sqlite so listing (queryIndexMetas) can serve migrated
// sessions from one indexed query instead of opening every session file.
func upsertIndexRow(dir string, meta ocodeMeta) error {
	db, err := openIndexDB(dir)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO sessions (id, title, created_at, updated_at, clone_of) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET title=excluded.title, updated_at=excluded.updated_at, clone_of=excluded.clone_of`,
		meta.ID, meta.Title, meta.CreatedAt, meta.UpdatedAt, meta.CloneOf,
	)
	return err
}

// deleteIndexRow removes a session's row from the project's index.sqlite.
// A no-op (not an error) if the index file doesn't exist yet — Delete
// (Task 7) calls this unconditionally regardless of the session's
// on-disk format, including for projects that have never had a migrated
// session.
func deleteIndexRow(dir, id string) error {
	path := indexDBPath(dir)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	db, err := openIndexDB(dir)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// queryIndexMetas returns metadata for every migrated (.sqlite-format)
// session in dir, from the shared index — no per-session file opens.
// Returns an empty slice (not an error) if index.sqlite doesn't exist yet
// (no session in this project has migrated).
func queryIndexMetas(dir string) ([]ocodeMeta, error) {
	path := indexDBPath(dir)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	db, err := openIndexDB(dir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, title, created_at, updated_at, clone_of FROM sessions`)
	if err != nil {
		return nil, fmt.Errorf("session: query index: %w", err)
	}
	defer rows.Close()

	var metas []ocodeMeta
	for rows.Next() {
		var m ocodeMeta
		if err := rows.Scan(&m.ID, &m.Title, &m.CreatedAt, &m.UpdatedAt, &m.CloneOf); err != nil {
			return nil, fmt.Errorf("session: scan index row: %w", err)
		}
		metas = append(metas, m)
	}
	return metas, rows.Err()
}

// mergeMetas combines legacy-format metadata (from a directory scan) with
// the migrated-session index, letting an index row shadow a same-ID
// legacy entry. This is belt-and-suspenders for the narrow crash window
// in migrateToSqlite (Task 4) where a migrated session's old file could
// momentarily still be on disk — without this, that session would appear
// twice in a listing until the orphan is cleaned up on next Load.
func mergeMetas(legacy, indexed []ocodeMeta) []ocodeMeta {
	if len(indexed) == 0 {
		return legacy
	}
	migrated := make(map[string]struct{}, len(indexed))
	for _, m := range indexed {
		migrated[m.ID] = struct{}{}
	}
	merged := make([]ocodeMeta, 0, len(legacy)+len(indexed))
	for _, m := range legacy {
		if _, ok := migrated[m.ID]; !ok {
			merged = append(merged, m)
		}
	}
	return append(merged, indexed...)
}
