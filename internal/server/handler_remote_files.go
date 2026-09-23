package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/u007/ocode/internal/remote"
)

// remoteSearchFiles is the content search over the transport, backed by one
// remote grep invocation. AND-keyword mode chains grep -e k1 | grep -e k2 …
// (grep ORs multiple -e patterns in one invocation, so AND needs the pipe).
// Regex mode passes the pattern as a single -E -e. Caps mirror the local
// walk's bounds: at most maxSearchResults matches, truncated in memory.
//
// Flag mapping vs the local walk (parseSearchParams contract):
//   - caseSensitive: -i is passed only when matching case-insensitively
//     (both the first grep and the AND-pipe stages).
//   - wholeWord: -w is passed. Approximation: grep -w uses ASCII
//     [A-Za-z0-9_] word characters while the local matcher uses Unicode
//     \pL\pN_ boundaries — CJK/accented edge cases may differ.
//   - includeIgnored: when false (default) .git, node_modules, vendor,
//     target, .history and all dotfiles/dot-dirs are excluded via
//     --exclude-dir/--exclude, mirroring the local walk's skips. When true
//     everything is walked (same as the local includeIgnored path).
//   - regex: the pattern is validated as Go RE2 by parseSearchParams but
//     executed as POSIX grep -E (ERE). RE2-only constructs (e.g. \p{...},
//     (?:...), (?i:...)) pass validation then fail remotely — the grep
//     stderr surfaces in the returned error, never silent "no results"
//     (grep exit 2 propagates; exit 1 stays the empty-result case).
func remoteSearchFiles(ctx context.Context, rw remoteWork, p fileSearchParams) ([]FileSearchResult, bool, error) {
	if len(p.keywords) == 0 {
		return []FileSearchResult{}, false, nil
	}
	// Extra-keyword AND: run grep once per keyword over the previous
	// output. Simpler: pipe them.
	script := "cd " + remote.ShellQuotePath(rw.Path) + " && "
	var chain strings.Builder
	chain.WriteString("grep -rIn")
	if !p.caseSensitive {
		chain.WriteString(" -i")
	}
	if p.wholeWord {
		chain.WriteString(" -w")
	}
	if p.regex {
		chain.WriteString(" -E -e " + remote.ShellQuote(p.keywords[0]))
	} else {
		chain.WriteString(" -e " + remote.ShellQuote(p.keywords[0]))
	}
	if !p.includeIgnored {
		// Mirror the local walk's default skips (searchFiles: .git,
		// node_modules, vendor, target, .history, plus every dotfile/
		// dot-dir). --exclude-dir matches dir basenames, --exclude
		// matches file basenames.
		for _, d := range []string{".*", "node_modules", "vendor", "target", ".history"} {
			chain.WriteString(" --exclude-dir=" + remote.ShellQuote(d))
		}
		chain.WriteString(" --exclude=" + remote.ShellQuote(".*"))
	}
	for _, ig := range p.ignorePatterns {
		chain.WriteString(" --exclude=" + remote.ShellQuote(ig))
	}
	for _, e := range p.exts {
		chain.WriteString(" --include=" + remote.ShellQuote("*."+e))
	}
	chain.WriteString(" . 2>/dev/null")
	for _, kw := range extraKw(p.keywords) {
		chain.WriteString(" | grep")
		if !p.caseSensitive {
			chain.WriteString(" -i")
		}
		if p.wholeWord {
			chain.WriteString(" -w")
		}
		chain.WriteString(" -e " + remote.ShellQuote(kw))
	}
	chain.WriteString(" | head -n " + strconv.Itoa(maxSearchResults+1))
	script += chain.String()
	out, err := remoteRun(ctx, rw, script)
	if err != nil {
		// grep exits 1 on no matches — that's an empty result, not a
		// failure. Exit 2 (bad pattern/options, e.g. an RE2-only regex
		// under grep -E) propagates as an error so the caller can
		// surface stderr instead of reporting silent "no results".
		if strings.Contains(err.Error(), "exit status 1") || strings.Contains(err.Error(), "exit 1") {
			return []FileSearchResult{}, false, nil
		}
		return nil, false, err
	}
	results := []FileSearchResult{}
	capped := false
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		rel, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		lnStr, text, ok2 := strings.Cut(rest, ":")
		if !ok2 {
			continue
		}
		ln, lerr := strconv.Atoi(lnStr)
		if lerr != nil || ln < 1 {
			continue
		}
		if len(results) >= maxSearchResults {
			capped = true
			break
		}
		const maxRunes = 2000
		if len([]rune(text)) > maxRunes {
			text = string([]rune(text)[:maxRunes])
		}
		results = append(results, FileSearchResult{Path: strings.TrimPrefix(rel, "./"), Line: ln, Text: text})
	}
	return results, capped, nil
}

