package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/gitexec"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/session"
)

// Derived worktree state is best-effort by design: a probe that times out or
// fails leaves the field at its "unknown" value (-1 / nil) rather than failing
// the whole listing. A wrong "clean" or "contained" label is worse than no
// label, because the whole point of this surface is to stop the UI from
// guessing where a session is working.
const (
	// worktreeGitTimeout bounds ONE git call for ONE checkout.
	worktreeGitTimeout = 5 * time.Second
	// worktreeEnrichBudget bounds the entire enrichment pass (all checkouts).
	// Past this the remaining probes are cancelled and reported unknown; the
	// endpoint still answers.
	worktreeEnrichBudget = 20 * time.Second
	// worktreeProbeConcurrency bounds parallel per-checkout probes. One bad
	// checkout (a directory on a dead network mount) must not serialize the
	// rest of the response.
	worktreeProbeConcurrency = 4
	// worktreeOwnerLimit caps the owner sessions returned per checkout; the
	// uncapped total still travels as owner_session_count.
	worktreeOwnerLimit = 10

	// worktreeListDefaultLimit / Max bound the list endpoint. A repository
	// normally has a handful of checkouts, so this is insurance against a
	// pathological setup rather than real pagination pressure.
	worktreeListDefaultLimit = 100
	worktreeListMaxLimit     = 500
)

// worktreePageParams parses ?limit/?offset for the worktree list. Invalid or
// missing values fall back to the default rather than erroring: a list is a
// read, and a bad query should not hide worktrees.
func worktreePageParams(r *http.Request) (limit, offset int) {
	limit = worktreeListDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > worktreeListMaxLimit {
		limit = worktreeListMaxLimit
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			offset = v
		}
	}
	return limit, offset
}

// enrichWorktrees attaches the derived state to already-parsed entries. It
// never fails: each field falls back to "unknown" independently.
//
// Locking: this deliberately touches neither h.mu nor agentSession.mu. The only
// lock taken is SessionManager.mu (inside Snapshot), which is a short copy and
// is released before any git call.
func (h *Handler) enrichWorktrees(ctx context.Context, dir string, entries []GitWorktree) []GitWorktree {
	if len(entries) == 0 {
		return entries
	}
	base, baseKnown := resolveWorktreeBase(ctx, dir, entries)

	out := make([]GitWorktree, len(entries))
	copy(out, entries)

	probeCtx, cancel := context.WithTimeout(ctx, worktreeEnrichBudget)
	defer cancel()

	// Per-checkout probes run in parallel with a bounded worker count so one
	// slow checkout cannot delay the rest (the "one project must never block
	// another" rule, applied within a single repository).
	sem := make(chan struct{}, worktreeProbeConcurrency)
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			probe := probeWorktree(probeCtx, out[i], base, !out[i].IsMain)
			out[i].Exists = probe.exists
			out[i].Dirty = probe.dirty
			out[i].Ahead = probe.ahead
			out[i].Behind = probe.behind
			out[i].LastCommit = probe.lastCommit
			if probe.containedKnown {
				contained := probe.contained
				out[i].ContainedInBase = &contained
			}
		}(i)
	}
	wg.Wait()

	// Live sessions are snapshotted ONCE (one SessionManager lock, released
	// before the per-checkout disk reads below) and indexed by canonical root,
	// so N checkouts do not re-resolve every session's project root.
	liveByRoot := map[string][]string{}
	if h.sessions != nil {
		for _, e := range h.sessions.Snapshot() {
			if e == nil || e.SessionID == "" || e.ProjectRoot == "" {
				continue
			}
			root := resolvePath(e.ProjectRoot)
			liveByRoot[root] = append(liveByRoot[root], e.SessionID)
		}
	}

	// One global-data-dir resolution for the whole pass: GlobalDataDir()
	// ensures the directory on every call, and one request must not repeat
	// that per checkout.
	sessionsBase, _ := paths.GlobalDataDir()

	for i := range out {
		out[i].BaseRef = base
		out[i].BaseKnown = baseKnown
		owners, total := worktreeOwners(out[i].Path, sessionsBase, liveByRoot)
		out[i].OwnerSessions = owners
		out[i].OwnerSessionCount = total
	}
	return out
}

// worktreeProbe is the outcome of probing one checkout. Zero/unknown values
// are explicit: dirty/ahead/behind are -1 when they could not be read.
type worktreeProbe struct {
	exists         bool
	dirty          int
	ahead          int
	behind         int
	contained      bool
	containedKnown bool
	lastCommit     string
}

