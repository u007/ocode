// Package browsertabs persists the embedded browser's tab strip and each tab's
// navigation state, so every client on a server shows the same browser tabs
// and URLs. It is the browser counterpart of internal/termtabs and follows the
// same discipline: the file is shared by every server process, so the store
// merges under a cross-process lock, and a PUT replaces only the projects it
// names.
//
// What is shared, per project key:
//
//   - the tab strip: each tab's id, title and manual rename, in strip order
//     (the array position IS the order; there is no separate order field to
//     drift out of sync);
//   - the active tab id;
//   - each tab's surface: current URL, history, history index, the user mode
//     override and the page title.
//
// What is deliberately NOT shared, and stays per-viewer in the web client's
// localStorage: scroll offsets per URL. Scroll is a view convenience, and one
// window scrolling must not move another window's page.
//
// Keys are opaque and are NOT filepath.Clean'd. A key is the client's
// `host::path` composite for a remote project (projectTerminalsKey in the web
// client) and the bare project path for a local one. Clean would corrupt
// `wsl:Ubuntu::/home/x` and Windows drive-letter paths.
//
// Every value is validated before it is stored (see Validate). The store is
// the single source of truth for these fields, so an invalid payload is
// rejected whole rather than silently repaired: a repaired value would differ
// from what the sending client holds, and the two would then fight.
package browsertabs

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/filelock"
	"github.com/u007/ocode/internal/paths"
)

// Bounds mirror the web client's caps so a payload the web produces always
// passes. MaxTitleBytes is generous because the web caps titles at 200 UTF-16
// code units, which can be up to 800 UTF-8 bytes.
const (
	MaxTabs       = 25
	MaxHistory    = 20
	MaxURLBytes   = 2048
	MaxTitleBytes = 1024
)

// Tab is one browser tab in the strip.
type Tab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// ManualTitle is the user's rename. Empty means the strip follows the page.
	ManualTitle string `json:"manual_title,omitempty"`
}

// Surface is one tab's navigation state. An empty UserMode or PageTitle means
// the value is absent (null on the web).
type Surface struct {
	URL          string   `json:"url"`
	History      []string `json:"history"`
	HistoryIndex int      `json:"history_index"`
	UserMode     string   `json:"user_mode,omitempty"`
	PageTitle    string   `json:"page_title,omitempty"`
}

// ProjectBrowsers is the shared browser state for one project key. ActiveID is
// empty when no tab is active, which happens for a strip whose only tab was
// opened in the background.
//
// Revision is server-assigned optimistic concurrency. Every accepted write of
// a project stamps it with the next store-wide counter value, so it is always
// above any revision handed out before. A client echoes back the revision it
// read, and ApplyBulk refuses the write when the stored revision has moved on.
// A project that is not stored is revision 0.
type ProjectBrowsers struct {
	Tabs     []Tab              `json:"tabs"`
	ActiveID string             `json:"active_id,omitempty"`
	Surfaces map[string]Surface `json:"surfaces"`
	Revision int64              `json:"revision"`
}

// RevisionConflictError reports that a patch was built against a stale copy of
// one project. Have is the revision the patch carried and Want is the revision
// the store holds, 0 when the project is not stored. ApplyBulk writes nothing
// when it returns this error.
type RevisionConflictError struct {
	Key  string
	Have int64
	Want int64
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("browser tabs for %q are at revision %d, not %d", e.Key, e.Want, e.Have)
}

// Validate reports the first reason pb cannot be stored. It enforces the
// structural rules the web client relies on: unique non-empty tab ids, an
// active id that names a tab, surfaces only for tabs that exist, http(s) URLs
// only, and a history index inside its history.
func (pb ProjectBrowsers) Validate() error {
	if pb.Revision < 0 {
		return fmt.Errorf("revision %d is negative", pb.Revision)
	}
	if len(pb.Tabs) > MaxTabs {
		return fmt.Errorf("%d tabs exceeds the limit of %d", len(pb.Tabs), MaxTabs)
	}
	ids := make(map[string]bool, len(pb.Tabs))
	for i, t := range pb.Tabs {
		if t.ID == "" {
			return fmt.Errorf("tab %d has no id", i)
		}
		if ids[t.ID] {
			return fmt.Errorf("tab id %q appears twice", t.ID)
		}
		ids[t.ID] = true
		if strings.TrimSpace(t.Title) == "" {
			return fmt.Errorf("tab %q has an empty title", t.ID)
		}
		if len(t.Title) > MaxTitleBytes || len(t.ManualTitle) > MaxTitleBytes {
			return fmt.Errorf("tab %q title exceeds %d bytes", t.ID, MaxTitleBytes)
		}
	}
	if pb.ActiveID != "" && !ids[pb.ActiveID] {
		return fmt.Errorf("active id %q is not a tab in this project", pb.ActiveID)
	}
	for id, s := range pb.Surfaces {
		if !ids[id] {
			return fmt.Errorf("surface %q has no tab", id)
		}
		if err := validateSurface(s); err != nil {
			return fmt.Errorf("surface %q: %w", id, err)
		}
	}
	return nil
}

