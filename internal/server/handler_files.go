package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

const (
	// maxSearchFileSize is the largest file whose contents are inspected.
	maxSearchFileSize = 1 << 20 // 1 MiB
	// maxSearchResults caps how many matches a single search returns. Beyond
	// this the scan stops and the response/stream reports capped=true.
	maxSearchResults = 5000
	// maxSearchPageSize clamps the limit param of the paginated endpoint.
	maxSearchPageSize = 100
	// searchStreamBatchSize and searchStreamBatchInterval drive the stream
	// endpoint's hybrid batching: a batch is flushed when it reaches this many
	// results OR this much time has elapsed since the previous flush, so a
	// slow walk still produces regular progress frames.
	searchStreamBatchSize     = 25
	searchStreamBatchInterval = 100 * time.Millisecond
)

// fileSearchParams are the validated inputs shared by HandleFileSearch and
// HandleFileSearchStream.
type fileSearchParams struct {
	query          string
	anchor         string // absolute root to walk
	keywords       []string
	exts           []string
	ignorePatterns []string
	regex          bool
	caseSensitive  bool
	wholeWord      bool
	includeIgnored bool
}

func parseBoolParam(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// compileFileSearchPattern mirrors internal/tui/files_search.go:compileContentSearchPattern
// but lives in the server so the API and TUI stay in sync.
func compileFileSearchPattern(query string, caseSensitive, wholeWord, isRegex bool) (*regexp.Regexp, error) {
	pattern := query
	if !isRegex {
		pattern = regexp.QuoteMeta(pattern)
	}
	if wholeWord {
		pattern = `(?:^|[^\pL\pN_])(?:` + pattern + `)(?:$|[^\pL\pN_])`
	}
	if !caseSensitive {
		pattern = `(?i:` + pattern + `)`
	}
	return regexp.Compile(pattern)
}

func parseIgnorePatterns(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Normalize to forward slashes for matching and keep the raw glob.
		out = append(out, p)
	}
	return out
}

