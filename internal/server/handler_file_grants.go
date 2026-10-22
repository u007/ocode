package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// File-access GRANTS — the editor/preview's own allowlist for directories OUTSIDE
// any project root.
//
// Why this is a separate concept from `extra_allowed_paths`:
//
//   - `extra_allowed_paths` is GLOBAL, PERSISTENT, and SHARED WITH THE AGENT.
//     `applyExtraAllowedPathsToRuntime` → `tool.SetPersistentExtraAllowedPaths`
//     → `tool.ExtraAllowedRoots()` → `PermissionManager.AllowedRoots()`. Adding
//     a path there to let the editor open a file would ALSO stop the agent from
//     asking permission for that path — a silent widening of the agent's
//     read/write scope, which is exactly what this feature must not do.
//   - `allowedProjectRoots()` is the PROJECT trust boundary: which directories
//     this server will browse, mutate and open at all.
//
// A grant is therefore an EDITOR-ONLY capability: a directory the user
// explicitly pointed the editor at. It is consulted ONLY by the file
// read/write/open surfaces (handler_files.go, handler_open.go) and never by
// `internal/agent`, `internal/tool` or the bash permission layer.
//
// Grants are IN-MEMORY and PER-SERVER-PROCESS: they die with the server, so a
// restart re-asks. That keeps the standing capability as narrow as the user's
// actual intent ("open THIS file"), instead of persisting a permanent hole.

// fileGrantStore holds the granted directory roots.
//
// It has its own mutex rather than reusing h.mu: h.mu is the map lock taken by
// every endpoint, and the grant list is read on the hot path of every file
// read. A dedicated RWMutex keeps the file surfaces off the global lock.
type fileGrantStore struct {
	mu    sync.RWMutex
	roots map[string]string // canonical absolute path -> original path as granted
}

func newFileGrantStore() *fileGrantStore {
	return &fileGrantStore{roots: map[string]string{}}
}

// canonicalGrantRoot normalizes a granted path to an absolute, symlink-resolved
// directory path. The path may name a FILE (the common case: the user pointed
// the editor at /Users/x/Downloads/notes.md) — then its parent directory is the
// root, so sibling files in that directory are reachable too. A path that names
// no existing file is treated as a directory.
//
// Returns ("", false) when the path cannot be resolved at all (e.g. an empty
// string or a path whose parent does not exist).
func canonicalGrantRoot(path string) (string, bool) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", false
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", false
	}
	abs = filepath.Clean(abs)

	// A path that names an existing FILE grants its parent directory.
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	} else {
		// Not an existing file. It may be a directory (grant as-is) or a
		// not-yet-existing path whose PARENT exists (grant the parent, so a
		// file the user is about to create is still openable).
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			parent := filepath.Dir(abs)
			if _, perr := os.Stat(parent); perr != nil {
				return "", false
			}
			abs = parent
		}
	}

	// Resolve symlinks so two grants of the same directory (one via the link,
	// one via the real path) collapse to one entry, and so containment checks
	// compare like with like.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(real)
	}
	return abs, true
}

// add grants a root. It reports whether the root was newly added (false = it
// was already granted).
func (s *fileGrantStore) add(root, original string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.roots == nil {
		s.roots = map[string]string{}
	}
	if _, ok := s.roots[root]; ok {
		return false
	}
	s.roots[root] = original
	return true
}

// remove revokes a grant. It reports whether a grant was removed.
func (s *fileGrantStore) remove(root string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.roots[root]; !ok {
		return false
	}
	delete(s.roots, root)
	return true
}

// list returns the granted roots, sorted for a stable response.
func (s *fileGrantStore) list() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.roots))
	for root := range s.roots {
		out = append(out, root)
	}
	sort.Strings(out)
	return out
}

// contains reports whether root is already granted.
func (s *fileGrantStore) contains(root string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.roots[root]
	return ok
}

// fileGrants returns the handler's grant store, creating it on first use.
// The lazy create is safe: the handler is created once and the store pointer
// is published under h.mu.
func (h *Handler) fileGrants() *fileGrantStore {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fileGrantStore == nil {
		h.fileGrantStore = newFileGrantStore()
	}
	return h.fileGrantStore
}

// HandleGrantFilePath grants the editor access to a directory outside any
// project root.
//
// POST body: {"path": "/absolute/or/~/path"} → {"root": "<canonical dir>"}.
func (h *Handler) HandleGrantFilePath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	root, ok := canonicalGrantRoot(req.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "path could not be resolved")
		return
	}
	file, err := canonicalGrantFile(req.Path, root)
	if err != nil {
		writeError(w, http.StatusBadRequest, "path could not be resolved")
		return
	}
	added := h.fileGrants().add(root, req.Path)
	writeJSON(w, http.StatusOK, map[string]any{
		"root":    root,
		"path":    file,
		"granted": added,
		"roots":   h.fileGrants().list(),
	})
}

// canonicalGrantFile spells the requested path in the same canonical form as
// its granted root, so the web can open it relative to that root. The root's
// symlinks are resolved, so a path typed through a link (macOS /var/... names
// /private/var/...) must come back spelled the same way, or it cannot be
// placed under the root. A path that named the granted directory itself is
// the root; any other path is the root joined with the file's name.
func canonicalGrantFile(requested, root string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(requested))
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return root, nil
	}
	return filepath.Join(root, filepath.Base(abs)), nil
}

// HandleRevokeFilePath revokes a previously granted directory.
//
// DELETE body: {"path": "<root>"} → {"revoked": bool}.
func (h *Handler) HandleRevokeFilePath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	root, ok := canonicalGrantRoot(req.Path)
	if !ok {
		root = filepath.Clean(strings.TrimSpace(req.Path))
	}
	revoked := h.fileGrants().remove(root)
	writeJSON(w, http.StatusOK, map[string]any{
		"root":    root,
		"revoked": revoked,
		"roots":   h.fileGrants().list(),
	})
}

// HandleListFileGrants lists the currently granted directories.
func (h *Handler) HandleListFileGrants(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"roots": h.fileGrants().list(),
	})
}
