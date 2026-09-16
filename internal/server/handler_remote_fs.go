package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/remote"
)

// Remote variants of the FS mutation endpoints (handler_fs.go). Admission is
// the same registered (host, path) rule; every operation becomes one
// POSIX shell invocation on the remote host. Paths are root-joined and
// shell-quoted; metacharacter-bearing names are rejected by remoteSafeSpec
// (mirroring the local endpoints' resolveFSPath containment).

// remoteFSRequest mirrors fsActionRequest plus the ?host= query value.
func (h *Handler) remoteDecodeFSRequest(w http.ResponseWriter, r *http.Request, host string) (fsActionRequest, remoteWork, bool) {
	var req fsActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return req, remoteWork{}, false
	}
	rw, err := h.remoteWorkFor(host, r.URL.Query().Get("project"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return req, remoteWork{}, false
	}
	return req, rw, true
}

// remoteTargetOf resolves one request path against the remote root, checks
// containment, and returns the quoted absolute remote path plus its
// repo-relative spec. ok=false means the error response is already written.
func (h *Handler) remoteTargetOf(w http.ResponseWriter, rw remoteWork, p, mode string) (string, string, bool) {
	if p == "" {
		writeError(w, http.StatusBadRequest, "empty path")
		return "", "", false
	}
	abs := remoteAbsJoin(rw.Path, p)
	rel, err := remoteRelCheck(rw.Path, abs)
	if err != nil || rel == "." {
		writeError(w, http.StatusBadRequest, "path is outside the project root")
		return "", "", false
	}
	spec, serr := remoteSafeSpec(rel)
	if serr != nil {
		writeError(w, http.StatusBadRequest, serr.Error())
		return "", "", false
	}
	if mode == "read" && !remoteExists(contextBackground(), rw, abs) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("path %q does not exist", p))
		return "", "", false
	}
	if mode != "read" {
		parent := remoteParentPath(abs)
		if !remoteExists(contextBackground(), rw, parent) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("parent directory of %q does not exist", p))
			return "", "", false
		}
	}
	return abs, spec, true
}

// remoteParentPath returns the remote parent directory of p (slash-based;
// remote project paths are POSIX).
func remoteParentPath(p string) string {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return p
	}
	return p[:i]
}

