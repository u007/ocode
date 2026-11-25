package server

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/gitexec"
	"github.com/u007/ocode/internal/projects"
)

// workspaceRootProbeTimeout bounds one git probe in resolveWorkspaceRoot. The
// resolver sits on request paths that must stay responsive (project session
// lists, Git reads, file-tree roots), so a hung mount or a wedged repository
// must fail closed instead of pinning the handler.
const workspaceRootProbeTimeout = 3 * time.Second

// workspaceRootCacheTTL is deliberately short: a linked worktree can be
// removed (or a saved project deleted) while the server runs, and a stale
// acceptance would then route Git and file access at a vanished checkout.
const workspaceRootCacheTTL = 30 * time.Second

// workspaceRootCacheLimit bounds the success cache. Only validated linked
// worktrees are cached, but every distinct checkout spelling would otherwise
// add an entry forever; a full map is cleared wholesale rather than growing
// without bound.
const workspaceRootCacheLimit = 256

type workspaceRootCacheEntry struct {
	root      string
	savedRoot string
	at        time.Time
}

var (
	workspaceRootMu    sync.Mutex
	workspaceRootCache = map[string]workspaceRootCacheEntry{}
)

// resolveWorkspaceRoot maps a client-supplied path to the checkout root the
// rest of the server may act on, plus the saved project that owns it.
//
// It accepts exactly three shapes, and nothing else:
//
//   - a saved LOCAL project root (h.workDir or a local projects.json entry),
//   - a path inside one of those roots, and
//   - an EXACT linked Git worktree root whose main checkout is saved (or a path
//     inside such a worktree, which resolves to the worktree root).
//
// A linked worktree is a security boundary of its own: it is not added to
// allowedProjectRoots or projects.json, so it is validated per request instead.
// The probe must report `--show-toplevel` equal to the worktree root, its
// common Git dir must belong to the saved repository, and its `.git` file must
// point inside that repository's `.git/worktrees` directory. An unrelated
// repository, a path outside every saved root, a failed probe, or a forged
// `.git` file that points anywhere else is rejected. Rejections are never
// cached; validated worktrees are cached for workspaceRootCacheTTL.
//
// Returned paths are absolute and symlink-resolved when they exist (a saved
// root may be absent on disk and keeps its cleaned spelling), so /var and
// /private/var spellings of one checkout compare equal. h.mu is never held
// across a probe.
func (h *Handler) resolveWorkspaceRoot(p string) (root, savedRoot string, ok bool) {
	if p == "" {
		return "", "", false
	}
	expanded, err := projects.ExpandHome(p)
	if err != nil {
		return "", "", false
	}
	if !filepath.IsAbs(expanded) {
		return "", "", false
	}
	// resolvePath resolves a symlink chain when the path exists and otherwise
	// keeps the cleaned spelling. A saved root may legitimately be absent
	// (a remote host's project listed through a tilde form); anything that
	// claims to be a linked worktree still has to pass the git probe below.
	candidate := resolvePath(expanded)

	savedRoots := h.localWorkspaceRoots()
	for _, saved := range savedRoots {
		if samePath(candidate, saved) {
			return saved, saved, true
		}
	}
	if root, savedRoot, ok := cachedWorkspaceRoot(candidate); ok {
		return root, savedRoot, true
	}

	// Pick the MOST SPECIFIC saved root, not the first: h.workDir is often a
	// broad directory (a desktop .app anchored at home), so a saved project
	// nested under it must win or its worktrees resolve to the wrong repo.
	best := ""
	for _, saved := range savedRoots {
		if (containedIn(candidate, saved) || containedLexical(candidate, saved)) && len(saved) > len(best) {
			best = saved
		}
	}
	if best != "" {
		// A linked worktree keeps its own checkout identity even when it lives
		// inside the saved checkout (ocode's own worktrees are under
		// .worktrees/, a subdirectory of the main checkout). Walk up to the
		// nearest `.git` file so a nested directory resolves to its worktree.
		if worktree := gitFileAncestorWithin(candidate, best); worktree != "" && !samePath(worktree, best) {
			return h.validateWorktreeRoot(worktree, best)
		}
		return best, best, true
	}

	// Outside every saved root, only a validated linked worktree (or a path
	// inside one) is admitted. The probe decides the checkout root.
	top, ok := workspaceTopLevel(candidate)
	if !ok {
		return "", "", false
	}
	for _, saved := range savedRoots {
		if root, savedRoot, ok := h.validateWorktreeRoot(top, saved); ok {
			return root, savedRoot, true
		}
	}
	return "", "", false
}

