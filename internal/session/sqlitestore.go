// Package-level: this file holds all SQLite-format session storage code —
// the third on-disk format alongside the legacy .json and .ojsonl formats
// in session.go and ojsonl.go. See docs/superpowers/plans/2026-08-28-sqlite-session-storage/INDEX.md
// for the design this implements.
package session

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

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