// extraKw returns keywords[1:] (the grep pipeline's AND keywords).
func extraKw(kws []string) []string {
	if len(kws) <= 1 {
		return nil
	}
	return kws[1:]
}

// remoteFileTree is buildFileTree's response over the transport. The walk
// runs remotely in one exec: a POSIX shell script emits one line per entry
// in "T<tab>relpath" form (T: d=file, f=file; sorted dirs-first), bounded by
// maxTreeNodes and depth — then the client annotates git badges in a second
// exec. Output order matches the local pipeline's (dirs first, name-sorted).
func (h *Handler) remoteFileTree(w http.ResponseWriter, r *http.Request) {
	host := hostParam(r)
	root := r.URL.Query().Get("path")
	rw, err := h.remoteWorkFor(host, root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	maxDepth := 4
	if raw := r.URL.Query().Get("depth"); raw != "" {
		if n, derr := atoiNonNegative(raw); derr == nil && n >= 0 {
			maxDepth = n
		}
	}
	showHidden := parseBoolParam(r.URL.Query().Get("show_hidden"))
	children, truncated, isRepo := remoteWalkTree(ctx, rw, maxDepth, showHidden)
	writeJSON(w, http.StatusOK, FileTreeResponse{
		Children:  children,
		Truncated: truncated,
		IsGitRepo: isRepo,
	})
}

// atoiNonNegative is a strict non-negative int parser.
func atoiNonNegative(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("not a non-negative integer")
	}
	return n, nil
}

