package server

import (
	"context"
	"errors"
	"io/fs"
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

// projectWorktreeProbeTimeout bounds one project's git probe so a hung or
// unmounted checkout cannot stall the project list for every other project.
const projectWorktreeProbeTimeout = 3 * time.Second

// projectWorktreeCacheTTL keeps the list endpoint (polled by every client)
// from spawning a git process per project per request. A checkout rarely
// changes between "main" and "linked worktree".
const projectWorktreeCacheTTL = 60 * time.Second

// projectWorktreeProbeLimit caps concurrent `git rev-parse` probes for the whole
// process. On macOS /usr/bin/git is a ~130 ms shim, so one probe per project
// (42 projects, two list requests at once) starts dozens of git processes
// together; they then all hit the deadline and every project loses its badge.
const projectWorktreeProbeLimit = 8

// projectWorktreeProbeSlots is the semaphore behind projectWorktreeProbeLimit.
var projectWorktreeProbeSlots = make(chan struct{}, projectWorktreeProbeLimit)

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

// worktreeProbeCall is one in-flight probe. Concurrent callers for the same
// path wait on done and read parent instead of starting another git.
type worktreeProbeCall struct {
	done   chan struct{}
	parent string
}

var (
	worktreeParentMu    sync.Mutex
	worktreeParentCache = map[string]worktreeParentEntry{}

	// worktreeProbeInFlight is the probe running for each path. Guarded by worktreeParentMu.
	worktreeProbeInFlight = map[string]*worktreeProbeCall{}
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
	if call, ok := worktreeProbeInFlight[path]; ok {
		worktreeParentMu.Unlock()
		<-call.done
		return call.parent
	}
	call := &worktreeProbeCall{done: make(chan struct{})}
	worktreeProbeInFlight[path] = call
	worktreeParentMu.Unlock()

	parent, ok := probeWorktreeParent(path)

	worktreeParentMu.Lock()
	if ok {
		worktreeParentCache[path] = worktreeParentEntry{parent: parent, at: time.Now()}
	}
	delete(worktreeProbeInFlight, path)
	worktreeParentMu.Unlock()

	call.parent = parent
	close(call.done)
	return parent
}

// probeWorktreeParent compares --git-dir with --git-common-dir: they differ
// only inside a linked worktree, whose common dir is the main checkout's
// `.git`. It returns ok=false when the probe itself failed (no probe slot in
// time, timeout, git error), so the caller never caches that as "no parent";
// a folder that simply is not a repository, or no longer exists, is a
// definitive ok=true with "".
func probeWorktreeParent(path string) (string, bool) {
	// A deleted project cannot be a linked worktree. Answer it here, before a
	// slot or git, so the cached "" stands instead of re-probing every request.
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return "", true
	}

	// The wait for a slot is bounded by the same deadline, so a burst of
	// queued probes cannot stall the list. The git deadline starts only once
	// the slot is held, so queueing never eats into it.
	wait, cancelWait := context.WithTimeout(context.Background(), projectWorktreeProbeTimeout)
	select {
	case projectWorktreeProbeSlots <- struct{}{}:
		cancelWait()
	case <-wait.Done():
		cancelWait()
		log.Printf("project worktree probe failed for %s: no probe slot within %s", path, projectWorktreeProbeTimeout)
		return "", false
	}
	defer func() { <-projectWorktreeProbeSlots }()

	ctx, cancel := context.WithTimeout(context.Background(), projectWorktreeProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitBinary, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
	cmd.Dir = path
	cmd.Env = gitexec.Env()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(stderr.String(), "not a git repository") {
			return "", true
		}
		log.Printf("project worktree probe failed for %s: %v: %s", path, err, strings.TrimSpace(stderr.String()))
		return "", false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 || lines[0] == lines[1] {
		return "", true
	}
	common := filepath.Clean(lines[1])
	if filepath.Base(common) != ".git" {
		return "", true
	}
	return filepath.Dir(common), true
}
