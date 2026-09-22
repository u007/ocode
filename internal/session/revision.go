package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/u007/ocode/internal/agent"
)

// StoredRevisionForDir returns an opaque token that changes whenever id's
// stored transcript changes — whichever process wrote it. It is the
// cross-process change signal behind client-side revalidation of open chat
// sessions: a client polls GET /api/sessions/:id/state, compares the token
// it last fetched a transcript at, and refetches when the token moves.
//
// This is deliberately cheap (one single-row SELECT on the sqlite store, or a
// stat for the legacy formats) so it can be evaluated on every state poll:
//
//   - sqlite (authoritative once a session has migrated): meta.updated_at and
//     meta.history_gen. updated_at is bumped by every write — appends,
//     replacements, even metadata-only writes — and history_gen by
//     synchronous shrinks (compaction/truncate/rewind), so together they move
//     on any transcript mutation.
//   - legacy .ojsonl / .json: file mtime+size.
//
// The empty string means "no stored session" (or nothing readable) and is a
// stable "never revalidate" signal for callers. A sqlite file that exists but
// whose schema cannot be read (pre-migration, empty, corrupt) falls back to
// the file token rather than erroring, so a cold file cannot break the poll.
//
// This is the revision-only view of StoredTranscriptStateForDir, which reads
// the same token plus the last stored row in ONE sqlite open.
func StoredRevisionForDir(wd, id string) (string, error) {
	rev, _, err := StoredTranscriptStateForDir(wd, id)
	return rev, err
}

// StoredTranscriptStateForDir returns the stored-transcript change token (see
// StoredRevisionForDir) together with id's LAST stored row, read in one SQLite
// open. It exists so the interrupted-turn rule can classify the stored tail on
// the same cheap read the state poll already performs.
//
// tail is nil whenever the store cannot be classified: an absent session, a
// legacy .ojsonl/.json file, an empty transcript, or a sqlite row that fails
// to decode. A nil tail is a deliberate FAIL-OPEN signal — a format or row we
// cannot read must never look like an interrupted turn. The revision token is
// still returned for those cases (legacy/schema-less sqlite falls back to the
// file token, exactly as StoredRevisionForDir always did).
func StoredTranscriptStateForDir(wd, id string) (string, []agent.Message, error) {
	if id == "" {
		return "", nil, nil
	}
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		return "", nil, err
	}
	return storedTranscriptStateForDir(dir, id)
}

// storedTranscriptStateForDir is the dir-scoped core, split out so tests can
// target a resolved sessions dir directly.
func storedTranscriptStateForDir(dir, id string) (string, []agent.Message, error) {
	for _, candidate := range sessionCandidateIDs(id) {
		sqlitePath := sqliteSessionPath(dir, candidate)
		if !fileExists(sqlitePath) {
			continue
		}
		rev, tail, err := sqliteStoredTranscriptState(sqlitePath, candidate)
		if err == nil {
			return rev, tail, nil
		}
		// Schema-less / unreadable sqlite: fall back to the file token so the
		// revision still moves on writes instead of reporting "unknown", and
		// report no tail (fail open — we cannot classify what we cannot read).
		if statRev, statErr := fileStoredRevision(sqlitePath); statErr == nil {
			return statRev, nil, nil
		}
		return "", nil, err
	}
	for _, candidate := range sessionCandidateIDs(id) {
		ojsonlPath := ojsonlSessionPath(dir, candidate)
		if !fileExists(ojsonlPath) {
			continue
		}
		rev, err := fileStoredRevision(ojsonlPath)
		return rev, nil, err
	}
	for _, path := range sessionLoadPaths(dir, id) {
		if !fileExists(path) {
			continue
		}
		rev, err := fileStoredRevision(path)
		return rev, nil, err
	}
	return "", nil, nil
}

// storedRevisionForDir is the revision-only wrapper over
// storedTranscriptStateForDir, kept so the existing revision callers and tests
// need no change.
func storedRevisionForDir(dir, id string) (string, error) {
	rev, _, err := storedTranscriptStateForDir(dir, id)
	return rev, err
}

// sqliteStoredTranscriptState reads the two columns that move on a stored
// transcript change PLUS the last stored row, in one DDL-free open
// (mirroring readHistoryGen): it must not create schema or wait on a
// cross-process write lock. A missing meta table/column or a missing meta row
// is returned as an error so the caller can fall back to the file token; a
// missing/empty/undecodable messages tail is returned as a nil tail (fail
// open) alongside the valid revision.
func sqliteStoredTranscriptState(path, id string) (string, []agent.Message, error) {
	db, err := openDBRaw(path)
	if err != nil {
		return "", nil, err
	}
	defer db.Close()
	var updated any
	var gen int64
	if err := db.QueryRow(
		`SELECT updated_at, history_gen FROM meta WHERE id = ?`, id,
	).Scan(&updated, &gen); err != nil {
		return "", nil, fmt.Errorf("session: read revision %s: %w", id, err)
	}
	// updated_at is bound as a time.Time on write (modernc stores its String()
	// form), so scan into `any` and format rather than assuming a Go type —
	// the token only has to be stable-per-write, not parseable.
	rev := fmt.Sprintf("%v|%d", updated, gen)

	var data string
	err = db.QueryRow(`SELECT data FROM messages ORDER BY seq DESC LIMIT 1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		// Empty transcript: nothing to classify, revision still valid.
		return rev, nil, nil
	}
	if err != nil {
		// Missing messages table on a partially-migrated file: fail open.
		return rev, nil, nil
	}
	var tail agent.Message
	if err := json.Unmarshal([]byte(data), &tail); err != nil {
		// An undecodable row must not fabricate an interruption.
		return rev, nil, nil
	}
	return rev, []agent.Message{tail}, nil
}

// fileStoredRevision is the legacy-format token (and the sqlite fallback):
// nanosecond mtime plus size. Both change on an append or a rewrite.
func fileStoredRevision(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d|%d", fi.ModTime().UnixNano(), fi.Size()), nil
}
