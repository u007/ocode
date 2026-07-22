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
	"sort"
	"time"
)

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

	now := time.Now()
	// Deterministic order so two concurrent bash events on the
	// same file produce the same in-place update. The file
	// listing is the user's view; the order they ran in is not
	// part of that view.
	touches := append([]BashTouch(nil), event.Touches...)
	sort.Slice(touches, func(i, j int) bool {
		return touches[i].Path < touches[j].Path
	})

	for _, t := range touches {
		// Build a partial FileChange. We need a value (not a
		// pointer) so the map can be populated atomically.
		fc := FileChange{
			OriginalPath:    t.Path,
			Undoable:        false,
			LastBashCommand: event.Command,
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
			// If the bash op indicates the file is gone
			// but the snapshot store still has a backup,
			// promote the row to FileDeleted (the user
			// can still undo via the restore path,
			// because the backup file is on disk).
			if t.Op == BashDeleted {
				existing.Status = FileDeleted
			}
			r.files[t.Path] = existing
			continue
		}
		r.files[t.Path] = &fc
	}
}
