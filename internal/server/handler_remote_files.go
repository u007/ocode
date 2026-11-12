package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os/exec"
	"path"
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
//     (?:...), (?i:...)) pass validation then fail remotely. KNOWN
//     LIMITATION: the output is bounded by `head`, so the pipeline's exit
//     status is head's — grep's own exit 2 (bad pattern/option) is masked
//     and shows up as an empty result. Only a failed cd/root check (exit 3,
//     emitted before the pipeline) reaches the caller as an error.
func remoteSearchFiles(ctx context.Context, rw remoteWork, p fileSearchParams) ([]FileSearchResult, bool, error) {
	if len(p.keywords) == 0 {
		return []FileSearchResult{}, false, nil
	}
	script, base, cerr := remoteSearchCommand(rw, p)
	if cerr != nil {
		return nil, false, cerr
	}
	out, err := remoteRun(ctx, rw, script)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// grep exits 1 on no matches — that's an empty result, not a
			// failure. Any other status (2: bad pattern/options; 3: the root
			// could not be entered) propagates as an error so the caller can
			// surface it instead of reporting a silent "no results".
			return []FileSearchResult{}, false, nil
		}
		return nil, false, err
	}
	results, capped := parseRemoteSearchOutput(out, base)
	return results, capped, nil
}

// remoteSearchCommand builds the single remote shell command for the content
// search and returns the path prefix its output lines carry (the searched
// directory's basename).
//
// The command must NOT be `cd <root> && grep … .`. grep matches --exclude-dir
// patterns against the search ROOT as well as its children, and the ".*"
// pattern that skips dot-directories also matches the operand "." — so grep
// prunes the root itself and every remote content search silently returns
// zero matches (GNU grep 3.11 and BSD grep 2.6.0 both do this). Naming the
// directory instead (`cd <parent> && grep … <name>`) keeps the hidden-dir
// exclusion while the root, now a plain basename, never matches a dot
// pattern. Output paths are therefore "<name>/rel"; parseRemoteSearchOutput
// strips that prefix to recover project-relative paths.
func remoteSearchCommand(rw remoteWork, p fileSearchParams) (command, base string, err error) {
	root := strings.TrimRight(rw.Path, "/")
	if root == "" {
		root = "/"
	}
	dir, base := path.Dir(root), path.Base(root)
	if base == "." || base == "/" {
		// A relative "." (or filesystem root) cannot be named without
		// re-introducing the root-prune bug. Registered remote projects are
		// absolute or "~/"-rooted, so refuse instead of returning nothing.
		return "", "", fmt.Errorf("remote search root must name a directory, got %q", rw.Path)
	}
	// Extra-keyword AND: run grep once per keyword over the previous
	// output. Simpler: pipe them.
	var chain strings.Builder
	// A stale/missing project path exits 3 so it is an error, not an empty
	// result (grep's own status is hidden behind the pipe to head). The root
	// itself is checked because the cd targets its PARENT.
	chain.WriteString("cd " + remote.ShellQuotePath(dir) + " || exit 3; [ -d " + remote.ShellQuote(base) + " ] || exit 3; ")
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
	// Extensions go BEFORE any --exclude: GNU grep only honours --include
	// when the --include/--exclude pair starts with --include. With
	// --exclude first, grep searches EVERY file and the include filter is
	// silently dropped. (--exclude-dir is not part of the pair, so it can
	// precede --include, but keeping includes first is simpler and safe on
	// both GNU and BSD grep.)
	for _, e := range p.exts {
		chain.WriteString(" --include=" + remote.ShellQuote("*."+e))
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
	chain.WriteString(" " + remote.ShellQuote(base) + " 2>/dev/null")
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
	return chain.String(), base, nil
}

// parseRemoteSearchOutput parses `grep -rIn` output whose paths are prefixed
// with base (the searched directory's basename), decoding each
// "path:line:text" record and stripping the prefix to a project-relative
// path. More than maxSearchResults records sets capped.
func parseRemoteSearchOutput(out, base string) ([]FileSearchResult, bool) {
	results := []FileSearchResult{}
	capped := false
	prefix := base + "/"
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
		results = append(results, FileSearchResult{Path: strings.TrimPrefix(rel, prefix), Line: ln, Text: text})
	}
	return results, capped
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
	// Expanding a folder requests a subdirectory of the project, not the
	// registered project path itself, so admit it through its containing
	// project root and walk the subdirectory.
	if containsDotDot(root) {
		writeError(w, http.StatusBadRequest, "path outside project root")
		return
	}
	anchor, ok := h.remoteTreeRoot(host, root)
	if !ok {
		writeError(w, http.StatusBadRequest, "host/project_path is not a remote project registered with this server")
		return
	}
	rw, err := h.remoteWorkFor(host, anchor)
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
	walk := remoteWork{Target: rw.Target, Path: root}
	children, truncated, isRepo := remoteWalkTree(ctx, walk, maxDepth, showHidden)
	// remoteWalkTree reports paths relative to the walked directory. Anchor
	// them to the project root, as the local handler does, so the frontend can
	// pass a child's Path straight back as projectRoot/Path on the next expand.
	// Badges were applied in the walk's own frame, so this runs after them.
	if rel := strings.Trim(strings.TrimPrefix(root, strings.TrimRight(anchor, "/")), "/"); rel != "" {
		for i := range children {
			children[i].Path = rel + "/" + children[i].Path
		}
	}
	writeJSON(w, http.StatusOK, FileTreeResponse{
		Children:  children,
		Truncated: truncated,
		IsGitRepo: isRepo,
	})
}