// matchesIgnore checks if rel path (forward-slash relative to anchor) matches any ignore glob.
// Globs without "/" match the basename at any depth so "*.log" ignores any .log file.
// Patterns with "/" or "**" are matched against the full relative path.
func matchesIgnore(rel string, patterns []string) bool {
	if len(patterns) == 0 || rel == "" {
		return false
	}
	relSlash := filepath.ToSlash(rel)
	lowerRel := strings.ToLower(relSlash)
	base := filepath.Base(relSlash)
	lowerBase := strings.ToLower(base)
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		patSlash := filepath.ToSlash(pat)
		lowerPat := strings.ToLower(patSlash)
		// Pattern without slash → match basename only (any depth)
		if !strings.Contains(patSlash, "/") {
			if ok, _ := filepath.Match(lowerPat, lowerBase); ok {
				return true
			}
			// Also try direct case-sensitive match for exactness (already lower)
			continue
		}
		// Patterns with slash: support ** handling via recursive match
		if strings.Contains(lowerPat, "**") {
			if doublestarMatch(lowerPat, lowerRel) {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(lowerPat, lowerRel); ok {
			return true
		}
		// Also match "**/pat" implicitly for basename globs that slipped through? Already handled.
	}
	return false
}

func doublestarMatch(pattern, path string) bool {
	patParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	return matchDoublestarParts(patParts, pathParts)
}

func matchDoublestarParts(patParts, pathParts []string) bool {
	// Simple recursive doublestar: ** matches zero or more segments.
	pi, pj := 0, 0
	for pi < len(patParts) && pj < len(pathParts) {
		if patParts[pi] == "**" {
			// ** at end matches rest
			if pi == len(patParts)-1 {
				return true
			}
			// Try to match ** as 0..n segments
			for k := pj; k <= len(pathParts); k++ {
				if matchDoublestarParts(patParts[pi+1:], pathParts[k:]) {
					return true
				}
			}
			return false
		}
		matched, _ := filepath.Match(patParts[pi], pathParts[pj])
		if !matched {
			return false
		}
		pi++
		pj++
	}
	// Consume trailing ** in pattern
	for pi < len(patParts) && patParts[pi] == "**" {
		pi++
	}
	return pi == len(patParts) && pj == len(pathParts)
}

// parseSearchParams validates the request and resolves the anchored walk root,
// following the same anchoring rules as HandleFileTree (see fileTreeRootFor).
// An empty query or keyword list is valid (yields no results); the returned
// params have an empty anchor in that case.
func (h *Handler) parseSearchParams(r *http.Request) (fileSearchParams, error) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		query = strings.TrimSpace(r.URL.Query().Get("query"))
	}
	if query == "" {
		return fileSearchParams{}, nil
	}
	root := r.URL.Query().Get("path")
	if root == "" {
		root = h.workDir
	}
	if root == "" {
		return fileSearchParams{}, errors.New("server has no working directory configured")
	}
	if !filepath.IsAbs(root) && h.workDir != "" {
		root = filepath.Join(h.workDir, root)
	}
	matchedRoot := h.workDir
	if r.URL.Query().Get("path") != "" {
		if h.workDir == "" {
			return fileSearchParams{}, errors.New("server has no working directory configured; explicit path not allowed")
		}
		dir, ok := h.fileTreeRootFor(root)
		if !ok {
			return fileSearchParams{}, errors.New("path outside working directory")
		}
		matchedRoot = dir
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return fileSearchParams{}, errors.New("invalid path")
	}
	anchor := base
	if matchedRoot != "" {
		if abs, err := filepath.Abs(matchedRoot); err == nil {
			anchor = abs
		}
	}
	// New filter flags (all default false to preserve existing case-insensitive literal behavior)
	isRegex := parseBoolParam(r.URL.Query().Get("regex"))
	if raw := r.URL.Query().Get("match"); raw != "" && strings.EqualFold(strings.TrimSpace(raw), "regex") {
		isRegex = true
	}
	caseSensitive := parseBoolParam(r.URL.Query().Get("caseSensitive"))
	if !caseSensitive {
		caseSensitive = parseBoolParam(r.URL.Query().Get("case"))
	}
	wholeWord := parseBoolParam(r.URL.Query().Get("wholeWord"))
	includeIgnored := parseBoolParam(r.URL.Query().Get("includeIgnored"))

	var keywords []string
	if !isRegex {
		// Keywords split preserves today's whitespace-AND semantics; case is deferred to matcher.
		keywords = strings.Fields(query)
		if len(keywords) == 0 {
			return fileSearchParams{}, nil
		}
	} else {
		// Regex mode validates the pattern eagerly so the client gets a 400 instead of empty results.
		if _, err := compileFileSearchPattern(query, caseSensitive, wholeWord, true); err != nil {
			return fileSearchParams{}, errors.New("invalid regex: " + err.Error())
		}
		keywords = []string{query}
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
	ignorePatterns := parseIgnorePatterns(r.URL.Query().Get("ignore"))
	if len(ignorePatterns) == 0 {
		ignorePatterns = parseIgnorePatterns(r.URL.Query().Get("exclude"))
	}
	return fileSearchParams{query: query, anchor: anchor, keywords: keywords, exts: exts, ignorePatterns: ignorePatterns, regex: isRegex, caseSensitive: caseSensitive, wholeWord: wholeWord, includeIgnored: includeIgnored}, nil
}

