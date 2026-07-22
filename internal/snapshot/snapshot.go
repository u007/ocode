package snapshot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Snapshot records one file's state before a write-tool modified it.
type Snapshot struct {
	OriginalPath string
	BackupPath   string // empty = file was new; undo = delete
	Timestamp    time.Time
	ToolCallID   string // LLM tool call that triggered this backup; "" for non-agent callers
	AgentStep    int    // agent loop iteration at backup time
	WriteSeq     uint64 // monotonic sequence number assigned by RegisterWrite after the write
	BaseDir      string // snapshot store base dir at backup time (provenance for undo/redo)
}

// Store is a per-agent snapshot store. Each agent creates one via NewStore.
// It is safe for concurrent use.
type Store struct {
	mu        sync.Mutex
	snapshots []Snapshot
	redoStack []Snapshot
	step      int
	agentID   string
	baseDir   string // where backup files are written; empty => legacy ".opencode/snapshots"
}

// NewStore creates a Store for one agent. agentID must be unique across all
// concurrent agents in the process (use NewAgentID).
func NewStore(agentID, baseDir string) *Store {
	return &Store{agentID: agentID, baseDir: baseDir}
}

// SetBaseDir points the store at the directory where backup files are written.
// An empty dir falls back to the legacy relative ".opencode/snapshots" path.
func (s *Store) SetBaseDir(dir string) {
	s.mu.Lock()
	s.baseDir = dir
	s.mu.Unlock()
}

// NewAgentID returns a random hex string suitable as a unique Store identifier.
func NewAgentID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// AdvanceStep increments the per-agent loop counter. Call at the top of each
// LLM iteration inside the agent Step loop so expiry uses agent steps, not time.
func (s *Store) AdvanceStep() {
	s.mu.Lock()
	s.step++
	s.mu.Unlock()
}

// -------- context keys --------

type storeKey struct{}
type toolCallIDKey struct{}

// WithStore returns a context carrying s.
func WithStore(ctx context.Context, s *Store) context.Context {
	return context.WithValue(ctx, storeKey{}, s)
}

// WithToolCallID returns a context carrying id.
func WithToolCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, toolCallIDKey{}, id)
}

// FromContext returns the Store in ctx, falling back to the global store for
// non-agent callers (e.g. ocodeconfig.go). Never returns nil.
func FromContext(ctx context.Context) *Store {
	if s, ok := ctx.Value(storeKey{}).(*Store); ok && s != nil {
		return s
	}
	return globalStore
}

// ToolCallIDFromContext returns the tool call ID stored in ctx, or "".
func ToolCallIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(toolCallIDKey{}).(string)
	return id
}

// -------- global cross-agent write registry --------

var (
	globalWriteSeq atomic.Uint64
	fileWriteMu    sync.RWMutex
	fileWrites     = make(map[string][]fileWrite) // path → ordered writes across all agents
)

type fileWrite struct {
	AgentID    string
	Seq        uint64
	ToolCallID string
}

// UnregisterAgent removes all registry entries owned by agentID. Call when an
// agent's session ends so stale entries do not permanently block other agents.
func UnregisterAgent(agentID string) {
	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()
	for path, writes := range fileWrites {
		kept := writes[:0]
		for _, w := range writes {
			if w.AgentID != agentID {
				kept = append(kept, w)
			}
		}
		if len(kept) == 0 {
			delete(fileWrites, path)
		} else {
			fileWrites[path] = kept
		}
	}
}

// crossAgentWriteAfterSeq returns the first write to path by any agent other
// than myAgentID whose Seq is strictly greater than afterSeq. Uses a monotonic
// sequence counter (not wall-clock time) so concurrent goroutine ordering is
// well-defined.
func crossAgentWriteAfterSeq(path, myAgentID string, afterSeq uint64) *fileWrite {
	fileWriteMu.RLock()
	defer fileWriteMu.RUnlock()
	for i := range fileWrites[path] {
		w := &fileWrites[path][i]
		if w.AgentID != myAgentID && w.Seq > afterSeq {
			return w
		}
	}
	return nil
}

// -------- global store (backward compat for TUI undo/redo and ocodeconfig) --------

var globalStore = NewStore("global", "")

