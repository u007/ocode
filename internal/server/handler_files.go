package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
		root = "."
	}

	node, err := buildFileTree(root, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Return only the children (top-level entries) so the frontend receives
	// an array of FileNode that it can iterate with .map().
	if node.Children == nil {
		node.Children = []FileNode{}
	}
	writeJSON(w, http.StatusOK, node.Children)
}

func (h *Handler) HandleFileContent(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
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

func buildFileTree(root string, depth int) (FileNode, error) {
	info, err := os.Stat(root)
	if err != nil {
		return FileNode{}, err
	}

	node := FileNode{
		Name:  info.Name(),
		Path:  root,
		IsDir: info.IsDir(),
	}

	if !info.IsDir() || depth > 3 {
		return node, nil
	}

	entries, err := os.ReadDir(root)
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
		if strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
			continue
		}
		child, err := buildFileTree(filepath.Join(root, e.Name()), depth+1)
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
