// Package termtabs persists which terminal tabs are open, so every client on a
// server sees the same terminal strip.
//
// This is the same problem internal/tabs already solved for SESSION tabs, and
// its doc comment states the failure verbatim: tab state in browser
// localStorage is per-origin, so "a project can show tabs in the desktop app
// but 0 in a browser". Terminals were never migrated, which is why a terminal
// started in the desktop app was invisible to a second browser: the tab list
// lived in `ocode.ui.terminals.project.v1` (per-origin localStorage) while the
// shell itself lived on the remote host's `serve --remote` and was perfectly
// shared. The list was the only thing that could not cross.
//
// Scope, and what is deliberately NOT shared:
//
//   - The LIST of open terminal tabs is shared. That is the thing the user
//     expects to see in another window.
//   - `activeId` is NOT stored here. Which tab a window has focused is
//     per-client view state — it can even be PROCESSES_TAB_ID, which is not a
//     terminal at all — and sharing it would let one window yank another's
//     selection on every click. The client keeps it locally.
//   - Scrollback buffers are NOT stored here. They are per-client render state
//     with a localStorage GC, and the server already persists the real pty
//     transcript per terminal id (terminal_history.go) for replay on attach.
//
// Keys are opaque and are NOT filepath.Clean'd. A key is the client's
// `host::path` composite (projectTerminalsKey in the web client): the bare
// project path for a local project, `<host>::<path>` for a remote one. Clean
// would corrupt `wsl:Ubuntu::/home/x` and Windows drive-letter paths, and
// there is no canonical form to normalize toward — the client is the authority
// for which project it means.
package termtabs

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/filelock"
	"github.com/u007/ocode/internal/paths"
)

// Terminal is one open terminal tab's persisted metadata.
type Terminal struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Renamed records that the user typed a title, which must beat the OSC
	// title the running program reports.
	Renamed bool `json:"renamed,omitempty"`
	// OSCTitle is the last title the program set via OSC 0/2.
	OSCTitle string `json:"osc_title,omitempty"`
}

// ProjectTerminals is the shared terminal list for one project key.
type ProjectTerminals struct {
	Terminals []Terminal `json:"terminals"`
}

// Store persists per-project open-terminal state.
type Store struct {
	mu sync.Mutex
	// path is the terminals.json location; lockPath serializes
	// read-modify-write cycles across processes so two servers can't interleave
	// and lose one another's projects.
	path     string
	lockPath string
	cache    map[string]ProjectTerminals
	// stamp identifies the on-disk revision this cache was loaded from
	// (mtime + size). Get/All reload when it no longer matches, so a write by
	// another process becomes visible without re-reading on every call.
	stamp fileStamp
}

// fileStamp is a cheap change-detector for the on-disk file.
type fileStamp struct {
	modTime time.Time
	size    int64
	valid   bool
}

// NewStore creates or loads a terminal-tab store from the global data dir.
func NewStore() (*Store, error) {
	globalDir, err := paths.GlobalDataDir()
	if err != nil {
		return nil, fmt.Errorf("termtabs: resolve global data dir: %w", err)
	}
	return NewStoreAt(filepath.Join(globalDir, "terminals.json"))
}

// NewStoreAt creates or loads a terminal-tab store at an explicit JSON path.
// Used by the server tests to keep the store out of the real global data dir.
func NewStoreAt(path string) (*Store, error) {
	s := &Store{
		path:     path,
		lockPath: path + ".lock",
		cache:    map[string]ProjectTerminals{},
	}
	if err := s.load(); err != nil {
		log.Printf("termtabs: loading terminal tab state: %v (starting fresh)", err)
		s.cache = map[string]ProjectTerminals{}
		s.stamp = fileStamp{}
	}
	return s, nil
}

// normalizeKey trims surrounding whitespace and rejects an empty key. It
// deliberately does not Clean the path — see the package comment.
func normalizeKey(key string) string {
	return strings.TrimSpace(key)
}