// localWorkspaceRoots returns this server's saved LOCAL roots, canonicalized so
// comparisons are spelling-independent. Remote (SSH/WSL) entries are skipped
// for the same reason as allowedProjectRoots: their path belongs to another
// machine. It never mutates the shared allowlist and never holds h.mu.
func (h *Handler) localWorkspaceRoots() []string {
	raw := make([]string, 0, 4)
	if h.workDir != "" {
		raw = append(raw, h.workDir)
	}
	if h.projects != nil {
		for _, p := range h.projects.List() {
			if p.Host == "" && p.Path != "" {
				raw = append(raw, p.Path)
			}
		}
	}

	roots := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, r := range raw {
		expanded, err := projects.ExpandHome(r)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(expanded) {
			abs, err := filepath.Abs(expanded)
			if err != nil {
				continue
			}
			expanded = abs
		}
		// resolvePath keeps a missing saved root's cleaned spelling instead of
		// dropping it; the exact-match gate must still recognise it.
		resolved := resolvePath(expanded)
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		roots = append(roots, resolved)
	}
	return roots
}

// validateWorktreeRoot is the Git-proof half of the resolver. The worktree
// must be the toplevel of its own work tree, its main checkout must be the
// saved root, and its `.git` file must point into that repository's worktrees
// directory (a forged file pointing elsewhere is rejected).
func (h *Handler) validateWorktreeRoot(worktree, savedRoot string) (root, saved string, ok bool) {
	if !hasGitFile(worktree) {
		return "", "", false
	}
	top, ok := workspaceTopLevel(worktree)
	if !ok || !samePath(top, worktree) {
		return "", "", false
	}
	parent := worktreeParent(worktree)
	if parent == "" || !samePath(parent, savedRoot) {
		return "", "", false
	}
	if !worktreeGitFileWithin(worktree, savedRoot) {
		return "", "", false
	}
	rememberWorkspaceRoot(worktree, worktree, savedRoot)
	return worktree, savedRoot, true
}

// gitFileAncestorWithin walks from candidate up to savedRoot and returns the
// nearest directory carrying a `.git` FILE (a linked-worktree root), or "".
// It never leaves savedRoot, so an ordinary subdirectory costs no git call.
func gitFileAncestorWithin(candidate, savedRoot string) string {
	dir := candidate
	for {
		if hasGitFile(dir) {
			return dir
		}
		if samePath(dir, savedRoot) {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir || (!containedIn(parent, savedRoot) && !containedLexical(parent, savedRoot)) {
			return ""
		}
		dir = parent
	}
}

// workspaceTopLevel runs one bounded, read-only `git rev-parse --show-toplevel`
// for a candidate checkout. A failure is reported as ok=false so the caller
// fails closed; an expected "not a git repository" stays quiet, any other git
// failure is logged.
func workspaceTopLevel(dir string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), workspaceRootProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitBinary, "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	cmd.Env = gitexec.Env()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := stderr.String(); !strings.Contains(msg, "not a git repository") {
			log.Printf("workspace root probe failed for %s: %v: %s", dir, err, strings.TrimSpace(msg))
		}
		return "", false
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return "", false
	}
	return top, true
}

// hasGitFile reports whether dir carries a `.git` FILE, the marker of a linked
// worktree (a main checkout's `.git` is a directory).
func hasGitFile(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.Mode().IsRegular()
}

// worktreeGitFileWithin reports whether the worktree's `.git` file points
// inside savedRoot's worktrees directory (`<savedRoot>/.git/worktrees`). This is
// what rejects a forged `.git` file aimed at another repository.
func worktreeGitFileWithin(worktree, savedRoot string) bool {
	data, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		return false
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return false
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(worktree, target)
	}
	return containedIn(target, filepath.Join(savedRoot, ".git", "worktrees"))
}

func cachedWorkspaceRoot(candidate string) (root, savedRoot string, ok bool) {
	workspaceRootMu.Lock()
	defer workspaceRootMu.Unlock()
	entry, ok := workspaceRootCache[candidate]
	if !ok {
		return "", "", false
	}
	if time.Since(entry.at) >= workspaceRootCacheTTL {
		delete(workspaceRootCache, candidate)
		return "", "", false
	}
	return entry.root, entry.savedRoot, true
}

func rememberWorkspaceRoot(candidate, root, savedRoot string) {
	workspaceRootMu.Lock()
	defer workspaceRootMu.Unlock()
	if len(workspaceRootCache) >= workspaceRootCacheLimit {
		clear(workspaceRootCache)
	}
	workspaceRootCache[candidate] = workspaceRootCacheEntry{root: root, savedRoot: savedRoot, at: time.Now()}
}

// resetWorkspaceRootCache drops the success cache (tests run fixtures whose
// paths are reused across cases).
func resetWorkspaceRootCache() {
	workspaceRootMu.Lock()
	workspaceRootCache = map[string]workspaceRootCacheEntry{}
	workspaceRootMu.Unlock()
}
