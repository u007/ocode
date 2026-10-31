package server

import (
	"context"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/gitexec"
	"github.com/u007/ocode/internal/projects"
)

// projectWorktreeProbeTimeout bounds one project's git probe so a hung or
// unmounted checkout cannot stall the project list for every other project.
const projectWorktreeProbeTimeout = 3 * time.Second

// projectWorktreeCacheTTL keeps the list endpoint (polled by every client)
// from spawning a git process per project per request. A checkout rarely
// changes between "main" and "linked worktree".
const projectWorktreeCacheTTL = 60 * time.Second

// projectDTO is a saved project plus its derived worktree parent. WorktreeOf
// is never persisted: it is computed per list so it cannot go stale on disk.
type projectDTO struct {
	projects.Project
	// WorktreeOf is the main checkout's path when Path is a linked git
	// worktree, empty otherwise (main checkout, non-repo, remote project, or
	// probe failure).
	WorktreeOf string `json:"worktree_of,omitempty"`
}

type worktreeParentEntry struct {
	parent string
	at     time.Time
}

var (
	worktreeParentMu    sync.Mutex
	worktreeParentCache = map[string]worktreeParentEntry{}
)

// withWorktreeParents annotates each local project with its main checkout.
// Probes run concurrently, each under its own deadline.
func withWorktreeParents(list []projects.Project) []projectDTO {
	out := make([]projectDTO, len(list))
	var wg sync.WaitGroup
	for i, p := range list {
		out[i].Project = p
		if p.Host != "" {
			continue
		}
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			out[i].WorktreeOf = worktreeParent(path)
		}(i, p.Path)
	}
	wg.Wait()
	return out
}

func worktreeParent(path string) string {
	worktreeParentMu.Lock()
	if e, ok := worktreeParentCache[path]; ok && time.Since(e.at) < projectWorktreeCacheTTL {
		worktreeParentMu.Unlock()
		return e.parent
	}
	worktreeParentMu.Unlock()

	parent := probeWorktreeParent(path)

	worktreeParentMu.Lock()
	worktreeParentCache[path] = worktreeParentEntry{parent: parent, at: time.Now()}
	worktreeParentMu.Unlock()
	return parent
}

// probeWorktreeParent compares --git-dir with --git-common-dir: they differ
// only inside a linked worktree, whose common dir is the main checkout's
// `.git`. A probe failure yields "" (shown as an ordinary top-level project)
// and is logged unless the folder simply is not a repository.
func probeWorktreeParent(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), projectWorktreeProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitBinary, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
	cmd.Dir = path
	cmd.Env = gitexec.Env()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if !strings.Contains(stderr.String(), "not a git repository") {
			log.Printf("project worktree probe failed for %s: %v: %s", path, err, strings.TrimSpace(stderr.String()))
		}
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || lines[0] == lines[1] {
		return ""
	}
	common := filepath.Clean(lines[1])
	if filepath.Base(common) != ".git" {
		return ""
	}
	return filepath.Dir(common)
}
