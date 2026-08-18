package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/u007/ocode/internal/snapshot"
)

type FileNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	IsDir    bool       `json:"is_dir"`
	Children []FileNode `json:"children,omitempty"`
}

func (h *Handler) HandleFileTree(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("path")
	if root == "" {
		// Anchor to the server's project root, not the process CWD: a
		// Finder-launched desktop .app starts with cwd "/" and serve may be
		// started from anywhere, so "."-relative trees list the wrong
		// directory (the whole filesystem on desktop).
		root = h.workDir
	}
	if root == "" {
		root = "."
	}
	// Resolve relative roots against the anchored workDir so they are stable
	// regardless of the process CWD, and confine an explicit ?path= to the
	// workDir so the endpoint cannot be used to list arbitrary directories.
	if !filepath.IsAbs(root) && h.workDir != "" {
		root = filepath.Join(h.workDir, root)
	}
	if r.URL.Query().Get("path") != "" {
		if h.workDir == "" {
			writeError(w, http.StatusBadRequest, "server has no working directory configured; explicit path not allowed")
			return
		}
		if !containedIn(root, h.workDir) {
			writeError(w, http.StatusBadRequest, "path outside working directory")
			return
		}
	}

	// Depth cap for the returned tree. The default (4) preserves the
	// historical behavior of listing files nested up to four levels; depth=0
	// returns the full tree (the file picker needs every file, not just the
	// shallow ones).
	maxDepth := 4
	if raw := r.URL.Query().Get("depth"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			maxDepth = n
		}
	}

	base, err := filepath.Abs(root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	count := 0
	node, err := buildFileTree(base, base, 0, maxDepth, &count)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if node.Children == nil {
		node.Children = []FileNode{}
	}
	writeJSON(w, http.StatusOK, FileTreeResponse{
		Children:  node.Children,
		Truncated: count >= maxTreeNodes,
	})
}

// FileTreeResponse wraps the top-level entries returned by HandleFileTree.
// Truncated is true when buildFileTree hit maxTreeNodes and stopped walking
// before covering the whole tree, so the frontend can warn instead of
// silently rendering an incomplete tree as if it were complete.
type FileTreeResponse struct {
	Children  []FileNode `json:"children"`
	Truncated bool       `json:"truncated"`
}

// containedIn reports whether path p (an absolute path) is inside dir, after
// resolving symlinks so a symlink inside dir cannot reach outside it. Mirrors
// the containment check in HandleSaveFileContent.
func containedIn(p, dir string) bool {
	absP, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	realP, err := filepath.EvalSymlinks(absP)
	if err != nil {
		return false
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realDir, realP)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (h *Handler) HandleFileContent(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	// Resolve relative paths (as returned by the file tree) against the
	// anchored workDir so they stay correct when the process CWD differs
	// from the project root (e.g. a Finder-launched desktop .app).
	if !filepath.IsAbs(path) && h.workDir != "" {
		path = filepath.Join(h.workDir, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"path":    path,
		"content": string(data),
	})
}

type saveFileContentRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (h *Handler) HandleSaveFileContent(w http.ResponseWriter, r *http.Request) {
	var req saveFileContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	root := h.workDir
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve work dir")
		return
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve work dir")
		return
	}
	absTarget, err := filepath.Abs(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	// Resolve symlinks in the target's parent directory (the target itself
	// may not exist yet), then re-check containment against the real root so
	// a symlink inside the workspace can't be used to write outside it.
	realParent, err := filepath.EvalSymlinks(filepath.Dir(absTarget))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	realTarget := filepath.Join(realParent, filepath.Base(absTarget))
	rel, err := filepath.Rel(realRoot, realTarget)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		writeError(w, http.StatusBadRequest, "path is outside the workspace")
		return
	}

	// O_NOFOLLOW rejects the write if the final path component is itself a
	// symlink, closing the window between the containment check above and
	// the write below.
	f, err := os.OpenFile(realTarget, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	if _, err := f.Write([]byte(req.Content)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":  req.Path,
		"saved": true,
	})
}

// ignoredDirNames are directories skipped during the tree walk because they
// hold generated/vendored content that is large and not source-navigable.
var ignoredDirNames = map[string]bool{
	"node_modules":     true,
	"vendor":           true,
	"dist":             true,
	"build":            true,
	"target":           true,
	".next":            true,
	".venv":            true,
	"venv":             true,
	"__pycache__":      true,
	"coverage":         true,
	"bower_components": true,
	".cache":           true,
}

// maxTreeNodes bounds the total number of nodes a single tree walk will
// visit, so an unbounded depth=0 request (e.g. from the file picker) on a
// huge monorepo can't hang the request goroutine indefinitely.
const maxTreeNodes = 20000

// buildFileTree walks the directory tree rooted at cur and returns a FileNode
// whose Path values are relative to base (the resolved root), so the frontend
// contract stays the same whether the root is "." or an absolute path.
// maxDepth caps how deep the walk descends; maxDepth == 0 means unlimited.
// count tracks the total nodes visited across the whole walk and stops
// descending further once maxTreeNodes is reached.
func buildFileTree(base, cur string, depth, maxDepth int, count *int) (FileNode, error) {
	info, err := os.Stat(cur)
	if err != nil {
		return FileNode{}, err
	}

	rel, err := filepath.Rel(base, cur)
	if err != nil {
		rel = cur
	}
	node := FileNode{
		Name:  info.Name(),
		Path:  rel,
		IsDir: info.IsDir(),
	}

	*count++
	if !info.IsDir() || (maxDepth > 0 && depth >= maxDepth) || *count >= maxTreeNodes {
		return node, nil
	}

	entries, err := os.ReadDir(cur)
	if err != nil {
		return node, nil
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		if *count >= maxTreeNodes {
			break
		}
		if strings.HasPrefix(e.Name(), ".") || ignoredDirNames[e.Name()] {
			continue
		}
		child, err := buildFileTree(base, filepath.Join(cur, e.Name()), depth+1, maxDepth, count)
		if err != nil {
			continue
		}
		node.Children = append(node.Children, child)
	}

	return node, nil
}

func (h *Handler) HandleUndo(w http.ResponseWriter, r *http.Request) {
	path, err := snapshot.Undo()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "action": "undo"})
}

func (h *Handler) HandleRedo(w http.ResponseWriter, r *http.Request) {
	path, err := snapshot.Redo()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "action": "redo"})
}