// searchFiles walks the tree rooted at anchor in filesystem order, invoking
// emit for every matching line until maxTotal matches are emitted, the context
// is cancelled, or emit returns an error (which aborts the walk and is
// returned as-is). It returns the number of matches emitted, whether the scan
// was stopped by the cap, and any aborting error (emit error or ctx.Err()).
// emit is called synchronously inside the walk.
func searchFiles(ctx context.Context, params fileSearchParams, maxTotal int, emit func(FileSearchResult) error) (int, bool, error) {
	anchor := params.anchor
	exts := params.exts
	keywords := params.keywords
	ignorePatterns := params.ignorePatterns
	includeIgnored := params.includeIgnored
	isRegex := params.regex
	// Precompile matchers once so the per-line hot path is cheap.
	var regexMatcher *regexp.Regexp
	var keywordRegexes []*regexp.Regexp
	if isRegex && params.query != "" {
		re, err := compileFileSearchPattern(params.query, params.caseSensitive, params.wholeWord, true)
		if err != nil {
			return 0, false, err
		}
		regexMatcher = re
	} else if (params.caseSensitive || params.wholeWord) && len(keywords) > 0 {
		for _, kw := range keywords {
			re, err := compileFileSearchPattern(kw, params.caseSensitive, params.wholeWord, false)
			if err != nil {
				return 0, false, err
			}
			keywordRegexes = append(keywordRegexes, re)
		}
	}
	total := 0
	capped := false
	var abortErr error
	_ = filepath.WalkDir(anchor, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			abortErr = err
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// When includeIgnored is false (default) we skip common ignored dirs and hidden dirs.
			// When true we walk everything except we keep skipping .git to avoid huge noisy walks? Mirror TUI:
			// TUI only skips when !includeIgnored, otherwise walks hidden. Keep behavior identical here.
			if !includeIgnored {
				if name == ".git" || name == "node_modules" || name == "vendor" || name == "target" || name == ".history" {
					return filepath.SkipDir
				}
				if strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
			} else {
				// Even when including ignored, still skip .git to avoid scanning massive .git objects?
				// TUI walks .git when includeIgnored is true (only skips when !includeIgnored). Keep parity with TUI: don't skip.
			}
			// Honor ignore globs on directories: if the directory itself matches ignore, skip descending.
			if len(ignorePatterns) > 0 {
				relDir, _ := filepath.Rel(anchor, path)
				if relDir != "." && matchesIgnore(relDir, ignorePatterns) {
					return filepath.SkipDir
				}
				// Also test directory name alone for convenience (e.g. ignore "dist")
				if matchesIgnore(name, ignorePatterns) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !includeIgnored {
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}
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
		rel, _ := filepath.Rel(anchor, path)
		if len(ignorePatterns) > 0 && matchesIgnore(rel, ignorePatterns) {
			return nil
		}
		if len(ignorePatterns) > 0 && matchesIgnore(d.Name(), ignorePatterns) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxSearchFileSize {
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
			line := scanner.Text()
			matchedLine := false
			if regexMatcher != nil {
				matchedLine = regexMatcher.MatchString(line)
			} else if len(keywordRegexes) > 0 {
				matchedLine = true
				for _, re := range keywordRegexes {
					if !re.MatchString(line) {
						matchedLine = false
						break
					}
				}
			} else {
				lower := strings.ToLower(line)
				ok := true
				for _, kw := range keywords {
					// keywords preserved original case but default mode is case-insensitive, so lower both.
					if !strings.Contains(lower, strings.ToLower(kw)) {
						ok = false
						break
					}
				}
				matchedLine = ok
			}
			if !matchedLine {
				continue
			}
			// Preserve exact line; cap by runes to avoid splitting UTF-8 and bound payload
			const maxRunes = 2000
			if len([]rune(line)) > maxRunes {
				runes := []rune(line)
				line = string(runes[:maxRunes])
			}
			if err := emit(FileSearchResult{
				Path: rel,
				Line: lineNum,
				Text: line,
			}); err != nil {
				f.Close()
				abortErr = err
				return err
			}
			total++
			if total >= maxTotal {
				capped = true
				f.Close()
				return filepath.SkipAll
			}
		}
		f.Close()
		return nil
	})
	return total, capped, abortErr
}

