package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	writeJSON(w, http.StatusOK, node)
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
