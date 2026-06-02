package lsp

import (
	"errors"
	"log"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// fileWatcher fans fsnotify events out to per-client update handlers. It is
// owned by the Manager and shut down when the Manager is closed.
//
// We use fsnotify (a transitive dep, already pinned) rather than rolling a
// polling loop: it gives sub-millisecond latency on file save, and on Linux
// it leverages inotify so we don't pay a wakeup per file. A single watcher
// handles all open documents across all language servers, which keeps the
// fd count low even for big projects.
type fileWatcher struct {
	w     *fsnotify.Watcher
	root  string
	owner *Manager
}

// newFileWatcher creates the watcher and starts its event loop. Callers must
// invoke Close on the returned value (via Manager.Close) to release the fd.
func newFileWatcher(root string, owner *Manager) (*fileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	fw := &fileWatcher{w: w, root: root, owner: owner}
	go fw.loop()
	return fw, nil
}

// Add registers an absolute path with the watcher. Idempotent; fsnotify
// silently replaces a duplicate watch on the same path.
func (fw *fileWatcher) Add(absPath string) {
	if fw == nil || fw.w == nil {
		return
	}
	// fsnotify watches a single file (not a directory) — passing a directory
	// path here triggers a warning and no events.
	if err := fw.w.Add(absPath); err != nil {
		// File may not exist yet (e.g. didOpen on a brand-new buffer) — log
		// at debug level rather than failing the open.
		log.Printf("lsp: watcher add %s: %v", absPath, err)
	}
}

// Remove drops a watch. Safe to call with a path that was never added.
func (fw *fileWatcher) Remove(absPath string) {
	if fw == nil || fw.w == nil {
		return
	}
	if err := fw.w.Remove(absPath); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
		log.Printf("lsp: watcher remove %s: %v", absPath, err)
	}
}

func (fw *fileWatcher) Close() error {
	if fw == nil || fw.w == nil {
		return nil
	}
	return fw.w.Close()
}

func (fw *fileWatcher) loop() {
	defer func() {
		// Defensive: if the loop exits unexpectedly, close the watcher so the
		// fd is released.
		_ = fw.w.Close()
	}()
	for {
		select {
		case ev, ok := <-fw.w.Events:
			if !ok {
				return
			}
			// We only care about file modifications and creations (rename
			// edits often arrive as CREATE after a temp-file swap).
			if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			abs, err := filepath.Abs(ev.Name)
			if err != nil {
				continue
			}
			// Defer to the Manager, which knows which client (if any) holds
			// this file open and can decide what to ship.
			fw.owner.handleFileChange(abs)
		case err, ok := <-fw.w.Errors:
			if !ok {
				return
			}
			log.Printf("lsp: watcher error: %v", err)
		}
	}
}
