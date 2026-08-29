package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// fsActionRequest carries the paths for a file-system mutation. Sources/paths
// may be project-relative or absolute inside the project root. The target
// project is taken from the ?project= query param, mirroring the git endpoints.
type fsActionRequest struct {
	Paths   []string `json:"paths"`
	Path    string   `json:"path"`
	DestDir string   `json:"dest_dir"`
	NewName string   `json:"new_name"`
	Project string   `json:"-"` // not used; project comes from the query
}

// resolveFSPath validates a single path against dir. mode is "read" (the path
// must exist) or "create"/"write" (the path's parent must exist). Containment
// and the .git boundary are enforced via symlink resolution of the nearest
// existing ancestor plus a lexical check of the full path.
func resolveFSPath(dir, p, mode string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	// Resolve the repo root through symlinks first, then build the candidate
	// from the resolved root — otherwise containment checks against
	// EvalSymlinks(root) would wrongly reject in-repo paths (e.g. /var vs
	// /private/var on macOS).
	realDir := dir
	if rd, dErr := filepath.EvalSymlinks(dir); dErr == nil {
		realDir = rd
	}
	candidate := p
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(realDir, p)
	}
	candidate = filepath.Clean(candidate)

	if mode == "read" {
		if _, err := os.Lstat(candidate); err != nil {
			return "", fmt.Errorf("path %q does not exist", p)
		}
	} else {
		parent := filepath.Dir(candidate)
		if _, err := os.Stat(parent); err != nil {
			return "", fmt.Errorf("parent directory of %q does not exist", p)
		}
	}

	// Resolve the nearest existing ancestor (the candidate itself for "read",
	// its parent otherwise) through symlinks and confirm it stays inside the
	// repo root and outside .git.
	anc := candidate
	if mode != "read" {
		anc = filepath.Dir(candidate)
	}
	for {
		if _, err := os.Stat(anc); err == nil {
			break
		}
		parent := filepath.Dir(anc)
		if parent == anc {
			return "", fmt.Errorf("path %q escapes the project root", p)
		}
		anc = parent
	}
	realAnc, err := filepath.EvalSymlinks(anc)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %q", p)
	}
	relAnc, err := filepath.Rel(realDir, realAnc)
	if err != nil || relAnc == ".." || strings.HasPrefix(relAnc, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the project root", p)
	}
	if relAnc == ".git" || strings.HasPrefix(relAnc, ".git"+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is inside .git and cannot be modified", p)
	}
	// The full (possibly new) path, symlink-resolved when it exists, must also
	// remain lexically inside the repo root and outside .git.
	realCand := candidate
	if rc, cErr := filepath.EvalSymlinks(candidate); cErr == nil {
		realCand = rc
	}
	relFull, err := filepath.Rel(realDir, realCand)
	if err != nil || relFull == ".." || strings.HasPrefix(relFull, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the project root", p)
	}
	if relFull == ".git" || strings.HasPrefix(relFull, ".git"+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is inside .git and cannot be modified", p)
	}
	return candidate, nil
}

func (h *Handler) decodeFSRequest(r *http.Request) (fsActionRequest, bool, string, bool) {
	var req fsActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, false, "", false
	}
	dir, valid := h.mutationProjectDir(r)
	if !valid {
		return req, false, "", false
	}
	return req, true, dir, true
}