// Package-level functions delegate to the global store so existing call sites
// (TUI undo/redo, config backup) continue to work unchanged.
func Backup(path string) error      { return globalStore.Backup(path, "") }
func ChangedFiles() []string        { return globalStore.ChangedFiles() }
func Reset()                        { globalStore.Reset() }
func Undo() (string, error)         { return globalStore.Undo() }
func Redo() (string, error)         { return globalStore.Redo() }
func DiscardRecent(count int) error { return globalStore.DiscardRecent(count) }
func Restore(path string) error     { return globalStore.Restore(path) }

// -------- Store methods --------

// Backup saves a copy of path before a write. toolCallID may be "" for
// non-agent callers. The backup content is written to disk (.opencode/snapshots/)
// so large files are safe — only metadata lives in RAM.
func (s *Store) Backup(path, toolCallID string) error {
	s.mu.Lock()
	baseDir := s.baseDir
	s.mu.Unlock()
	return s.backupAtDir(path, toolCallID, baseDir)
}

// backupAtDir is the implementation behind Backup. Passing an explicit baseDir
// lets callers such as Redo keep the snapshot history pinned to the project
// that originally owned the file, even if the store's active base dir changes.
func (s *Store) backupAtDir(path, toolCallID, baseDir string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	newFile := os.IsNotExist(err)

	var backupPath string
	dir := baseDir
	if dir == "" {
		dir = filepath.Join(".opencode", "snapshots") // legacy fallback
	}
	if !newFile {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		// Include agentID so concurrent agents editing same-basename files in
		// the same project at the same nanosecond cannot collide on disk.
		backupName := fmt.Sprintf("%d_%s_%s", time.Now().UnixNano(), s.agentID, filepath.Base(path))
		backupPath = filepath.Join(dir, backupName)
		if err := os.WriteFile(backupPath, data, 0644); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.snapshots = append(s.snapshots, Snapshot{
		OriginalPath: path,
		BackupPath:   backupPath,
		BaseDir:      dir,
		Timestamp:    time.Now(),
		ToolCallID:   toolCallID,
		AgentStep:    s.step,
	})
	s.mu.Unlock()
	return nil
}

// RegisterWrite records a successful file write in the global cross-agent
// registry and back-fills the WriteSeq on the matching snapshot. Must be
// called after every successful write that was preceded by Backup.
func (s *Store) RegisterWrite(path, toolCallID string) {
	seq := globalWriteSeq.Add(1)

	fileWriteMu.Lock()
	fileWrites[path] = append(fileWrites[path], fileWrite{
		AgentID:    s.agentID,
		Seq:        seq,
		ToolCallID: toolCallID,
	})
	fileWriteMu.Unlock()

	// Back-fill the WriteSeq on the most recent snapshot for this path + toolCallID
	// so UndoByToolCallID can compare against other agents' write seqs.
	if toolCallID == "" {
		return
	}
	s.mu.Lock()
	for i := len(s.snapshots) - 1; i >= 0; i-- {
		snap := &s.snapshots[i]
		if snap.OriginalPath == path && snap.ToolCallID == toolCallID && snap.WriteSeq == 0 {
			snap.WriteSeq = seq
			break
		}
	}
	s.mu.Unlock()
}

// UndoByToolCallID restores all files changed by toolCallID to their pre-edit
// state. maxAgeDelta is the maximum number of agent step increments since the
// backup was taken. Returns the list of restored file paths.
//
// Refuses if:
//   - the snapshots are older than maxAgeDelta agent steps (expired)
//   - another agent wrote any affected file after this agent's write (cross-agent conflict)
//   - this agent itself made a newer active write to the same file (same-agent conflict,
//     only blocked when that newer write is also still within the undo window)
func (s *Store) UndoByToolCallID(toolCallID string, maxAgeDelta int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Collect all snapshot indices for this toolCallID.
	var indices []int
	for i, snap := range s.snapshots {
		if snap.ToolCallID == toolCallID {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return nil, fmt.Errorf("no snapshot found for tool_call_id %q", toolCallID)
	}

	// Expiry: measured from the oldest snapshot for this call.
	firstSnap := s.snapshots[indices[0]]
	age := s.step - firstSnap.AgentStep
	if age > maxAgeDelta {
		return nil, fmt.Errorf("undo expired: %d agent steps old (max %d)", age, maxAgeDelta)
	}

	// Per-file conflict checks.
	for _, idx := range indices {
		snap := s.snapshots[idx]

		// Cross-agent: did another agent write this file after our write?
		if snap.WriteSeq > 0 {
			if conflict := crossAgentWriteAfterSeq(snap.OriginalPath, s.agentID, snap.WriteSeq); conflict != nil {
				return nil, fmt.Errorf("cannot undo %s: modified by another agent (tool_call_id=%s) after this change",
					snap.OriginalPath, conflict.ToolCallID)
			}
		}

		// Same-agent: is there a newer snapshot for the same file that is
		// still within the undo window? If so, refuse — the newer change
		// depends on this one.
		for i := idx + 1; i < len(s.snapshots); i++ {
			other := s.snapshots[i]
			if other.OriginalPath != snap.OriginalPath || other.ToolCallID == toolCallID {
				continue
			}
			if s.step-other.AgentStep <= maxAgeDelta {
				return nil, fmt.Errorf("cannot undo %s: a newer active change (tool_call_id=%s) still exists",
					snap.OriginalPath, other.ToolCallID)
			}
		}
	}

	// Out-of-order removal invalidates the redo chain — clear it explicitly.
	s.clearRedoLocked()

	// Restore files and remove snapshots in reverse index order so earlier
	// indices stay valid as we shrink the slice.
	var restored []string
	for i := len(indices) - 1; i >= 0; i-- {
		idx := indices[i]
		snap := s.snapshots[idx]
		if err := restoreSnapshot(snap); err != nil {
			return restored, fmt.Errorf("restore %s: %w", snap.OriginalPath, err)
		}
		restored = append([]string{snap.OriginalPath}, restored...) // prepend to keep natural order
		s.snapshots = append(s.snapshots[:idx], s.snapshots[idx+1:]...)
	}
	return restored, nil
}

func (s *Store) clearRedoLocked() {
	for _, snap := range s.redoStack {
		if snap.BackupPath != "" {
			os.Remove(snap.BackupPath) //nolint:errcheck
		}
	}
	s.redoStack = nil
}

func restoreSnapshot(snap Snapshot) error {
	if snap.BackupPath == "" {
		// File was newly created by the write — undo = delete.
		if err := os.Remove(snap.OriginalPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := os.ReadFile(snap.BackupPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(snap.OriginalPath, data, 0644); err != nil {
		return err
	}
	os.Remove(snap.BackupPath) //nolint:errcheck
	return nil
}

// Len returns the number of snapshots currently stored. Unlike Snapshots,
// it does not copy the slice, so it is cheap to call as a change-detection
// signal (e.g. the changes registry uses it to decide whether its cached
// file list is stale without re-walking every snapshot).
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.snapshots)
}

// Snapshots returns a copy of this store's snapshot slice in chronological
// order. Used by the changes package (and any other read-only consumer that
// needs the full per-write metadata: timestamp, tool call id, backup path).
// Returning a copy keeps the slice header from racing with concurrent
// appends and matches the rest of this package's read API.
func (s *Store) Snapshots() []Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.snapshots) == 0 {
		return nil
	}
	out := make([]Snapshot, len(s.snapshots))
	copy(out, s.snapshots)
	return out
}

// ChangedFiles returns a deduplicated sorted list of all backed-up file paths.
func (s *Store) ChangedFiles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]struct{})
	files := make([]string, 0, len(s.snapshots))
	for _, snap := range s.snapshots {
		if _, ok := seen[snap.OriginalPath]; ok {
			continue
		}
		seen[snap.OriginalPath] = struct{}{}
		files = append(files, snap.OriginalPath)
	}
	sort.Strings(files)
	return files
}

