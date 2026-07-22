// Undo operations. These all delegate to the underlying snapshot.Store
// (which has its own conflict guards and max-age window). The Registry
// layers in (a) per-file aggregation, (b) oldest-first iteration so the
// conflict guard is satisfied, and (c) the bash-only / non-undoable
// case via ErrNotUndoable.

package changes

import (
	"errors"
	"sort"

	"github.com/u007/ocode/internal/snapshot"
)

// UndoFile restores path to its pre-session state. It calls
// UndoByToolCallID once per distinct tool_call_id that touched the
// file, oldest-first across all authors. The oldest-first ordering is
// what makes the conflict guard inside UndoByToolCallID succeed: by
// the time we undo the most recent write, all earlier writes have
// already been reverted.
//
// Returns ErrNotUndoable if the file's FileChange.Undoable is false
// (bash-only entries — no backup exists). Returns ErrNoChanges if the
// file is unknown to the registry.
func (r *Registry) UndoFile(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	change, ok := r.lookupLocked(path)
	if !ok {
		return ErrNoChanges
	}
	if !change.Undoable {
		return ErrNotUndoable
	}
	if change.UndoAllTCID == "" {
		// Defensive: undoable with no tool_call_id means an empty
		// backup was the only thing recorded. There is nothing
		// to restore.
		return ErrNotUndoable
	}

	// Collect every (store, tcid, agent step, timestamp) for snapshots
	// touching path. The same tool call may have multiple snapshot
	// entries (one per file touched), so we dedup by tcid below.
	type callEntry struct {
		store *snapshot.Store
		tcid  string
		step  int
		at    int64
	}
	var calls []callEntry
	for agentID, store := range r.byAgent {
		if store == nil {
			continue
		}
		for _, snap := range store.Snapshots() {
			if snap.OriginalPath != path {
				continue
			}
			if snap.ToolCallID == "" {
				continue
			}
			calls = append(calls, callEntry{
				store: store,
				tcid:  snap.ToolCallID,
				step:  snap.AgentStep,
				at:    snap.Timestamp.UnixNano(),
			})
		}
		_ = agentID // reserved for future per-agent filtering
	}
	if len(calls) == 0 {
		return ErrNoChanges
	}

	// Sort the calls NEWEST-first (LIFO). The snapshot store's
	// UndoByToolCallID refuses if a newer active change still exists
	// for the same file (same-agent newer-block guard), so we must
	// undo from the most recent call backwards. After the newest
	// snapshot is removed, the next-newest becomes the new "newest"
	// and its undo succeeds. Iteration stops cleanly when every
	// call has been undone.
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].step != calls[j].step {
			return calls[i].step > calls[j].step
		}
		return calls[i].at > calls[j].at
	})
	// Dedup identical (store, tcid) pairs — multiple snapshots for
	// the same call (e.g. a write that touched several files) collapse
	// to one UndoByToolCallID call.
	seen := make(map[string]struct{})
	for _, c := range calls {
		key := c.tcid
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, err := c.store.UndoByToolCallID(c.tcid, snapshotUndoMaxAge); err != nil {
			// Cross-agent conflict / expiry / not found. Wrap the
			// underlying error so callers can errors.Is the
			// specific store-level failure.
			return errors.Join(ErrNoChanges, err)
		}
	}
	return nil
}

// UndoBlock undoes a single tool call. Looks up the (path, toolCallID)
// pair in the relevant store and calls UndoByToolCallID.
//
// Returns ErrNotUndoable if the file's FileChange.Undoable is false.
// Returns ErrNoChanges if the (path, toolCallID) pair isn't recorded in
// any attached store.
func (r *Registry) UndoBlock(path, toolCallID string) error {
	if toolCallID == "" {
		return ErrNoChanges
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if change, ok := r.lookupLocked(path); ok && !change.Undoable {
		return ErrNotUndoable
	}

	for _, store := range r.byAgent {
		if store == nil {
			continue
		}
		for _, snap := range store.Snapshots() {
			if snap.OriginalPath == path && snap.ToolCallID == toolCallID {
				if _, err := store.UndoByToolCallID(toolCallID, snapshotUndoMaxAge); err != nil {
					return errors.Join(ErrNoChanges, err)
				}
				return nil
			}
		}
	}
	return ErrNoChanges
}

// LatestToolCall returns the tool_call_id of the most recent snapshot
// for path. Returns ErrNoChanges if no snapshot records the path
// (e.g. a row remembered only by the session-persisted list whose
// live stores have been GC'd). Returns ErrNotUndoable if the file
// is not undoable (no tool_call_id was ever recorded).
func (r *Registry) LatestToolCall(path string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	change, ok := r.lookupLocked(path)
	if !ok {
		return "", ErrNoChanges
	}
	if !change.Undoable {
		return "", ErrNotUndoable
	}

	var latestTCID string
	var latestAt int64
	for _, store := range r.byAgent {
		if store == nil {
			continue
		}
		for _, snap := range store.Snapshots() {
			if snap.OriginalPath != path || snap.ToolCallID == "" {
				continue
			}
			if snap.Timestamp.UnixNano() > latestAt {
				latestAt = snap.Timestamp.UnixNano()
				latestTCID = snap.ToolCallID
			}
		}
	}
	if latestTCID == "" {
		return "", ErrNoChanges
	}
	return latestTCID, nil
}

// lookupLocked returns the FileChange for path from a freshly-rebuilt
// view of the attached stores. It does NOT consult r.files (which is
// reserved for future caching); it always re-aggregates so the result
// matches what List() would return for this path. Caller must hold r.mu.
func (r *Registry) lookupLocked(path string) (FileChange, bool) {
	all := r.listInternal()
	for _, fc := range all {
		if fc.OriginalPath == path {
			return fc, true
		}
	}
	return FileChange{}, false
}

// snapshotUndoMaxAge is the per-tool-call undo window passed to
// UndoByToolCallID. The snapshot store uses it as a max agent-step
// delta; a generous value (matching the snapshot package's own
// internal callers) is appropriate here.
const snapshotUndoMaxAge = 1000
