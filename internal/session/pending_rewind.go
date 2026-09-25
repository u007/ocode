package session

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// PendingRewindStatus is the durable lifecycle state of a session's single
// pending-rewind resource.
type PendingRewindStatus string

const (
	PendingRewindStatusArmed     PendingRewindStatus = "armed"
	PendingRewindStatusCommitted PendingRewindStatus = "committed"
	PendingRewindStatusStale     PendingRewindStatus = "stale"
)

// Valid reports whether s is a state persisted by this package.
func (s PendingRewindStatus) Valid() bool {
	switch s {
	case PendingRewindStatusArmed, PendingRewindStatusCommitted, PendingRewindStatusStale:
		return true
	default:
		return false
	}
}

const pendingRewindTTL = 24 * time.Hour

var (
	// ErrPendingRewindNotFound means the session has no resource for the
	// supplied token, including sessions whose on-demand table does not exist
	// yet. The token is a capability, so no distinction is exposed to callers.
	ErrPendingRewindNotFound = errors.New("session: pending rewind not found")
	// ErrPendingRewindExpired means an armed resource is past its expiry and
	// can no longer commit.
	ErrPendingRewindExpired = errors.New("session: pending rewind expired")
	// ErrPendingRewindStale means transcript validation failed. The resource is
	// permanently stale and cannot succeed on a later retry.
	ErrPendingRewindStale = errors.New("session: pending rewind is stale")
	// ErrPendingRewindAlreadyCommitted means the token was already consumed.
	// The returned result carries the committed resource for response-loss
	// recovery, but callers must not append again or start another turn.
	ErrPendingRewindAlreadyCommitted = errors.New("session: pending rewind already committed")
	// ErrPendingRewindInvalidTarget means the supplied raw index and optional
	// durable user sequence do not identify the exact requested user message.
	ErrPendingRewindInvalidTarget = errors.New("session: invalid pending rewind target")
)

// PendingRewind is the durable, single-use rewind capability for one session.
// TargetContent is persisted so commit can re-resolve legacy targets by raw
// index plus exact content; it is never part of a client-facing status payload.
type PendingRewind struct {
	Token            string              `json:"token"`
	SessionID        string              `json:"session_id"`
	TargetIndex      int                 `json:"target_index"`
	UserSeq          int                 `json:"user_seq,omitempty"`
	Fingerprint      [sha256.Size]byte   `json:"-"`
	Status           PendingRewindStatus `json:"status"`
	CreatedAt        time.Time           `json:"created_at"`
	ExpiresAt        time.Time           `json:"expires_at"`
	CommittedUserSeq int                 `json:"committed_user_seq,omitempty"`
	TargetContent    string              `json:"-"`
}

// PendingRewindCommitResult is returned after the one-transaction rewind and
// append. KeptPrefix is the complete raw prefix before the target, suitable for
// reconciling a resident agent to the authoritative shortened transcript.
type PendingRewindCommitResult struct {
	Rewind     PendingRewind   `json:"rewind"`
	KeptPrefix []agent.Message `json:"kept_prefix"`
}

type rawTranscriptRow struct {
	Seq  int
	Data string
}

const createPendingRewindsTableSQL = `
CREATE TABLE IF NOT EXISTS pending_rewinds (
	session_id          TEXT PRIMARY KEY,
	token               TEXT NOT NULL UNIQUE,
	target_index        INTEGER NOT NULL CHECK (target_index >= 0),
	user_seq            INTEGER,
	target_content      TEXT NOT NULL,
	fingerprint         BLOB NOT NULL CHECK (length(fingerprint) = 32),
	status              TEXT NOT NULL CHECK (status IN ('armed', 'committed', 'stale')),
	created_at          DATETIME NOT NULL,
	expires_at          DATETIME NOT NULL,
	committed_user_seq  INTEGER
)`

const selectPendingRewindColumns = `
	session_id, token, target_index, user_seq, target_content, fingerprint,
	status, created_at, expires_at, committed_user_seq`

