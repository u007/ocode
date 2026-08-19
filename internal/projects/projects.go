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
	Order      int       `json:"order"` // manual sort position (lower = higher)
	Group      string    `json:"group"` // group name, "" = ungrouped
}

// ProjectGroup represents a named group of projects.
type ProjectGroup struct {
	Name      string `json:"name"`
	Order     int    `json:"order"`
	Collapsed bool   `json:"collapsed"`
}

// Store persists the list of project roots.
type Store struct {
	mu    sync.Mutex
	path  string
	cache []Project
}

// GroupStore persists project group definitions.
type GroupStore struct {
	mu    sync.Mutex
	path  string
	cache []ProjectGroup
}

// NewStore creates or loads a project store from the global data dir.
// It also returns a GroupStore for managing project groups.
func NewStore() (*Store, *GroupStore, error) {
	globalDir, err := paths.GlobalDataDir()
	if err != nil {
		return nil, nil, fmt.Errorf("projects: resolve global data dir: %w", err)
	}
	s, err := NewStoreAt(filepath.Join(globalDir, "projects.json"))
	if err != nil {
		return nil, nil, err
	}
	gs, err := NewGroupStoreAt(filepath.Join(globalDir, "project_groups.json"))
	if err != nil {
		return nil, nil, err
	}
	return s, gs, nil
}

// NewStoreAt creates or loads a project store rooted at an explicit JSON path.
// Used by the server tests to keep the store out of the real global data dir.
func NewStoreAt(path string) (*Store, error) {
	s := &Store{path: path}
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

// List returns all saved projects, sorted by manual Order (ascending).
// Projects without an explicit order are appended at the end, sorted by AddedAt.
func (s *Store) List() []Project {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Project, len(s.cache))
	copy(out, s.cache)
	sort.SliceStable(out, func(i, j int) bool {
		// Projects with Order=0 (legacy/unassigned) go to the end.
		if out[i].Order == 0 && out[j].Order == 0 {
			return out[i].AddedAt.Before(out[j].AddedAt)
		}
		if out[i].Order == 0 {
			return false
		}
		if out[j].Order == 0 {
			return true
		}
		return out[i].Order < out[j].Order
	})
	return out
}

// Add inserts a project root, or updates its LastUsedAt if already present.
// New projects are appended with Order=0 (will sort to end until reordered).
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

// Rename changes the display name of a project.
func (s *Store) Rename(path, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned := filepath.Clean(path)
	for i := range s.cache {
		if s.cache[i].Path == cleaned {
			s.cache[i].Name = name
			return s.save()
		}
	}
	return fmt.Errorf("project %q not found", path)
}

// Reorder sets the manual sort order for all projects. The paths slice
// defines the new order (first = lowest Order value = highest position).
func (s *Store) Reorder(paths []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build a lookup of path → index for the incoming order.
	orderMap := make(map[string]int, len(paths))
	for i, p := range paths {
		orderMap[filepath.Clean(p)] = i + 1 // 1-based
	}

	// Apply order to cache. Projects not in the reorder list keep their
	// existing order (but this shouldn't happen in normal usage).
	for i := range s.cache {
		if order, ok := orderMap[s.cache[i].Path]; ok {
			s.cache[i].Order = order
		}
	}
	return s.save()
}

// SetGroup assigns a project to a group, or clears its group if group is "".
func (s *Store) SetGroup(path, group string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned := filepath.Clean(path)
	for i := range s.cache {
		if s.cache[i].Path == cleaned {
			s.cache[i].Group = group
			return s.save()
		}
	}
	return fmt.Errorf("project %q not found", path)
}

// ── GroupStore ─────────────────────────────────────────────────────────────

// NewGroupStoreAt creates or loads a group store at an explicit JSON path.
func NewGroupStoreAt(path string) (*GroupStore, error) {
	gs := &GroupStore{path: path}
	if err := gs.load(); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("projects: loading groups: %w", err)
		}
	}
	return gs, nil
}

func (gs *GroupStore) load() error {
	data, err := os.ReadFile(gs.path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var list []ProjectGroup
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse %s: %w", gs.path, err)
	}
	gs.cache = list
	return nil
}

