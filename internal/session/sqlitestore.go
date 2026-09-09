// Package-level: this file holds all SQLite-format session storage code —
// the third on-disk format alongside the legacy .json and .ojsonl formats
// in session.go and ojsonl.go. See docs/superpowers/plans/2026-08-28-sqlite-session-storage/INDEX.md
// for the design this implements.
package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
	sqlite "modernc.org/sqlite"
)

// ErrTranscriptConflict reports that a synchronous save's in-memory
// transcript disagrees with the stored one: either the overlap check found
// differing content at a shared sequence number, or the snapshot is shorter
// than the stored transcript (another writer appended messages this
// process has not seen). Callers should reload the session from disk and
// rebuild their snapshot — retrying the same snapshot verbatim keeps
// failing. Only the explicit replace path (Replace/ReplaceForDir:
// compaction, truncation, transcript rewind) is allowed to shrink or
// rewrite overlapping history, by design.
var ErrTranscriptConflict = errors.New("session: transcript conflict (concurrent writers diverged)")

// isConflictErr reports whether err is (or wraps) ErrTranscriptConflict.
func isConflictErr(err error) bool {
	return errors.Is(err, ErrTranscriptConflict)
}

// IsConflictErr reports whether err is (or wraps) ErrTranscriptConflict —
// the typed signal that a synchronous save raced a concurrent writer and
// the caller must reload from disk (see ReconcileAppendForDir for the
// bounded rebase helper).
func IsConflictErr(err error) bool {
	return isConflictErr(err)
}

// isConstraintErr reports a SQLite constraint violation (primary key /
// unique), the backstop that surfaces a lost append race as an error
// instead of silently discarding a message. Extended result codes keep the
// primary class in the low byte: SQLITE_CONSTRAINT = 19, and the
// primary-key/unique violations here arrive as 1555/2067.
func isConstraintErr(err error) bool {
	var e *sqlite.Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Code()&0xff == 19
}

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

