package session

import (
	"fmt"
	"os"
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
func StoredRevisionForDir(wd, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		return "", err
	}
	return storedRevisionForDir(dir, id)
}

// storedRevisionForDir is the dir-scoped core, split out so tests can target a
// resolved sessions dir directly.
func storedRevisionForDir(dir, id string) (string, error) {
	for _, candidate := range sessionCandidateIDs(id) {
		sqlitePath := sqliteSessionPath(dir, candidate)
		if !fileExists(sqlitePath) {
			continue
		}
		rev, err := sqliteStoredRevision(sqlitePath, candidate)
		if err == nil {
			return rev, nil
		}
		// Schema-less / unreadable sqlite: fall back to the file token so the
		// revision still moves on writes instead of reporting "unknown".
		if statRev, statErr := fileStoredRevision(sqlitePath); statErr == nil {
			return statRev, nil
		}
		return "", err
	}
	for _, candidate := range sessionCandidateIDs(id) {
		ojsonlPath := ojsonlSessionPath(dir, candidate)
		if !fileExists(ojsonlPath) {
			continue
		}
		return fileStoredRevision(ojsonlPath)
	}
	for _, path := range sessionLoadPaths(dir, id) {
		if !fileExists(path) {
			continue
		}
		return fileStoredRevision(path)
	}
	return "", nil
}

// sqliteStoredRevision reads the two columns that move on a stored transcript
// change. Deliberately a DDL-free single SELECT via openDBRaw (mirroring
// readHistoryGen): it must not create schema or wait on a cross-process write
// lock. A missing table/column or a missing row is returned as an error so the
// caller can fall back to the file token.
func sqliteStoredRevision(path, id string) (string, error) {
	db, err := openDBRaw(path)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var updated any
	var gen int64
	if err := db.QueryRow(
		`SELECT updated_at, history_gen FROM meta WHERE id = ?`, id,
	).Scan(&updated, &gen); err != nil {
		return "", fmt.Errorf("session: read revision %s: %w", id, err)
	}
	// updated_at is bound as a time.Time on write (modernc stores its String()
	// form), so scan into `any` and format rather than assuming a Go type —
	// the token only has to be stable-per-write, not parseable.
	return fmt.Sprintf("%v|%d", updated, gen), nil
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
