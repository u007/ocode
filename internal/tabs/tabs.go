// Package tabs persists the open-session tab state for the web/desktop
// multi-project UI. The store keeps, per project root, the list of open
// session tabs and the active tab id, as a JSON file under the global data
// dir (same pattern as internal/projects).
//
// Tab state lives server-side — not in browser localStorage — because the
// desktop shell boots its API server on a random loopback port every launch,
// which changes the webview origin and silently wipes any origin-scoped
// localStorage. A file under the global data dir survives restarts regardless
// of port.
//
// The file is shared by every ocode server process on the machine (the
// desktop .app, a `make dev`/`go run` server, extra `ocode serve` instances,
// worktree servers) because they all resolve the same global data dir. The
// store is therefore written as a read-modify-write merge under a
// cross-process file lock, with atomic temp-file renames, and it reloads the
// file when another process has changed it. Without that, one process's
// whole-map PUT would silently drop another process's projects — which is
// exactly how a project can show tabs in the desktop app but 0 in a browser
// pointed at a different server sharing the same data dir.
package tabs

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/u007/ocode/internal/filelock"
	"github.com/u007/ocode/internal/paths"
)

// Tab is one open session tab in the UI.
type Tab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// SubTab is the session sub-tab (chat/agents/changes/...) the tab was
	// last viewing, so a restore lands where the user left off.
	SubTab string `json:"sub_tab,omitempty"`
}

// ProjectTabs is the tab state for a single project root.
type ProjectTabs struct {
	Tabs   []Tab  `json:"tabs"`
	Active string `json:"active,omitempty"`
}

// Store persists per-project open-tab state.
type Store struct {
	mu sync.Mutex
	// path is the tabs.json location; lockPath serializes read-modify-write
	// cycles across processes so two servers can't interleave and lose one
	// another's projects.
	path     string
	lockPath string
	cache    map[string]ProjectTabs
	// stamp identifies the on-disk revision this cache was loaded from
	// (mtime + size). Get/All reload when it no longer matches, so a write
	// by another process becomes visible without re-reading on every call.
	stamp fileStamp
}

// fileStamp is a cheap change-detector for the on-disk file.
type fileStamp struct {
	modTime time.Time
	size    int64
	valid   bool
}

// NewStore creates or loads a tab store from the global data dir.
func NewStore() (*Store, error) {
	globalDir, err := paths.GlobalDataDir()
	if err != nil {
		return nil, fmt.Errorf("tabs: resolve global data dir: %w", err)
	}
	return NewStoreAt(filepath.Join(globalDir, "tabs.json"))
}

// NewStoreAt creates or loads a tab store at an explicit JSON path. Used by
// the server tests to keep the store out of the real global data dir.
func NewStoreAt(path string) (*Store, error) {
	s := &Store{
		path:     path,
		lockPath: path + ".lock",
		cache:    map[string]ProjectTabs{},
	}
	if err := s.load(); err != nil {
		log.Printf("tabs: loading tab state: %v (starting fresh)", err)
		s.cache = map[string]ProjectTabs{}
		s.stamp = fileStamp{}
	}
	return s, nil
}

// load reads the whole file into the cache and records its stamp. Callers
// must hold s.mu.
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cache = map[string]ProjectTabs{}
			s.stamp = fileStamp{}
			return nil // fresh install, no state yet
		}
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		s.cache = map[string]ProjectTabs{}
		s.recordStamp()
		return nil
	}
	var cache map[string]ProjectTabs
	if err := json.Unmarshal(data, &cache); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	if cache == nil {
		cache = map[string]ProjectTabs{}
	}
	s.cache = cache
	s.recordStamp()
	return nil
}

// recordStamp stats the file and stores its revision. Callers must hold s.mu.
func (s *Store) recordStamp() {
	fi, err := os.Stat(s.path)
	if err != nil {
		s.stamp = fileStamp{}
		return
	}
	s.stamp = fileStamp{modTime: fi.ModTime(), size: fi.Size(), valid: true}
}