// remoteWalkTree lists entries under rw.Path up to maxDepth (0 = unlimited)
// using a single remote exec, mirroring buildFileTree's filtering (hidden
// files, ignored dirs) and ordering (directories first, then name-sorted).
// The count cap mirrors maxTreeNodes. The remote script is pure POSIX;
// paths are quoted with remote.ShellQuotePath.
func remoteWalkTree(ctx context.Context, rw remoteWork, maxDepth int, showHidden bool) ([]FileNode, bool, bool) {
	q := remote.ShellQuotePath(rw.Path)
	skip := ""
	if !showHidden {
		skip = `-name ".*" -o -name node_modules -o -name vendor -o -name dist -o -name build -o -name target -o -name .next -o -name .venv -o -name venv -o -name __pycache__ -o -name coverage -o -name bower_components -o -name .cache`
	}
	// find with -maxdepth relative to the root; depth semantics: 1 lists the
	// root's immediate children (same as depth=1 local), 0/negative =
	// unlimited (local depth=0 contract).
	maxd := ""
	if maxDepth > 0 {
		maxd = fmt.Sprintf("-maxdepth %d", maxDepth)
	}
	prune := ""
	if skip != "" {
		prune = `-name ".*" -o -name node_modules -o -name vendor -o -name dist -o -name build -o -name target -o -name .next -o -name .venv -o -name venv -o -name __pycache__ -o -name coverage -o -name bower_components -o -name .cache`
	}
	script := `cd ` + q + ` 2>/dev/null || exit 3; ` +
		`if [ ! -d .git ] && ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then repo=0; else repo=1; fi; ` +
		`printf 'R%s\n' "$repo"; ` +
		// Prune-then-print idiom: matching subtrees are pruned, everything
		// else printed. With no prune list -false matches nothing, so
		// -prune prunes nothing and the -o -print arm prints everything.
		// -mindepth 1 is REQUIRED when the prune list contains -name ".*":
		// the root itself is named ".", matches the hidden-name pattern, and
		// -prune then prunes the ENTIRE tree — find printed nothing at all
		// (empty Files tab on remote projects). -mindepth 1 skips the root so
		// the prune clause only ever matches real child entries. Paths are
		// also stripped of the leading "./" so FileNode.Path is repo-relative
		// (matches git status --short keys for the badge map and the frontend's
		// projectRoot-joined open paths).
		`find . -mindepth 1 ` + maxd + ` \( ` + pruneOrTrue(prune) + ` \) -prune -o -print | LC_ALL=C sort | sed 's|^\./||' | head -n ` + strconv.Itoa(maxTreeNodes+2) + ` | while IFS= read -r p; do ` +
		`[ -z "$p" ] && continue; ` +
		`if [ -d "$p" ]; then printf 'd\t%s\n' "$p"; elif [ -f "$p" ]; then printf 'f\t%s\n' "$p"; fi; done; ` +
		`printf 'END\n'`
	out, err := remoteRun(ctx, rw, script)
	if err != nil {
		// Unreachable host / not a directory: an empty tree (the frontend
		// renders "no files" rather than an error, mirroring local walk
		// tolerance of unreadable dirs).
		return []FileNode{}, false, false
	}
	truncated := false
	isRepo := false
	// git status map for badges (only when the repo marker said 1).
	statusMap := map[string]string{}
	lines := strings.Split(out, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "R") {
		if lines[0] == "R1" {
			isRepo = true
		}
		lines = lines[1:]
	}
	// Collect entries; count children against the cap the same way
	// buildFileTree counts nodes.
	count := 0
	var entries []FileNode
	for _, line := range lines {
		if line == "END" {
			break
		}
		if line == "" {
			continue
		}
		kind, rel, ok2 := strings.Cut(line, "\t")
		if !ok2 {
			continue
		}
		if kind != "d" && kind != "f" {
			continue
		}
		count++
		if count >= maxTreeNodes {
			truncated = true
			break
		}
		name := rel[strings.LastIndex(rel, "/")+1:]
		node := FileNode{Name: name, Path: rel, IsDir: kind == "d"}
		entries = append(entries, node)
	}
	// Git badges: one `status --short` per tree (cheap, mirrors the local
	// gitStatusMapForDir). Applied only when this is a repo.
	if isRepo {
		if sm, serr := remoteGitStatusMap(ctx, rw); serr == nil {
			statusMap = sm
		}
	}
	// Directories first, then byte-sorted by name — the same ordering contract
	// as the local buildFileTree walk (the frontend renders server order as-is
	// for the tree view).
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	applyRemoteTreeBadges(entries, statusMap)
	if len(entries) == 0 {
		entries = []FileNode{}
	}
	return entries, truncated, isRepo
}

// pruneOrTrue returns the prune expression, or -false when nothing should
// be pruned (showHidden): a -false clause matches no subtree, so -prune
// prunes nothing and the -o -print arm prints everything.
func pruneOrTrue(prune string) string {
	if prune == "" {
		return `-false`
	}
	return prune
}

// applyRemoteTreeBadges mirrors annotateFileTreeGitStatus: sets the badge on
// matching nodes and propagates child badges to parent directories. entries
// are in sorted (parents-before-children) order from find, so a simple map
// lookup per node plus a child→parent pass reproduces the local behavior.
func applyRemoteTreeBadges(entries []FileNode, m map[string]string) {
	if len(m) == 0 {
		return
	}
	// Index nodes by path for parent propagation.
	byPath := make(map[string]*FileNode, len(entries))
	for i := range entries {
		byPath[entries[i].Path] = &entries[i]
	}
	for i := range entries {
		if entries[i].IsDir {
			continue
		}
		if badge, ok := m[entries[i].Path]; ok {
			entries[i].GitStatus = badge
		}
	}
	// Propagate up: walk in reverse (children come after parents in find
	// output order... actually find lists parents before children), so walk
	// forward and mark parents from children using longest-prefix match.
	for i := range entries {
		if entries[i].GitStatus == "" {
			continue
		}
		// Mark every ancestor directory of this node.
		p := entries[i].Path
		for {
			idx := strings.LastIndex(p, "/")
			if idx <= 0 {
				break
			}
			p = p[:idx]
			if parent, ok := byPath[p]; ok && parent.IsDir && parent.GitStatus == "" {
				parent.GitStatus = entries[i].GitStatus
			}
		}
	}
}

