// Package projects manages the list of project roots for the desktop (and web)
// multi-project UI. The list is stored as a JSON array under the global data dir.
package projects

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/u007/ocode/internal/paths"
)

// Project represents a saved project root.
type Project struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	AddedAt    time.Time `json:"added_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// Store persists the list of project roots.
type Store struct {
	mu    sync.Mutex
	path  string
	cache []Project
}

// NewStore creates or loads a project store from the global data dir.
func NewStore() (*Store, error) {
	globalDir, err := paths.GlobalDataDir()
	if err != nil {
		return nil, fmt.Errorf("projects: resolve global data dir: %w", err)
	}
	s := &Store{
		path: filepath.Join(globalDir, "projects.json"),
	}
	if err := s.load(); err != nil {
		log.Printf("projects: loading projects list: %v (starting fresh)", err)
		s.cache = nil
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var list []Project
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	s.cache = list
	return nil
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal projects: %w", err)
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

// List returns all saved projects, sorted by last used time (most recent first).
func (s *Store) List() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Project, len(s.cache))
	copy(out, s.cache)
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastUsedAt.After(out[j].LastUsedAt)
	})
	return out
}

// Add inserts a project root, or updates its LastUsedAt if already present.
func (s *Store) Add(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned := filepath.Clean(path)
	now := time.Now()

	// Update existing entry.
	for i := range s.cache {
		if s.cache[i].Path == cleaned {
			s.cache[i].LastUsedAt = now
			return s.save()
		}
	}

	// Derive name from the directory base name.
	name := filepath.Base(cleaned)

	s.cache = append(s.cache, Project{
		Path:       cleaned,
		Name:       name,
		AddedAt:    now,
		LastUsedAt: now,
	})
	return s.save()
}

// Remove deletes a project root from the list.
func (s *Store) Remove(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned := filepath.Clean(path)
	idx := -1
	for i, p := range s.cache {
		if p.Path == cleaned {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("project %q not found", path)
	}
	s.cache = append(s.cache[:idx], s.cache[idx+1:]...)
	return s.save()
}

// Touch updates the LastUsedAt for a project, so it rises to the top of the list.
func (s *Store) Touch(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned := filepath.Clean(path)
	for i := range s.cache {
		if s.cache[i].Path == cleaned {
			s.cache[i].LastUsedAt = time.Now()
			return s.save()
		}
	}
	return nil
}