// probeWorktree reads the state of ONE checkout. computeContainment is false
// for the main worktree, where "ahead of the base branch" is meaningless (the
// base usually IS its branch).
func probeWorktree(ctx context.Context, wt GitWorktree, base string, computeContainment bool) worktreeProbe {
	p := worktreeProbe{dirty: -1, ahead: -1, behind: -1}

	info, err := os.Stat(wt.Path)
	if err != nil || !info.IsDir() {
		// git still lists a removed checkout; nothing else can be read.
		return p
	}
	p.exists = true

	if out, err := gitRunInDirTimeout(ctx, wt.Path, "status", "--porcelain"); err == nil {
		dirty := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) != "" {
				dirty++
			}
		}
		p.dirty = dirty
	}

	// `rev-list --left-right --count BASE...HEAD` prints two numbers:
	// commits unique to BASE (behind) then commits unique to HEAD (ahead). A
	// zero "ahead" therefore means HEAD is an ancestor of BASE — containment.
	if computeContainment && base != "" && wt.Head != "" {
		if out, err := gitRunInDirTimeout(ctx, wt.Path, "rev-list", "--left-right", "--count", base+"..."+wt.Head); err == nil {
			fields := strings.Fields(out)
			if len(fields) == 2 {
				behind, berr := strconv.Atoi(fields[0])
				ahead, aerr := strconv.Atoi(fields[1])
				if berr == nil && aerr == nil {
					p.behind, p.ahead = behind, ahead
					p.contained, p.containedKnown = ahead == 0, true
				}
			}
		}
	}

	if out, err := gitRunInDirTimeout(ctx, wt.Path, "log", "-1", "--format=%cI"); err == nil {
		p.lastCommit = out
	}
	return p
}

// resolveWorktreeBase picks the ref that "contained in X" comparisons use.
// Order: origin/HEAD, the main checkout's branch, a local main/master, else
// unknown. A returned ref is always verified to resolve; a base that cannot be
// resolved is reported as unknown rather than defaulted to "main".
func resolveWorktreeBase(ctx context.Context, dir string, entries []GitWorktree) (string, bool) {
	candidates := make([]string, 0, 4)
	if out, err := gitRunInDirTimeout(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			candidates = append(candidates, ref)
		}
	}
	for _, e := range entries {
		if e.IsMain && e.Branch != "" {
			candidates = append(candidates, e.Branch)
		}
	}
	candidates = append(candidates, "main", "master")

	for _, ref := range candidates {
		if refResolves(ctx, dir, ref) {
			return ref, true
		}
	}
	return "", false
}

// refResolves reports whether ref names a commit in dir.
func refResolves(ctx context.Context, dir, ref string) bool {
	_, err := gitRunInDirTimeout(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// worktreeOwners merges the sessions rooted at a checkout: persisted sessions
// on disk (which carry a title) and live ones in this process (which may not
// appear on disk yet). It is read-only — it checks for the session directory
// instead of calling session.ListRefsForDir unconditionally, because that
// helper's ProjectSessionsDir creates the directory as a side effect and a GET
// must not litter storage for checkouts nobody has opened.
func worktreeOwners(worktreePath, sessionsBase string, liveByRoot map[string][]string) ([]WorktreeOwner, int) {
	byID := map[string]WorktreeOwner{}

	slug := session.ProjectSlugForPath(worktreePath)
	if sessionsBase != "" {
		sessionsDir := filepath.Join(sessionsBase, "project", slug, "sessions")
		if _, statErr := os.Stat(sessionsDir); statErr == nil {
			if refs, err := session.ListRefsForDir(worktreePath); err == nil {
				for _, ref := range refs {
					if ref.ID == "" {
						continue
					}
					byID[ref.ID] = WorktreeOwner{SessionID: ref.ID, Title: ref.Title}
				}
			}
		}
	}

	for _, id := range liveByRoot[resolvePath(worktreePath)] {
		if _, ok := byID[id]; !ok {
			byID[id] = WorktreeOwner{SessionID: id}
		}
	}

	out := make([]WorktreeOwner, 0, len(byID))
	for _, owner := range byID {
		out = append(out, owner)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })

	total := len(out)
	if total > worktreeOwnerLimit {
		out = out[:worktreeOwnerLimit]
	}
	return out, total
}

// gitRunInDirTimeout runs one git command in dir under a per-call deadline.
// Unlike gitRunInDir it uses exec.CommandContext (so a hung checkout cannot
// outlive its budget) and folds stderr into the error. gitexec.Env() keeps
// ocode from taking optional index locks while the user's own git runs.
func gitRunInDirTimeout(ctx context.Context, dir string, args ...string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, worktreeGitTimeout)
	defer cancel()

	cmd := exec.CommandContext(callCtx, gitBinary, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = gitexec.Env()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, cmdErr := cmd.Output()

	if callCtx.Err() != nil {
		return "", fmt.Errorf("git %s: %w (after %s)", strings.Join(args, " "), callCtx.Err(), worktreeGitTimeout)
	}
	return strings.TrimSpace(string(out)), gitexec.WithOutput(cmdErr, stderr.String(), strings.TrimSpace(string(out)))
}