// PreparePendingRewindForDir validates target against the complete raw stored
// transcript and atomically replaces the session's one pending resource. It
// never updates session meta or the project index: creating or cancelling a
// rewind must not look like a transcript write to revision/session ordering.
func PreparePendingRewindForDir(projectRoot, id string, targetIndex int, targetContent string, userSeq int) (*PendingRewind, error) {
	if id == "" {
		return nil, ErrNoStoredSession
	}
	if targetIndex < 0 || userSeq < 0 {
		return nil, fmt.Errorf("session: prepare pending rewind %s: %w", id, ErrPendingRewindInvalidTarget)
	}
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		return nil, err
	}

	mu := lockFor(dir, id)
	mu.Lock()
	defer mu.Unlock()

	if err := ensurePendingRewindSQLite(dir, id); err != nil {
		return nil, err
	}
	return retryPendingRewindWrite(func() (*PendingRewind, error) {
		return preparePendingRewindOnce(dir, id, targetIndex, targetContent, userSeq)
	})
}

// PendingRewindStatusForDir returns the durable state for token. Status is
// read-only: expired armed rows are invalid, while committed rows remain
// discoverable for lost-response recovery. This phase introduces no cleanup
// policy that could remove a committed row before the promised 24-hour window.
func PendingRewindStatusForDir(projectRoot, id, token string) (*PendingRewind, error) {
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		return nil, err
	}
	return pendingRewindStatusFromDir(dir, id, token)
}

// CancelPendingRewindForDir removes an armed or stale resource without changing
// messages, meta, history_gen, StoredRevisionForDir, or index ordering.
func CancelPendingRewindForDir(projectRoot, id, token string) error {
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		return err
	}
	mu := lockFor(dir, id)
	mu.Lock()
	defer mu.Unlock()

	return retryPendingRewindError(func() error {
		return cancelPendingRewindOnce(dir, id, token)
	})
}

// CommitPendingRewindForDir validates and consumes token in one immediate
// SQLite transaction. Success deletes the target and every later raw row,
// inserts exactly one replacement user row stamped with NextUserSeq semantics,
// bumps meta.history_gen and meta.updated_at, and marks the same token
// committed with its committed_user_seq. Known transcript/target mismatches
// commit only the stale transition; all other failures roll back completely.
func CommitPendingRewindForDir(projectRoot, id, token, content string) (*PendingRewindCommitResult, error) {
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		return nil, err
	}
	mu := lockFor(dir, id)
	mu.Lock()
	defer mu.Unlock()

	result, err := retryPendingRewindWrite(func() (*PendingRewindCommitResult, error) {
		return commitPendingRewindOnce(dir, id, token, content)
	})
	if err != nil {
		return result, err
	}
	if result != nil && result.Rewind.Status == PendingRewindStatusCommitted {
		// The per-session transaction above is authoritative. The shared project
		// index is derived listing state; a refresh failure is logged but must not
		// turn a durable commit into an apparent send failure and strand the new
		// user row without a turn.
		if refreshErr := refreshIndexMeta(dir, id); refreshErr != nil {
			log.Printf("session: pending rewind commit for %s succeeded but index refresh failed: %v", id, refreshErr)
		}
	}
	return result, nil
}

// FingerprintRawMessages returns the stable SHA-256 fingerprint for a complete
// raw logical transcript. Message JSON is framed by row count, sequence, and
// byte length so no concatenation of adjacent values can collide by framing.
func FingerprintRawMessages(messages []agent.Message) ([sha256.Size]byte, error) {
	rows := make([]rawTranscriptRow, len(messages))
	for i, message := range messages {
		data, err := json.Marshal(message)
		if err != nil {
			return [sha256.Size]byte{}, fmt.Errorf("session: fingerprint message %d: %w", i, err)
		}
		rows[i] = rawTranscriptRow{Seq: i, Data: string(data)}
	}
	return fingerprintRawTranscriptRows(rows), nil
}

func fingerprintRawTranscriptRows(rows []rawTranscriptRow) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte("ocode-session-raw-transcript-v1\n"))
	var boundary [8]byte
	binary.BigEndian.PutUint64(boundary[:], uint64(len(rows)))
	h.Write(boundary[:])
	for _, row := range rows {
		binary.BigEndian.PutUint64(boundary[:], uint64(row.Seq))
		h.Write(boundary[:])
		binary.BigEndian.PutUint64(boundary[:], uint64(len(row.Data)))
		h.Write(boundary[:])
		h.Write([]byte(row.Data))
	}
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], h.Sum(nil))
	return fingerprint
}