// remoteGitStatusMap is gitStatusMapForDir over the transport.
func remoteGitStatusMap(ctx context.Context, rw remoteWork) (map[string]string, error) {
	out, err := remoteRun(ctx, rw, remoteGitCommand(rw.Path,
		"-c", "core.quotepath=false", "status", "--short"))
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
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
		return nil, fmt.Errorf("no status entries")
	}
	return m, nil
}

// remoteFileContent is HandleFileContent's GET over the transport. Returns
// {"path","content","is_binary"}; found=false maps to 404 like os.ReadFile.
func (h *Handler) remoteFileContent(w http.ResponseWriter, r *http.Request) {
	host := hostParam(r)
	path := r.URL.Query().Get("path")
	root := r.URL.Query().Get("project_root")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	rw, err := h.remoteWorkFor(host, root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if root != "" {
		abs := remoteAbsJoin(rw.Path, path)
		if _, cerr := remoteRelCheck(rw.Path, abs); cerr != nil {
			writeError(w, http.StatusBadRequest, "path is outside the project root")
			return
		}
		path = abs
	} else {
		path = remoteAbsJoin(rw.Path, path)
	}
	data, found, rerr := remoteReadFile(r.Context(), rw, path)
	if rerr != nil {
		writeError(w, http.StatusInternalServerError, rerr.Error())
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	data, isBinary := editorTextContent(data)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":      path,
		"content":   string(data),
		"is_binary": isBinary,
	})
}