func (h *Handler) HandleFSCopy(w http.ResponseWriter, r *http.Request) {
	req, decoded, dir, ok := h.decodeFSRequest(r)
	if !ok {
		if !decoded {
			writeError(w, http.StatusBadRequest, "invalid request body")
		} else {
			writeError(w, http.StatusBadRequest, "unknown project")
		}
		return
	}
	if req.DestDir == "" {
		writeError(w, http.StatusBadRequest, "dest_dir is required")
		return
	}
	destDir, err := resolveFSPath(dir, req.DestDir, "read")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sources := append([]string{}, req.Paths...)
	if req.Path != "" {
		sources = append(sources, req.Path)
	}
	if len(sources) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	if realDir == "" {
		realDir = dir
	}
	for _, s := range sources {
		src, err := resolveFSPath(dir, s, "read")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		realSrc, _ := filepath.EvalSymlinks(src)
		if realSrc == "" {
			realSrc = src
		}
		if rel, _ := filepath.Rel(realDir, realSrc); rel == "." {
			writeError(w, http.StatusBadRequest, "cannot copy project root")
			return
		}
		// Dest must not be inside source.
		realDestDir, _ := filepath.EvalSymlinks(destDir)
		if realDestDir == "" {
			realDestDir = destDir
		}
		if realSrc == realDestDir || strings.HasPrefix(realDestDir, realSrc+string(filepath.Separator)) {
			writeError(w, http.StatusBadRequest, "cannot copy a directory into itself")
			return
		}
		dst := uniqueDest(destDir, filepath.Base(src))
		realDst := filepath.Join(realDestDir, filepath.Base(src))
		if strings.HasPrefix(realDst, realSrc+string(filepath.Separator)) || realDst == realSrc {
			writeError(w, http.StatusBadRequest, "cannot copy a directory into itself")
			return
		}
		if err := copyPath(src, dst); err != nil {
			writeError(w, http.StatusInternalServerError, "copy failed: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) HandleFSMove(w http.ResponseWriter, r *http.Request) {
	req, decoded, dir, ok := h.decodeFSRequest(r)
	if !ok {
		if !decoded {
			writeError(w, http.StatusBadRequest, "invalid request body")
		} else {
			writeError(w, http.StatusBadRequest, "unknown project")
		}
		return
	}
	if req.DestDir == "" {
		writeError(w, http.StatusBadRequest, "dest_dir is required")
		return
	}
	destDir, err := resolveFSPath(dir, req.DestDir, "read")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sources := append([]string{}, req.Paths...)
	if req.Path != "" {
		sources = append(sources, req.Path)
	}
	if len(sources) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	if realDir == "" {
		realDir = dir
	}
	realDestDir, _ := filepath.EvalSymlinks(destDir)
	if realDestDir == "" {
		realDestDir = destDir
	}
	for _, s := range sources {
		src, err := resolveFSPath(dir, s, "read")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		realSrc, _ := filepath.EvalSymlinks(src)
		if realSrc == "" {
			realSrc = src
		}
		if rel, _ := filepath.Rel(realDir, realSrc); rel == "." {
			writeError(w, http.StatusBadRequest, "cannot move project root")
			return
		}
		if realSrc == realDestDir || strings.HasPrefix(realDestDir, realSrc+string(filepath.Separator)) {
			writeError(w, http.StatusBadRequest, "cannot move a directory into itself")
			return
		}
		realDst := filepath.Join(realDestDir, filepath.Base(src))
		if realDst == realSrc || strings.HasPrefix(realDst, realSrc+string(filepath.Separator)) {
			writeError(w, http.StatusBadRequest, "cannot move a directory into itself")
			return
		}
		dst := uniqueDest(destDir, filepath.Base(src))
		if err := movePath(src, dst); err != nil {
			writeError(w, http.StatusInternalServerError, "move failed: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) HandleFSDelete(w http.ResponseWriter, r *http.Request) {
	req, decoded, dir, ok := h.decodeFSRequest(r)
	if !ok {
		if !decoded {
			writeError(w, http.StatusBadRequest, "invalid request body")
		} else {
			writeError(w, http.StatusBadRequest, "unknown project")
		}
		return
	}
	targets := append([]string{}, req.Paths...)
	if req.Path != "" {
		targets = append(targets, req.Path)
	}
	if len(targets) == 0 {
		writeError(w, http.StatusBadRequest, "no paths provided")
		return
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	if realDir == "" {
		realDir = dir
	}
	for _, t := range targets {
		path, err := resolveFSPath(dir, t, "read")
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		realPath, _ := filepath.EvalSymlinks(path)
		if realPath == "" {
			realPath = path
		}
		rel, _ := filepath.Rel(realDir, realPath)
		if rel == "." {
			writeError(w, http.StatusBadRequest, "cannot delete project root")
			return
		}
		if err := os.RemoveAll(path); err != nil {
			writeError(w, http.StatusInternalServerError, "delete failed: "+err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) HandleFSRename(w http.ResponseWriter, r *http.Request) {
	req, decoded, dir, ok := h.decodeFSRequest(r)
	if !ok {
		if !decoded {
			writeError(w, http.StatusBadRequest, "invalid request body")
		} else {
			writeError(w, http.StatusBadRequest, "unknown project")
		}
		return
	}
	if req.Path == "" || req.NewName == "" {
		writeError(w, http.StatusBadRequest, "path and new_name are required")
		return
	}
	if strings.Contains(req.NewName, string(filepath.Separator)) {
		writeError(w, http.StatusBadRequest, "new_name must not contain path separators")
		return
	}
	src, err := resolveFSPath(dir, req.Path, "read")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dst := filepath.Join(filepath.Dir(src), req.NewName)
	if _, err := resolveFSPath(dir, dst, "create"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := os.Lstat(dst); err == nil {
		writeError(w, http.StatusConflict, "destination already exists")
		return
	}
	if err := os.Rename(src, dst); err != nil {
		writeError(w, http.StatusInternalServerError, "rename failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": dst})
}

func (h *Handler) HandleFSNewFile(w http.ResponseWriter, r *http.Request) {
	req, decoded, dir, ok := h.decodeFSRequest(r)
	if !ok {
		if !decoded {
			writeError(w, http.StatusBadRequest, "invalid request body")
		} else {
			writeError(w, http.StatusBadRequest, "unknown project")
		}
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	target, err := resolveFSPath(dir, req.Path, "create")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := os.Lstat(target); err == nil {
		writeError(w, http.StatusBadRequest, "path already exists")
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create parent: "+err.Error())
		return
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create file: "+err.Error())
		return
	}
	f.Close()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": target})
}

func (h *Handler) HandleFSNewFolder(w http.ResponseWriter, r *http.Request) {
	req, decoded, dir, ok := h.decodeFSRequest(r)
	if !ok {
		if !decoded {
			writeError(w, http.StatusBadRequest, "invalid request body")
		} else {
			writeError(w, http.StatusBadRequest, "unknown project")
		}
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	target, err := resolveFSPath(dir, req.Path, "create")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": target})
}

func (h *Handler) HandleFSDuplicate(w http.ResponseWriter, r *http.Request) {
	req, decoded, dir, ok := h.decodeFSRequest(r)
	if !ok {
		if !decoded {
			writeError(w, http.StatusBadRequest, "invalid request body")
		} else {
			writeError(w, http.StatusBadRequest, "unknown project")
		}
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	src, err := resolveFSPath(dir, req.Path, "read")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	realDir, _ := filepath.EvalSymlinks(dir)
	if realDir == "" {
		realDir = dir
	}
	realSrc, _ := filepath.EvalSymlinks(src)
	if realSrc == "" {
		realSrc = src
	}
	if rel, _ := filepath.Rel(realDir, realSrc); rel == "." {
		writeError(w, http.StatusBadRequest, "cannot duplicate project root")
		return
	}
	dst := uniqueDest(filepath.Dir(src), filepath.Base(src))
	if err := copyPath(src, dst); err != nil {
		writeError(w, http.StatusInternalServerError, "duplicate failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": dst})
}

// uniqueDest returns a path inside destDir with the given base name, appending
// " copy" (and a numeric suffix on further collisions) when the name is taken,
// matching Finder/Explorer behaviour.
func uniqueDest(destDir, name string) string {
	cand := filepath.Join(destDir, name)
	if _, err := os.Lstat(cand); err != nil {
		return cand
	}
	ext := filepath.Ext(name)
	base := name
	if ext != "" {
		base = name[:len(name)-len(ext)]
	}
	cand = filepath.Join(destDir, base+" copy"+ext)
	for i := 2; ; i++ {
		if _, err := os.Lstat(cand); err != nil {
			return cand
		}
		cand = filepath.Join(destDir, fmt.Sprintf("%s copy %d%s", base, i, ext))
	}
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d); err != nil {
				return err
			}
		} else {
			if err := copyFile(s, d); err != nil {
				return err
			}
		}
	}
	return nil
}

func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDeviceError(err) {
		return err
	}
	// Cross-device fallback: copy then remove.
	if err := copyPath(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func isCrossDeviceError(err error) bool {
	return strings.Contains(err.Error(), "cross-device") || strings.Contains(err.Error(), "EXDEV") || strings.Contains(err.Error(), "invalid cross-device")
}