func ensurePendingRewindSQLite(dir, id string) error {
	path := sqliteSessionPath(dir, id)
	if fileExists(path) {
		return nil
	}

	jsonPath := filepath.Join(dir, id+".json")
	ojsonlPath := ojsonlSessionPath(dir, id)
	wasJSON := fileExists(jsonPath)
	if !wasJSON && !fileExists(ojsonlPath) {
		return ErrNoStoredSession
	}
	messages, err := loadRawMessages(dir, id)
	if err != nil {
		return fmt.Errorf("session: prepare pending rewind %s: load legacy raw transcript: %w", id, err)
	}
	if err := migrateToSqlite(dir, id, "", messages, nil, wasJSON); err != nil {
		return fmt.Errorf("session: prepare pending rewind %s: migrate legacy raw transcript: %w", id, err)
	}
	return nil
}

func preparePendingRewindOnce(dir, id string, targetIndex int, targetContent string, userSeq int) (*PendingRewind, error) {
	// Do not use openSessionDB here. Its compatibility ALTER for an old
	// missing history_gen column is a meta-schema migration and would move
	// StoredRevisionForDir during a resource-only prepare. Legacy .json/.ojsonl
	// sessions are migrated by ensurePendingRewindSQLite before this point.
	db, err := openDB(sqliteSessionPath(dir, id))
	if err != nil {
		return nil, fmt.Errorf("session: prepare pending rewind %s: open sqlite: %w", id, err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("session: prepare pending rewind %s: begin immediate transaction: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck

	// A schema-less/old SQLite file falls back to mtime+size for
	// StoredRevisionForDir. Creating pending_rewinds would necessarily move that
	// revision, so fail closed before DDL. Current sessions have history_gen;
	// .json/.ojsonl inputs were migrated to that schema before this transaction.
	var historyGen int64
	if err := tx.QueryRow(`SELECT history_gen FROM meta WHERE id = ?`, id).Scan(&historyGen); err != nil {
		return nil, fmt.Errorf("session: prepare pending rewind %s: current sqlite schema required: %w", id, err)
	}
	if _, err := tx.Exec(createPendingRewindsTableSQL); err != nil {
		return nil, fmt.Errorf("session: prepare pending rewind %s: create resource table: %w", id, err)
	}
	rows, err := readRawTranscriptRows(tx)
	if err != nil {
		return nil, fmt.Errorf("session: prepare pending rewind %s: read raw transcript: %w", id, err)
	}
	resolvedIndex, err := resolvePendingRewindTarget(rows, targetIndex, userSeq, targetContent)
	if err != nil {
		return nil, fmt.Errorf("session: prepare pending rewind %s: %w", id, err)
	}
	token, err := newPendingRewindToken()
	if err != nil {
		return nil, fmt.Errorf("session: prepare pending rewind %s: generate token: %w", id, err)
	}
	now := time.Now()
	resource := &PendingRewind{
		Token:         token,
		SessionID:     id,
		TargetIndex:   resolvedIndex,
		UserSeq:       userSeq,
		Fingerprint:   fingerprintRawTranscriptRows(rows),
		Status:        PendingRewindStatusArmed,
		CreatedAt:     now,
		ExpiresAt:     now.Add(pendingRewindTTL),
		TargetContent: targetContent,
	}

	var existingStatus string
	var existingExpiresAt time.Time
	err = tx.QueryRow(`SELECT status, expires_at FROM pending_rewinds WHERE session_id = ?`, id).Scan(&existingStatus, &existingExpiresAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.Exec(`
			INSERT INTO pending_rewinds (
				session_id, token, target_index, user_seq, target_content,
				fingerprint, status, created_at, expires_at, committed_user_seq
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
			resource.SessionID, resource.Token, resource.TargetIndex, nullablePendingRewindUserSeq(resource.UserSeq),
			resource.TargetContent, resource.Fingerprint[:], resource.Status, resource.CreatedAt, resource.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("session: prepare pending rewind %s: insert resource: %w", id, err)
		}
	case err != nil:
		return nil, fmt.Errorf("session: prepare pending rewind %s: read existing resource: %w", id, err)
	default:
		existing := PendingRewindStatus(existingStatus)
		if !existing.Valid() {
			return nil, fmt.Errorf("session: prepare pending rewind %s: existing resource has invalid status %q", id, existingStatus)
		}
		if existing == PendingRewindStatusCommitted && now.Before(existingExpiresAt) {
			return nil, fmt.Errorf("session: prepare pending rewind %s while committed resource is retained: %w", id, ErrPendingRewindAlreadyCommitted)
		}
		if _, err := tx.Exec(`
			UPDATE pending_rewinds SET
				token = ?, target_index = ?, user_seq = ?, target_content = ?,
				fingerprint = ?, status = ?, created_at = ?, expires_at = ?,
				committed_user_seq = NULL
			WHERE session_id = ?`,
			resource.Token, resource.TargetIndex, nullablePendingRewindUserSeq(resource.UserSeq),
			resource.TargetContent, resource.Fingerprint[:], resource.Status, resource.CreatedAt,
			resource.ExpiresAt, resource.SessionID,
		); err != nil {
			return nil, fmt.Errorf("session: prepare pending rewind %s: replace resource: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("session: prepare pending rewind %s: commit: %w", id, err)
	}
	return resource, nil
}

func pendingRewindStatusFromDir(dir, id, token string) (*PendingRewind, error) {
	path := sqliteSessionPath(dir, id)
	if !fileExists(path) {
		return nil, fmt.Errorf("session %s: %w", id, ErrPendingRewindNotFound)
	}
	db, err := openDBRaw(path)
	if err != nil {
		return nil, fmt.Errorf("session: read pending rewind %s: open sqlite: %w", id, err)
	}
	defer db.Close()

	exists, err := pendingRewindsTableExists(db)
	if err != nil {
		return nil, fmt.Errorf("session: read pending rewind %s: inspect resource table: %w", id, err)
	}
	if !exists {
		return nil, fmt.Errorf("session %s: %w", id, ErrPendingRewindNotFound)
	}
	resource, err := readPendingRewind(db, id, token)
	if err != nil {
		return nil, err
	}
	if resource.Status == PendingRewindStatusArmed && !time.Now().Before(resource.ExpiresAt) {
		return resource, fmt.Errorf("session: pending rewind for %s expired at %s: %w", id, resource.ExpiresAt.Format(time.RFC3339Nano), ErrPendingRewindExpired)
	}
	return resource, nil
}

func cancelPendingRewindOnce(dir, id, token string) error {
	path := sqliteSessionPath(dir, id)
	if !fileExists(path) {
		return fmt.Errorf("session %s: %w", id, ErrPendingRewindNotFound)
	}
	db, err := openDB(path)
	if err != nil {
		return fmt.Errorf("session: cancel pending rewind %s: open sqlite: %w", id, err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("session: cancel pending rewind %s: begin immediate transaction: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck

	exists, err := pendingRewindsTableExists(tx)
	if err != nil {
		return fmt.Errorf("session: cancel pending rewind %s: inspect resource table: %w", id, err)
	}
	if !exists {
		return fmt.Errorf("session %s: %w", id, ErrPendingRewindNotFound)
	}
	result, err := tx.Exec(`DELETE FROM pending_rewinds WHERE session_id = ? AND token = ? AND status IN ('armed', 'stale')`, id, token)
	if err != nil {
		return fmt.Errorf("session: cancel pending rewind %s: delete resource: %w", id, err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("session: cancel pending rewind %s: count deleted resource: %w", id, err)
	}
	if deleted == 0 {
		return fmt.Errorf("session %s: %w", id, ErrPendingRewindNotFound)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: cancel pending rewind %s: commit: %w", id, err)
	}
	return nil
}

func commitPendingRewindOnce(dir, id, token, content string) (*PendingRewindCommitResult, error) {
	path := sqliteSessionPath(dir, id)
	if !fileExists(path) {
		return nil, fmt.Errorf("session %s: %w", id, ErrPendingRewindNotFound)
	}
	db, err := openDB(path)
	if err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: open sqlite: %w", id, err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: begin immediate transaction: %w", id, err)
	}
	defer tx.Rollback() //nolint:errcheck

	exists, err := pendingRewindsTableExists(tx)
	if err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: inspect resource table: %w", id, err)
	}
	if !exists {
		return nil, fmt.Errorf("session %s: %w", id, ErrPendingRewindNotFound)
	}
	resource, err := readPendingRewind(tx, id, token)
	if err != nil {
		return nil, err
	}
	if resource.Status == PendingRewindStatusStale {
		return nil, fmt.Errorf("session: pending rewind for %s: %w", id, ErrPendingRewindStale)
	}
	if resource.Status == PendingRewindStatusCommitted {
		return &PendingRewindCommitResult{Rewind: *resource}, fmt.Errorf("session: pending rewind for %s: %w", id, ErrPendingRewindAlreadyCommitted)
	}

	rows, err := readRawTranscriptRows(tx)
	if err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: read raw transcript: %w", id, err)
	}
	if !time.Now().Before(resource.ExpiresAt) {
		return nil, fmt.Errorf("session: pending rewind for %s expired at %s: %w", id, resource.ExpiresAt.Format(time.RFC3339Nano), ErrPendingRewindExpired)
	}

	resolvedIndex, targetErr := resolvePendingRewindTarget(rows, resource.TargetIndex, resource.UserSeq, resource.TargetContent)
	currentFingerprint := fingerprintRawTranscriptRows(rows)
	fingerprintMatches := bytes.Equal(resource.Fingerprint[:], currentFingerprint[:])
	if targetErr != nil || !fingerprintMatches {
		if err := markPendingRewindStale(tx, id, token); err != nil {
			return nil, fmt.Errorf("session: pending rewind for %s became stale but transition failed: %w", id, err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("session: pending rewind for %s became stale but transition commit failed: %w", id, err)
		}
		log.Printf("session: pending rewind for %s marked stale: transcript fingerprint or target no longer matches", id)
		return nil, fmt.Errorf("session: pending rewind for %s: %w", id, ErrPendingRewindStale)
	}

	prefix, err := decodePendingRewindPrefix(rows, resolvedIndex)
	if err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: decode kept prefix: %w", id, err)
	}
	committedUserSeq := NextUserSeq(prefix)
	data, err := json.Marshal(agent.Message{Role: "user", Content: content, UserSeq: committedUserSeq})
	if err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: marshal replacement user message: %w", id, err)
	}
	var historyGen int64
	if err := tx.QueryRow(`SELECT history_gen FROM meta WHERE id = ?`, id).Scan(&historyGen); err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: read history_gen: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE seq >= ?`, resolvedIndex); err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: delete target tail: %w", id, err)
	}
	if _, err := tx.Exec(`INSERT INTO messages (seq, data) VALUES (?, ?)`, resolvedIndex, string(data)); err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: insert replacement user message: %w", id, err)
	}
	now := time.Now()
	result, err := tx.Exec(`UPDATE meta SET updated_at = ?, history_gen = ? WHERE id = ?`, now, historyGen+1, id)
	if err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: update meta: %w", id, err)
	}
	if err := requireOnePendingRewindRow(result, "update meta"); err != nil {
		return nil, err
	}
	result, err = tx.Exec(`
		UPDATE pending_rewinds
		SET status = ?, committed_user_seq = ?
		WHERE session_id = ? AND token = ? AND status = ?`,
		PendingRewindStatusCommitted, committedUserSeq, id, token, PendingRewindStatusArmed,
	)
	if err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: mark resource committed: %w", id, err)
	}
	if err := requireOnePendingRewindRow(result, "mark resource committed"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("session: commit pending rewind %s: commit: %w", id, err)
	}

	log.Printf("session: pending rewind commit %s: shrink %d -> %d stored rows at target %d, history_gen %d -> %d", id, len(rows), resolvedIndex+1, resolvedIndex, historyGen, historyGen+1)
	resource.Status = PendingRewindStatusCommitted
	resource.CommittedUserSeq = committedUserSeq
	return &PendingRewindCommitResult{Rewind: *resource, KeptPrefix: prefix}, nil
}

