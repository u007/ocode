// Bash event ingestion for the changes registry. The recorder
// (bash.go) calls NotifyBashWrite with a BashWriteEvent; the
// registry folds the touches into the per-file aggregate and
// rebuilds the file list.
//
// A bash touch is ALWAYS non-undoable: the bash tool's destructive
// paths are already routed through the snapshot store (so any
// touch with a real backup is already on the snapshot side), and
// every other path (heredoc writes, `sed -i`, `rm`, etc.) has no
// pre-session backup to restore from. The row is therefore
// "(bash)" only, and UndoFile returns ErrNotUndoable.

package changes

import (
	"fmt"
	"sort"
	"time"
)

// NotifyBashSkipped records that the bash recorder declined to turn one
// command's diff into change rows. Nothing else retains that fact, so without
// this a dropped event is indistinguishable from a command that changed
// nothing — which is exactly the ambiguity that made a 4,214-row false
// positive undiagnosable after the fact.
//
// The history is a bounded ring (maxSkipNotices): skips are diagnostic, not
// inventory.
func (r *Registry) NotifyBashSkipped(n SkipNotice) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifyBashSkippedLocked(n)
}

// notifyBashSkippedLocked is NotifyBashSkipped for callers already holding
// r.mu (NotifyBashWrite hits the ceiling while holding it).
func (r *Registry) notifyBashSkippedLocked(n SkipNotice) {
	if n.At.IsZero() {
		n.At = time.Now()
	}
	r.bashSkips = append(r.bashSkips, n)
	if len(r.bashSkips) > maxSkipNotices {
		r.bashSkips = append([]SkipNotice(nil), r.bashSkips[len(r.bashSkips)-maxSkipNotices:]...)
	}
}

// BashSkips returns the retained SkipNotice history, oldest first.
func (r *Registry) BashSkips() []SkipNotice {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SkipNotice(nil), r.bashSkips...)
}

// NotifyBashWrite materializes one FileChange per touch in
// event.Touches. The entries are Undoable:false (no snapshot
// backup to restore from). If a touch's path already exists in the
// registry (e.g. a snapshot-tracked edit followed by a bash
// `sed -i`), the existing entry's LastBashCommand and
// LastBashExitCode are refreshed and the row's Status is recomputed
// from the live filesystem.
//
// NotifyBashWrite is safe for concurrent use; it takes r.mu for the
// duration of the update.
func (r *Registry) NotifyBashWrite(event BashWriteEvent) {
	if len(event.Touches) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Bump even for in-place updates below so signatureLocked observes
	// every bash event (len(r.files) alone would miss a re-touch of an
	// already-known path).
	r.bashVersion++

	now := time.Now()
	// Deterministic order so two concurrent bash events on the
	// same file produce the same in-place update. The file
	// listing is the user's view; the order they ran in is not
	// part of that view.
	touches := append([]BashTouch(nil), event.Touches...)
	sort.Slice(touches, func(i, j int) bool {
		return touches[i].Path < touches[j].Path
	})

	limit := r.maxFiles
	if limit <= 0 {
		limit = maxTrackedFiles
	}
	refused := 0
	for _, t := range touches {
		// The ceiling gates NEW paths only. A bash touch on a path the
		// registry already tracks must still refresh its metadata, or
		// LastBashCommand would silently go stale for every row once a
		// session neared the ceiling.
		if _, known := r.files[t.Path]; !known && len(r.files) >= limit {
			refused++
			continue
		}
		// Build a partial FileChange. We need a value (not a
		// pointer) so the map can be populated atomically.
		fc := FileChange{
			OriginalPath:     t.Path,
			Undoable:         false,
			LastBashCommand:  event.Command,
			LastBashExitCode: event.ExitCode,
		}
		// FileStatus is derived from the live filesystem and
		// the bash op. The aggregate logic in registry.go
		// overwrites these on the next List() call, so we
		// only need to get the visible state right for the
		// very first render after the event.
		switch t.Op {
		case BashAdded:
			fc.Status = FileAdded
		case BashModified:
			fc.Status = FileModified
		case BashDeleted:
			fc.Status = FileDeleted
		}
		fc.UpdatedAt = now

		// If there's already an entry for this path, merge
		// the bash metadata into it instead of clobbering the
		// snapshot-derived fields (FirstBackupPath,
		// UndoAllTCID, etc.). An existing entry may have
		// been Undoable: true; once a bash touch lands on
		// it, we keep the snapshot side as authoritative for
		// undo (the backup still exists), but record the
		// most recent bash command for the per-row
		// details strip.
		if existing, ok := r.files[t.Path]; ok {
			existing.LastBashCommand = event.Command
			existing.LastBashExitCode = event.ExitCode
			existing.UpdatedAt = now
			// Track the op on every in-place update, not just BashDeleted:
			// a delete followed by a re-add must read as FileAdded again,
			// not linger as FileDeleted from the earlier event.
			switch t.Op {
			case BashAdded:
				existing.Status = FileAdded
			case BashModified:
				existing.Status = FileModified
			case BashDeleted:
				existing.Status = FileDeleted
			}
			r.files[t.Path] = existing
			continue
		}
		r.files[t.Path] = &fc
	}
	if refused > 0 {
		r.notifyBashSkippedLocked(SkipNotice{
			Reason:  SkipRegistryCeiling,
			Detail:  fmt.Sprintf("registry already tracks %d paths (ceiling); refused %d new path(s) from this command", limit, refused),
			Command: event.Command,
			// ExitCode is intentionally the event's: the shell's status says
			// nothing about why the registry refused a path.
			ExitCode: event.ExitCode,
			Paths:    refused,
		})
	}
}
