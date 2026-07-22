// Registry aggregation implementation. The Registry owns the per-session
// file-change list that powers the changes TUI tab. It is safe for
// concurrent use; all mutations go through r.mu and the returned
// slices/maps are copies.
//
// Phases:
//  1. List() walks each attached store's ChangedFiles() (deduplicated
//     across agents) and emits a stub FileChange per path.
//  3. List() is replaced with a full per-snapshot walk that
//     materializes FirstBackupPath, UndoAllTCID, Authors, CreatedAt,
//     and UpdatedAt from the store's internal snapshot slice.

package changes

import (
	"os"
	"sort"
	"time"

	"github.com/u007/ocode/internal/snapshot"
)

// fileAggregate is the per-file aggregate we maintain while walking the
// stores' snapshot slices. It is not exposed; listInternal converts to
// FileChange at the end.
type fileAggregate struct {
	originalPath string
	firstBackup  string    // "" for in-session creations
	firstTCID    string    // tool_call_id of the oldest snapshot
	firstAt      time.Time // timestamp of the oldest snapshot
	lastAt       time.Time // timestamp of the most recent snapshot

	// authors tracks per-agent write counts in attach order. main
	// (id="main") is added first by convention; sub-agents follow
	// in the order their snapshot store attached.
	authors    []ChangeAuthor
	authorIdx  map[string]int // agentID -> index in authors
	changeCnt  int            // distinct tool calls touching this file
	undoable   bool           // true when the file has at least one backup
	hasCreated bool           // true if the file's first backup was an empty-file "added" backup
}

// listInternal rebuilds the file map from the attached stores and returns
// a deduped, sorted slice. Caller must hold r.mu.
//
// The walk is per-snapshot (Phase 3): for each store we enumerate its
// snapshot slice in chronological order, aggregating per path. The oldest
// snapshot supplies FirstBackupPath, UndoAllTCID, CreatedAt; the newest
// supplies UpdatedAt; per-agent write counts roll up into Authors. The
// dedup merge across stores prefers the oldest-first-snapshot and the
// newest-last-snapshot for timestamps (chronological max).
func (r *Registry) listInternal() []FileChange {
	sig := r.signatureLocked()
	if !r.cacheValid || sig != r.cacheSig {
		r.cache = r.rebuildAggsLocked()
		r.cacheSig = sig
		r.cacheValid = true
	}
	// finalizeLocked runs on every call, cache hit or not: FileStatus
	// depends on a live os.Stat (see fileAggregate.toFileChange), which
	// can change (a file gets deleted or recreated on disk) without any
	// snapshot or bash event firing, so it can never be part of the
	// cached signature. This is still a real win: it's O(distinct files)
	// instead of the O(total snapshots across every store) walk that
	// rebuildAggsLocked does.
	return r.finalizeLocked(r.cache)
}

// signatureLocked returns a cheap, order-independent summary of everything
// listInternal's rebuild depends on: the number of attached stores, the
// number of bash-only entries, and the snapshot count of every attached
// store. It is a sum rather than e.g. a hash chain specifically so that
// map iteration order (which Go randomizes) can't change the result for
// unchanged state.
//
// This is a heuristic, not a cryptographic digest: two different states
// could in principle sum to the same signature (e.g. one store gaining a
// snapshot while another loses one in the same tick). That would only
// cause the changes tab to show a stale render for one extra frame, which
// is already the tolerance the TUI accepts elsewhere (View() only
// refreshes once per render). Caller must hold r.mu.
func (r *Registry) signatureLocked() int64 {
	sig := int64(len(r.byAgent))*1_000_000_007 + int64(len(r.files))
	for _, store := range r.byAgent {
		if store != nil {
			sig += int64(store.Len())
		}
	}
	return sig
}

// rebuildAggsLocked performs the full per-snapshot walk described above and
// returns the per-file aggregates, unsorted and without live-filesystem
// status applied (that's finalizeLocked's job, which runs every call).
// Caller must hold r.mu; listInternal is the only caller and handles caching.
func (r *Registry) rebuildAggsLocked() []*fileAggregate {
	aggs := make(map[string]*fileAggregate)

	// Iterate byAgent in deterministic order so list output is stable for
	// the same input. Main (id="main") sorts first; sub-agents follow
	// alphabetically.
	agentIDs := make([]string, 0, len(r.byAgent))
	for id := range r.byAgent {
		agentIDs = append(agentIDs, id)
	}
	sort.Strings(agentIDs)

	for _, agentID := range agentIDs {
		store := r.byAgent[agentID]
		// Defensive: nil store shouldn't happen (AttachSnapshotStore
		// guards), but a future detacher might leave a dangling entry.
		if store == nil {
			continue
		}
		for _, snap := range store.Snapshots() {
			r.aggregateSnapshotLocked(aggs, agentID, snap)
		}
	}

	out := make([]*fileAggregate, 0, len(aggs))
	for _, agg := range aggs {
		out = append(out, agg)
	}
	return out
}