func resolvePendingRewindTarget(rows []rawTranscriptRow, targetIndex, userSeq int, targetContent string) (int, error) {
	if userSeq > 0 {
		resolved := -1
		for _, row := range rows {
			var message agent.Message
			if err := json.Unmarshal([]byte(row.Data), &message); err != nil {
				return -1, fmt.Errorf("decode raw message seq %d: %w", row.Seq, err)
			}
			if message.Role != "user" || message.UserSeq != userSeq {
				continue
			}
			if resolved >= 0 {
				return -1, fmt.Errorf("user_seq %d matches multiple raw messages: %w", userSeq, ErrPendingRewindInvalidTarget)
			}
			resolved = row.Seq
		}
		if resolved < 0 {
			return -1, fmt.Errorf("user_seq %d has no raw user message: %w", userSeq, ErrPendingRewindInvalidTarget)
		}
		return verifyPendingRewindTargetContent(rows, resolved, targetContent)
	}
	return verifyPendingRewindTargetContent(rows, targetIndex, targetContent)
}

func verifyPendingRewindTargetContent(rows []rawTranscriptRow, targetIndex int, targetContent string) (int, error) {
	for _, row := range rows {
		if row.Seq != targetIndex {
			continue
		}
		var message agent.Message
		if err := json.Unmarshal([]byte(row.Data), &message); err != nil {
			return -1, fmt.Errorf("decode raw message seq %d: %w", row.Seq, err)
		}
		if message.Role != "user" || message.Content != targetContent {
			return -1, fmt.Errorf("raw message seq %d is not the exact requested user content: %w", targetIndex, ErrPendingRewindInvalidTarget)
		}
		return row.Seq, nil
	}
	return -1, fmt.Errorf("raw target index %d is out of range: %w", targetIndex, ErrPendingRewindInvalidTarget)
}