// reloadIfChangedLocked re-reads the file when another process has rewritten
// it since the last load/save. Callers must hold s.mu.
func (s *Store) reloadIfChangedLocked() {
	fi, err := os.Stat(s.path)
	if err != nil {
		if os.IsNotExist(err) && s.stamp.valid {
			// Another process deleted the file (e.g. its last project was
			// closed). Drop our stale cache rather than resurrect it.
			s.cache = map[string]ProjectTabs{}
			s.stamp = fileStamp{}
		}
		return
	}
	if s.stamp.valid && fi.ModTime().Equal(s.stamp.modTime) && fi.Size() == s.stamp.size {
		return
	}
	if err := s.load(); err != nil {
		log.Printf("tabs: reloading %s: %v (keeping cached state)", s.path, err)
	}
}

// saveLocked writes the cache atomically (temp file + rename) so a concurrent
// reader never observes a half-written file. Callers must hold s.mu.
func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tabs: %w", err)
	}
	// Ensure the directory exists (first save after fresh install).
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tabs-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp tabs file: %w", err)
	}
	tmpName := tmp.Name()
	// Remove the temp file on every failure path; after a successful rename
	// this is a harmless no-op.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp tabs file: %w", err)
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp tabs file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp tabs file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("rename temp tabs file: %w", err)
	}
	s.recordStamp()
	return nil
}

// withLock runs fn while holding the cross-process advisory lock for this
// store. The directory is created first because a fresh install has no
// data dir yet.
func (s *Store) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.lockPath), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return filelock.WithFileLock(s.lockPath, fn)
}

// Get returns the tab state for a project root. A missing project yields an
// empty (zero) ProjectTabs without error.
func (s *Store) Get(path string) ProjectTabs {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	return s.cache[filepath.Clean(path)]
}

// All returns a copy of every project's tab state, keyed by project root.
func (s *Store) All() map[string]ProjectTabs {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	out := make(map[string]ProjectTabs, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}

// ApplyBulk merges a set of per-project tab states into the persisted state.
// Every key in patch replaces that project's entry; an entry with no tabs
// deletes the project. Projects ABSENT from patch are preserved.
//
// The merge is intentional and load-bearing for multi-window/multi-process
// safety: a client only ever sends the projects it knows about, so a window
// (or server) that has never seen another window's project must not drop it.
// Deletion is expressed explicitly by sending an empty tab list for a project
// the client knows.
//
// The whole read-modify-write cycle runs under a cross-process file lock and
// reloads the latest on-disk state first, so concurrent writers merge instead
// of clobbering each other.
func (s *Store) ApplyBulk(patch map[string]ProjectTabs) error {
	return s.withLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reloadIfChangedLocked()
		for k, v := range patch {
			cleaned := filepath.Clean(k)
			if len(v.Tabs) == 0 {
				delete(s.cache, cleaned)
				continue
			}
			s.cache[cleaned] = v
		}
		return s.saveLocked()
	})
}

// Set replaces the tab state for a single project root. An empty tab list
// clears the project (deletes its entry).
func (s *Store) Set(path string, pt ProjectTabs) error {
	return s.withLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reloadIfChangedLocked()
		cleaned := filepath.Clean(path)
		if len(pt.Tabs) == 0 {
			delete(s.cache, cleaned)
		} else {
			s.cache[cleaned] = pt
		}
		return s.saveLocked()
	})
}

// Remove deletes the tab state for a project root (e.g. when the project is
// removed). Deleting a non-existent key is a no-op.
func (s *Store) Remove(path string) error {
	return s.withLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reloadIfChangedLocked()
		cleaned := filepath.Clean(path)
		if _, ok := s.cache[cleaned]; !ok {
			return nil
		}
		delete(s.cache, cleaned)
		return s.saveLocked()
	})
}