// finalizeLocked converts cached aggregates (plus any bash-only entries in
// r.files) into the sorted []FileChange the TUI consumes. It always
// re-derives FileStatus from a live os.Stat via toFileChange, so it must run
// on every listInternal call regardless of whether rebuildAggsLocked ran.
// Caller must hold r.mu.
func (r *Registry) finalizeLocked(aggs []*fileAggregate) []FileChange {
	out := make([]FileChange, 0, len(aggs)+len(r.files))
	for _, agg := range aggs {
		out = append(out, agg.toFileChange())
	}
	// Phase 5: bash-only entries (recorded via NotifyBashWrite
	// into r.files but never appearing in any snapshot store) are
	// appended here. The per-snapshot walk above already covers
	// files with snapshot backups; we only emit r.files entries
	// that did not surface in aggs.
	bashSeen := make(map[string]struct{}, len(aggs))
	for _, fc := range out {
		bashSeen[fc.OriginalPath] = struct{}{}
	}
	for path, bash := range r.files {
		if _, covered := bashSeen[path]; covered {
			continue
		}
		out = append(out, *bash)
	}
	// Sort by UpdatedAt desc, then by OriginalPath asc for stability
	// when two files have identical UpdatedAt (rare but possible when
	// multiple backups were created in the same nanosecond).
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].OriginalPath < out[j].OriginalPath
	})
	return out
}

// aggregateSnapshotLocked folds one snapshot into the per-file
// aggregate map. Caller must hold r.mu.
func (r *Registry) aggregateSnapshotLocked(
	aggs map[string]*fileAggregate,
	agentID string,
	snap snapshot.Snapshot,
) {
	if snap.OriginalPath == "" {
		return
	}
	agg, ok := aggs[snap.OriginalPath]
	if !ok {
		agg = &fileAggregate{
			originalPath: snap.OriginalPath,
			authorIdx:    make(map[string]int),
		}
		aggs[snap.OriginalPath] = agg
	}

	// Authors rollup: one ChangeAuthor per agent, count of distinct
	// tool calls. We don't dedup by tool_call_id here — repeated calls
	// to the same agent (e.g. several `edit` calls) count as separate
	// writes.
	idx, seen := agg.authorIdx[agentID]
	if !seen {
		idx = len(agg.authors)
		agg.authors = append(agg.authors, ChangeAuthor{
			AgentID:   agentID,
			AgentName: agentID, // spec.Name wiring lands in Phase 11
			Changes:   0,
		})
		agg.authorIdx[agentID] = idx
	}
	agg.authors[idx].Changes++
	agg.changeCnt++

	// Backup: the OLDEST snapshot wins (it has the pre-session bytes).
	// We set firstBackup / firstTCID only on first observation so a
	// later snapshot with a different ToolCallID doesn't overwrite the
	// pre-session anchor.
	if !agg.hasCreated {
		if agg.firstAt.IsZero() || snap.Timestamp.Before(agg.firstAt) {
			agg.firstBackup = snap.BackupPath
			agg.firstTCID = snap.ToolCallID
			agg.firstAt = snap.Timestamp
			// If the first snapshot has no backup path, the file was
			// created in-session (FileAdded semantics).
			agg.hasCreated = snap.BackupPath == ""
		}
		// An empty BackupPath on the very first snapshot is the
		// "added" marker. We don't reset this on later snapshots —
		// the first write's nature is what defines FileAdded vs
		// FileModified.
	}

	// UpdatedAt is the timestamp of the newest snapshot for this path.
	if snap.Timestamp.After(agg.lastAt) {
		agg.lastAt = snap.Timestamp
	}

	// The file is undoable iff at least one snapshot has a non-empty
	// backup path (i.e. a pre-write copy exists on disk). Bash-only
	// files (Phase 5) are not undoable and enter via a different path.
	agg.undoable = agg.undoable || snap.BackupPath != ""
}

// toFileChange converts the aggregate to the public FileChange. The
// FileStatus is derived: added (first snapshot had no backup),
// modified (first snapshot had a backup), deleted (current file
// missing — detected here via a follow-up os.Stat).
func (a *fileAggregate) toFileChange() FileChange {
	fc := FileChange{
		OriginalPath:    a.originalPath,
		FirstBackupPath: a.firstBackup,
		Undoable:        a.undoable,
		UndoAllTCID:     a.firstTCID,
		ChangeCount:     a.changeCnt,
		Authors:         a.authors,
		CreatedAt:       a.firstAt,
		UpdatedAt:       a.lastAt,
	}
	switch {
	case !pathExists(a.originalPath):
		// File is missing on disk — deleted regardless of whether
		// it was created in-session (hasCreated) or pre-existing.
		fc.Status = FileDeleted
	case a.hasCreated:
		// Added in-session: file existed at some point with no
		// pre-session bytes. The first snapshot is the "added"
		// marker. If the file is now missing on disk, it's Deleted
		// (handled above by !pathExists); otherwise it remains Added.
		fc.Status = FileAdded
	default:
		fc.Status = FileModified
	}
	return fc
}

// pathExists is a best-effort existence check used to flip a status
// from Added to Deleted when the file is gone. The registry never
// mutates the file system, so a missing file is a strong signal that
// some external process (bash `rm`, the delete tool, etc.) removed it
// after the snapshot. Indirected through a package var so tests can
// stub it.
var pathExists = func(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
