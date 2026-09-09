package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/tool"
)

type Session struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// TitleGenerated is true when Title was set explicitly (LLM-generated or
	// user /title), false when it is the auto-title fallback derived from the
	// first user message. Callers use it to decide whether title generation
	// should still run for a resumed session.
	TitleGenerated bool            `json:"title_generated,omitempty"`
	Messages       []agent.Message `json:"messages"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
}

type Source string

const (
	SourceOcode  Source = "ocode"
	SourceClaude Source = "claude"
)

type Ref struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Source    Source
}

type sessionIndex struct {
	LastSessionID string            `json:"last_session_id"`
	Sessions      map[string]string `json:"sessions"` // ID -> Title
}

const canonicalSessionPrefix = "ses_"

// NewSessionID generates a session ID. The timestamp alone only has
// second-resolution: two "new chat" requests (e.g. from two browser tabs)
// arriving in the same second produced identical IDs, silently merging the
// two sessions in Handler.agents and in the on-disk session file, and making
// the second request block on the first's per-session mutex until its whole
// turn finished. The random suffix guarantees uniqueness across concurrent
// callers regardless of timing.
func NewSessionID() string {
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	return canonicalSessionPrefix + time.Now().Format("2006-01-02-150405") + "-" + hex.EncodeToString(suffix[:])
}

// GetStorageDir returns the per-project sessions directory under the
// global data dir. It always uses the cross-platform global path
// (see internal/paths.GlobalDataDir).
func GetStorageDir() (string, error) {
	slug := ProjectSlug()
	return paths.ProjectSessionsDir(slug)
}

// GetStorageDirForPath returns the per-project sessions directory for a given
// working directory path. Unlike GetStorageDir, this does not depend on the
// global workDir override or os.Getwd().
func GetStorageDirForPath(wd string) (string, error) {
	slug := ProjectSlugForPath(wd)
	return paths.ProjectSessionsDir(slug)
}

// workDirOverride overrides os.Getwd() for project slug resolution in
// GetStorageDir and getClaudeProjectDir. Set via SetWorkDir so that session
// storage follows the TUI's explicit workDir instead of the process-wide CWD
// (which can change under /cd or -dir without the session package noticing).
// Empty means fall back to os.Getwd().
var (
	workDirOverride   string
	workDirOverrideMu sync.RWMutex
)

// SetWorkDir sets the working directory used for project slug resolution.
// Symlinks are resolved so the stored path matches os.Getwd() behavior.
// Pass "" to revert to os.Getwd().
func SetWorkDir(dir string) {
	if dir != "" {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			dir = resolved
		}
	}
	workDirOverrideMu.Lock()
	workDirOverride = dir
	workDirOverrideMu.Unlock()
}

// effectiveWorkDir returns the configured work dir or falls back to os.Getwd().
func effectiveWorkDir() string {
	workDirOverrideMu.RLock()
	dir := workDirOverride
	workDirOverrideMu.RUnlock()
	if dir != "" {
		return dir
	}
	wd, _ := os.Getwd()
	return wd
}

// ProjectSlug returns the stable slug for the current workspace root.
func ProjectSlug() string {
	return ProjectSlugForPath(effectiveWorkDir())
}

// ProjectSlugForPath returns the stable slug for the workspace containing wd.
func ProjectSlugForPath(wd string) string {
	if wd == "" {
		wd = effectiveWorkDir()
	}
	return paths.ProjectSlug(wd)
}

func Save(id string, title string, messages []agent.Message, metadata map[string]any) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}
	// persistToDir serializes in-process writers itself; the authoritative
	// transcript is durable on return.
	if err := persistToDir(dir, id, title, messages, metadata, false, 0, false); err != nil {
		return err
	}
	// The authoritative transcript is durable: queued live snapshots are
	// superseded, so satisfy their barriers instead of leaving a shutdown
	// FlushAll waiting on writes that no longer need to happen.
	markLiveCovered(dir, id)
	return nil
}

// SaveForDir persists a session into the storage dir associated with wd, so
// multi-project servers can save a session under its owning project root
// instead of the process workdir (Save keeps using the process/session
// workdir). The on-disk format is identical to Save — same JSON/ojsonl
// selection, same index updates — so sessions written by one path load via
// the other.
func SaveForDir(wd, id string, title string, messages []agent.Message, metadata map[string]any) error {
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		return err
	}
	// persistToDir serializes in-process writers itself (see Save).
	if err := persistToDir(dir, id, title, messages, metadata, false, 0, false); err != nil {
		return err
	}
	// See Save: an authoritative sync save satisfies queued live barriers.
	markLiveCovered(dir, id)
	return nil
}

// PaginatedLoad returns a window of messages for id, most-recent-first paging
// (offset = skip this many from the tail, limit = max from end-offset). It is
// the single pagination helper shared by server and TUI so the slice logic
// does not drift. limit<=0 means return all up to the offset point.
func PaginatedLoad(wd, id string, limit, offset int) (msgs []agent.Message, total int, err error) {
	sess, err := LoadForDir(wd, id)
	if err != nil {
		return nil, 0, err
	}
	total = len(sess.Messages)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	end := total - offset
	if end < 0 {
		end = 0
	}
	if limit <= 0 || limit >= end {
		return sess.Messages[:end], total, nil
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	return sess.Messages[start:end], total, nil
}

// saveToDir is the append-mode core used by the live worker and tests: it
// delegates to persistToDir with live/append semantics (never shrinks or
// replaces stored history).
func saveToDir(dir, id string, title string, messages []agent.Message, metadata map[string]any, live bool, liveGen int64) error {
	return persistToDir(dir, id, title, messages, metadata, live, liveGen, false)
}

// Replace persists an authoritative transcript replacement into the
// process-default storage dir; ReplaceForDir targets the storage dir of
// wd (session-owning project). Use these for compaction, truncation, and
// transcript rewind: the stored history is rewritten to exactly messages
// (which may be shorter or differ in content) and history_gen is bumped so
// queued pre-replacement live snapshots drop instead of resurrecting
// replaced history. Ordinary Save/SaveForDir must NOT be used for that:
// since the concurrent-writer hardening, a shorter snapshot conflicts
// (ErrTranscriptConflict) instead of silently deleting the other writer's
// rows.
func Replace(id string, title string, messages []agent.Message, metadata map[string]any) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}
	return persistToDir(dir, id, title, messages, metadata, false, 0, true)
}

// ReplaceForDir persists an authoritative transcript replacement under the
// storage dir associated with wd. See Replace.
func ReplaceForDir(wd, id string, title string, messages []agent.Message, metadata map[string]any) error {
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		return err
	}
	return persistToDir(dir, id, title, messages, metadata, false, 0, true)
}

// AppendUserMessageForDir durably appends a single user message to session
// id before its turn is dispatched (the server path's persist-before-202
// rule). The .sqlite fast path is an append-only tail insert
// (appendUserMessageTail): it never rewrites stored rows, so it cannot
// conflict with the overlap check even when the stored transcript contains
// rows the load path filters out (incomplete tool results, the
// PERMISSION_ASK sentinel) — the exact shape that made the previous
// load-append-save implementation conflict forever ("conflicting message
// at seq N (concurrent writers diverged)") and drop the user's message.
// A primary-key violation (another writer took the same tail seq first)
// retries with a re-read count; the legacy/missing-session path goes
// through the normal create/migrate save with the RAW stored transcript.
//
// Retry is bounded (8 attempts, matching the store's BUSY retry) and only
// for classified conflict/constraint errors; transient lock contention is
// already retried inside the store, and any other error is returned
// immediately.
func AppendUserMessageForDir(projectRoot, id, content string) error {
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		return err
	}
	if id == "" {
		id = NewSessionID()
	}
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		if attempt > 0 {
			// The retry itself is the convergence step (tail count re-read
			// inside the next transaction, or a raw reload for the legacy
			// path); the small backoff keeps a hot multi-writer loop from
			// spinning. Bounded, never indefinite.
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
		if fileExists(sqliteSessionPath(dir, id)) {
			err := appendUserMessageTail(dir, id, content)
			if err == nil {
				return refreshIndexMeta(dir, id)
			}
			lastErr = err
			if !isConstraintErr(err) {
				return err
			}
			continue
		}
		// Legacy .json/.ojsonl or missing session: migrate/create through
		// the normal save path with the RAW stored transcript (no
		// incomplete-tool-request filtering) plus the user message, so the
		// migrated file matches the legacy file row-for-row and the
		// message lands at the true tail.
		msgs, err := loadRawMessages(dir, id)
		if err != nil {
			return err
		}
		msgs = append(msgs, agent.Message{Role: "user", Content: content, UserSeq: NextUserSeq(msgs)})
		err = persistToDir(dir, id, "", msgs, nil, false, 0, false)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isConflictErr(err) && !isConstraintErr(err) {
			return err
		}
	}
	return fmt.Errorf("session: append user message to %s: %w", id, lastErr)
}

// NextUserSeq returns the durable per-session sequence for the NEXT user
// message: one more than the highest UserSeq already present in the
// transcript (1 for an empty/legacy transcript). Every writer that appends
// a user message — the async pre-persist (AppendUserMessageForDir) and the
// turn's in-memory append (server runTurn) — must derive the seq the same
// way so the two copies of one message serialize byte-identically; the
// overlap/conflict check compares persistence form, and a seq present on
// one copy but not the other reads as a diverged transcript.
func NextUserSeq(messages []agent.Message) int {
	max := 0
	for _, m := range messages {
		if m.Role == "user" && m.UserSeq > max {
			max = m.UserSeq
		}
	}
	return max + 1
}

// loadRawMessages reads the stored transcript WITHOUT the LLM-oriented
// incomplete-tool-request filtering loadFromDir applies. Persist paths use
// it so a saved snapshot matches the stored rows row-for-row; feeding the
// filtered view back would drop stored rows (sentinels, orphaned tool
// results) or shift positions and trip the overlap conflict check. A
// missing session yields an empty transcript (the save creates the file).
func loadRawMessages(dir, id string) ([]agent.Message, error) {
	for _, candidate := range sessionCandidateIDs(id) {
		sqlitePath := sqliteSessionPath(dir, candidate)
		if !fileExists(sqlitePath) {
			continue
		}
		s, err := readSqliteSession(sqlitePath)
		if err != nil {
			return nil, err
		}
		return s.Messages, nil
	}
	for _, candidate := range sessionCandidateIDs(id) {
		ojsonlPath := ojsonlSessionPath(dir, candidate)
		if !fileExists(ojsonlPath) {
			continue
		}
		s, err := loadOjsonlSession(ojsonlPath)
		if err != nil {
			return nil, err
		}
		return s.Messages, nil
	}
	path, data, err := readSessionFile(dir, id)
	if err != nil {
		return nil, nil
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session file %s is corrupt: %w", path, err)
	}
	return s.Messages, nil
}

// ReconcileAppend persists msgs (a full transcript whose first baseLen
// messages are the caller's trusted base) into the process-default storage
// dir with one bounded rebase: on a concurrent-writer conflict, the raw
// disk transcript is reloaded and merged — stored rows kept in place, the
// caller's not-yet-stored suffix appended after them — so BOTH writers'
// messages survive instead of the turn's response being left memory-only.
// Divergence inside the base (two writers produced different content for
// the same position) has no safe merge and returns the conflict. See
// rebaseAppend for the merge rule.
func ReconcileAppend(id, title string, messages []agent.Message, baseLen int, metadata map[string]any) error {
	_, err := ReconcileAppendWithMessages(id, title, messages, baseLen, metadata)
	return err
}

// ReconcileAppendWithMessages is ReconcileAppend, but returns the filtered
// transcript when a concurrent append required a rebase. The returned slice is
// nil when the initial save succeeded without a rebase. Callers that keep a
// resident transcript can replace it with the returned slice to stay aligned
// with rows inserted by another writer.
func ReconcileAppendWithMessages(id, title string, messages []agent.Message, baseLen int, metadata map[string]any) ([]agent.Message, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}
	return reconcileAppendToDirWithMessages(dir, id, title, messages, baseLen, metadata)
}

// ReconcileAppendForDir is ReconcileAppend targeting the storage dir of wd
// (the session-owning project).
func ReconcileAppendForDir(projectRoot, id, title string, messages []agent.Message, baseLen int, metadata map[string]any) error {
	_, err := ReconcileAppendForDirWithMessages(projectRoot, id, title, messages, baseLen, metadata)
	return err
}

// ReconcileAppendForDirWithMessages is the project-root-scoped form of
// ReconcileAppendWithMessages.
func ReconcileAppendForDirWithMessages(projectRoot, id, title string, messages []agent.Message, baseLen int, metadata map[string]any) ([]agent.Message, error) {
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		return nil, err
	}
	return reconcileAppendToDirWithMessages(dir, id, title, messages, baseLen, metadata)
}

func reconcileAppendToDir(dir, id, title string, messages []agent.Message, baseLen int, metadata map[string]any) error {
	_, err := reconcileAppendToDirWithMessages(dir, id, title, messages, baseLen, metadata)
	return err
}

func reconcileAppendToDirWithMessages(dir, id, title string, messages []agent.Message, baseLen int, metadata map[string]any) ([]agent.Message, error) {
	err := persistToDir(dir, id, title, messages, metadata, false, 0, false)
	if err == nil {
		return nil, nil
	}
	if !isConflictErr(err) {
		return nil, err
	}
	var lastErr = err
	for attempt := 0; attempt < 8; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
		stored, err := loadRawMessages(dir, id)
		if err != nil {
			return nil, err
		}
		merged, ok := rebaseAppend(stored, messages, baseLen)
		if !ok {
			// The base itself diverged: two writers produced different
			// content for the same position. No safe merge exists; the
			// caller (server) converges to disk and logs the dropped
			// suffix instead of silently staying diverged forever.
			return nil, fmt.Errorf("session: reconcile append to %s: base diverged from stored transcript: %w", id, lastErr)
		}
		err = persistToDir(dir, id, title, merged, metadata, false, 0, false)
		if err == nil {
			return removeIncompleteToolRequests(merged), nil
		}
		lastErr = err
		if !isConflictErr(err) && !isConstraintErr(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("session: reconcile append to %s: %w", id, lastErr)
}

// rebaseAppend merges ours (a transcript whose first baseLen messages are
// the caller's trusted base) onto stored (the current disk transcript) for
// the concurrent-append case. Rows beyond baseLen in stored may be a mix of
// the caller's own earlier live writes and another writer's appends, so the
// merge walks stored in order and consumes the next unsaved message of ours
// whenever a stored row matches it byte-for-byte (our live writes land in
// order, so a matching row is ours; a non-matching row is foreign and is
// kept). Rows of ours never matched stay in order at the tail. Returns
// ok=false when the base itself diverged — same-position content written by
// two writers cannot be merged without dropping one side.
//
// The caller's base may also be the LOADER's view of stored (loadFromDir →
// removeIncompleteToolRequests dropped an unanswered ask round), which is
// shorter and shifted relative to the raw rows. That is not a divergence:
// the raw rows are kept in place and the unsaved suffix lands after them,
// so the loader view of the result equals the caller's transcript.
func rebaseAppend(stored, ours []agent.Message, baseLen int) ([]agent.Message, bool) {
	if baseLen < 0 || baseLen > len(ours) {
		return nil, false
	}
	view := stored
	if !samePrefix(view, ours, baseLen) {
		view = removeIncompleteToolRequests(stored)
		if !samePrefix(view, ours, baseLen) {
			return nil, false
		}
	}
	merged := append([]agent.Message(nil), stored...)
	next := baseLen
	for i := baseLen; i < len(view); i++ {
		if next < len(ours) && sameMessage(view[i], ours[next]) {
			next++
		}
	}
	return append(merged, ours[next:]...), true
}

// samePrefix reports whether the first n messages of a and b are byte-equal
// in persistence form (false when either is shorter than n).
func samePrefix(a, b []agent.Message, n int) bool {
	if len(a) < n || len(b) < n {
		return false
	}
	for i := 0; i < n; i++ {
		if !sameMessage(a[i], b[i]) {
			return false
		}
	}
	return true
}

// sameMessage reports byte-equal persistence form (the overlap check's
// notion of identity).
func sameMessage(a, b agent.Message) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(ab) == string(bb)
}

// persistToDir is the shared save core: it resolves the session's storage
// dir and dispatches on which format already exists for id. New sessions
// are always created directly as .sqlite. An existing .json or .ojsonl
// session is migrated to .sqlite — and its old file deleted — the first
// time it is written to again after being loaded; sessions that are only
// ever read are left in their original format. See
// docs/superpowers/plans/2026-08-28-sqlite-session-storage/INDEX.md.
//
// Callers hold no lock: persistToDir serializes in-process writers on the
// session's stripe mutex (lockFor), so the sync Save/SaveForDir path, the
// async live worker, and direct callers all funnel through one lock.
// live selects the async live-write mode:
// appends never regress (see appendSqliteSession) and the index refresh is
// meta-only, avoiding a full transcript re-read per streamed message.
func persistToDir(dir, id string, title string, messages []agent.Message, metadata map[string]any, live bool, liveGen int64, replace bool) error {
	mu := lockFor(dir, id)
	mu.Lock()
	defer mu.Unlock()
	if id == "" {
		id = NewSessionID()
	}

	if fileExists(sqliteSessionPath(dir, id)) {
		changed, err := appendSqliteSession(dir, id, title, messages, metadata, live, liveGen, replace)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if live {
			return refreshIndexMeta(dir, id)
		}
		return refreshIndexRow(dir, id)
	}

	jsonPath := filepath.Join(dir, id+".json")
	ojsonlPath := ojsonlSessionPath(dir, id)
	wasJSON := fileExists(jsonPath)
	if wasJSON || fileExists(ojsonlPath) {
		return migrateToSqlite(dir, id, title, messages, metadata, wasJSON)
	}

	// Brand-new session: create directly in sqlite.
	now := time.Now()
	// Derive auto-title from first user message when caller didn't supply one,
	// mirroring saveJSON/saveOjsonl fallback so new sqlite sessions aren't
	// titleless.
	resolvedTitle := title
	titleGenerated := title != ""
	if resolvedTitle == "" && len(messages) > 0 {
		for _, m := range messages {
			t := strings.TrimSpace(m.Content)
			if m.Role == "user" && t != "" && LooksLikeAutoTitleCandidate(t) {
				resolvedTitle = t
				break
			}
		}
	}
	s := Session{
		ID:             id,
		Title:          resolvedTitle,
		TitleGenerated: titleGenerated,
		Messages:       messages,
		CreatedAt:      now,
		UpdatedAt:      now,
		Metadata:       metadata,
	}
	if err := writeSqliteSessionFull(dir, s); err != nil {
		return err
	}
	return refreshIndexRow(dir, id)
}

// migrateToSqlite converts an existing .json or .ojsonl session to
// .sqlite on save, preserving created_at (and title/title_generated when
// this save doesn't set an explicit new title) from the old file. It
// writes the new file and reads it back to confirm it round-trips before
// deleting the original — a transcript is never lost to a bad migration.
// wasJSON selects which legacy format id is currently stored in.
func migrateToSqlite(dir, id, title string, messages []agent.Message, metadata map[string]any, wasJSON bool) error {
	now := time.Now()
	s := Session{
		ID:             id,
		Title:          title,
		TitleGenerated: title != "",
		Messages:       messages,
		CreatedAt:      now,
		UpdatedAt:      now,
		Metadata:       metadata,
	}

	if wasJSON {
		jsonPath := filepath.Join(dir, id+".json")
		data, err := os.ReadFile(jsonPath)
		if err != nil {
			return fmt.Errorf("session: migrate %s: cannot read legacy %s, original left in place: %w", id, jsonPath, err)
		}
		var old Session
		if err := json.Unmarshal(data, &old); err != nil {
			return fmt.Errorf("session: migrate %s: legacy %s is corrupt, original left in place: %w", id, jsonPath, err)
		}
		if !old.CreatedAt.IsZero() {
			s.CreatedAt = old.CreatedAt
		}
		if s.Title == "" {
			s.Title = old.Title
			s.TitleGenerated = old.TitleGenerated
		}
		if s.Metadata == nil && old.Metadata != nil {
			s.Metadata = old.Metadata
		}
	} else {
		ojsonlPath := ojsonlSessionPath(dir, id)
		// The legacy file is the only source of created_at/title/metadata
		// here, so a file that cannot be parsed must fail the migration —
		// writing only the caller's snapshot and deleting the original
		// would destroy the transcript it still holds.
		legacy, err := loadOjsonlSession(ojsonlPath)
		if err != nil {
			return fmt.Errorf("session: migrate %s: legacy %s is unreadable, original left in place: %w", id, ojsonlPath, err)
		}
		if !legacy.CreatedAt.IsZero() {
			s.CreatedAt = legacy.CreatedAt
		}
		if s.Title == "" {
			s.Title = legacy.Title
			s.TitleGenerated = legacy.TitleGenerated
		}
		// Preserve legacy metadata when caller supplies nil (e.g. server
		// compaction path).
		if s.Metadata == nil {
			s.Metadata = legacy.Metadata
		}
	}

	if err := writeSqliteSessionFull(dir, s); err != nil {
		return fmt.Errorf("session: migrate %s to sqlite: %w", id, err)
	}

	// Verify the new file round-trips before touching the original.
	if _, err := readSqliteSession(sqliteSessionPath(dir, id)); err != nil {
		return fmt.Errorf("session: migrated sqlite file for %s failed verification, original left in place: %w", id, err)
	}

	if err := refreshIndexRow(dir, id); err != nil {
		log.Printf("session: index upsert for migrated %s failed (non-fatal): %v", id, err)
	}

	if wasJSON {
		os.Remove(filepath.Join(dir, id+".json")) //nolint:errcheck
	} else {
		ojsonlPath := ojsonlSessionPath(dir, id)
		os.Remove(ojsonlPath) //nolint:errcheck
		clearOjsonlWriteState(ojsonlPath)
	}
	return nil
}

// refreshIndexMeta is the lightweight index refresh used by live writes: it
// reads only the meta row (no transcript scan) and upserts it into the
// project's shared index. Live writes fire per streamed message, so the full
// readSqliteSession round-trip in refreshIndexRow would re-read a growing
// transcript on every message.
func refreshIndexMeta(dir, id string) error {
	db, err := openDB(sqliteSessionPath(dir, id))
	if err != nil {
		return err
	}
	defer db.Close()
	var title string
	var createdAt, updatedAt time.Time
	var metaJSON string
	if err := db.QueryRow(`SELECT title, created_at, updated_at, metadata_json FROM meta WHERE id = ?`, id).
		Scan(&title, &createdAt, &updatedAt, &metaJSON); err != nil {
		return fmt.Errorf("session: read meta %s for index refresh: %w", id, err)
	}
	cloneOf := ""
	if metaJSON != "" && metaJSON != "{}" && metaJSON != "null" {
		var meta map[string]any
		if err := json.Unmarshal([]byte(metaJSON), &meta); err == nil {
			if v, ok := meta["claude_original_session_id"].(string); ok {
				cloneOf = v
			}
		}
	}
	return upsertIndexRow(dir, ocodeMeta{
		ID:        id,
		Title:     title,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		CloneOf:   cloneOf,
	})
}
func refreshIndexRow(dir, id string) error {
	s, err := readSqliteSession(sqliteSessionPath(dir, id))
	if err != nil {
		return fmt.Errorf("session: read back %s for index refresh: %w", id, err)
	}
	cloneOf := ""
	if s.Metadata != nil {
		if v, ok := s.Metadata["claude_original_session_id"].(string); ok {
			cloneOf = v
		}
	}
	return upsertIndexRow(dir, ocodeMeta{
		ID:        s.ID,
		Title:     s.Title,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		CloneOf:   cloneOf,
	})
}

// LooksLikeAutoTitleCandidate reports whether trimmed user-message content is
// suitable prose to seed a fallback session title. Slash commands and raw
// JSON (e.g. a tool-call payload posted directly as a headless prompt, such
// as `ocode run '{"command":"..."}'`) are excluded — echoing either verbatim
// produces a garbled tab title instead of no title at all.
func LooksLikeAutoTitleCandidate(t string) bool {
	if t == "" || strings.HasPrefix(t, "/") {
		return false
	}
	return !strings.HasPrefix(t, "{") && !strings.HasPrefix(t, "[")
}

// autoTitleFromMessages derives a fallback session title from the first user
// message that passes LooksLikeAutoTitleCandidate. Returns "" when no
// suitable candidate exists, in which case the caller keeps whatever title
// (possibly none) it already had.
func autoTitleFromMessages(messages []agent.Message) string {
	for _, m := range messages {
		t := strings.TrimSpace(m.Content)
		if m.Role != "user" || !LooksLikeAutoTitleCandidate(t) {
			continue
		}
		if len(t) > 40 {
			t = t[:37] + "..."
		}
		return t
	}
	return ""
}

// saveJSON is the legacy whole-file-rewrite path, kept for sessions that
// were originally created as .json. Once a session is .json, it stays .json.
func saveJSON(dir, path, id, title string, messages []agent.Message, metadata map[string]any) error {
	var s Session
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("session file %s is corrupt: %w", path, err)
		}
	} else {
		s.ID = id
		s.CreatedAt = time.Now()
	}

	if title != "" {
		s.Title = title
		s.TitleGenerated = true
	} else if s.Title == "" && len(messages) > 0 {
		if t := autoTitleFromMessages(messages); t != "" {
			title = t
			s.Title = t
		}
	}

	s.Messages = messages
	if metadata != nil {
		s.Metadata = metadata
	}
	s.UpdatedAt = time.Now()

	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, out, 0644); err != nil {
		return err
	}

	return updateIndex(dir, id, s.Title)
}

// saveOjsonl is the append-only save path used for all new sessions, and
// for any id whose file already exists as .ojsonl.
func saveOjsonl(dir, id, title string, messages []agent.Message, metadata map[string]any) error {
	path := ojsonlSessionPath(dir, id)

	state, existed, err := getOjsonlWriteState(path)
	if err != nil {
		return err
	}

	newTitle := title
	titleGenerated := false
	if title != "" {
		titleGenerated = true
	} else if !existed && len(messages) > 0 {
		newTitle = autoTitleFromMessages(messages)
	} else {
		newTitle = "" // no title change this save; appendOjsonlSession keeps the cached one
	}

	resolvedTitle := newTitle
	if resolvedTitle == "" {
		resolvedTitle = state.title
	}

	if existed && state.count > len(messages) {
		// Message count shrank (e.g. /compact spliced out old messages).
		// The append-only path can't represent this, so rewrite the whole
		// file instead of erroring out and silently leaving stale content
		// on disk (that stale content is what a later resume would load).
		rewriteTitle := resolvedTitle
		rewriteTitleGenerated := titleGenerated || state.titleGenerated
		if err := rewriteOjsonlFull(path, id, state.createdAt, messages, metadata, rewriteTitle, rewriteTitleGenerated); err != nil {
			return err
		}
		return updateIndex(dir, id, rewriteTitle)
	}

	newMessages := messages
	if existed {
		newMessages = messages[state.count:]
	}

	if err := appendOjsonlSession(path, id, time.Now(), newMessages, metadata, newTitle, titleGenerated); err != nil {
		return err
	}

	return updateIndex(dir, id, resolvedTitle)
}

func updateIndex(dir, id, title string) error {
	indexPath := filepath.Join(dir, "index.json")
	var idx sessionIndex
	data, err := os.ReadFile(indexPath)
	if err == nil {
		// Best-effort: ignore corrupt index (it will be rebuilt over time).
		json.Unmarshal(data, &idx) //nolint:errcheck
	}
	if idx.Sessions == nil {
		idx.Sessions = make(map[string]string)
	}
	idx.LastSessionID = id
	idx.Sessions[id] = title

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session index: %w", err)
	}
	return os.WriteFile(indexPath, out, 0644)
}

func Load(id string) (*Session, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}
	return loadFromDir(dir, id)
}

// LoadForDir loads a session from the storage associated with wd. It is used
// by multi-project servers; Load continues to use the process/session workdir.
func LoadForDir(wd, id string) (*Session, error) {
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		return nil, err
	}
	return loadFromDir(dir, id)
}

// loadFromDir is the shared load core for Load/LoadForDir: try .sqlite
// first (authoritative once a session has migrated — see saveToDir), then
// .ojsonl, then fall back to the legacy .json candidates exactly as
// before.
func loadFromDir(dir, id string) (*Session, error) {
	for _, candidate := range sessionCandidateIDs(id) {
		sqlitePath := sqliteSessionPath(dir, candidate)
		if !fileExists(sqlitePath) {
			continue
		}
		s, err := readSqliteSession(sqlitePath)
		if err != nil {
			return nil, err
		}
		s.Messages = removeIncompleteToolRequests(s.Messages)
		cleanupMigrationOrphans(dir, candidate)
		return s, nil
	}

	for _, candidate := range sessionCandidateIDs(id) {
		ojsonlPath := ojsonlSessionPath(dir, candidate)
		if !fileExists(ojsonlPath) {
			continue
		}
		s, err := loadOjsonlSession(ojsonlPath)
		if err != nil {
			return nil, err
		}
		s.Messages = removeIncompleteToolRequests(s.Messages)
		return s, nil
	}

	path, data, err := readSessionFile(dir, id)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session file %s is corrupt: %w", path, err)
	}
	s.Messages = removeIncompleteToolRequests(s.Messages)
	return &s, nil
}

// cleanupMigrationOrphans removes a leftover .json/.ojsonl file for a
// session that already has a valid .sqlite file. This can only happen if
// migrateToSqlite's process was killed after the new file was written and
// verified but before the old one was removed (see saveToDir). Best-effort:
// a failure here just means the orphan lingers — harmless, since .sqlite
// always wins on Load — until the next successful Load of the same id.
func cleanupMigrationOrphans(dir, id string) {
	jsonPath := filepath.Join(dir, id+".json")
	if fileExists(jsonPath) {
		if err := os.Remove(jsonPath); err != nil {
			log.Printf("session: cleanup orphan %s: %v", jsonPath, err)
		}
	}
	ojsonlPath := ojsonlSessionPath(dir, id)
	if fileExists(ojsonlPath) {
		if err := os.Remove(ojsonlPath); err != nil {
			log.Printf("session: cleanup orphan %s: %v", ojsonlPath, err)
		}
		clearOjsonlWriteState(ojsonlPath)
	}
}

// fileExists reports whether path exists and is readable as a regular
// file entry (any stat error, including permission errors, is treated as
// "does not exist" for dispatch purposes — Load's fallback paths below
// will surface a clearer error if it turns out to be a real problem).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sessionCandidateIDs(id string) []string {
	if strings.HasPrefix(id, canonicalSessionPrefix) {
		return []string{id, strings.TrimPrefix(id, canonicalSessionPrefix)}
	}
	return []string{id, canonicalSessionPrefix + id}
}

func readSessionFile(dir, id string) (string, []byte, error) {
	paths := sessionLoadPaths(dir, id)
	var firstErr error
	for i, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			if i > 0 {
				log.Printf("session: fallback load for %q via %s", id, path)
			}
			return path, data, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		if !os.IsNotExist(err) {
			return "", nil, err
		}
	}
	return "", nil, firstErr
}

func sessionLoadPaths(dir, id string) []string {
	ids := sessionCandidateIDs(id)
	paths := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, candidate := range ids {
		if candidate == "" {
			continue
		}
		path := filepath.Join(dir, candidate+".json")
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func removeIncompleteToolRequests(messages []agent.Message) []agent.Message {
	completedToolIDs := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role == "tool" {
			if msg.ToolID == "" {
				log.Printf("session: tool message with empty ToolID (content: %.80q) — treating as incomplete", msg.Content)
				continue
			}
			if !isIncompleteToolResult(msg.Content) {
				completedToolIDs[msg.ToolID] = struct{}{}
			}
		}
	}

	out := make([]agent.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "tool" && (msg.ToolID == "" || isIncompleteToolResult(msg.Content)) {
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			completedCalls := make([]agent.ToolCall, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				if _, ok := completedToolIDs[call.ID]; ok {
					completedCalls = append(completedCalls, call)
				}
			}
			msg.ToolCalls = completedCalls
			if msg.Content == "" && msg.ReasoningContent == "" && len(msg.ToolCalls) == 0 {
				continue
			}
		}
		out = append(out, msg)
	}
	return out
}

func isIncompleteToolResult(content string) bool {
	return strings.Contains(content, tool.SentinelWaitingForUser) || strings.HasPrefix(content, tool.SentinelPermissionAsk)
}

// legacyOcodeMetas scans dir for un-migrated .json and .ojsonl session
// files and returns their metadata. Shared by ListRefsForDir and
// ListRefsPaginated, which previously duplicated this scan verbatim.
func legacyOcodeMetas(dir string, entries []os.DirEntry) []ocodeMeta {
	metas := mapDirEntries(dir, entries, ".json", func(path string, e os.DirEntry) (ocodeMeta, bool) {
		info, err := e.Info()
		if err != nil {
			log.Printf("session list: stat %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		meta, err := readOcodeMeta(path, info.ModTime())
		if err != nil {
			log.Printf("session list: read meta %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		return meta, true
	})
	ojsonlMetas := mapDirEntries(dir, entries, ".ojsonl", func(path string, e os.DirEntry) (ocodeMeta, bool) {
		info, err := e.Info()
		if err != nil {
			log.Printf("session list: stat %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		meta, err := readOjsonlListMeta(path, info.ModTime())
		if err != nil {
			log.Printf("session list: read ojsonl meta %s: %v", e.Name(), err)
			return ocodeMeta{}, false
		}
		return meta, true
	})
	return append(metas, ojsonlMetas...)
}

func List() ([]Session, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var sessions []Session
	for _, e := range entries {
		if e.IsDir() || e.Name() == "index.json" || e.Name() == "index.sqlite" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		ext := filepath.Ext(e.Name())
		id := strings.TrimSuffix(e.Name(), ext)
		switch ext {
		case ".sqlite":
			s, err := readSqliteSession(path)
			if err == nil {
				s.Messages = removeIncompleteToolRequests(s.Messages)
				sessions = append(sessions, *s)
			}
		case ".json":
			// A migrated session's .sqlite is authoritative; a .json left
			// behind by the narrow migrateToSqlite crash window (see Task
			// 4/5) would otherwise show up twice.
			if fileExists(sqliteSessionPath(dir, id)) {
				continue
			}
			data, err := os.ReadFile(path)
			if err == nil {
				var s Session
				if err := json.Unmarshal(data, &s); err == nil {
					s.Messages = removeIncompleteToolRequests(s.Messages)
					sessions = append(sessions, s)
				}
			}
		case ".ojsonl":
			if fileExists(sqliteSessionPath(dir, id)) {
				continue
			}
			s, err := loadOjsonlSession(path)
			if err == nil {
				s.Messages = removeIncompleteToolRequests(s.Messages)
				sessions = append(sessions, *s)
			}
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// ListRefsForDir returns lightweight refs (id/title/timestamps) for sessions
// scoped to a specific working directory, without materializing the messages
// array. Mirrors the metadata-only approach in ListRefsPaginated — use this
// instead of ListForDir for anything that only needs list display fields.
func ListRefsForDir(wd string) ([]Ref, error) {
	dir, err := GetStorageDirForPath(wd)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	metas := legacyOcodeMetas(dir, entries)
	indexMetas, err := queryIndexMetas(dir)
	if err != nil {
		log.Printf("session list: query index: %v", err)
		indexMetas = nil
	}
	metas = mergeMetas(metas, indexMetas)

	refs := make([]Ref, 0, len(metas))
	for _, meta := range metas {
		refs = append(refs, Ref{
			ID:        meta.ID,
			Title:     meta.Title,
			CreatedAt: meta.CreatedAt,
			UpdatedAt: meta.UpdatedAt,
			Source:    SourceOcode,
		})
	}

	sort.Slice(refs, func(i, j int) bool {
		return refs[i].UpdatedAt.After(refs[j].UpdatedAt)
	})

	return refs, nil
}

func ListAll() ([]Ref, error) {
	ocodeSessions, err := List()
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(ocodeSessions))
	clonedClaude := make(map[string]struct{})
	for _, s := range ocodeSessions {
		refs = append(refs, Ref{ID: s.ID, Title: s.Title, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Source: SourceOcode})
		if s.Metadata != nil {
			if originalID, ok := s.Metadata["claude_original_session_id"].(string); ok && originalID != "" {
				clonedClaude[originalID] = struct{}{}
			}
		}
	}

	claudeRefs, err := listClaudeSessions()
	if err != nil {
		return nil, err
	}
	for _, ref := range claudeRefs {
		if _, ok := clonedClaude[strings.TrimPrefix(ref.ID, "claude:")]; ok {
			continue
		}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].UpdatedAt.After(refs[j].UpdatedAt)
	})
	return refs, nil
}

// ListRefs returns all session refs sorted by updated time (newest first).
// For paginated access, use ListRefsPaginated.
func ListRefs() ([]Ref, error) {
	refs, _, err := ListRefsPaginated(0, 0)
	return refs, err
}

// listWorkers bounds concurrency when reading session files for the list.
// File reads dominate listing cost; fanning them across cores turns a
// sequential walk of the whole session dir into a parallel one.
func listWorkers() int {
	n := runtime.NumCPU()
	if n > 12 {
		n = 12
	}
	if n < 1 {
		n = 1
	}
	return n
}

// mapDirEntries runs fn over each .json/.jsonl entry concurrently (bounded by
// listWorkers) and returns the successful results. Order is not preserved;
// callers sort afterwards. fn returns ok=false to drop an entry.
func mapDirEntries[T any](dir string, entries []os.DirEntry, ext string, fn func(string, os.DirEntry) (T, bool)) []T {
	sem := make(chan struct{}, listWorkers())
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out []T
	)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ext || e.Name() == "index.json" {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(e os.DirEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			v, ok := fn(filepath.Join(dir, e.Name()), e)
			if !ok {
				return
			}
			mu.Lock()
			out = append(out, v)
			mu.Unlock()
		}(e)
	}
	wg.Wait()
	return out
}

// ocodeMeta holds the cheap-to-extract fields needed to list a session,
// decoded without materializing the (potentially multi-MB) messages array.
type ocodeMeta struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	CloneOf   string // metadata.claude_original_session_id; "" when not a clone
}

// readOcodeMeta streams a session file token-by-token, capturing only the list
// fields. The messages array is consumed as raw bytes and discarded, so we skip
// the dominant cost of unmarshalling thousands of agent.Message structs per
// file. It stays correct regardless of on-disk key order (older files store
// messages mid-object, newer ones may not), so dedup of cloned Claude sessions
// remains exact. modTime is the fallback for updated_at when the field is absent.
// skipJSONValue consumes exactly one JSON value from dec (whatever the
// decoder is positioned at next) without retaining its bytes, unlike decoding
// into json.RawMessage which copies the entire value into one allocation.
// For a large array/object this walks its tokens one at a time so no
// single allocation grows past a few bytes.
func skipJSONValue(dec *json.Decoder) error {
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
		if depth == 0 {
			return nil
		}
	}
}

func readOcodeMeta(path string, modTime time.Time) (ocodeMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return ocodeMeta{}, err
	}
	defer f.Close()

	dec := json.NewDecoder(bufio.NewReader(f))
	// Consume the opening '{'.
	if _, err := dec.Token(); err != nil {
		return ocodeMeta{}, err
	}

	var meta ocodeMeta
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return ocodeMeta{}, err
		}
		key, _ := keyTok.(string)
		switch key {
		case "id":
			if err := dec.Decode(&meta.ID); err != nil {
				return ocodeMeta{}, err
			}
		case "title":
			if err := dec.Decode(&meta.Title); err != nil {
				return ocodeMeta{}, err
			}
		case "updated_at":
			if err := dec.Decode(&meta.UpdatedAt); err != nil {
				return ocodeMeta{}, err
			}
		case "created_at":
			if err := dec.Decode(&meta.CreatedAt); err != nil {
				return ocodeMeta{}, err
			}
		case "metadata":
			var m map[string]any
			if err := dec.Decode(&m); err != nil {
				return ocodeMeta{}, err
			}
			if v, ok := m["claude_original_session_id"].(string); ok {
				meta.CloneOf = v
			}
		default:
			// Skip any other value (notably the heavy "messages" array) by
			// walking its tokens without retaining them — decoding into
			// json.RawMessage still materializes the entire skipped value as
			// one contiguous byte slice (confirmed via heap profile: it was
			// the dominant allocation source when listing sessions with large
			// legacy .json files). Must consume exactly one value here or the
			// decoder desyncs for every subsequent key.
			if err := skipJSONValue(dec); err != nil {
				return ocodeMeta{}, err
			}
		}
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = modTime
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = meta.UpdatedAt
	}
	return meta, nil
}

// ListRefsPaginated returns a page of session refs with optional limit and offset.
// If limit <= 0, returns all refs. Returns (refs, totalCount, error).
func ListRefsPaginated(limit, offset int) ([]Ref, int, error) {
	dir, err := GetStorageDir()
	if err != nil {
		return nil, 0, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}

	metas := legacyOcodeMetas(dir, entries)
	indexMetas, err := queryIndexMetas(dir)
	if err != nil {
		log.Printf("session list: query index: %v", err)
		indexMetas = nil
	}
	metas = mergeMetas(metas, indexMetas)

	allRefs := make([]Ref, 0, len(metas))
	clonedClaude := make(map[string]struct{})
	for _, meta := range metas {
		allRefs = append(allRefs, Ref{
			ID:        meta.ID,
			Title:     meta.Title,
			CreatedAt: meta.CreatedAt,
			UpdatedAt: meta.UpdatedAt,
			Source:    SourceOcode,
		})
		if meta.CloneOf != "" {
			clonedClaude[meta.CloneOf] = struct{}{}
		}
	}

	sort.Slice(allRefs, func(i, j int) bool {
		return allRefs[i].UpdatedAt.After(allRefs[j].UpdatedAt)
	})

	claudeRefs, err := listClaudeSessions()
	if err == nil {
		for _, ref := range claudeRefs {
			if _, ok := clonedClaude[strings.TrimPrefix(ref.ID, "claude:")]; ok {
				continue
			}
			allRefs = append(allRefs, ref)
		}
		sort.Slice(allRefs, func(i, j int) bool {
			return allRefs[i].UpdatedAt.After(allRefs[j].UpdatedAt)
		})
	}

	total := len(allRefs)

	// Apply pagination
	if limit > 0 {
		start := offset
		if start > total {
			start = total
		}
		end := start + limit
		if end > total {
			end = total
		}
		allRefs = allRefs[start:end]
	}

	return allRefs, total, nil
}

// Delete removes a session file and updates the index — whichever
// on-disk format the session id currently exists in.
func Delete(id string) error {
	dir, err := GetStorageDir()
	if err != nil {
		return err
	}

	for _, p := range []string{
		sqliteSessionPath(dir, id),
		filepath.Join(dir, id+".json"),
		ojsonlSessionPath(dir, id),
	} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	clearOjsonlWriteState(ojsonlSessionPath(dir, id))

	if err := deleteIndexRow(dir, id); err != nil {
		log.Printf("session: delete index row for %s: %v", id, err)
	}

	// Update legacy index.json (write-only today, unused for reads — see
	// this plan's INDEX.md Global Constraints; kept as-is).
	indexPath := filepath.Join(dir, "index.json")
	var idx sessionIndex
	data, err := os.ReadFile(indexPath)
	if err == nil {
		json.Unmarshal(data, &idx) //nolint:errcheck
	}
	if idx.Sessions == nil {
		idx.Sessions = make(map[string]string)
	}
	delete(idx.Sessions, id)

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session index: %w", err)
	}
	return os.WriteFile(indexPath, out, 0644)
}

func LoadAny(id string) (*Session, error) {
	if strings.HasPrefix(id, "claude:") {
		return CloneClaudeSession(strings.TrimPrefix(id, "claude:"))
	}
	return Load(id)
}

func CloneClaudeSession(id string) (*Session, error) {
	cloneID := "claude-" + id
	if s, err := Load(cloneID); err == nil {
		return s, nil
	}

	claudeSession, err := loadClaudeSession(id)
	if err != nil {
		return nil, err
	}
	claudeSession.ID = cloneID
	if claudeSession.Metadata == nil {
		claudeSession.Metadata = make(map[string]any)
	}
	claudeSession.Metadata["source"] = string(SourceClaude)
	claudeSession.Metadata["claude_original_session_id"] = id
	if err := Save(cloneID, claudeSession.Title, claudeSession.Messages, claudeSession.Metadata); err != nil {
		return nil, err
	}
	return Load(cloneID)
}

func listClaudeSessions() ([]Ref, error) {
	dir, err := getClaudeProjectDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	refs := mapDirEntries(dir, entries, ".jsonl", func(path string, e os.DirEntry) (Ref, bool) {
		info, err := e.Info()
		if err != nil {
			log.Printf("session list: stat claude %s: %v", e.Name(), err)
			return Ref{}, false
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		return claudeRefQuick(id, path, info.ModTime()), true
	})
	return refs, nil
}

// claudeRefQuick builds a list ref for a Claude transcript without parsing the
// whole .jsonl. It reads only until the first user message (for the title) and
// uses the file mtime for updated_at — the last append is the last activity, so
// mtime is an accurate sort key. Full transcripts (up to multi-MB) are only
// parsed when a session is actually opened (loadClaudeSession).
func claudeRefQuick(id, path string, modTime time.Time) Ref {
	title := ""
	f, err := os.Open(path)
	if err != nil {
		log.Printf("session list: open claude %s: %v", id, err)
	} else {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			var entry struct {
				Type    string          `json:"type"`
				IsMeta  bool            `json:"isMeta"`
				Message json.RawMessage `json:"message"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
				continue // skip malformed line; next line may still yield a title
			}
			if entry.IsMeta || entry.Type != "user" {
				continue
			}
			role, content, _, ok := claudeMessage(entry.Message)
			if ok && role == "user" && content != "" {
				title = titleFromContent(content)
				break
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("session list: scan claude %s: %v", id, err)
		}
	}
	if title == "" {
		title = id
	}
	return Ref{ID: "claude:" + id, Title: title, CreatedAt: modTime, UpdatedAt: modTime, Source: SourceClaude}
}

func getClaudeProjectDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	wd := paths.ProjectRoot(effectiveWorkDir())
	return filepath.Join(home, ".claude", "projects", claudeProjectSlug(wd)), nil
}

func claudeProjectSlug(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.ReplaceAll(clean, "/", "-")
}

func loadClaudeSession(id string) (*Session, error) {
	dir, err := getClaudeProjectDir()
	if err != nil {
		return nil, err
	}
	return parseClaudeSessionFile(id, filepath.Join(dir, id+".jsonl"))
}

func parseClaudeSessionFile(id, path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &Session{ID: id, Metadata: map[string]any{"source": string(SourceClaude), "claude_path": path}}
	if err := parseClaudeJSONL(f, s); err != nil {
		return nil, err
	}
	if s.Title == "" {
		s.Title = id
	}
	if s.CreatedAt.IsZero() {
		if info, err := os.Stat(path); err == nil {
			s.CreatedAt = info.ModTime()
			s.UpdatedAt = info.ModTime()
		}
	}
	return s, nil
}

func parseClaudeJSONL(r io.Reader, s *Session) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var entry struct {
			Type      string          `json:"type"`
			IsMeta    bool            `json:"isMeta"`
			Timestamp string          `json:"timestamp"`
			Message   json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return err
		}
		if entry.IsMeta || (entry.Type != "user" && entry.Type != "assistant") {
			continue
		}
		role, content, model, ok := claudeMessage(entry.Message)
		if !ok || content == "" {
			continue
		}
		parsedTime, hasTime := parseClaudeTime(entry.Timestamp)
		if hasTime {
			if s.CreatedAt.IsZero() || parsedTime.Before(s.CreatedAt) {
				s.CreatedAt = parsedTime
			}
			if parsedTime.After(s.UpdatedAt) {
				s.UpdatedAt = parsedTime
			}
		}
		if s.Title == "" && role == "user" && !strings.HasPrefix(strings.TrimSpace(content), "/") {
			s.Title = titleFromContent(content)
		}
		s.Messages = append(s.Messages, agent.Message{Role: role, Content: content, Model: model})
	}
	return scanner.Err()
}

func parseClaudeTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t, err == nil
}

func titleFromContent(content string) string {
	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "\n", " ")
	if len(content) > 40 {
		return content[:37] + "..."
	}
	return content
}

func claudeMessage(raw json.RawMessage) (role, content, model string, ok bool) {
	var msg struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Model   string          `json:"model"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Role == "" {
		return "", "", "", false
	}
	content = claudeContentText(msg.Content)
	return msg.Role, content, msg.Model, content != ""
}

func claudeContentText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			out = append(out, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(out, "\n\n")
}

// UpdateMetadataForDir applies mutate to the stored metadata of session id
// under projectRoot's storage dir and persists only that: for a .sqlite
// session it rewrites the meta row's metadata_json in one immediate
// transaction and never reads or rewrites message rows, so — like
// AppendUserMessageForDir — it cannot trip the overlap/shrink conflict when
// the stored transcript holds rows the load path filters out (orphan tool
// results, PERMISSION_ASK sentinels). A full load→modify→save of a filtered
// view conflicts forever in that state, which is what blocked per-session
// model picks from the web sidebar. Legacy .json/.ojsonl sessions are
// migrated through the normal save path with the RAW transcript.
func UpdateMetadataForDir(projectRoot, id string, mutate func(map[string]any)) error {
	dir, err := GetStorageDirForPath(projectRoot)
	if err != nil {
		return err
	}
	if fileExists(sqliteSessionPath(dir, id)) {
		if err := updateSqliteMetadata(dir, id, mutate); err != nil {
			return err
		}
		return refreshIndexMeta(dir, id)
	}
	s, err := loadFromDir(dir, id)
	if err != nil {
		return err
	}
	msgs, err := loadRawMessages(dir, id)
	if err != nil {
		return err
	}
	if s.Metadata == nil {
		s.Metadata = map[string]any{}
	}
	mutate(s.Metadata)
	return persistToDir(dir, id, s.Title, msgs, s.Metadata, false, 0, false)
}