// Reset clears all snapshot and redo state and removes this agent's entries
// from the global file-write registry.
func (s *Store) Reset() {
	s.mu.Lock()
	s.snapshots = nil
	s.clearRedoLocked()
	s.step = 0
	s.mu.Unlock()
	if s.agentID != "global" {
		UnregisterAgent(s.agentID)
	}
}

// Undo pops the most recent snapshot and restores the file. The current state
// is saved to the redo stack.
func (s *Store) Undo() (string, error) {
	s.mu.Lock()
	if len(s.snapshots) == 0 {
		s.mu.Unlock()
		return "", fmt.Errorf("no snapshots available to undo")
	}
	last := s.snapshots[len(s.snapshots)-1]
	s.snapshots = s.snapshots[:len(s.snapshots)-1]
	s.mu.Unlock()

	// Save current state for redo before restoring.
	redoDir := last.BaseDir
	if redoDir == "" {
		redoDir = filepath.Join(".opencode", "snapshots") // legacy fallback
	}
	redoBase := last.BackupPath
	if redoBase == "" {
		redoBase = filepath.Join(redoDir,
			fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(last.OriginalPath)))
	}
	redoBackupPath := redoBase + ".redo"
	currentData, err := os.ReadFile(last.OriginalPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read current file for redo backup %s: %w", last.OriginalPath, err)
	}
	if err := os.WriteFile(redoBackupPath, currentData, 0644); err != nil {
		return "", fmt.Errorf("failed to save redo backup for %s: %w", last.OriginalPath, err)
	}
	s.mu.Lock()
	s.redoStack = append(s.redoStack, Snapshot{
		OriginalPath: last.OriginalPath,
		BackupPath:   redoBackupPath,
		BaseDir:      redoDir,
		Timestamp:    time.Now(),
	})
	s.mu.Unlock()

	if last.BackupPath == "" {
		if err := os.Remove(last.OriginalPath); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to remove new file %s: %w", last.OriginalPath, err)
		}
		return last.OriginalPath, nil
	}

	data, err := os.ReadFile(last.BackupPath)
	if err != nil {
		return "", fmt.Errorf("failed to read backup file %s: %w", last.BackupPath, err)
	}
	if err := os.WriteFile(last.OriginalPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to restore file %s: %w", last.OriginalPath, err)
	}
	os.Remove(last.BackupPath) //nolint:errcheck
	return last.OriginalPath, nil
}