// remoteFileRaw is HandleFileRaw's GET over the transport: the same
// extension allowlist and per-type size cap as the local path (previewRawCap:
// 32 MiB documents/images, 128 MiB audio/video), enforced remotely (stat +
// byte count) before the base64 read so a huge binary can't OOM the server.
// found=false maps to 404 like os.ReadFile.
func (h *Handler) remoteFileRaw(w http.ResponseWriter, r *http.Request) {
	host := hostParam(r)
	path := r.URL.Query().Get("path")
	root := r.URL.Query().Get("project_root")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	rw, err := h.remoteWorkFor(host, root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if root != "" {
		abs := remoteAbsJoin(rw.Path, path)
		if _, cerr := remoteRelCheck(rw.Path, abs); cerr != nil {
			writeError(w, http.StatusBadRequest, "path is outside the project root")
			return
		}
		path = abs
	} else {
		path = remoteAbsJoin(rw.Path, path)
	}

	ct, ok := previewRawTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		writeError(w, http.StatusBadRequest, "file type is not previewable as raw bytes")
		return
	}

	data, found, rerr := remoteReadFileCapped(r.Context(), rw, path, previewRawCap(ct))
	if rerr != nil {
		if errors.Is(rerr, errTooLarge) {
			writeError(w, http.StatusBadRequest, "file exceeds the "+strconv.Itoa(int(previewRawCap(ct)>>20))+" MiB preview limit")
		} else if isRemoteTransportError(rerr) {
			writeError(w, http.StatusBadGateway, "remote read failed: "+rerr.Error())
		} else {
			writeError(w, http.StatusInternalServerError, rerr.Error())
		}
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// remoteSaveFileContent is HandleSaveFileContent's PUT over the transport.
// The expected-hash guard mirrors the local compare-and-write: the current
// remote content is hashed with hashContent and a mismatch is a 409 unless
// force is set. Writes travel via stdin (remoteWriteFile).
func (h *Handler) remoteSaveFileContent(w http.ResponseWriter, r *http.Request) {
	var req saveFileContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	host := hostParam(r)
	root := req.ProjectRoot
	if root == "" {
		writeError(w, http.StatusBadRequest, "project_root is required for remote saves")
		return
	}
	rw, err := h.remoteWorkFor(host, root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	abs := remoteAbsJoin(rw.Path, req.Path)
	rel, cerr := remoteRelCheck(rw.Path, abs)
	if cerr != nil || rel == "." {
		writeError(w, http.StatusBadRequest, "path is outside the workspace")
		return
	}
	// Serialize concurrent saves for the same remote path (same contract as
	// saveLockFor; keyed on host:path:rel so two remote projects don't clash).
	saveMu := h.saveLockFor("remote:" + rw.Target.String() + ":" + abs)
	saveMu.Lock()
	defer saveMu.Unlock()

	if req.ExpectedHash != "" && !req.Force {
		data, found, rerr := remoteReadFile(ctx, rw, abs)
		diskHash := "__missing__"
		if rerr == nil && found {
			diskHash = hashContent(string(data))
		}
		if diskHash != req.ExpectedHash {
			writeError(w, http.StatusConflict, "file has changed on disk since it was opened; reload or force-save to overwrite")
			return
		}
	}
	if werr := remoteWriteFile(ctx, rw, abs, []byte(req.Content)); werr != nil {
		writeError(w, http.StatusInternalServerError, werr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"path": abs, "saved": true})
}

// remoteFileSearch is the paginated content search over the transport,
// backed by a single remote grep invocation. Grep semantics differ slightly
// from the local walk (keyword AND is translated to consecutive grep -e
// patterns), and caps mirror maxSearchResults/maxSearchFileSize loosely via
// grep's own limits (head -c is not used; results are capped in memory).
func (h *Handler) remoteFileSearch(w http.ResponseWriter, r *http.Request) error {
	host := hostParam(r)
	root := r.URL.Query().Get("path")
	rw, err := h.remoteWorkFor(host, root)
	if err != nil {
		return err
	}
	p, perr := h.parseSearchParams(r)
	if perr != nil {
		return perr
	}
	if len(p.keywords) == 0 {
		writeJSON(w, http.StatusOK, FileSearchResponse{Results: []FileSearchResult{}})
		return nil
	}
	results, capped, serr := remoteSearchFiles(r.Context(), rw, p)
	if serr != nil {
		return serr
	}
	offset := 0
	limit := 50
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, e := strconv.Atoi(raw); e == nil && v >= 0 {
			offset = v
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, e := strconv.Atoi(raw); e == nil && v > 0 && v <= maxSearchPageSize {
			limit = v
		}
	}
	total := len(results)
	hasMore := capped || total > offset+limit
	if offset >= total {
		writeJSON(w, http.StatusOK, FileSearchResponse{Results: []FileSearchResult{}, Truncated: capped, Total: total, HasMore: false, Capped: capped})
		return nil
	}
	end := offset + limit
	if end > total {
		end = total
		hasMore = capped
	}
	writeJSON(w, http.StatusOK, FileSearchResponse{
		Results:   results[offset:end],
		Truncated: hasMore || capped,
		Total:     total,
		HasMore:   hasMore,
		Capped:    capped,
	})
	return nil
}

// remoteFileSearchStream is HandleFileSearchStream over the transport: the
// whole remote grep runs first, then the same SSE frame sequence (result
// batch, done) is emitted. Params are parsed BEFORE any SSE bytes are
// written so an invalid regex gets the local path's 400, not a 200 with
// an empty done frame. Remote grep failures emit an `error` frame (not a
// silent empty done) so a failed grep is distinguishable from an empty
// repo — the FileTree stream consumer clears its spinner on done but logs
// error frames.
func (h *Handler) remoteFileSearchStream(w http.ResponseWriter, r *http.Request) {
	host := hostParam(r)
	root := r.URL.Query().Get("path")
	rw, rerr := h.remoteWorkFor(host, root)
	if rerr != nil {
		writeError(w, http.StatusBadRequest, rerr.Error())
		return
	}
	p, perr := h.parseSearchParams(r)
	if perr != nil {
		writeError(w, http.StatusBadRequest, perr.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	if _, err := fmt.Fprintf(w, ": search started\n\n"); err != nil {
		return
	}
	flusher.Flush()
	writeFrame := func(event string, data interface{}) error {
		jsonData, jerr := json.Marshal(data)
		if jerr != nil {
			return jerr
		}
		_, werr := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
		flusher.Flush()
		return werr
	}
	if len(p.keywords) == 0 {
		// Match the local stream: empty query is a 200 with an empty
		// done frame, not a 400 (only parse errors are 400).
		_ = writeFrame("done", FileSearchStreamDone{Total: 0})
		return
	}
	results, capped, serr := remoteSearchFiles(r.Context(), rw, p)
	if serr != nil {
		_ = writeFrame("error", map[string]string{"error": serr.Error()})
		_ = writeFrame("done", FileSearchStreamDone{Total: 0})
		return
	}
	total := len(results)
	if len(results) > 0 {
		_ = writeFrame("result", map[string]interface{}{"results": results})
	}
	_ = writeFrame("done", FileSearchStreamDone{Total: total, Capped: capped, Truncated: capped})
}
