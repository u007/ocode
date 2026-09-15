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
	"github.com/u007/ocode/internal/remote"
)

// Project represents a saved project root.
type Project struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	AddedAt    time.Time `json:"added_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Order      int       `json:"order"` // manual sort position (lower = higher)
	Group      string    `json:"group"` // group name, "" = ungrouped
	// Host identifies the ocode Remote target this project lives on
	// (Target.String() — "[user@]host" or "wsl:<distro>"). Empty means a
	// local project. Local (Add/Remove/Touch/Rename/Reorder/SetGroup) and
	// remote (AddRemote/TouchRemote/FindRemote) operations key on Path
	// scoped to Host so a remote project never collides with a local one,
	// or with a same-path project on a different host.
	Host string `json:"host,omitempty"`
	// Structured remote fields are persisted alongside Host so older clients
	// can continue to use the canonical target while newer clients can edit a
	// username, hostname, port, or WSL distro independently.
	RemoteKind   string `json:"remote_kind,omitempty"`
	RemoteUser   string `json:"remote_user,omitempty"`
	RemoteHost   string `json:"remote_host,omitempty"`
	RemotePort   int    `json:"remote_port,omitempty"`
	RemoteDistro string `json:"remote_distro,omitempty"`
	// PortMaps is user-added extra SSH -L forwards (beyond the fixed
	// api/browse tunnel `ocode remote <host> --web` always opens), managed
	// by the `/port` command during a --web session. Meaningless for local
	// (Host == "") and WSL (RemoteKind == "wsl") projects — WSL2 already
	// shares localhost with Windows natively, so `/port` is a no-op there.
	PortMaps []PortMap `json:"port_maps,omitempty"`
}

// PortMap is one user-added "remote:local" TCP forward on top of the fixed
// api/browse tunnel, keyed by RemotePort (one entry per remote port).
// Enabled tracks the persisted intent; the live SSH child process for it
// exists only while a --web session is connected and Enabled is true (see
// internal/remote's port-forward manager).
type PortMap struct {
	RemotePort int  `json:"remote_port"`
	LocalPort  int  `json:"local_port"`
	Enabled    bool `json:"enabled"`
}

// ProjectRef identifies a project entry for scoped mutations (rename,
// group, reorder, remote update). Host scopes the match: "" targets a
// local project (path is filepath.Cleaned), non-empty targets a remote
// (host, path) entry (path matched verbatim — remote separator
// conventions are the remote's, not this machine's).
type ProjectRef struct {
	Path string
	Host string
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
	migrated := false
	for i := range list {
		if list[i].Host == "" || list[i].RemoteKind != "" {
			continue
		}
		target, parseErr := remote.ParseTarget(list[i].Host)
		if parseErr != nil {
			return fmt.Errorf("migrate remote project %q: %w", list[i].Host, parseErr)
		}
		setRemoteFields(&list[i], target)
		migrated = true
	}
	s.cache = list
	if migrated {
		if err := s.save(); err != nil {
			return fmt.Errorf("save migrated projects: %w", err)
		}
	}
	return nil
}

func setRemoteFields(p *Project, target remote.Target) {
	p.RemoteKind = "ssh"
	if target.Kind == remote.KindWSL {
		p.RemoteKind = "wsl"
	}
	p.RemoteUser = target.User
	p.RemoteHost = target.Host
	p.RemotePort = target.Port
	p.RemoteDistro = target.Distro
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
		if s.cache[i].Path == cleaned && s.cache[i].Host == "" {
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
		if p.Path == cleaned && p.Host == "" {
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

// RemoveRemote deletes a remote (host, path) entry. Path is matched verbatim
// (not Cleaned) for the same reason as AddRemote.
func (s *Store) RemoveRemote(host, path string) error {
	if host == "" {
		return fmt.Errorf("projects: RemoveRemote requires a non-empty host")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, p := range s.cache {
		if p.Host == host && p.Path == path {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("remote project %s:%s not found", host, path)
	}
	s.cache = append(s.cache[:idx], s.cache[idx+1:]...)
	return s.save()
}

// findRemoteIdx locates ref's cache entry. ref.Host must be non-empty (this
// helper backs the PortMap operations, which only apply to remote/WSL
// projects).
func (s *Store) findRemoteIdx(ref ProjectRef) (int, error) {
	if ref.Host == "" {
		return -1, fmt.Errorf("projects: port maps require a remote project (empty host)")
	}
	for i, p := range s.cache {
		if p.Host == ref.Host && p.Path == ref.Path {
			return i, nil
		}
	}
	return -1, fmt.Errorf("remote project %s:%s not found", ref.Host, ref.Path)
}

// PortMaps returns ref's persisted extra port forwards.
func (s *Store) PortMaps(ref ProjectRef) ([]PortMap, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.findRemoteIdx(ref)
	if err != nil {
		return nil, err
	}
	out := make([]PortMap, len(s.cache[idx].PortMaps))
	copy(out, s.cache[idx].PortMaps)
	return out, nil
}

// AddPortMap upserts a forward for remotePort (matched by RemotePort),
// enabled by default, and persists it.
func (s *Store) AddPortMap(ref ProjectRef, remotePort, localPort int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.findRemoteIdx(ref)
	if err != nil {
		return err
	}
	maps := s.cache[idx].PortMaps
	for i := range maps {
		if maps[i].RemotePort == remotePort {
			maps[i].LocalPort = localPort
			maps[i].Enabled = true
			return s.save()
		}
	}
	s.cache[idx].PortMaps = append(maps, PortMap{RemotePort: remotePort, LocalPort: localPort, Enabled: true})
	return s.save()
}

// RemovePortMap deletes remotePort's forward entirely.
func (s *Store) RemovePortMap(ref ProjectRef, remotePort int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.findRemoteIdx(ref)
	if err != nil {
		return err
	}
	maps := s.cache[idx].PortMaps
	for i := range maps {
		if maps[i].RemotePort == remotePort {
			s.cache[idx].PortMaps = append(maps[:i], maps[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("port map for remote port %d not found", remotePort)
}

// SetPortMapEnabled flips remotePort's persisted Enabled flag.
func (s *Store) SetPortMapEnabled(ref ProjectRef, remotePort int, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.findRemoteIdx(ref)
	if err != nil {
		return err
	}
	maps := s.cache[idx].PortMaps
	for i := range maps {
		if maps[i].RemotePort == remotePort {
			maps[i].Enabled = enabled
			return s.save()
		}
	}
	return fmt.Errorf("port map for remote port %d not found", remotePort)
}

// Touch updates the LastUsedAt for a project, so it rises to the top of the list.
func (s *Store) Touch(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned := filepath.Clean(path)
	for i := range s.cache {
		if s.cache[i].Path == cleaned && s.cache[i].Host == "" {
			s.cache[i].LastUsedAt = time.Now()
			return s.save()
		}
	}
	return nil
}

// matchRef reports whether a cached entry matches a ProjectRef.
// Local refs (Host == "") match by cleaned path against local entries
// only. Remote refs match (Host, Path) verbatim — never Cleaned, since
// remote separator conventions are the remote's, not this machine's.
func matchRef(p Project, ref ProjectRef) bool {
	if ref.Host != "" {
		return p.Host == ref.Host && p.Path == ref.Path
	}
	return p.Host == "" && p.Path == filepath.Clean(ref.Path)
}

// refOrderKey is the identity key for reorder maps. Local entries key on
// the cleaned path, remote entries on host + verbatim path, so the same
// path on two hosts (or local vs remote) never collides.
func refOrderKey(host, path string) string {
	if host != "" {
		return host + "\x00" + path
	}
	return "\x00" + filepath.Clean(path)
}

// Rename changes the display name of a project.
func (s *Store) Rename(path, name string) error {
	return s.RenameRef(ProjectRef{Path: path}, name)
}

// RenameRef changes the display name of the project identified by ref.
// A local ref (Host == "") never matches a remote entry sharing the same
// path string, and vice versa.
func (s *Store) RenameRef(ref ProjectRef, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.cache {
		if matchRef(s.cache[i], ref) {
			s.cache[i].Name = name
			return s.save()
		}
	}
	if ref.Host != "" {
		return fmt.Errorf("remote project %s:%s not found", ref.Host, ref.Path)
	}
	return fmt.Errorf("project %q not found", ref.Path)
}

// AddRemote upserts a remote project entry, identified by (host, path)
// rather than path alone — host is a Target.String() value
// ("[user@]host" or "wsl:<distro>"), so the same remote path on two
// different hosts stays two distinct entries. path is used verbatim (no
// filepath.Clean — a remote path's separator conventions are the remote's,
// not this machine's, and "~" is meaningful only to the remote shell).
func (s *Store) AddRemote(host, path string, ports ...int) error {
	target, err := remote.ParseTarget(host)
	if err != nil {
		return fmt.Errorf("projects: add remote: %w", err)
	}
	if len(ports) > 0 {
		target.Port = ports[0]
	}
	if err := target.Validate(); err != nil {
		return fmt.Errorf("projects: add remote: %w", err)
	}
	canonicalHost := target.String()
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for i := range s.cache {
		if s.cache[i].Host == canonicalHost && s.cache[i].Path == path {
			s.cache[i].LastUsedAt = now
			setRemoteFields(&s.cache[i], target)
			return s.save()
		}
	}

	project := Project{
		Path:       path,
		Name:       canonicalHost + ":" + path,
		Host:       target.String(),
		AddedAt:    now,
		LastUsedAt: now,
	}
	setRemoteFields(&project, target)
	s.cache = append(s.cache, project)
	return s.save()
}

// UpdateRemote atomically changes a saved remote project's connection and/or
// remote path. Identity is located by the old (host, path) pair; collisions
// with another saved project are rejected without changing either entry.
func (s *Store) UpdateRemote(oldRef ProjectRef, target remote.Target, path string) (Project, error) {
	if oldRef.Host == "" || oldRef.Path == "" {
		return Project{}, fmt.Errorf("projects: old remote reference is required")
	}
	if path == "" {
		return Project{}, fmt.Errorf("projects: remote path is required")
	}
	if err := target.Validate(); err != nil {
		return Project{}, fmt.Errorf("projects: invalid remote target: %w", err)
	}
	newHost := target.String()
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.cache {
		if s.cache[i].Host == oldRef.Host && s.cache[i].Path == oldRef.Path {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Project{}, fmt.Errorf("remote project %s:%s not found", oldRef.Host, oldRef.Path)
	}
	for i := range s.cache {
		if i != idx && s.cache[i].Host == newHost && s.cache[i].Path == path {
			return Project{}, fmt.Errorf("remote project %s:%s already exists", newHost, path)
		}
	}
	updated := s.cache[idx]
	updated.Host = newHost
	updated.Path = path
	setRemoteFields(&updated, target)
	s.cache[idx] = updated
	if err := s.save(); err != nil {
		return Project{}, err
	}
	return updated, nil
}

// TouchRemote updates LastUsedAt for a remote (host, path) entry. Unlike
// Touch, a missing entry is not silently ignored — callers use this only
// after a successful AddRemote/connect, so a miss indicates a caller bug.
func (s *Store) TouchRemote(host, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.cache {
		if s.cache[i].Host == host && s.cache[i].Path == path {
			s.cache[i].LastUsedAt = time.Now()
			return s.save()
		}
	}
	return fmt.Errorf("remote project %s:%s not found", host, path)
}

// FindLastRemote returns the most-recently-used project entry for host, if
// any — the "omitted path → last remote project for that host" default
// from the connect flow.
func (s *Store) FindLastRemote(host string) (Project, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var best Project
	found := false
	for _, p := range s.cache {
		if p.Host != host {
			continue
		}
		if !found || p.LastUsedAt.After(best.LastUsedAt) {
			best = p
			found = true
		}
	}
	return best, found
}

// Reorder sets the manual sort order for all projects. The paths slice
// defines the new order (first = lowest Order value = highest position).
func (s *Store) Reorder(paths []string) error {
	refs := make([]ProjectRef, 0, len(paths))
	for _, p := range paths {
		refs = append(refs, ProjectRef{Path: p})
	}
	return s.ReorderRefs(refs)
}

// ReorderRefs sets the manual sort order for all projects, keyed by
// scoped identity so remote entries sharing a path string with a local
// project (or with a project on another host) keep their own order.
func (s *Store) ReorderRefs(refs []ProjectRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build a lookup of identity key → index for the incoming order.
	orderMap := make(map[string]int, len(refs))
	for i, r := range refs {
		orderMap[refOrderKey(r.Host, r.Path)] = i + 1 // 1-based
	}

	// Apply order to cache. Projects not in the reorder list keep their
	// existing order (but this shouldn't happen in normal usage).
	for i := range s.cache {
		if order, ok := orderMap[refOrderKey(s.cache[i].Host, s.cache[i].Path)]; ok {
			s.cache[i].Order = order
		}
	}
	return s.save()
}

// SetGroup assigns a project to a group, or clears its group if group is "".
func (s *Store) SetGroup(path, group string) error {
	return s.SetGroupRef(ProjectRef{Path: path}, group)
}

// SetGroupRef assigns the project identified by ref to a group, or clears
// its group if group is "". Scoped like RenameRef: a local ref never
// touches a remote entry sharing the same path string, and vice versa.
func (s *Store) SetGroupRef(ref ProjectRef, group string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.cache {
		if matchRef(s.cache[i], ref) {
			s.cache[i].Group = group
			return s.save()
		}
	}
	if ref.Host != "" {
		return fmt.Errorf("remote project %s:%s not found", ref.Host, ref.Path)
	}
	return fmt.Errorf("project %q not found", ref.Path)
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