func validateSurface(s Surface) error {
	if err := validateHTTPURL(s.URL); err != nil {
		return fmt.Errorf("url: %w", err)
	}
	if len(s.History) == 0 || len(s.History) > MaxHistory {
		return fmt.Errorf("history has %d entries, want 1 to %d", len(s.History), MaxHistory)
	}
	for _, h := range s.History {
		if err := validateHTTPURL(h); err != nil {
			return fmt.Errorf("history entry: %w", err)
		}
	}
	if s.HistoryIndex < 0 || s.HistoryIndex >= len(s.History) {
		return fmt.Errorf("history_index %d outside history of %d", s.HistoryIndex, len(s.History))
	}
	switch s.UserMode {
	case "", "local", "chrome":
	default:
		return fmt.Errorf("user_mode %q is not local or chrome", s.UserMode)
	}
	if len(s.PageTitle) > MaxTitleBytes {
		return fmt.Errorf("page_title exceeds %d bytes", MaxTitleBytes)
	}
	return nil
}

// validateHTTPURL accepts only absolute http(s) URLs with a host. The surface
// URL is loaded into an iframe on every client, so anything else (javascript:,
// data:, file:) must never be stored.
func validateHTTPURL(raw string) error {
	if len(raw) == 0 || len(raw) > MaxURLBytes {
		return fmt.Errorf("url must be 1 to %d bytes", MaxURLBytes)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%q is not an http or https URL", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("%q has no host", raw)
	}
	return nil
}

// Store persists per-project browser state.
type Store struct {
	mu sync.Mutex
	// path is the browser-tabs.json location; lockPath serializes
	// read-modify-write cycles across processes.
	path     string
	lockPath string
	cache    map[string]ProjectBrowsers
	// seq is the store-wide revision counter, persisted with the projects.
	seq int64
	// stamp identifies the on-disk revision this cache was loaded from, so a
	// write by another process becomes visible without re-reading every call.
	stamp fileStamp
}

// fileStamp is a cheap change-detector for the on-disk file.
type fileStamp struct {
	modTime time.Time
	size    int64
	valid   bool
}

// storeFile is the on-disk layout. Seq is the store-wide revision counter. It
// only rises, so a revision handed to a project is never handed out again, even
// after that project is deleted and re-created.
type storeFile struct {
	Seq      int64                      `json:"seq"`
	Projects map[string]ProjectBrowsers `json:"projects"`
}

// parseStoreFile reads the current layout, or a legacy file that is a bare map
// of projects. A legacy file has no counter, so the counter is seeded to the
// highest revision it holds (0 for files written before revisions existed).
func parseStoreFile(data []byte) (storeFile, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return storeFile{}, err
	}
	_, hasSeq := probe["seq"]
	_, hasProjects := probe["projects"]
	if hasSeq && hasProjects {
		var file storeFile
		if err := json.Unmarshal(data, &file); err != nil {
			return storeFile{}, err
		}
		return file, nil
	}
	var projects map[string]ProjectBrowsers
	if err := json.Unmarshal(data, &projects); err != nil {
		return storeFile{}, err
	}
	file := storeFile{Projects: projects}
	for _, pb := range projects {
		if pb.Revision > file.Seq {
			file.Seq = pb.Revision
		}
	}
	return file, nil
}

// NewStore creates or loads a browser-tab store from the global data dir.
func NewStore() (*Store, error) {
	globalDir, err := paths.GlobalDataDir()
	if err != nil {
		return nil, fmt.Errorf("browsertabs: resolve global data dir: %w", err)
	}
	return NewStoreAt(filepath.Join(globalDir, "browser-tabs.json"))
}

