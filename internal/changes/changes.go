// Package changes owns the per-session file-change registry that powers the
// "changes" TUI tab. The registry aggregates writes from the main agent and
// any sub-agents (each backed by its own *snapshot.Store), tracks bash
// invocations that did not go through the snapshot store via a pre/post
// stat-walk, and offers whole-file and per-block undo routed through the
// snapshot store's conflict-guarded UndoByToolCallID.
//
// The TUI is a thin renderer on top of Registry.List(); no TUI code lives
// in this package. Conversely, this package never imports internal/tui, so
// it can be reused by the headless server, ACP, and a future web SPA
// without a UI dependency.
package changes

import (
	"errors"
	"sync"
	"time"

	"github.com/u007/ocode/internal/snapshot"
)

// FileStatus reflects the file's state at UpdatedAt relative to the
// agent's FIRST snapshot (not session-start). See spec §4.1.
type FileStatus int

const (
	// FileAdded: the file did not exist pre-session; the first snapshot
	// captured an empty backup, so undoing means deleting the file.
	FileAdded FileStatus = iota
	// FileModified: the file existed pre-session; the first snapshot has
	// bytes different from the current state.
	FileModified
	// FileDeleted: the file was removed (by bash `rm` or a delete tool)
	// after the session's first write. The first snapshot still has the
	// pre-session bytes; undoing means recreating the file.
	FileDeleted
)

// String returns a single-character label for the status, used in the
// changes-tab row rendering. Single-width per the AGENTS.md TUI rules.
func (s FileStatus) String() string {
	switch s {
	case FileAdded:
		return "+"
	case FileModified:
		return "M"
	case FileDeleted:
		return "-"
	default:
		return "?"
	}
}

// ChangeAuthor identifies one of the agents (main or a sub-agent) that
// wrote to a file. ChangeCount is how many distinct tool calls this
// author made to this file.
type ChangeAuthor struct {
	AgentID   string // "main" or sub-agent id (e.g. "a1")
	AgentName string // human-readable (e.g. "build", "scout", "explore")
	Changes   int    // how many tool calls this author made on this file
}

// FileChange is one row in the changes tab. It represents the cumulative
// effect of every write the session has made to OriginalPath, merged into
// a single view against the pre-session snapshot. The Authors list is
// ordered: main first, then sub-agents in the order they attached.
type FileChange struct {
	OriginalPath    string         // absolute path; never empty
	Status          FileStatus     // Added | Modified | Deleted
	FirstBackupPath string         // "" for files created in-session with no pre-session backup
	Undoable        bool           // false for bash-only changes (no backup to restore)
	UndoAllTCID     string         // tool_call_id of the FIRST snapshot; used for "undo all"
	ChangeCount     int            // number of distinct tool calls touching this file
	Authors         []ChangeAuthor // ordered: main agent first, then sub-agents
	CreatedAt       time.Time      // first change
	UpdatedAt       time.Time      // most recent change

	// LastBashCommand is set when the most recent write came from a bash
	// invocation (the pre/post stat walker inferred the path), and is
	// surfaced in the per-row details strip. Empty for snapshot-only
	// entries.
	LastBashCommand string
	// LastBashExitCode mirrors NotifyBashWrite's exit code so the row can
	// indicate (in a future details pass) whether the bash that touched
	// this path actually succeeded.
	LastBashExitCode int
}

// BashOp is the inferred operation type for a file the bash shell
// touched. The bash recorder's pre/post stat walker decides which one
// applies by comparing the file's existence + content between pre and
// post.
type BashOp int

const (
	// BashAdded: file did not exist before the command, exists after.
	BashAdded BashOp = iota
	// BashModified: file existed before, content/size changed.
	BashModified
	// BashDeleted: file existed before, missing after.
	BashDeleted
)

// BashTouch is one file the bash shell touched during a single
// invocation.
type BashTouch struct {
	Path string
	Op   BashOp
}

// BashWriteEvent is the payload NotifyBashWrite receives. Touches is
// the set of files the shell actually changed (the conservative
// intersection of pre/post stat diff with the command's path tokens).
type BashWriteEvent struct {
	Command  string
	WorkDir  string
	ExitCode int
	Touches  []BashTouch // ordered, deduplicated; UI shows the first command that touched each path
}

// Registry is the per-session aggregation of file changes. It is safe
// for concurrent use. All public methods take the registry's internal
// mutex; the returned slices/maps are copies.
type Registry struct {
	mu      sync.Mutex
	files   map[string]*FileChange     // path → entry; built eagerly on every store event
	byAgent map[string]*snapshot.Store // agentID → store to subscribe to

	// cache and cacheSig memoize the last per-snapshot walk (rebuildAggsLocked).
	// cacheSig is a cheap signature (see signatureLocked) recomputed on every
	// call; the expensive walk across every attached store's snapshots only
	// runs when the signature changes. finalizeLocked still runs every call
	// to re-derive live filesystem status, but that's O(distinct files)
	// rather than O(total snapshots). This matters because List() (and
	// therefore lookupLocked) is called on every TUI render of the changes
	// tab, but the underlying snapshot stores usually haven't changed
	// between renders.
	cacheValid bool
	cacheSig   int64
	cache      []*fileAggregate
}

// NewRegistry returns an empty Registry. Attach at least one
// *snapshot.Store before calling List().
func NewRegistry() *Registry {
	return &Registry{
		files:   make(map[string]*FileChange),
		byAgent: make(map[string]*snapshot.Store),
	}
}

// AttachSnapshotStore wires store into the registry as the snapshot
// source for agentID. After Phase 1 this triggers an eager rebuild of
// the file map; in Phase 0 it just records the binding.
func (r *Registry) AttachSnapshotStore(agentID string, store *snapshot.Store) error {
	if agentID == "" {
		return errors.New("changes: agentID is required")
	}
	if store == nil {
		return errors.New("changes: store is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byAgent[agentID] = store
	return nil
}

// DetachSnapshotStore removes the binding for agentID. The file map is
// not invalidated; existing rows remain visible until a future
// Attach/Detach or NotifyBashWrite triggers a rebuild. Phase 0 stub.
func (r *Registry) DetachSnapshotStore(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byAgent, agentID)
}

// List returns the current snapshot of FileChange, sorted by OriginalPath
// for determinism. Phase 1 implementation: walks each attached store's
// ChangedFiles() (deduplicated across agents) and emits a stub
// FileChange per path. Phase 3 replaces this with a full per-snapshot
// walk that materializes the first backup, undoable flag, authors, and
// timestamps.
func (r *Registry) List() []FileChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listInternal()
}

// ErrNotUndoable is returned by UndoFile, UndoBlock, and LatestToolCall
// when the file's FileChange.Undoable is false (e.g. a bash-only entry).
// The TUI surfaces this as a status-bar message and skips the confirm
// dialog entirely.
var ErrNotUndoable = errors.New("changes: file is not undoable from the changes tab")

// ErrNoChanges is returned by LatestToolCall when the file has no
// recorded tool calls (e.g. a row that exists only because the saved
// session list remembered it, but the live stores have no snapshot for
// the path).
var ErrNoChanges = errors.New("changes: no recorded tool calls for this file")
