package server

import (
	"bufio"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type FileNode struct {
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	IsDir     bool       `json:"is_dir"`
	Children  []FileNode `json:"children,omitempty"`
	GitStatus string     `json:"git_status,omitempty"`
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
		writeError(w, http.StatusBadRequest, "server has no working directory configured")
		return
	}
	// Resolve relative roots against the anchored workDir so they are stable
	// regardless of the process CWD, and confine an explicit ?path= to the
	// workDir so the endpoint cannot be used to list arbitrary directories.
	if !filepath.IsAbs(root) && h.workDir != "" {
		root = filepath.Join(h.workDir, root)
	}
	// matchedRoot is the specific project root or extra-allowed-path that
	// contains the requested root, when one was given explicitly; it becomes
	// the anchor for returned Path values below. An implicit request (no
	// ?path=) always means "the server's own project", so it anchors to
	// workDir directly without needing a match.
	matchedRoot := h.workDir
	if r.URL.Query().Get("path") != "" {
		if h.workDir == "" {
			writeError(w, http.StatusBadRequest, "server has no working directory configured; explicit path not allowed")
			return
		}
		dir, ok := h.fileTreeRootFor(root)
		if !ok {
			writeError(w, http.StatusBadRequest, "path outside working directory")
			return
		}
		matchedRoot = dir
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
	// Anchor returned Path values to matchedRoot (the project root or extra
	// dir the request resolved into) rather than to the requested subtree:
	// the frontend re-requests a subdirectory's children by passing a
	// child's Path straight back as ?path=, so those paths must stay valid
	// (relative to the same fixed root) no matter how deep the query root
	// is, or a second-level expand silently resolves to the wrong directory
	// and renders empty.
	anchor := base
	if matchedRoot != "" {
		if absMatchedRoot, err := filepath.Abs(matchedRoot); err == nil {
			anchor = absMatchedRoot
		}
	}
	count := 0
	node, err := buildFileTree(anchor, base, 0, maxDepth, &count)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if node.Children == nil {
		node.Children = []FileNode{}
	}
	// Annotate with git status badges (M/?/A/D/R) when this is a repo.
	if m := gitStatusMapForDir(anchor); len(m) > 0 {
		annotateFileTreeGitStatus(&node, anchor, m)
	}
	// Use git itself so worktrees and non-standard layouts are recognised,
	// not just a literal ".git" directory entry.
	isGitRepo := false
	if out, err := exec.Command("git", "-C", anchor, "rev-parse", "--is-inside-work-tree").Output(); err == nil && strings.TrimSpace(string(out)) == "true" {
		isGitRepo = true
	}
	writeJSON(w, http.StatusOK, FileTreeResponse{
		Children:  node.Children,
		Truncated: count >= maxTreeNodes,
		IsGitRepo: isGitRepo,
	})
}

// FileTreeResponse wraps the top-level entries returned by HandleFileTree.
// Truncated is true when buildFileTree hit maxTreeNodes and stopped walking
// before covering the whole tree, so the frontend can warn instead of
// silently rendering an incomplete tree as if it were complete.
// IsGitRepo reports whether anchor is inside a git working tree, so the
// frontend can enable/disable git actions in the file-tree context menu.
type FileTreeResponse struct {
	Children  []FileNode `json:"children"`
	Truncated bool       `json:"truncated"`
	IsGitRepo bool       `json:"is_git_repo"`
}

// gitStatusMapForDir runs `git status --short` in dir and returns a map
// of relative path -> badge (M/?/A/D/R), mirroring the TUI's
// parseGitStatusShort. Returns nil for non-repos or on error. Uses git
// plumbing so worktrees (where .git is a file) are handled.
func gitStatusMapForDir(dir string) map[string]string {
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		return nil
	}
	out, err := exec.Command("git", "-c", "core.quotepath=false", "-C", dir, "status", "--short").Output()
	if err != nil {
		return nil
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		code := strings.TrimSpace(line[:2])
		p := strings.TrimSpace(line[3:])
		if idx := strings.LastIndex(p, " -> "); idx >= 0 {
			p = p[idx+4:]
		}
		badge := "M"
		if strings.Contains(code, "?") {
			badge = "?"
		} else if strings.Contains(code, "A") {
			badge = "A"
		} else if strings.Contains(code, "D") {
			badge = "D"
		} else if strings.Contains(code, "R") {
			badge = "R"
		}
		m[p] = badge
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func annotateFileTreeGitStatus(node *FileNode, anchor string, m map[string]string) {
	if !node.IsDir {
		if badge, ok := m[node.Path]; ok {
			node.GitStatus = badge
		} else {
			norm := filepath.ToSlash(node.Path)
			if badge, ok := m[norm]; ok {
				node.GitStatus = badge
			}
		}
	}
	for i := range node.Children {
		annotateFileTreeGitStatus(&node.Children[i], anchor, m)
	}
	// Propagate child git status to parent directories so a folder containing
	// only untracked/modified files still shows a badge, rather than appearing
	// clean while its children are dirty.
	if node.IsDir && node.GitStatus == "" {
		for _, child := range node.Children {
			if child.GitStatus != "" {
				node.GitStatus = child.GitStatus
				break
			}
		}
	}
}

// FileSearchResult is one matching line for the content search endpoint.
type FileSearchResult struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type FileSearchResponse struct {
	Results   []FileSearchResult `json:"results"`
	Truncated bool               `json:"truncated"`
	Total     int                `json:"total"`
	HasMore   bool               `json:"has_more"`
	Capped    bool               `json:"capped,omitempty"`
}

// HandleFileSearch searches file contents for keywords (whitespace-AND,
// case-insensitive) within the anchored project root. Query param `q`
// holds the keywords; `path` selects the project root (same anchoring as
// HandleFileTree). It walks the tree similarly to buildFileTree but
// inspects file contents line-by-line.
func (h *Handler) HandleFileSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		query = strings.TrimSpace(r.URL.Query().Get("query"))
	}
	if query == "" {
		writeJSON(w, http.StatusOK, FileSearchResponse{Results: []FileSearchResult{}})
		return
	}
	root := r.URL.Query().Get("path")
	if root == "" {
		root = h.workDir
	}
	if root == "" {
		writeError(w, http.StatusBadRequest, "server has no working directory configured")
		return
	}
	if !filepath.IsAbs(root) && h.workDir != "" {
		root = filepath.Join(h.workDir, root)
	}
	matchedRoot := h.workDir
	if r.URL.Query().Get("path") != "" {
		if h.workDir == "" {
			writeError(w, http.StatusBadRequest, "server has no working directory configured; explicit path not allowed")
			return
		}
		dir, ok := h.fileTreeRootFor(root)
		if !ok {
			writeError(w, http.StatusBadRequest, "path outside working directory")
			return
		}
		matchedRoot = dir
	}
	base, err := filepath.Abs(root)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	anchor := base
	if matchedRoot != "" {
		if abs, err := filepath.Abs(matchedRoot); err == nil {
			anchor = abs
		}
	}
	keywords := strings.Fields(strings.ToLower(query))
	if len(keywords) == 0 {
		writeJSON(w, http.StatusOK, FileSearchResponse{Results: []FileSearchResult{}})
		return
	}
	var exts []string
	if raw := r.URL.Query().Get("exts"); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(strings.ToLower(p))
			p = strings.TrimPrefix(p, "*.")
			p = strings.TrimPrefix(p, ".")
			p = strings.TrimPrefix(p, "*")
			if p != "" {
				exts = append(exts, p)
			}
		}
	}
	// Pagination: infinite scroll support via offset/limit
	offset := 0
	limit := 50
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	const maxFileSize = 1 << 20
	const maxTotal = 5000
	allResults := make([]FileSearchResult, 0, 512)
	hasMoreDueToCap := false
	_ = filepath.WalkDir(anchor, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "target" || name == ".history" {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if len(exts) > 0 {
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
			matched := false
			for _, e := range exts {
				if ext == e {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileSize {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		buf := make([]byte, 8000)
		n, _ := f.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == 0 {
				f.Close()
				return nil
			}
		}
		rel, _ := filepath.Rel(anchor, path)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return nil
		}
		scanner := bufio.NewScanner(f)
		// 1 MiB token limit to preserve exact long lines
		scanner.Buffer(make([]byte, 0, 4096), 1<<20)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			if len(allResults) >= maxTotal {
				hasMoreDueToCap = true
				break
			}
			line := scanner.Text()
			lower := strings.ToLower(line)
			ok := true
			for _, kw := range keywords {
				if !strings.Contains(lower, kw) {
					ok = false
					break
				}
			}
			if ok {
				// Preserve exact line; cap by runes to avoid splitting UTF-8 and bound payload
				const maxRunes = 2000
				if len([]rune(line)) > maxRunes {
					runes := []rune(line)
					line = string(runes[:maxRunes])
				}
				allResults = append(allResults, FileSearchResult{
					Path: rel,
					Line: lineNum,
					Text: line,
				})
				if len(allResults) >= maxTotal {
					hasMoreDueToCap = true
					break
				}
			}
		}
		f.Close()
		if hasMoreDueToCap {
			return filepath.SkipAll
		}
		return nil
	})
	totalCollected := len(allResults)
	hasMore := hasMoreDueToCap || totalCollected > offset+limit
	if offset >= totalCollected {
		writeJSON(w, http.StatusOK, FileSearchResponse{
			Results:   []FileSearchResult{},
			Truncated: hasMoreDueToCap,
			Total:     totalCollected,
			HasMore:   false,
			Capped:    hasMoreDueToCap,
		})
		return
	}
	end := offset + limit
	if end > totalCollected {
		end = totalCollected
		hasMore = hasMoreDueToCap
	}
	page := allResults[offset:end]
	writeJSON(w, http.StatusOK, FileSearchResponse{
		Results:   page,
		Truncated: hasMoreDueToCap || hasMore,
		Total:     totalCollected,
		HasMore:   hasMore,
		Capped:    hasMoreDueToCap,
	})
}