// Redo re-applies the most recently undone change.
func (s *Store) Redo() (string, error) {
	s.mu.Lock()
	if len(s.redoStack) == 0 {
		s.mu.Unlock()
		return "", fmt.Errorf("nothing to redo")
	}
	last := s.redoStack[len(s.redoStack)-1]
	s.redoStack = s.redoStack[:len(s.redoStack)-1]
	s.mu.Unlock()

	data, err := os.ReadFile(last.BackupPath)
	if err != nil {
		return "", fmt.Errorf("failed to read redo file %s: %w", last.BackupPath, err)
	}
	if err := s.backupAtDir(last.OriginalPath, "", last.BaseDir); err != nil {
		return "", fmt.Errorf("failed to backup before redo: %w", err)
	}
	if err := os.WriteFile(last.OriginalPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to restore file %s: %w", last.OriginalPath, err)
	}
	os.Remove(last.BackupPath) //nolint:errcheck
	return last.OriginalPath, nil
}

// DiscardRecent removes the most recent count snapshots and deletes their
// backup files. Used by PatchTool for atomic rollback.
func (s *Store) DiscardRecent(count int) error {
	if count < 0 {
		return fmt.Errorf("invalid discard count %d", count)
	}
	if count == 0 {
		return nil
	}
	s.mu.Lock()
	if count > len(s.snapshots) {
		s.mu.Unlock()
		return fmt.Errorf("cannot discard %d snapshots; only %d available", count, len(s.snapshots))
	}
	removed := append([]Snapshot(nil), s.snapshots[len(s.snapshots)-count:]...)
	s.snapshots = s.snapshots[:len(s.snapshots)-count]
	s.mu.Unlock()

	var firstErr error
	for _, snap := range removed {
		if snap.BackupPath == "" {
			continue
		}
		if err := os.Remove(snap.BackupPath); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = fmt.Errorf("failed to remove snapshot %s: %w", snap.BackupPath, err)
		}
	}
	return firstErr
}

// Restore reverts a specific file to its most recent backup state without
// modifying the snapshot list. Used for atomic rollback only (e.g. PatchTool).
func (s *Store) Restore(path string) error {
	s.mu.Lock()
	var last *Snapshot
	for i := len(s.snapshots) - 1; i >= 0; i-- {
		if s.snapshots[i].OriginalPath == path {
			snap := s.snapshots[i]
			last = &snap
			break
		}
	}
	s.mu.Unlock()

	if last == nil {
		return nil
	}
	if last.BackupPath == "" {
		return os.Remove(last.OriginalPath)
	}
	data, err := os.ReadFile(last.BackupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup for %s: %w", path, err)
	}
	return os.WriteFile(last.OriginalPath, data, 0644)
}