func (h *Handler) fsCopyRemote(w http.ResponseWriter, r *http.Request, host string) {
	{
		req, rw, ok := h.remoteDecodeFSRequest(w, r, host)
		if !ok {
			return
		}
		if req.DestDir == "" {
			writeError(w, http.StatusBadRequest, "dest_dir is required")
			return
		}
		destAbs, _, destOK := h.remoteTargetOf(w, rw, req.DestDir, "read")
		if !destOK {
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
		for _, s := range sources {
			src, _, srcOK := h.remoteTargetOf(w, rw, s, "read")
			if !srcOK {
				return
			}
			if src == destAbs || strings.HasPrefix(destAbs, src+"/") {
				writeError(w, http.StatusBadRequest, "cannot copy a directory into itself")
				return
			}
			dst := remoteUniqueDest(r.Context(), rw, destAbs, remoteBaseName(src))
			if _, err := remoteRun(r.Context(), rw, "cp -a -- "+remote.ShellQuote(src)+" "+remote.ShellQuote(dst)); err != nil {
				writeError(w, http.StatusInternalServerError, "copy failed: "+err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
}

func (h *Handler) fsMoveRemote(w http.ResponseWriter, r *http.Request, host string) {
	{
		req, rw, ok := h.remoteDecodeFSRequest(w, r, host)
		if !ok {
			return
		}
		if req.DestDir == "" {
			writeError(w, http.StatusBadRequest, "dest_dir is required")
			return
		}
		destAbs, _, destOK := h.remoteTargetOf(w, rw, req.DestDir, "read")
		if !destOK {
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
		for _, s := range sources {
			src, _, srcOK := h.remoteTargetOf(w, rw, s, "read")
			if !srcOK {
				return
			}
			if src == destAbs || strings.HasPrefix(destAbs, src+"/") {
				writeError(w, http.StatusBadRequest, "cannot move a directory into itself")
				return
			}
			dst := remoteUniqueDest(r.Context(), rw, destAbs, remoteBaseName(src))
			if _, err := remoteRun(r.Context(), rw, "mv -- "+remote.ShellQuote(src)+" "+remote.ShellQuote(dst)); err != nil {
				writeError(w, http.StatusInternalServerError, "move failed: "+err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
}

func (h *Handler) fsDeleteRemote(w http.ResponseWriter, r *http.Request, host string) {
	{
		req, rw, ok := h.remoteDecodeFSRequest(w, r, host)
		if !ok {
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
		for _, t := range targets {
			abs, _, ok := h.remoteTargetOf(w, rw, t, "read")
			if !ok {
				return
			}
			if _, err := remoteRun(r.Context(), rw, "rm -rf -- "+remote.ShellQuote(abs)); err != nil {
				writeError(w, http.StatusInternalServerError, "delete failed: "+err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
		return
	}
}

func (h *Handler) fsRenameRemote(w http.ResponseWriter, r *http.Request, host string) {
	{
		req, rw, ok := h.remoteDecodeFSRequest(w, r, host)
		if !ok {
			return
		}
		if req.Path == "" || req.NewName == "" {
			writeError(w, http.StatusBadRequest, "path and new_name are required")
			return
		}
		name, nerr := remoteSafeSpec(req.NewName)
		if nerr != nil || name != req.NewName || containsSlash(name) {
			writeError(w, http.StatusBadRequest, "new_name must be a plain name without path separators")
			return
		}
		src, _, ok := h.remoteTargetOf(w, rw, req.Path, "read")
		if !ok {
			return
		}
		dst := remoteParentPath(src) + "/" + name
		if remoteExists(contextBackground(), rw, dst) {
			writeError(w, http.StatusConflict, "destination already exists")
			return
		}
		if _, err := remoteRun(r.Context(), rw, "mv -- "+remote.ShellQuote(src)+" "+remote.ShellQuote(dst)); err != nil {
			writeError(w, http.StatusInternalServerError, "rename failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": dst})
		return
	}
}

func (h *Handler) fsNewFileRemote(w http.ResponseWriter, r *http.Request, host string) {
	{
		req, rw, ok := h.remoteDecodeFSRequest(w, r, host)
		if !ok {
			return
		}
		if req.Path == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		target, _, ok := h.remoteTargetOf(w, rw, req.Path, "create")
		if !ok {
			return
		}
		if remoteExists(contextBackground(), rw, target) {
			writeError(w, http.StatusBadRequest, "path already exists")
			return
		}
		if err := remoteMkdirAll(r.Context(), rw, remoteParentPath(target)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create parent: "+err.Error())
			return
		}
		if _, err := remoteRun(r.Context(), rw, ": > "+remote.ShellQuote(target)); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create file: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": target})
		return
	}
}

func (h *Handler) fsNewFolderRemote(w http.ResponseWriter, r *http.Request, host string) {
	{
		req, rw, ok := h.remoteDecodeFSRequest(w, r, host)
		if !ok {
			return
		}
		if req.Path == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		target, _, ok := h.remoteTargetOf(w, rw, req.Path, "create")
		if !ok {
			return
		}
		if err := remoteMkdirAll(r.Context(), rw, target); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create folder: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": target})
		return
	}
}

func (h *Handler) fsDuplicateRemote(w http.ResponseWriter, r *http.Request, host string) {
	{
		req, rw, ok := h.remoteDecodeFSRequest(w, r, host)
		if !ok {
			return
		}
		if req.Path == "" {
			writeError(w, http.StatusBadRequest, "path is required")
			return
		}
		src, _, ok := h.remoteTargetOf(w, rw, req.Path, "read")
		if !ok {
			return
		}
		dst := remoteUniqueDest(r.Context(), rw, remoteParentPath(src), remoteBaseName(src))
		if _, err := remoteRun(r.Context(), rw, "cp -a -- "+remote.ShellQuote(src)+" "+remote.ShellQuote(dst)); err != nil {
			writeError(w, http.StatusInternalServerError, "duplicate failed: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": dst})
		return
	}
}

// remoteUniqueDest mirrors uniqueDest with remote existence checks: base,
// "base copy", "base copy 2", …
func remoteUniqueDest(ctx context.Context, rw remoteWork, destDir, name string) string {
	cand := destDir + "/" + name
	if !remoteExists(ctx, rw, cand) {
		return cand
	}
	ext := remoteExt(name)
	base := name
	if ext != "" {
		base = name[:len(name)-len(ext)]
	}
	cand = destDir + "/" + base + " copy" + ext
	for i := 2; ; i++ {
		if !remoteExists(ctx, rw, cand) {
			return cand
		}
		cand = destDir + "/" + fmt.Sprintf("%s copy %d%s", base, i, ext)
	}
}

// containsSlash reports whether name contains a path separator.
func containsSlash(name string) bool { return strings.Contains(name, "/") }

// remoteExt is filepath.Ext for POSIX remote paths.
func remoteExt(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 || strings.Contains(name[i:], "/") {
		return ""
	}
	return name[i:]
}

// remoteBaseName is filepath.Base for POSIX remote paths.
func remoteBaseName(p string) string {
	if p == "" {
		return ""
	}
	p = strings.TrimRight(p, "/")
	i := strings.LastIndex(p, "/")
	return p[i+1:]
}

// contextBackground returns context.Background() for helpers that don't
// receive a request context.
func contextBackground() context.Context { return context.Background() }
