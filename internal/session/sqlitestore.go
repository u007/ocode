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
			metadata_json   TEXT NOT NULL DEFAULT '{}',
			history_gen     INTEGER NOT NULL DEFAULT 0
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("session: create meta table: %w", err)
	}
	// history_gen guards live writes against post-compaction resurrection
	// (see appendSqliteSession). Files created before the column existed
	// gain it here; fresh files already have it via the CREATE above.
	if err := ensureHistoryGenColumn(db); err != nil {
		db.Close()
		return nil, err
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

// ensureHistoryGenColumn adds meta.history_gen to session files created
// before the column existed. It inspects PRAGMA table_info rather than
// matching error strings, so only a genuinely missing column triggers the
// ALTER — any other failure propagates.
func ensureHistoryGenColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(meta)`)
	if err != nil {
		return fmt.Errorf("session: inspect meta schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("session: scan meta schema: %w", err)
		}
		if name == "history_gen" {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("session: iterate meta schema: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE meta ADD COLUMN history_gen INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("session: add history_gen column: %w", err)
	}
	return nil
}

// readHistoryGen returns the session file's current history generation: 0
// for a missing file (a new session starts at generation 0, matching the
// column default) or an error when the file cannot be read. It opens via
// openSessionDB so pre-column files gain history_gen (defaulting 0, the
// correct value for a file with no recorded shrinks) before reading.
func readHistoryGen(dir, id string) (int64, error) {
	path := sqliteSessionPath(dir, id)
	if !fileExists(path) {
		return 0, nil
	}
	db, err := openSessionDB(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var gen int64
	if err := db.QueryRow(`SELECT history_gen FROM meta WHERE id = ?`, id).Scan(&gen); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("session: read history_gen %s: %w", id, err)
	}
	return gen, nil
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
//
// live selects the async live-write mode used by the per-session worker in
// live.go: the message count is read inside the write transaction, and a
// snapshot that adds no new messages is a complete no-op (messages, title,
// metadata, and updated_at are all left untouched) — a live write must never
// shrink the transcript (compaction goes through the synchronous path with
// live=false), replace same-length content, or regress a newer title/metadata
// with an older queued snapshot. Title/metadata ride along only with
// genuinely new messages; the turn-end synchronous save stays authoritative
// for them. The function reports whether anything changed so callers can skip
// the index refresh when the write was a stale no-op.
//
// liveGen is the history generation the live snapshot was taken against (see
// readHistoryGen): a live write whose generation no longer matches the stored
// one is a superseded pre-compaction snapshot and is dropped, so a queued
// write can never resurrect history a synchronous shrink already replaced.
// Synchronous shrinks bump history_gen in the same transaction as the
// replacement, so the check inside this transaction closes the race in both
// orders. liveGen is ignored when live is false.
// appendSqliteSession is the retrying entry point: concurrent writers on one
// session file can hit SQLITE_BUSY when both hold SHARED locks and one tries
// to upgrade (busy_timeout does not cover that upgrade deadlock — SQLite
// fails it immediately), so a busy failure retries the whole transaction with
// backoff instead of surfacing a transient lock as a write error. Conflict
// errors (diverged overlap) are NOT retried — retrying those would spin.
func appendSqliteSession(dir, id, title string, messages []agent.Message, metadata map[string]any, live bool, liveGen int64) (bool, error) {
	var changed bool
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		changed, err = appendSqliteSessionOnce(dir, id, title, messages, metadata, live, liveGen)
		if err == nil || !isBusyErr(err) {
			return changed, err
		}
		time.Sleep(time.Duration(5*(attempt+1)) * time.Millisecond)
	}
	return false, err
}

// isBusyErr reports a transient SQLite lock contention.
func isBusyErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "database is locked") || strings.Contains(s, "database table is locked")
}

func appendSqliteSessionOnce(dir, id, title string, messages []agent.Message, metadata map[string]any, live bool, liveGen int64) (bool, error) {
	path := sqliteSessionPath(dir, id)
	db, err := openSessionDB(path)
	if err != nil {
		return false, fmt.Errorf("session: open sqlite %s: %w", id, err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("session: begin tx %s: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck

	var existingTitle string
	var existingTitleGenerated bool
	var existingMetaJSON string
	var existingGen int64
	if err := tx.QueryRow(`SELECT title, title_generated, metadata_json, history_gen FROM meta WHERE id = ?`, id).
		Scan(&existingTitle, &existingTitleGenerated, &existingMetaJSON, &existingGen); err != nil {
		return false, fmt.Errorf("session: read meta %s: %w", id, err)
	}
	var existingCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&existingCount); err != nil {
		return false, fmt.Errorf("session: count messages %s: %w", id, err)
	}

	if live && existingGen != liveGen {
		// Superseded pre-compaction snapshot: a synchronous shrink replaced
		// the history after this snapshot was queued. Appending its suffix
		// would resurrect compacted messages, so drop it entirely.
		return false, nil
	}

	if live && existingCount >= len(messages) {
		// No new messages (stale or identical queued snapshot) — leave
		// everything untouched, including title, metadata, updated_at, and
		// the index row. Title/metadata ride along only with genuinely new
		// messages so an older snapshot can never regress them.
		return false, nil
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
			return false, fmt.Errorf("session: marshal metadata %s: %w", id, err)
		}
		metaJSON = string(b)
	}

	newGen := existingGen
	shrinking := existingCount > len(messages)
	if shrinking {
		// Synchronous shrink only: live writes with fewer messages than
		// stored returned early above, so reaching here with a shorter
		// snapshot means an authoritative compaction. Bump the generation
		// in this same transaction so any queued pre-compaction live
		// snapshot mismatches and drops instead of resurrecting history.
		newGen++
	}

	if _, err := tx.Exec(
		`UPDATE meta SET title = ?, title_generated = ?, updated_at = ?, metadata_json = ?, history_gen = ? WHERE id = ?`,
		resolvedTitle, titleGenerated, time.Now(), metaJSON, newGen, id,
	); err != nil {
		return false, fmt.Errorf("session: update meta %s: %w", id, err)
	}

	if shrinking {
		// Message count shrank (e.g. /compact) — the append-only path
		// can't represent that, so replace the message set wholesale.
		// Mirrors saveOjsonl's identical handling in ojsonl.go. Only the
		// synchronous path reaches here; live writes return early above.
		if _, err := tx.Exec(`DELETE FROM messages`); err != nil {
			return false, fmt.Errorf("session: clear messages %s: %w", id, err)
		}
		existingCount = 0
	}

	// Overlap check: the stored prefix and the incoming snapshot must agree
	// wherever they overlap. Two processes appending different messages at
	// the same seq (independent concurrent turns from the same base) must
	// never silently drop one of them — the old INSERT OR IGNORE did exactly
	// that. Identical overlap converges (idempotent retry); differing overlap
	// is a conflict: the synchronous path reports an error so the caller can
	// retry/reconcile, while the live path drops the stale snapshot (the
	// turn-end sync save stays authoritative).
	if overlap := min(existingCount, len(messages)); overlap > 0 {
		stored := make([]string, 0, overlap)
		rows, err := tx.Query(`SELECT data FROM messages ORDER BY seq ASC LIMIT ?`, overlap)
		if err != nil {
			return false, fmt.Errorf("session: read overlap %s: %w", id, err)
		}
		for rows.Next() {
			var data string
			if err := rows.Scan(&data); err != nil {
				rows.Close()
				return false, fmt.Errorf("session: scan overlap %s: %w", id, err)
			}
			stored = append(stored, data)
		}
		rowsCloseErr := rows.Err()
		if err := rows.Close(); err != nil {
			return false, fmt.Errorf("session: close overlap %s: %w", id, err)
		}
		if rowsCloseErr != nil {
			return false, fmt.Errorf("session: iterate overlap %s: %w", id, rowsCloseErr)
		}
		for i, want := range stored {
			got, err := json.Marshal(messages[i])
			if err != nil {
				return false, fmt.Errorf("session: marshal message %d of %s: %w", i, id, err)
			}
			if string(got) != want {
				if live {
					return false, nil
				}
				return false, fmt.Errorf("session: conflicting message at seq %d of %s (concurrent writers diverged)", i, id)
			}
		}
	}

	// A plain INSERT surfaces any residual primary-key race as an error
	// instead of silently discarding a message. In-process writers are
	// serialized by lockFor; a cross-process loser re-reads and retries via
	// the overlap check above on its next save rather than losing data.
	stmt, err := tx.Prepare(`INSERT INTO messages (seq, data) VALUES (?, ?)`)
	if err != nil {
		return false, fmt.Errorf("session: prepare insert %s: %w", id, err)
	}
	defer stmt.Close()
	for i := existingCount; i < len(messages); i++ {
		data, err := json.Marshal(messages[i])
		if err != nil {
			return false, fmt.Errorf("session: marshal message %d of %s: %w", i, id, err)
		}
		if _, err := stmt.Exec(i, string(data)); err != nil {
			return false, fmt.Errorf("session: insert message %d of %s: %w", i, id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
