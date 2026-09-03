package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/session"
)

// HandleListProjects returns all saved project roots.
func (h *Handler) HandleListProjects(w http.ResponseWriter, _ *http.Request) {
	if h.projects == nil {
		writeJSON(w, http.StatusOK, []projects.Project{})
		return
	}
	list := h.projects.List()
	if list == nil {
		list = []projects.Project{}
	}
	writeJSON(w, http.StatusOK, list)
}

// HandleAddProject adds a new project root to the saved list.
// Accepts either a local project (`{path}`) or a remote project
// (`{host, path}` where host is `[user@]host` or `wsl:<distro>`).
func (h *Handler) HandleAddProject(w http.ResponseWriter, r *http.Request) {
	if h.projects == nil {
		writeError(w, http.StatusInternalServerError, "project store not available")
		return
	}

	var body struct {
		Path string `json:"path"`
		Host string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	var err error
	if body.Host != "" {
		err = h.projects.AddRemote(body.Host, body.Path)
	} else {
		err = h.projects.Add(body.Path)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("add project: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleRemoveProject removes a project root from the saved list.
// For remote projects pass `?host=<host>` to scope the deletion to that
// (host, path) entry. Without `?host` the legacy local-only path is used.
func (h *Handler) HandleRemoveProject(w http.ResponseWriter, r *http.Request) {
	if h.projects == nil {
		writeError(w, http.StatusInternalServerError, "project store not available")
		return
	}

	// The path is URL-encoded in the path value.
	path := r.PathValue("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	host := r.URL.Query().Get("host")
	var err error
	if host != "" {
		err = h.projects.RemoveRemote(host, path)
	} else {
		err = h.projects.Remove(path)
	}
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("remove project: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleListProjectSessions returns sessions scoped to a specific project root.
// The project root is passed as a query parameter `path` (URL-encoded).
func (h *Handler) HandleListProjectSessions(w http.ResponseWriter, r *http.Request) {
	projectPath := r.URL.Query().Get("path")
	if projectPath == "" {
		writeError(w, http.StatusBadRequest, "path query parameter is required")
		return
	}

	// Verify this is a saved project.
	if h.projects != nil {
		found := false
		for _, p := range h.projects.List() {
			if p.Path == projectPath {
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "project not found in saved list")
			return
		}
	}

	refs, err := session.ListRefsForDir(projectPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list sessions: %v", err))
		return
	}

	result := make([]SessionInfo, 0, len(refs))
	for _, ref := range refs {
		result = append(result, SessionInfo{
			ID:        ref.ID,
			Title:     ref.Title,
			CreatedAt: ref.CreatedAt.Format(time.RFC3339),
			UpdatedAt: ref.UpdatedAt.Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// DirectoryEntry is a single directory in a browse listing.
type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// BrowseResponse is returned by the directory browser endpoint.
type BrowseResponse struct {
	CurrentPath string           `json:"current_path"`
	ParentPath  string           `json:"parent_path"`
	Directories []DirectoryEntry `json:"directories"`
}

// HandleBrowseDirectory lists subdirectories at the given path for the
// folder browser UI. The path is provided as a query parameter.
func (h *Handler) HandleBrowseDirectory(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	// Determine which directory to list.
	dir := path
	if dir == "" {
		// No path → list filesystem roots (platform-dependent).
		entries, err := listRoots()
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("list roots: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, BrowseResponse{
			CurrentPath: "",
			ParentPath:  "",
			Directories: entries,
		})
		return
	}

	// Resolve to absolute, clean path.
	absPath, err := filepath.Abs(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid path: %v", err))
		return
	}

	// Verify it's a directory.
	info, err := os.Stat(absPath)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("path not found: %v", err))
		return
	}
	if !info.IsDir() {
		writeError(w, http.StatusBadRequest, "path is not a directory")
		return
	}

	// Read directory contents.
	f, err := os.Open(absPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("open directory: %v", err))
		return
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("read directory: %v", err))
		return
	}

	var dirs []DirectoryEntry
	for _, name := range names {
		// Skip hidden directories on Unix.
		if name[0] == '.' {
			continue
		}
		fullPath := filepath.Join(absPath, name)
		if fi, sterr := os.Stat(fullPath); sterr == nil && fi.IsDir() {
			// Check readability.
			readable := true
			df, oerr := os.Open(fullPath)
			if oerr != nil {
				readable = false
			} else {
				df.Close()
			}
			if readable {
				dirs = append(dirs, DirectoryEntry{Name: name, Path: fullPath})
			}
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name < dirs[j].Name
	})

	parentPath := filepath.Dir(absPath)
	// If parent is same as current (root directory), clear it so the UI
	// knows there is no parent to navigate to.
	if parentPath == absPath {
		parentPath = ""
	}

	writeJSON(w, http.StatusOK, BrowseResponse{
		CurrentPath: absPath,
		ParentPath:  parentPath,
		Directories: dirs,
	})
}

// listRoots returns the filesystem root directories for the current platform.
func listRoots() ([]DirectoryEntry, error) {
	var roots []DirectoryEntry

	// Unix-like: single root "/".
	if filepath.VolumeName("/") == "" {
		roots = append(roots, DirectoryEntry{Name: "/", Path: "/"})
	} else {
		// Windows: enumerate drives A:-Z.
		for d := 'A'; d <= 'Z'; d++ {
			root := string(d) + ":\\"
			if _, err := os.Stat(root); err == nil {
				roots = append(roots, DirectoryEntry{Name: root, Path: root})
			}
		}
	}

	// Also include the user's home directory for convenience.
	if home, err := os.UserHomeDir(); err == nil {
		homeName := filepath.Base(home)
		if homeName == "" {
			homeName = home
		}
		roots = append(roots, DirectoryEntry{Name: "~ (" + homeName + ")", Path: home})
	}

	return roots, nil
}

// ── Rename / Reorder / Group endpoints ─────────────────────────────────────

// HandleRenameProject changes the display name of a project.
func (h *Handler) HandleRenameProject(w http.ResponseWriter, r *http.Request) {
	if h.projects == nil {
		writeError(w, http.StatusInternalServerError, "project store not available")
		return
	}
	var body struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.projects.Rename(body.Path, body.Name); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("rename project: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleReorderProjects sets the manual sort order for all projects.
func (h *Handler) HandleReorderProjects(w http.ResponseWriter, r *http.Request) {
	if h.projects == nil {
		writeError(w, http.StatusInternalServerError, "project store not available")
		return
	}
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if len(body.Paths) == 0 {
		writeError(w, http.StatusBadRequest, "paths is required")
		return
	}
	if err := h.projects.Reorder(body.Paths); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("reorder projects: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleSetProjectGroup assigns a project to a group (or clears it).
func (h *Handler) HandleSetProjectGroup(w http.ResponseWriter, r *http.Request) {
	if h.projects == nil {
		writeError(w, http.StatusInternalServerError, "project store not available")
		return
	}
	var body struct {
		Path  string `json:"path"`
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := h.projects.SetGroup(body.Path, body.Group); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("set group: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Group endpoints ────────────────────────────────────────────────────────

// HandleListGroups returns all project groups.
func (h *Handler) HandleListGroups(w http.ResponseWriter, _ *http.Request) {
	if h.projectGroups == nil {
		writeJSON(w, http.StatusOK, []projects.ProjectGroup{})
		return
	}
	groups := h.projectGroups.ListGroups()
	if groups == nil {
		groups = []projects.ProjectGroup{}
	}
	writeJSON(w, http.StatusOK, groups)
}

// HandleCreateGroup creates a new project group.
func (h *Handler) HandleCreateGroup(w http.ResponseWriter, r *http.Request) {
	if h.projectGroups == nil {
		writeError(w, http.StatusInternalServerError, "group store not available")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.projectGroups.CreateGroup(body.Name); err != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("create group: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleDeleteGroup removes a group and ungroups its projects.
func (h *Handler) HandleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if h.projectGroups == nil {
		writeError(w, http.StatusInternalServerError, "group store not available")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Ungroup all projects in this group.
	if h.projects != nil {
		for _, p := range h.projects.List() {
			if p.Group == name {
				_ = h.projects.SetGroup(p.Path, "")
			}
		}
	}

	if err := h.projectGroups.DeleteGroup(name); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("delete group: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleRenameGroup changes the name of a group.
func (h *Handler) HandleRenameGroup(w http.ResponseWriter, r *http.Request) {
	if h.projectGroups == nil {
		writeError(w, http.StatusInternalServerError, "group store not available")
		return
	}
	var body struct {
		OldName string `json:"old_name"`
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.OldName == "" {
		writeError(w, http.StatusBadRequest, "old_name is required")
		return
	}
	if body.NewName == "" {
		writeError(w, http.StatusBadRequest, "new_name is required")
		return
	}
	projs, err := h.projectGroups.RenameGroup(body.OldName, body.NewName, h.projects)
	if err != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("rename group: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, projs)
}

// HandleReorderGroups sets the order for all groups.
func (h *Handler) HandleReorderGroups(w http.ResponseWriter, r *http.Request) {
	if h.projectGroups == nil {
		writeError(w, http.StatusInternalServerError, "group store not available")
		return
	}
	var body struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if len(body.Names) == 0 {
		writeError(w, http.StatusBadRequest, "names is required")
		return
	}
	if err := h.projectGroups.ReorderGroups(body.Names); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("reorder groups: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleSetGroupCollapsed toggles the collapsed state of a group.
func (h *Handler) HandleSetGroupCollapsed(w http.ResponseWriter, r *http.Request) {
	if h.projectGroups == nil {
		writeError(w, http.StatusInternalServerError, "group store not available")
		return
	}
	var body struct {
		Name      string `json:"name"`
		Collapsed bool   `json:"collapsed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.projectGroups.SetCollapsed(body.Name, body.Collapsed); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("set collapsed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
