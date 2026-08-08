package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/projects"
)

// projectMarkers are files/dirs that mark a directory as a project root. Only
// directories carrying one of these are auto-added to the sidebar when the
// server's working directory isn't already a saved project.
var projectMarkers = []string{
	".git", ".hg", ".svn", ".ocode",
	"go.mod", "package.json", "Cargo.toml", "pyproject.toml",
	"pom.xml", "build.gradle", "composer.json", "mix.exs", "pubspec.yaml",
}

// HandleGetCurrentProject returns the saved project root that best matches the
// server's working directory, so the web UI can auto-select the right project
// on startup and open a chat tab against it. When the working directory is a
// real project root that isn't saved yet, it is added to the store first so it
// shows up in the sidebar. Non-project roots (filesystem root, home dir,
// marker-less directories) return project: null and are never auto-added.
func (h *Handler) HandleGetCurrentProject(w http.ResponseWriter, _ *http.Request) {
	cwd := h.workDir
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	cwd = filepath.Clean(cwd)

	// No project store (init failed) — nothing to match or add. Stay graceful
	// (200 + null) so the UI boot flow degrades to "no project" rather than
	// surfacing a 500 on every startup.
	if h.projects == nil {
		writeJSON(w, http.StatusOK, map[string]any{"project": nil, "cwd": cwd})
		return
	}

	// Exact/prefix match against saved projects first — a saved root may be an
	// ancestor of the cwd (e.g. launching the desktop from a subdirectory).
	if p := matchSavedProject(h.projects, cwd); p != nil {
		writeJSON(w, http.StatusOK, map[string]any{"project": p, "cwd": cwd})
		return
	}

	// Not saved: auto-add only when the cwd looks like a project root.
	if isAutoAddCandidate(cwd) {
		if err := h.projects.Add(cwd); err != nil {
			writeError(w, http.StatusInternalServerError, "add project: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"project": findProject(h.projects, cwd), "cwd": cwd})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"project": nil, "cwd": cwd})
}

// matchSavedProject returns the saved project whose root is the cwd itself or
// its closest ancestor (longest-path wins); nil when no saved project contains
// the cwd.
func matchSavedProject(store *projects.Store, cwd string) *projects.Project {
	if store == nil {
		return nil
	}
	list := store.List()
	var best *projects.Project
	bestLen := -1
	for i := range list {
		root := filepath.Clean(list[i].Path)
		if root == cwd || strings.HasPrefix(cwd, root+string(filepath.Separator)) {
			if len(root) > bestLen {
				bestLen = len(root)
				best = &list[i]
			}
		}
	}
	return best
}

// findProject returns the saved project with the exact path, or nil.
func findProject(store *projects.Store, path string) *projects.Project {
	if store == nil {
		return nil
	}
	target := filepath.Clean(path)
	list := store.List()
	for i := range list {
		if filepath.Clean(list[i].Path) == target {
			return &list[i]
		}
	}
	return nil
}

// isAutoAddCandidate reports whether the cwd is a plausible project root worth
// adding to the sidebar: not the filesystem root, not the user's home dir, and
// carrying at least one project marker.
func isAutoAddCandidate(cwd string) bool {
	if cwd == "" || cwd == string(filepath.Separator) {
		return false
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == cwd {
		return false
	}
	for _, m := range projectMarkers {
		if _, err := os.Stat(filepath.Join(cwd, m)); err == nil {
			return true
		}
	}
	return false
}