// remoteTreeRoot returns the registered remote project path (verbatim, as
// remoteWorkFor needs it) that contains p on host, the longest match when
// roots nest. p must equal the root or sit under root + "/", so a sibling
// sharing the root's prefix never matches.
func (h *Handler) remoteTreeRoot(host, p string) (string, bool) {
	if h.projects == nil || p == "" {
		return "", false
	}
	target, err := remote.ParseTarget(host)
	if err != nil {
		return "", false
	}
	best, bestLen := "", 0
	for _, proj := range h.projects.List() {
		if proj.Host != target.String() {
			continue
		}
		trimmed := strings.TrimRight(proj.Path, "/")
		if (p == trimmed || strings.HasPrefix(p, trimmed+"/")) && len(trimmed) > bestLen {
			best, bestLen = proj.Path, len(trimmed)
		}
	}
	return best, best != ""
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
	rw, err := h.remoteWorkForFileRead(host, root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if root != "" {
		abs := remoteAbsJoin(rw.Path, path)
		if _, cerr := remoteReadRelCheck(rw.Path, abs); cerr != nil {
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

// remoteRawContainment anchors a remote preview/read path the way remoteFileRaw,
// the access probe and the content endpoint all need: with a project_root the
// path must stay inside the remote project root (errMsg otherwise); without
// one it is only joined onto the work path. Read-side containment — the .git
// boundary is NOT applied (mirrors the local read path). It returns the
// absolute remote path, or errMsg.
func remoteRawContainment(rw remoteWork, path, root string) (string, string) {
	abs := remoteAbsJoin(rw.Path, path)
	if root != "" {
		if _, cerr := remoteReadRelCheck(rw.Path, abs); cerr != nil {
			return "", "path is outside the project root"
		}
	}
	return abs, ""
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
	rw, err := h.remoteWorkForFileRead(host, root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	path, errMsg := remoteRawContainment(rw, path, root)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
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

// remoteSaveFileRaw is HandleSaveFileRaw's PUT over the transport. It applies
// the local guards in the same order: lexical containment, a realpath check on
// the target's parent, a symlink refusal on the final component, the
// byte-exact SHA-256 expected-hash guard, and a one-time .bak of the existing
// bytes. project_root is required and the write travels via stdin
// (remoteWriteFile). It never falls back to a local write.
func (h *Handler) remoteSaveFileRaw(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path := q.Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || !strings.EqualFold(mediaType, "application/octet-stream") {
			writeError(w, http.StatusBadRequest, "Content-Type must be application/octet-stream")
			return
		}
	}
	expectedHash := q.Get("expected_hash")
	force := q.Get("force") == "1" || strings.EqualFold(q.Get("force"), "true")

	host := hostParam(r)
	root := q.Get("project_root")
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
	abs := remoteAbsJoin(rw.Path, path)
	rel, cerr := remoteRelCheck(rw.Path, abs)
	if cerr != nil || rel == "." {
		writeError(w, http.StatusBadRequest, "path is outside the workspace")
		return
	}
	// Raw-save allowlist on the resolved (remote-absolute) target path.
	if !rawSaveAllowedExt(filepath.Ext(abs)) {
		writeError(w, http.StatusBadRequest, "file type cannot be saved as raw")
		return
	}

	// Realpath containment: the parent must resolve inside the project root on
	// the host, so a symlinked directory cannot redirect the write elsewhere.
	inside, cerr := remoteDirInside(ctx, rw, filepath.Dir(abs))
	if cerr != nil {
		slog.Warn("raw save: resolve remote parent failed", "host", host, "path", abs, "err", cerr)
		if isRemoteTransportError(cerr) {
			writeError(w, http.StatusBadGateway, "failed to resolve path on the remote host")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if !inside {
		writeError(w, http.StatusBadRequest, "path is outside the workspace")
		return
	}

	data, ok := readRawSaveBody(w, r)
	if !ok {
		return
	}

	saveMu := h.saveLockFor("remote:" + rw.Target.String() + ":" + abs)
	saveMu.Lock()
	defer saveMu.Unlock()

	// Refuse a symlinked final component before reading it: remote reads and
	// the `cat >` write both follow links.
	kind, kerr := remoteLstatKind(ctx, rw, abs)
	if kerr != nil {
		slog.Error("raw save: remote stat failed", "host", host, "path", abs, "err", kerr)
		writeError(w, http.StatusBadGateway, "failed to inspect file on the remote host")
		return
	}
	if kind == "symlink" {
		writeError(w, http.StatusBadRequest, "refusing to save through a symlink")
		return
	}

	// The existing bytes are read on every save, not only when expected_hash
	// is set, because the one-time .bak needs them. Any read failure, including
	// a transport error, aborts with 502 and is never treated as a missing file.
	existing, found, rerr := remoteReadFile(ctx, rw, abs)
	if rerr != nil {
		slog.Error("raw save: remote read failed", "host", host, "path", abs, "err", rerr)
		writeError(w, http.StatusBadGateway, "failed to read current file on the remote host")
		return
	}
	// Overwriting an existing file needs expected_hash unless force=1, the same
	// rule as the local save. A missing target may still be created without one.
	if !force && expectedHash == "" && found {
		writeError(w, http.StatusBadRequest, "expected_hash required unless force=1")
		return
	}
	if expectedHash != "" && !force {
		diskHash := rawMissingHash
		if found {
			diskHash = rawContentHash(existing)
		}
		if diskHash != expectedHash {
			writeError(w, http.StatusConflict, "file has changed on disk since it was opened; reload or force-save to overwrite")
			return
		}
	}

	// One-time .bak of the pre-existing bytes. A backup that cannot be made
	// aborts the save, and an existing .bak (or a directory at that path) is
	// left alone.
	if found {
		bak := abs + ".bak"
		bakKind, berr := remoteLstatKind(ctx, rw, bak)
		if berr != nil {
			slog.Error("raw save: remote backup stat failed", "host", host, "path", bak, "err", berr)
			writeError(w, http.StatusBadGateway, "failed to inspect backup on the remote host")
			return
		}
		if bakKind == "symlink" {
			writeError(w, http.StatusBadRequest, "refusing to back up through a symlink")
			return
		}
		if bakKind == "missing" {
			if werr := remoteWriteFileAtomic(ctx, rw, bak, existing); werr != nil {
				slog.Error("raw save: remote backup write failed", "host", host, "path", bak, "err", werr)
				writeError(w, http.StatusInternalServerError, "failed to back up file before save")
				return
			}
		}
	}

	if werr := remoteWriteFileAtomic(ctx, rw, abs, data); werr != nil {
		writeError(w, http.StatusInternalServerError, werr.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"path": abs, "saved": true})
}

// remoteWriteFileAtomic replaces the remote file at target with data without
// ever leaving it half-written, mirroring the local raw save. The bytes go to a
// temp file in the target's directory, which is renamed over the target in the
// same remote command. An existing target's mode is kept (cp -p copies it onto
// the temp file before the bytes are written); a new file gets mktemp's 0600,
// the mode local raw saves create. The rename happens only if the temp file's
// size equals len(data), so a stdin stream cut short cannot install a truncated
// file. Every failure path removes the temp file and leaves the target as it was.
func remoteWriteFileAtomic(ctx context.Context, rw remoteWork, target string, data []byte) error {
	q := remote.ShellQuotePath(target)
	template := remote.ShellQuotePath(filepath.Dir(target)) + "/.raw-save-XXXXXX"
	script := `tmp=$(mktemp ` + template + `) || exit 1; ` +
		`if [ -e ` + q + ` ]; then cp -p ` + q + ` "$tmp" || { rm -f "$tmp"; exit 1; }; fi; ` +
		`cat > "$tmp" && [ "$(wc -c < "$tmp" | tr -d ' ')" = ` + strconv.Itoa(len(data)) + ` ] && mv -f "$tmp" ` + q +
		` || { rm -f "$tmp"; exit 1; }`
	return remoteWriteStdin(ctx, rw, script, data)
}

// remoteLstatKind reports what sits at path without following a final symlink:
// "symlink", "dir", "file" or "missing". Unlike remoteExists and
// remoteStatIsDir it returns the command error instead of folding it into
// "absent", so a failed probe is never mistaken for a missing file.
func remoteLstatKind(ctx context.Context, rw remoteWork, path string) (string, error) {
	q := remote.ShellQuotePath(path)
	out, err := remoteRun(ctx, rw, "if [ -L "+q+" ]; then echo symlink; elif [ -d "+q+" ]; then echo dir; elif [ -e "+q+" ]; then echo file; else echo missing; fi")
	if err != nil {
		return "", err
	}
	kind := strings.TrimSpace(out)
	switch kind {
	case "symlink", "dir", "file", "missing":
		return kind, nil
	}
	return "", fmt.Errorf("remote stat failed: unexpected output %q", kind)
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