// NewStoreAt creates or loads a browser-tab store at an explicit JSON path.
// Tests use it to keep the store out of the real global data dir.
func NewStoreAt(path string) (*Store, error) {
	s := &Store{
		path:     path,
		lockPath: path + ".lock",
		cache:    map[string]ProjectBrowsers{},
	}
	if err := s.load(); err != nil {
		log.Printf("browsertabs: loading browser tab state: %v (starting fresh)", err)
		s.cache = map[string]ProjectBrowsers{}
		s.seq = 0
		s.stamp = fileStamp{}
	}
	return s, nil
}

// load reads the whole file into the cache and records its stamp. Callers must
// hold s.mu.
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cache = map[string]ProjectBrowsers{}
			s.seq = 0
			s.stamp = fileStamp{}
			return nil // fresh install, no state yet
		}
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		s.cache = map[string]ProjectBrowsers{}
		s.seq = 0
		s.recordStamp()
		return nil
	}
	file, err := parseStoreFile(data)
	if err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	if file.Projects == nil {
		file.Projects = map[string]ProjectBrowsers{}
	}
	s.cache = file.Projects
	s.seq = file.Seq
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
// since the last load or save. Callers must hold s.mu.
func (s *Store) reloadIfChangedLocked() {
	fi, err := os.Stat(s.path)
	if err != nil {
		if os.IsNotExist(err) && s.stamp.valid {
			// Another process deleted the file. Drop the stale cache rather than
			// resurrect it.
			s.cache = map[string]ProjectBrowsers{}
			s.stamp = fileStamp{}
		}
		return
	}
	if s.stamp.valid && fi.ModTime().Equal(s.stamp.modTime) && fi.Size() == s.stamp.size {
		return
	}
	if err := s.load(); err != nil {
		log.Printf("browsertabs: reloading %s: %v (keeping cached state)", s.path, err)
	}
}

// saveLocked writes the cache atomically (temp file + rename) so a concurrent
// reader never observes a half-written file. Callers must hold s.mu.
func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(storeFile{Seq: s.seq, Projects: s.cache}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal browser tabs: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".browser-tabs-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp browser tabs file: %w", err)
	}
	tmpName := tmp.Name()
	// Remove the temp file on every failure path; after a successful rename
	// this is a harmless no-op.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp browser tabs file: %w", err)
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp browser tabs file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp browser tabs file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("rename temp browser tabs file: %w", err)
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

// All returns a copy of every project's browser state, keyed by project key.
func (s *Store) All() map[string]ProjectBrowsers {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadIfChangedLocked()
	out := make(map[string]ProjectBrowsers, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}

// ApplyBulk merges per-project browser state into the persisted state. Every
// key in patch replaces that project's entry. A project whose strip is empty
// is deleted. Projects absent from patch are preserved.
//
// The merge is load-bearing for multi-window safety: a client only sends the
// projects it knows about, so a client that has never seen another client's
// project must not drop it. Deleting a project is expressed by sending an empty
// strip for it, so the client must send that explicit empty entry when the user
// closes a project's last tab.
//
// The whole read-modify-write cycle runs under the cross-process file lock
// against freshly reloaded state, so concurrent writers merge. Callers must
// pass every entry through Validate first: the HTTP handler does, and answers
// 400 on failure, so an invalid entry never reaches this method.
//
// Every entry must carry the revision the store holds for its project (0 when
// the project is not stored), including an entry that deletes a project. The
// revisions of all entries are checked before any entry is written. A mismatch
// returns a *RevisionConflictError and writes nothing, so a stale client never
// partially overwrites state it has not seen.
//
// A written project takes the next value of the store-wide counter, so a
// revision is never reused. That holds even when the project is deleted and
// re-created: the re-created project gets a higher revision than any a stale
// client could still hold. The revision a patch carries is not kept.
func (s *Store) ApplyBulk(patch map[string]ProjectBrowsers) error {
	return s.withLock(func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reloadIfChangedLocked()
		keys := make([]string, 0, len(patch))
		for key := range patch {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			want := s.cache[key].Revision
			if have := patch[key].Revision; have != want {
				return &RevisionConflictError{Key: key, Have: have, Want: want}
			}
		}
		for _, key := range keys {
			pb := patch[key]
			if len(pb.Tabs) == 0 {
				delete(s.cache, key)
				continue
			}
			// A nil map would marshal as JSON null, which no reader expects for
			// a project that has tabs but no navigation state yet.
			if pb.Surfaces == nil {
				pb.Surfaces = map[string]Surface{}
			}
			s.seq++
			pb.Revision = s.seq
			s.cache[key] = pb
		}
		return s.saveLocked()
	})
}