// HandleFileSearch searches file contents within the anchored project root.
// Query params:
//   q / query : keywords (whitespace-AND when regex=0, single pattern when regex=1)
//   path      : project root (same anchoring as HandleFileTree)
//   exts      : comma-separated include extensions, e.g. "*.go,*.ts" (normalized)
//   ignore / exclude : comma-separated ignore globs, e.g. "*.log,dist/**,*.min.js"
//                      patterns without "/" match basename at any depth so "*.log" ignores all logs;
//                      "dist/**" ignores subtree. Wildcards * ? and ** are supported.
//   regex / match : when regex=1 or match=regex, query is a single RE2 regex instead of keywords-AND
//   caseSensitive / case : when 1, matching is case-sensitive (default case-insensitive)
//   wholeWord     : when 1, matches must be whole words (Unicode \pL\pN_ boundaries)
//   includeIgnored: when 1, hidden files/dirs are walked; default walks only visible files (skips .git, node_modules, dotfiles)
//
// Pagination: `limit` is page size (default 50, max 100, see maxSearchPageSize) with `offset`.
// Stream variant `limit` is a total-result cap instead (see HandleFileSearchStream).
// Invalid regex returns 400. The walk collects ALL matches then slices the page — see stream for incremental SSE.
func (h *Handler) HandleFileSearch(w http.ResponseWriter, r *http.Request) {
	p, err := h.parseSearchParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(p.keywords) == 0 {
		writeJSON(w, http.StatusOK, FileSearchResponse{Results: []FileSearchResult{}})
		return
	}
	// Pagination: infinite scroll support via offset/limit (page size, not total cap)
	offset := 0
	limit := 50
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			offset = v
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= maxSearchPageSize {
			limit = v
		}
	}
	allResults := make([]FileSearchResult, 0, 512)
	_, hasMoreDueToCap, _ := searchFiles(r.Context(), p, maxSearchResults, func(res FileSearchResult) error {
		allResults = append(allResults, res)
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

// previewRawMaxBytes caps GET /api/files/raw responses so a malicious or
// accidental request for a huge binary (video, disk image) can't OOM the
// server or the preview tab. 32 MiB covers real docx/pptx/pdf decks.
const previewRawMaxBytes = 32 << 20

// previewRawTypes allowlists previewable binary extensions. The browser
// renderers (pdf.js, docx-preview, jszip slide parser) only need these;
// anything else falls back to the text content endpoint or OS-open.
var previewRawTypes = map[string]string{
	".pdf":  "application/pdf",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".xls":  "application/vnd.ms-excel",
	".csv":  "text/csv; charset=utf-8",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".mmd":  "text/plain; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
}

// HandleFileRaw serves previewable file bytes for PreviewHost (pdf.js,
// docx-preview, pptx slide parser, mermaid). Same anchoring and containment
// rules as HandleFileContent; directories, unknown extensions, and files
// over the size cap are rejected so the endpoint can't be used as an
// arbitrary file exfiltration channel.
func (h *Handler) HandleFileRaw(w http.ResponseWriter, r *http.Request) {
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
	} else if !filepath.IsAbs(path) && h.workDir != "" {
		path = filepath.Join(h.workDir, path)
	}

	ct, ok := previewRawTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		writeError(w, http.StatusBadRequest, "file type is not previewable as raw bytes")
		return
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if info.Size() > previewRawMaxBytes {
		writeError(w, http.StatusBadRequest, "file exceeds the 32 MiB preview limit")
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type saveFileContentRequest struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	ProjectRoot  string `json:"project_root,omitempty"`
	ExpectedHash string `json:"expected_hash,omitempty"`
	Force        bool   `json:"force,omitempty"`
}

// hashContent mirrors the frontend's hashContent() (djb2, base-36) so the
// save guard can compare the client's base hash with the current disk
// content without transferring the full original text. It iterates over
// UTF-16 code units (surrogate pairs for non-BMP) to match JS's
// String.charCodeAt semantics.
func hashContent(s string) string {
	h := int32(5381)
	for _, r := range s {
		if r <= 0xFFFF {
			h = (h<<5 + h + int32(r))
		} else {
			r2 := r - 0x10000
			high := int32(0xD800 + (r2 >> 10))
			low := int32(0xDC00 + (r2 & 0x3FF))
			h = (h<<5 + h + high)
			h = (h<<5 + h + low)
		}
	}
	u := uint32(h)
	if u == 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var buf [13]byte
	i := len(buf)
	for u > 0 {
		i--
		buf[i] = digits[u%36]
		u /= 36
	}
	return string(buf[i:])
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

	// Serialize concurrent ocode saves for the same path so the
	// compare-and-write below is not TOCTOU-racy against other ocode
	// requests. External editors remain uncooperative (no mandatory lock).
	saveMu := h.saveLockFor(realTarget)
	saveMu.Lock()
	defer saveMu.Unlock()

	if req.ExpectedHash != "" && !req.Force {
		// Distinguish "missing" from "present but empty" — both would hash
		// to hashContent("") if we treated missing as "", allowing a normal
		// save to silently recreate a deleted empty file. Use a sentinel that
		// never collides with hashContent's base-36 output.
		const missingSentinel = "__missing__"
		diskHash := missingSentinel
		if data, err := os.ReadFile(realTarget); err == nil {
			diskHash = hashContent(string(data))
		} else if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
			diskHash = missingSentinel
		} else {
			// Unreadable (e.g. is a directory) — fall through to the normal
			// write path which will surface the OS error; don't block with 409.
			diskHash = req.ExpectedHash // force pass-through
		}
		// TOCTOU note: the hash is read, then the file is opened/truncated
		// separately. With saveLockFor, concurrent ocode saves for the same
		// path are serialized, closing the ocode-vs-ocode race. An external
		// writer can still race without kernel-level mandatory locking; the
		// 409 remains best-effort, not linearizable, for uncooperative editors.
		if diskHash != req.ExpectedHash {
			writeError(w, http.StatusConflict, "file has changed on disk since it was opened; reload or force-save to overwrite")
			return
		}
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