// openDB opens a sqlite file with WAL journaling, immediate write
// transactions, and a busy timeout, so concurrent access from multiple
// ocode processes (TUI, desktop, web server) against the same session or
// index file serializes cleanly: _txlock=immediate makes every BeginTx
// take its write lock at BEGIN (see the driver's tx.go), so the classic
// deferred-upgrade deadlock — two writers holding SHARED locks and both
// trying to upgrade, which busy_timeout does not cover — cannot arise. A
// second writer's BEGIN IMMEDIATE then blocks up to busy_timeout instead
// of failing fast, and the overlap check in appendSqliteSessionOnce
// catches genuine divergence after the wait. It does not create any
// schema — callers reading an existing file that don't want to implicitly
// create empty tables for a missing/corrupt file should use this
// directly; openSessionDB/openIndexDB below wrap it with CREATE TABLE IF
// NOT EXISTS.
func openDB(path string) (*sql.DB, error) {
	// Escape URI-significant characters in the filesystem path so a dir
	// containing "?" or "#" does not break the query string. Session ids
	// themselves are safe (generated hex), but project paths are user
	// controlled.
	escaped := strings.ReplaceAll(path, "%", "%25")
	escaped = strings.ReplaceAll(escaped, "?", "%3F")
	escaped = strings.ReplaceAll(escaped, "#", "%23")
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", escaped)
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
// shrink the transcript (compaction goes through the synchronous replace
// path — Replace/ReplaceForDir), replace same-length content, or regress a
// newer title/metadata
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
// appendSqliteSession is the retrying entry point: transient SQLite lock
// contention (SQLITE_BUSY) retries the whole transaction with backoff.
// With the _txlock=immediate DSN in openDB, a transaction takes its write
// lock at BEGIN, so the deferred-upgrade deadlock two writers used to hit
// (both holding SHARED and upgrading — SQLite fails that immediately, and
// busy_timeout does not cover it) can no longer arise; BUSY now only means
// genuine contention. Conflict errors (diverged overlap, stale shorter
// snapshot) are NOT retried — retrying those would spin.
func appendSqliteSession(dir, id, title string, messages []agent.Message, metadata map[string]any, live bool, liveGen int64, replace bool) (bool, error) {
	var changed bool
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		changed, err = appendSqliteSessionOnce(dir, id, title, messages, metadata, live, liveGen, replace)
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

func appendSqliteSessionOnce(dir, id, title string, messages []agent.Message, metadata map[string]any, live bool, liveGen int64, replace bool) (bool, error) {
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

	// startIdx is the first index of messages to insert; rows before it are
	// already stored (or, for a loader-view live base, filtered out of the
	// caller's transcript but still on disk).
	startIdx := existingCount
	if live {
		// A live snapshot carries no baseLen, so decide what is new here:
		// either the stored rows are a byte-identical prefix of the snapshot
		// (the ordinary case), or the LOADER's filtered view of them is (the
		// caller resumed from a transcript whose unanswered ask round the
		// loader dropped — see rebaseAppend). Anything else is a stale or
		// foreign snapshot and drops silently: the turn-end sync save is
		// authoritative.
		stored, err := readStoredRows(tx, id)
		if err != nil {
			return false, err
		}
		n, ok := liveAppendStart(stored, messages)
		if !ok || n >= len(messages) {
			// No new messages (stale or identical queued snapshot) — leave
			// everything untouched, including title, metadata, updated_at,
			// and the index row. Title/metadata ride along only with
			// genuinely new messages so an older snapshot can never
			// regress them.
			return false, nil
		}
		startIdx = n
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
	shrinking := !live && existingCount > len(messages)
	if shrinking && !replace {
		// An ordinary save must never delete stored history. A shorter
		// snapshot here means the caller's in-memory copy is stale relative
		// to disk: another writer appended messages it has not seen, or its
		// load failed. The old delete-all-and-rewrite silently destroyed the
		// other writer's rows in exactly that case, so report a conflict and
		// let the caller reload; only the explicit replace path
		// (Replace/ReplaceForDir: compaction, truncation, transcript rewind)
		// may shrink history. Live writes never reach this: liveAppendStart
		// already decided their suffix above.
		return false, fmt.Errorf("session: stale snapshot for %s: %d message(s) but %d stored (another writer appended; reload and retry): %w", id, len(messages), existingCount, ErrTranscriptConflict)
	}

	// Overlap check: the stored prefix and the incoming snapshot must agree
	// wherever they overlap. Two processes appending different messages at
	// the same seq (independent concurrent turns from the same base) must
	// never silently drop one of them — the old INSERT OR IGNORE did exactly
	// that. Identical overlap converges (idempotent retry). For the
	// synchronous path a differing overlap is a conflict error; for the
	// live path it drops the stale snapshot (the turn-end sync save stays
	// authoritative). The explicit replace path treats a differing overlap
	// as an authoritative content rewrite instead: the caller owns the
	// transcript, so it is applied wholesale below (history_gen bumps so
	// queued pre-replacement live snapshots drop instead of resurrecting
	// replaced rows).
	rewrite := shrinking
	if overlap := min(existingCount, len(messages)); overlap > 0 && !live {
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
				if replace {
					rewrite = true
					break
				}
				return false, fmt.Errorf("session: conflicting message at seq %d of %s (concurrent writers diverged): %w", i, id, ErrTranscriptConflict)
			}
		}
	}

	if rewrite {
		// Authoritative replacement (shrunk, or overlapping content
		// rewritten): bump the generation in this same transaction so any
		// queued pre-replacement live snapshot mismatches and drops
		// instead of resurrecting history.
		newGen = existingGen + 1
	}

	if _, err := tx.Exec(
		`UPDATE meta SET title = ?, title_generated = ?, updated_at = ?, metadata_json = ?, history_gen = ? WHERE id = ?`,
		resolvedTitle, titleGenerated, time.Now(), metaJSON, newGen, id,
	); err != nil {
		return false, fmt.Errorf("session: update meta %s: %w", id, err)
	}

	if rewrite {
		// The replace path rewrites the message set wholesale (count shrank
		// and/or overlapping content changed) — the append-only path can't
		// represent that. Mirrors saveOjsonl's identical handling in
		// ojsonl.go. Only the synchronous replace path reaches here; live
		// writes return early above or drop silently in the overlap check.
		if _, err := tx.Exec(`DELETE FROM messages`); err != nil {
			return false, fmt.Errorf("session: clear messages %s: %w", id, err)
		}
		existingCount = 0
		startIdx = 0
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
	for i := startIdx; i < len(messages); i++ {
		data, err := json.Marshal(messages[i])
		if err != nil {
			return false, fmt.Errorf("session: marshal message %d of %s: %w", i, id, err)
		}
		seq := existingCount + (i - startIdx)
		if _, err := stmt.Exec(seq, string(data)); err != nil {
			return false, fmt.Errorf("session: insert message %d of %s: %w", seq, id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// appendUserMessageTail durably appends one user message at the transcript
// tail (seq = stored count) in a single immediate transaction. It never
// reads or rewrites existing rows, so — unlike a full-snapshot save — it
// cannot trip the overlap conflict even when the stored transcript holds
// rows the load path filters out (incomplete tool results, the
// PERMISSION_ASK sentinel): loading such a file yields a SHORTER, shifted
// sequence, and a "disk+1" snapshot save would compare the shifted
// sequence against the stored rows and conflict forever. A primary-key
// violation means another writer appended to the same seq first; the
// caller retries and the count is re-read inside the next transaction.
// Title, metadata, and history_gen are intentionally untouched (keep
// semantics — the turn-end sync save stays authoritative for them);
// updated_at is bumped. Commit ambiguity needs no deduplication: a commit
// error on a local SQLite file rolls the tx back deterministically, so a
// retry cannot double-insert.
//
// The caller must have verified the .sqlite file exists (a missing session
// goes through the create/migrate path in AppendUserMessageForDir, which
// also writes the meta row and index entry that a bare insert here would
// not).
func appendUserMessageTail(dir, id, content string) error {
	mu := lockFor(dir, id)
	mu.Lock()
	defer mu.Unlock()

	db, err := openSessionDB(sqliteSessionPath(dir, id))
	if err != nil {
		return fmt.Errorf("session: open sqlite %s: %w", id, err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("session: begin tx %s: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck

	var existingCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&existingCount); err != nil {
		return fmt.Errorf("session: count messages %s: %w", id, err)
	}
	// Stamp the same user_seq the turn will assign in memory (server
	// nextUserSeq over the loaded transcript): max stored seq + 1. The
	// stored copy and the in-memory copy must serialize identically or the
	// live/turn-end overlap check reads the turn as a diverged writer.
	var maxSeq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(json_extract(data, '$.user_seq')), 0) FROM messages WHERE json_extract(data, '$.role') = 'user'`).Scan(&maxSeq); err != nil {
		return fmt.Errorf("session: max user_seq %s: %w", id, err)
	}
	data, err := json.Marshal(agent.Message{Role: "user", Content: content, UserSeq: maxSeq + 1})
	if err != nil {
		return fmt.Errorf("session: marshal user message %s: %w", id, err)
	}
	if _, err := tx.Exec(`INSERT INTO messages (seq, data) VALUES (?, ?)`, existingCount, string(data)); err != nil {
		return fmt.Errorf("session: insert user message %s: %w", id, err)
	}
	if _, err := tx.Exec(`UPDATE meta SET updated_at = ? WHERE id = ?`, time.Now(), id); err != nil {
		return fmt.Errorf("session: update meta %s: %w", id, err)
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

// updateSqliteMetadata rewrites only meta.metadata_json (and updated_at) for
// id inside one immediate transaction. Message rows, title, and history_gen
// are untouched. See UpdateMetadataForDir for why this must not go through
// the full-snapshot save.
func updateSqliteMetadata(dir, id string, mutate func(map[string]any)) error {
	mu := lockFor(dir, id)
	mu.Lock()
	defer mu.Unlock()

	db, err := openSessionDB(sqliteSessionPath(dir, id))
	if err != nil {
		return fmt.Errorf("session: open sqlite %s: %w", id, err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("session: begin tx %s: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck

	var metaJSON string
	if err := tx.QueryRow(`SELECT metadata_json FROM meta WHERE id = ?`, id).Scan(&metaJSON); err != nil {
		return fmt.Errorf("session: read metadata %s: %w", id, err)
	}
	// A nil-metadata save stores the literal "null", which unmarshals to a
	// nil map; normalize so mutate can assign.
	var metadata map[string]any
	if metaJSON != "" {
		if err := json.Unmarshal([]byte(metaJSON), &metadata); err != nil {
			return fmt.Errorf("session: unmarshal metadata %s: %w", id, err)
		}
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	mutate(metadata)
	b, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("session: marshal metadata %s: %w", id, err)
	}
	if _, err := tx.Exec(`UPDATE meta SET metadata_json = ?, updated_at = ? WHERE id = ?`, string(b), time.Now(), id); err != nil {
		return fmt.Errorf("session: update metadata %s: %w", id, err)
	}
	return tx.Commit()
}

// readStoredRows returns every stored message of the open transaction's
// session in seq order.
func readStoredRows(tx *sql.Tx, id string) ([]agent.Message, error) {
	rows, err := tx.Query(`SELECT data FROM messages ORDER BY seq ASC`)
	if err != nil {
		return nil, fmt.Errorf("session: read rows %s: %w", id, err)
	}
	defer rows.Close()
	var out []agent.Message
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("session: scan row %s: %w", id, err)
		}
		var m agent.Message
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return nil, fmt.Errorf("session: decode row %s: %w", id, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session: iterate rows %s: %w", id, err)
	}
	return out, nil
}

// liveAppendStart decides where a live snapshot's unsaved suffix begins:
// the stored rows must be a byte-identical prefix of the snapshot, or the
// loader's filtered view of them must be. Returns ok=false for any other
// shape (stale or foreign snapshot).
func liveAppendStart(stored, snapshot []agent.Message) (int, bool) {
	if samePrefix(stored, snapshot, len(stored)) {
		return len(stored), true
	}
	filtered := removeIncompleteToolRequests(stored)
	if len(filtered) < len(stored) && samePrefix(filtered, snapshot, len(filtered)) {
		return len(filtered), true
	}
	return 0, false
}