// fileTreeRootFor reports whether root may be browsed by HandleFileTree —
// inside the server's workDir, inside a saved project root
// (allowedProjectRoots), or inside a configured extra-allowed-path — and, if
// so, returns the specific containing root. Desktop users switch the file
// tree between the active project and their extra dirs, so the boundary
// can't be workDir alone, and the matched root (not workDir) is what the
// returned tree's Path values must be anchored to: HandleFileTree anchors
// every node's Path to it so a later depth=1 expand of a nested directory
// re-resolves against the same project/extra-dir root instead of drifting to
// wherever the server's workDir happens to be.
func (h *Handler) fileTreeRootFor(root string) (string, bool) {
	candidates := h.allowedProjectRoots()
	h.mu.Lock()
	if h.cfg != nil {
		candidates = append(candidates, h.cfg.Ocode.ExtraAllowedPaths...)
	}
	h.mu.Unlock()

	// Pick the most specific (longest) containing root rather than the
	// first match in list order: h.workDir is always listed first in
	// allowedProjectRoots, so a broader workDir (e.g. a Finder-launched
	// desktop .app anchored at the home dir) would otherwise win over the
	// exact project root the request actually resolved into, anchoring
	// returned Path values to the wrong directory.
	best := ""
	for _, dir := range candidates {
		if dir == "" || !containedIn(root, dir) {
			continue
		}
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if best == "" {
			best = absDir
			continue
		}
		if len(absDir) > len(best) {
			best = absDir
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
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

	if projectRoot := r.URL.Query().Get("project_root"); projectRoot != "" {
		root, ok := h.fileContentRootFor(projectRoot)
		if !ok {
			writeError(w, http.StatusBadRequest, "project_root is not an allowed project root")
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if !containedIn(path, root) {
			writeError(w, http.StatusBadRequest, "path is outside the project root")
			return
		}
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
	Path        string `json:"path"`
	Content     string `json:"content"`
	ProjectRoot string `json:"project_root,omitempty"`
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
	if req.ProjectRoot != "" {
		var ok bool
		root, ok = h.fileContentRootFor(req.ProjectRoot)
		if !ok {
			writeError(w, http.StatusBadRequest, "project_root is not an allowed project root")
			return
		}
		if !filepath.IsAbs(req.Path) {
			req.Path = filepath.Join(root, req.Path)
		}
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
	f, err := os.OpenFile(realTarget, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|oNoFollow, 0600)
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

// fileContentRootFor validates a project root supplied by the frontend. Roots
// are compared after resolving symlinks so an alias cannot bypass the project
// allowlist. Extra allowed paths are included because the Files tree can be
// switched to those roots as well.
func (h *Handler) fileContentRootFor(root string) (string, bool) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", false
	}

	allowed := h.allowedProjectRoots()
	h.mu.Lock()
	if h.cfg != nil {
		allowed = append(allowed, h.cfg.Ocode.ExtraAllowedPaths...)
	}
	h.mu.Unlock()
	for _, candidate := range allowed {
		if candidate == "" {
			continue
		}
		absCandidate, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		realCandidate, err := filepath.EvalSymlinks(absCandidate)
		if err == nil && filepath.Clean(realCandidate) == filepath.Clean(realRoot) {
			return absRoot, true
		}
	}
	return "", false
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
	ag := h.activeAgentForRuns(r.URL.Query().Get("session"))
	if ag == nil {
		writeError(w, http.StatusNotFound, "no active agent for session")
		return
	}
	path, err := ag.UndoLastChange()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "action": "undo"})
}

func (h *Handler) HandleRedo(w http.ResponseWriter, r *http.Request) {
	ag := h.activeAgentForRuns(r.URL.Query().Get("session"))
	if ag == nil {
		writeError(w, http.StatusNotFound, "no active agent for session")
		return
	}
	path, err := ag.RedoLastChange()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path, "action": "redo"})
}