func decodePendingRewindPrefix(rows []rawTranscriptRow, targetIndex int) ([]agent.Message, error) {
	prefix := make([]agent.Message, 0, min(targetIndex, len(rows)))
	for _, row := range rows {
		if row.Seq >= targetIndex {
			break
		}
		var message agent.Message
		if err := json.Unmarshal([]byte(row.Data), &message); err != nil {
			return nil, fmt.Errorf("decode raw message seq %d: %w", row.Seq, err)
		}
		prefix = append(prefix, message)
	}
	return prefix, nil
}

func readRawTranscriptRows(queryer interface {
	Query(string, ...any) (*sql.Rows, error)
}) ([]rawTranscriptRow, error) {
	rows, err := queryer.Query(`SELECT seq, data FROM messages ORDER BY seq ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]rawTranscriptRow, 0)
	for rows.Next() {
		var row rawTranscriptRow
		if err := rows.Scan(&row.Seq, &row.Data); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return result, nil
}

func pendingRewindsTableExists(queryer interface {
	QueryRow(string, ...any) *sql.Row
}) (bool, error) {
	var exists bool
	if err := queryer.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'pending_rewinds'
	)`).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func readPendingRewind(queryer interface {
	QueryRow(string, ...any) *sql.Row
}, id, token string) (*PendingRewind, error) {
	row := queryer.QueryRow(`SELECT `+selectPendingRewindColumns+`
		FROM pending_rewinds WHERE session_id = ? AND token = ?`, id, token)
	resource, err := scanPendingRewind(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session %s: %w", id, ErrPendingRewindNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("session: read pending rewind %s: %w", id, err)
	}
	return resource, nil
}

func scanPendingRewind(scanner interface{ Scan(...any) error }) (*PendingRewind, error) {
	var resource PendingRewind
	var userSeq, committedUserSeq sql.NullInt64
	var fingerprint []byte
	var status string
	if err := scanner.Scan(
		&resource.SessionID,
		&resource.Token,
		&resource.TargetIndex,
		&userSeq,
		&resource.TargetContent,
		&fingerprint,
		&status,
		&resource.CreatedAt,
		&resource.ExpiresAt,
		&committedUserSeq,
	); err != nil {
		return nil, err
	}
	if len(fingerprint) != sha256.Size {
		return nil, fmt.Errorf("session: pending rewind fingerprint has %d bytes, want %d", len(fingerprint), sha256.Size)
	}
	copy(resource.Fingerprint[:], fingerprint)
	resource.UserSeq = int(userSeq.Int64)
	resource.CommittedUserSeq = int(committedUserSeq.Int64)
	resource.Status = PendingRewindStatus(status)
	if !resource.Status.Valid() {
		return nil, fmt.Errorf("session: pending rewind has invalid status %q", status)
	}
	return &resource, nil
}

func markPendingRewindStale(tx *sql.Tx, id, token string) error {
	result, err := tx.Exec(`UPDATE pending_rewinds SET status = ? WHERE session_id = ? AND token = ? AND status = ?`, PendingRewindStatusStale, id, token, PendingRewindStatusArmed)
	if err != nil {
		return fmt.Errorf("update stale status: %w", err)
	}
	return requireOnePendingRewindRow(result, "update stale status")
}

func requireOnePendingRewindRow(result sql.Result, operation string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("session: pending rewind %s: count affected rows: %w", operation, err)
	}
	if rows != 1 {
		return fmt.Errorf("session: pending rewind %s affected %d rows, want 1", operation, rows)
	}
	return nil
}

func newPendingRewindToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func nullablePendingRewindUserSeq(userSeq int) any {
	if userSeq == 0 {
		return nil
	}
	return userSeq
}

func retryPendingRewindError(operation func() error) error {
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		err = operation()
		if err == nil || !isBusyErr(err) {
			return err
		}
		time.Sleep(time.Duration(5*(attempt+1)) * time.Millisecond)
	}
	return err
}

func retryPendingRewindWrite[T any](operation func() (T, error)) (T, error) {
	var zero T
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		var value T
		value, err = operation()
		if err == nil || !isBusyErr(err) {
			return value, err
		}
		time.Sleep(time.Duration(5*(attempt+1)) * time.Millisecond)
	}
	return zero, err
}

func movePendingRewindForRekey(dir, oldID, newID string) error {
	resource, exists, err := readPendingRewindForRekey(dir, oldID)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	db, err := openSessionDB(sqliteSessionPath(dir, newID))
	if err != nil {
		return fmt.Errorf("session: rekey pending rewind %s -> %s: open new sqlite: %w", oldID, newID, err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("session: rekey pending rewind %s -> %s: begin immediate transaction: %w", oldID, newID, err)
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(createPendingRewindsTableSQL); err != nil {
		return fmt.Errorf("session: rekey pending rewind %s -> %s: create resource table: %w", oldID, newID, err)
	}
	rows, err := readRawTranscriptRows(tx)
	if err != nil {
		return fmt.Errorf("session: rekey pending rewind %s -> %s: read rekeyed transcript: %w", oldID, newID, err)
	}
	resource.Fingerprint = fingerprintRawTranscriptRows(rows)
	if resource.Status == PendingRewindStatusArmed {
		resolvedIndex, err := resolvePendingRewindTarget(rows, resource.TargetIndex, resource.UserSeq, resource.TargetContent)
		if err != nil {
			return fmt.Errorf("session: rekey pending rewind %s -> %s: resolve moved target: %w", oldID, newID, err)
		}
		resource.TargetIndex = resolvedIndex
	}
	if _, err := tx.Exec(`
		INSERT INTO pending_rewinds (
			session_id, token, target_index, user_seq, target_content, fingerprint,
			status, created_at, expires_at, committed_user_seq
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID, resource.Token, resource.TargetIndex, nullablePendingRewindUserSeq(resource.UserSeq),
		resource.TargetContent, resource.Fingerprint[:], resource.Status, resource.CreatedAt,
		resource.ExpiresAt, nullablePendingRewindUserSeq(resource.CommittedUserSeq),
	); err != nil {
		return fmt.Errorf("session: rekey pending rewind %s -> %s: insert moved resource: %w", oldID, newID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("session: rekey pending rewind %s -> %s: commit: %w", oldID, newID, err)
	}
	return nil
}

func readPendingRewindForRekey(dir, id string) (*PendingRewind, bool, error) {
	path := sqliteSessionPath(dir, id)
	if !fileExists(path) {
		return nil, false, nil
	}
	db, err := openDBRaw(path)
	if err != nil {
		return nil, false, fmt.Errorf("session: rekey pending rewind %s: open sqlite: %w", id, err)
	}
	defer db.Close()
	exists, err := pendingRewindsTableExists(db)
	if err != nil {
		return nil, false, fmt.Errorf("session: rekey pending rewind %s: inspect resource table: %w", id, err)
	}
	if !exists {
		return nil, false, nil
	}
	var token string
	err = db.QueryRow(`SELECT token FROM pending_rewinds WHERE session_id = ?`, id).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("session: rekey pending rewind %s: read token: %w", id, err)
	}
	resource, err := readPendingRewind(db, id, token)
	if err != nil {
		return nil, false, err
	}
	return resource, true, nil
}