// load reads the whole file into the cache and records its stamp. Callers must
// hold s.mu.
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cache = map[string]ProjectTerminals{}
			s.stamp = fileStamp{}
			return nil // fresh install, no state yet
		}
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		s.cache = map[string]ProjectTerminals{}
		s.recordStamp()
		return nil
	}
	var cache map[string]ProjectTerminals
	if err := json.Unmarshal(data, &cache); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	if cache == nil {
		cache = map[string]ProjectTerminals{}
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

// reloadIfChangedLocked re-reads the file when another process has rewritten it
// since the last load/save. Callers must hold s.mu.
func (s *Store) reloadIfChangedLocked() {
	fi, err := os.Stat(s.path)
	if err != nil {
		if os.IsNotExist(err) && s.stamp.valid {
			// Another process deleted the file (e.g. its last project was
			// closed). Drop our stale cache rather than resurrect it.
			s.cache = map[string]ProjectTerminals{}
			s.stamp = fileStamp{}
		}
		return
	}
	if s.stamp.valid && fi.ModTime().Equal(s.stamp.modTime) && fi.Size() == s.stamp.size {
		return
	}
	if err := s.load(); err != nil {
		log.Printf("termtabs: reloading %s: %v (keeping cached state)", s.path, err)
	}
}

// saveLocked writes the cache atomically (temp file + rename) so a concurrent
// reader never observes a half-written file. Callers must hold s.mu.
func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal terminal tabs: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".terminals-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp terminals file: %w", err)
	}
	tmpName := tmp.Name()
	// Remove the temp file on every failure path; after a successful rename
	// this is a harmless no-op.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp terminals file: %w", err)
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp terminals file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp terminals file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("rename temp terminals file: %w", err)
	}
	s.recordStamp()
	return nil
}

// withLock runs fn while holding the cross-process advisory lock for this
// store. The directory is created first because a fresh install has no data
// dir yet.
func (s *Store) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.lockPath), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return filelock.WithFileLock(s.lockPath, fn)
}

// Get returns the terminal list for one project key. A missing key yields an
// empty (zero) ProjectTerminals without error.
func (s *Store) Get(key string) ProjectTerminals {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	return s.cache[normalizeKey(key)]
}

// All returns a copy of every project's terminal list, keyed by project key.
func (s *Store) All() map[string]ProjectTerminals {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	out := make(map[string]ProjectTerminals, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}

// ApplyBulk merges a set of per-project terminal lists into the persisted
// state. Every key in patch replaces that project's entry; an entry with no
// terminals deletes the project. Projects ABSENT from patch are preserved.
//
// The merge is load-bearing for multi-window/multi-process safety: a client only
// ever sends the projects it knows about, so a window (or server) that has
// never seen another window's project must not drop it. Deletion is expressed
// explicitly by sending an empty list for a project the client knows — the
// frontend must therefore emit an explicit empty entry for a project whose last
// tab it just closed, or omitting the key would read as "never heard of it" and
// the closed tab would reappear.
//
// The whole read-modify-write cycle runs under a cross-process file lock and
// reloads the latest on-disk state first, so concurrent writers merge instead
// of clobbering each other.
func (s *Store) ApplyBulk(patch map[string]ProjectTerminals) error {
	return s.withLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reloadIfChangedLocked()
		for k, v := range patch {
			cleaned := normalizeKey(k)
			if cleaned == "" {
				continue
			}
			if len(v.Terminals) == 0 {
				delete(s.cache, cleaned)
				continue
			}
			s.cache[cleaned] = v
		}
		return s.saveLocked()
	})
}

// Set replaces the terminal list for a single project key. An empty list
// clears the project (deletes its entry).
func (s *Store) Set(key string, pt ProjectTerminals) error {
	return s.withLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reloadIfChangedLocked()
		cleaned := normalizeKey(key)
		if cleaned == "" {
			return fmt.Errorf("termtabs: empty project key")
		}
		if len(pt.Terminals) == 0 {
			delete(s.cache, cleaned)
		} else {
			s.cache[cleaned] = pt
		}
		return s.saveLocked()
	})
}

// Remove deletes the terminal list for a project key (e.g. when the project is
// removed). Deleting a non-existent key is a no-op.
func (s *Store) Remove(key string) error {
	return s.withLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reloadIfChangedLocked()
		cleaned := normalizeKey(key)
		if _, ok := s.cache[cleaned]; !ok {
			return nil
		}
		delete(s.cache, cleaned)
		return s.saveLocked()
	})
}
