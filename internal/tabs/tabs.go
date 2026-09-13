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
package tabs

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

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
	mu    sync.Mutex
	path  string
	cache map[string]ProjectTabs
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
		path:  path,
		cache: map[string]ProjectTabs{},
	}
	if err := s.load(); err != nil {
		log.Printf("tabs: loading tab state: %v (starting fresh)", err)
		s.cache = map[string]ProjectTabs{}
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh install, no state yet
		}
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &s.cache); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	if s.cache == nil {
		s.cache = map[string]ProjectTabs{}
	}
	return nil
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tabs: %w", err)
	}
	// Ensure the directory exists (first save after fresh install).
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	return nil
}

// Get returns the tab state for a project root. A missing project yields an
// empty (zero) ProjectTabs without error.
func (s *Store) Get(path string) ProjectTabs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache[filepath.Clean(path)]
}

// All returns a copy of every project's tab state, keyed by project root.
func (s *Store) All() map[string]ProjectTabs {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]ProjectTabs, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}

// ReplaceAll swaps the entire tab state for the given map — a full
// replacement, so projects absent from all are dropped.
func (s *Store) ReplaceAll(all map[string]ProjectTabs) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]ProjectTabs, len(all))
	for k, v := range all {
		next[filepath.Clean(k)] = v
	}
	s.cache = next
	return s.save()
}

// Set stores the tab state for a project root.
func (s *Store) Set(path string, pt ProjectTabs) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[filepath.Clean(path)] = pt
	return s.save()
}

// Remove deletes the tab state for a project root (e.g. when the project is
// removed). Deleting a non-existent key is a no-op.
func (s *Store) Remove(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cleaned := filepath.Clean(path)
	if _, ok := s.cache[cleaned]; !ok {
		return nil
	}
	delete(s.cache, cleaned)
	return s.save()
}