func (gs *GroupStore) save() error {
	data, err := json.MarshalIndent(gs.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal groups: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(gs.path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(gs.path), ".project-groups-*")
	if err != nil {
		return fmt.Errorf("create temp groups file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp groups file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", gs.path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", gs.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp groups file: %w", err)
	}
	if err := os.Rename(tmpName, gs.path); err != nil {
		return fmt.Errorf("replace %s: %w", gs.path, err)
	}
	return nil
}

// ListGroups returns all groups sorted by Order.
func (gs *GroupStore) ListGroups() []ProjectGroup {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	out := make([]ProjectGroup, len(gs.cache))
	copy(out, gs.cache)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})
	return out
}

// CreateGroup adds a new group. Returns error if the name already exists.
func (gs *GroupStore) CreateGroup(name string) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	for _, g := range gs.cache {
		if g.Name == name {
			return fmt.Errorf("group %q already exists", name)
		}
	}

	// Append with order at the end.
	maxOrder := 0
	for _, g := range gs.cache {
		if g.Order > maxOrder {
			maxOrder = g.Order
		}
	}
	gs.cache = append(gs.cache, ProjectGroup{
		Name:  name,
		Order: maxOrder + 1,
	})
	return gs.save()
}

// DeleteGroup removes a group. Projects in this group should have their
// Group field cleared by the caller before calling this.
func (gs *GroupStore) DeleteGroup(name string) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	idx := -1
	for i, g := range gs.cache {
		if g.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("group %q not found", name)
	}
	gs.cache = append(gs.cache[:idx], gs.cache[idx+1:]...)
	return gs.save()
}

// RenameGroup changes the name of a group and updates all projects
// that reference the old name. Returns the updated project list.
func (gs *GroupStore) RenameGroup(oldName, newName string, projects *Store) ([]Project, error) {
	if projects == nil {
		return nil, fmt.Errorf("projects store is nil")
	}
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check new name doesn't already exist.
	for _, g := range gs.cache {
		if g.Name == newName {
			return nil, fmt.Errorf("group %q already exists", newName)
		}
	}
	oldGroups := append([]ProjectGroup(nil), gs.cache...)

	found := false
	for i, g := range gs.cache {
		if g.Name == oldName {
			gs.cache[i].Name = newName
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("group %q not found", oldName)
	}
	if err := gs.save(); err != nil {
		gs.cache = oldGroups
		return nil, err
	}

	// Update all projects that reference the old group name.
	projects.mu.Lock()
	defer projects.mu.Unlock()
	oldProjects := append([]Project(nil), projects.cache...)
	for i := range projects.cache {
		if projects.cache[i].Group == oldName {
			projects.cache[i].Group = newName
		}
	}
	if err := projects.save(); err != nil {
		projects.cache = oldProjects
		// Best-effort rollback: keep the two files aligned if the second write
		// fails. The original error remains the actionable result.
		if rollbackErr := gs.restoreLocked(oldGroups); rollbackErr != nil {
			return nil, fmt.Errorf("%w (rollback failed: %v)", err, rollbackErr)
		}
		return nil, err
	}

	out := make([]Project, len(projects.cache))
	copy(out, projects.cache)
	return out, nil
}

// restoreLocked restores group state while the caller holds gs.mu.
func (gs *GroupStore) restoreLocked(groups []ProjectGroup) error {
	gs.cache = append([]ProjectGroup(nil), groups...)
	return gs.save()
}

// ReorderGroups sets the order for all groups.
func (gs *GroupStore) ReorderGroups(names []string) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	orderMap := make(map[string]int, len(names))
	for i, n := range names {
		orderMap[n] = i + 1
	}

	for i := range gs.cache {
		if order, ok := orderMap[gs.cache[i].Name]; ok {
			gs.cache[i].Order = order
		}
	}
	return gs.save()
}

// SetCollapsed sets the collapsed state of a group.
func (gs *GroupStore) SetCollapsed(name string, collapsed bool) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	for i := range gs.cache {
		if gs.cache[i].Name == name {
			gs.cache[i].Collapsed = collapsed
			return gs.save()
		}
	}
	return fmt.Errorf("group %q not found", name)
}
