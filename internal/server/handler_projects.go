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
	writeJSON(w, http.StatusOK, list)
}

// HandleAddProject adds a new project root to the saved list.
func (h *Handler) HandleAddProject(w http.ResponseWriter, r *http.Request) {
	if h.projects == nil {
		writeError(w, http.StatusInternalServerError, "project store not available")
		return
	}

	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	if err := h.projects.Add(body.Path); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("add project: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleRemoveProject removes a project root from the saved list.
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

	if err := h.projects.Remove(path); err != nil {
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

	sessions, err := session.ListForDir(projectPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list sessions: %v", err))
		return
	}

	result := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, SessionInfo{
			ID:        s.ID,
			Title:     s.Title,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
			UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
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
