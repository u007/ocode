// Journal: the persistent index for snapshot backups. Backup files
// themselves have always been written to <baseDir>, but the metadata tying
// them to a session/tool-call lived only in the in-memory Store — so the
// changes tab emptied out whenever a session's agent was rebuilt (idle
// eviction, server restart, resume). The journal is a per-project sqlite
// file (<baseDir>/snapshots.sqlite) appended on every Backup and replayed
// on session resume via Store.Rehydrate. It also powers GC: journal rows
// (and their backup files) older than the retention window are pruned, as
// are orphan backup files no row references.
package snapshot

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// journalRetention is how long snapshot rows and backup files are kept.
// Older entries can no longer power the changes tab of any realistically
// resumable session and only grow the snapshots dir without bound.
const journalRetention = 30 * 24 * time.Hour

// journalFileName is the sqlite file inside a snapshot base dir. The -wal
// and -shm sidecars share the prefix; gcOrphans skips all three.
const journalFileName = "snapshots.sqlite"

// Journal is an append-mostly index over one project's snapshot dir. Safe
// for concurrent use (sql.DB serializes; WAL + busy_timeout handle
// cross-process access from TUI/desktop/web against the same file).
type Journal struct {
	db  *sql.DB
	dir string
}

// journalCache shares one Journal per base dir across every Store in the
// process, so concurrent agents (main + sub-agents) don't each hold a
// separate connection pool to the same file.
var (
	journalCacheMu sync.Mutex
	journalCache   = map[string]*Journal{}
)

// journalFor returns the shared Journal for dir, opening (and GC'ing) it on
// first use. Returns nil with the error logged when the journal cannot be
// opened — snapshot backups must keep working even if the index is broken.
func journalFor(dir string) *Journal {
	if dir == "" {
		return nil
	}
	journalCacheMu.Lock()
	defer journalCacheMu.Unlock()
	if j, ok := journalCache[dir]; ok {
		return j
	}
	j, err := openJournal(dir)
	if err != nil {
		log.Printf("snapshot: open journal in %s: %v", dir, err)
		journalCache[dir] = nil // don't retry every Backup
		return nil
	}
	journalCache[dir] = j
	go j.gc(time.Now().Add(-journalRetention))
	return j
}

// openJournal opens (creating if needed) dir's snapshots.sqlite.
func openJournal(dir string) (*Journal, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, journalFileName)
	// Escape URI-significant characters the same way session.openDB does —
	// project paths are user controlled and may contain "?"/"#"/"%".
	escaped := strings.ReplaceAll(path, "%", "%25")
	escaped = strings.ReplaceAll(escaped, "?", "%3F")
	escaped = strings.ReplaceAll(escaped, "#", "%23")
	dsn := "file:" + escaped + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS snapshot (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id    TEXT NOT NULL,
		agent_id      TEXT NOT NULL,
		tool_call_id  TEXT NOT NULL,
		original_path TEXT NOT NULL,
		backup_path   TEXT NOT NULL,
		agent_step    INTEGER NOT NULL,
		created_at    DATETIME NOT NULL
	)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS snapshot_session_id_idx ON snapshot(session_id)`); err != nil {
		db.Close()
		return nil, err
	}
	return &Journal{db: db, dir: dir}, nil
}

// append records one snapshot for a session. Best-effort by contract: the
// caller logs failures but never fails the write that triggered the backup.
func (j *Journal) append(sessionID, agentID string, snap Snapshot) error {
	_, err := j.db.Exec(
		`INSERT INTO snapshot (session_id, agent_id, tool_call_id, original_path, backup_path, agent_step, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sessionID, agentID, snap.ToolCallID, snap.OriginalPath, snap.BackupPath,
		snap.AgentStep, snap.Timestamp.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// loadSession returns a session's journaled snapshots in write order.
func (j *Journal) loadSession(sessionID string) ([]Snapshot, error) {
	rows, err := j.db.Query(
		`SELECT tool_call_id, original_path, backup_path, agent_step, created_at
		 FROM snapshot WHERE session_id = ? ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var snap Snapshot
		var created string
		if err := rows.Scan(&snap.ToolCallID, &snap.OriginalPath, &snap.BackupPath, &snap.AgentStep, &created); err != nil {
			return nil, err
		}
		ts, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse created_at %q: %w", created, err)
		}
		snap.Timestamp = ts
		snap.BaseDir = j.dir
		out = append(out, snap)
	}
	return out, rows.Err()
}

// gc prunes journal rows older than cutoff (deleting their backup files)
// and then removes orphan backup files in the dir older than cutoff that no
// remaining row references. The orphan pass also cleans up the pre-journal
// backlog of unindexed backup files.
func (j *Journal) gc(cutoff time.Time) {
	cutoffStr := cutoff.UTC().Format(time.RFC3339Nano)

	rows, err := j.db.Query(`SELECT DISTINCT backup_path FROM snapshot WHERE created_at < ? AND backup_path != ''`, cutoffStr)
	if err != nil {
		log.Printf("snapshot: journal gc query (%s): %v", j.dir, err)
		return
	}
	var expired []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			log.Printf("snapshot: journal gc scan (%s): %v", j.dir, err)
			return
		}
		expired = append(expired, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("snapshot: journal gc rows (%s): %v", j.dir, err)
		return
	}

	if _, err := j.db.Exec(`DELETE FROM snapshot WHERE created_at < ?`, cutoffStr); err != nil {
		log.Printf("snapshot: journal gc delete (%s): %v", j.dir, err)
		return
	}

	// A backup path can be shared by rows on both sides of the cutoff;
	// only unlink files no surviving row still references.
	for _, p := range expired {
		var n int
		if err := j.db.QueryRow(`SELECT COUNT(*) FROM snapshot WHERE backup_path = ?`, p).Scan(&n); err != nil || n > 0 {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Printf("snapshot: journal gc remove %s: %v", p, err)
		}
	}

	j.gcOrphans(cutoff)
}

// gcOrphans removes backup files in the journal's dir older than cutoff
// that no journal row references.
func (j *Journal) gcOrphans(cutoff time.Time) {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		log.Printf("snapshot: journal gc readdir %s: %v", j.dir, err)
		return
	}
	referenced := map[string]bool{}
	rows, err := j.db.Query(`SELECT DISTINCT backup_path FROM snapshot WHERE backup_path != ''`)
	if err != nil {
		log.Printf("snapshot: journal gc referenced query (%s): %v", j.dir, err)
		return
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			log.Printf("snapshot: journal gc referenced scan (%s): %v", j.dir, err)
			return
		}
		referenced[filepath.Clean(p)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("snapshot: journal gc referenced rows (%s): %v", j.dir, err)
		return
	}

	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), journalFileName) {
			continue
		}
		full := filepath.Join(j.dir, e.Name())
		if referenced[filepath.Clean(full)] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue // intentionally not logged: racing deletion or still fresh
		}
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			log.Printf("snapshot: journal gc remove orphan %s: %v", full, err)
		}
	}
}
