package server

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/projects"
)

func TestProbeWorktreeParent(t *testing.T) {
	main := worktreeFixture(t)
	linked := filepath.Join(t.TempDir(), "linked")
	run(t, main, "git", "worktree", "add", "-b", "feat", linked)
	linked, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatalf("resolve linked dir: %v", err)
	}

	if got, ok := probeWorktreeParent(linked); !ok || got != main {
		t.Fatalf("linked worktree parent = %q (ok=%v), want %q", got, ok, main)
	}
	if got, ok := probeWorktreeParent(main); !ok || got != "" {
		t.Fatalf("main checkout parent = %q (ok=%v), want empty", got, ok)
	}
	if got, ok := probeWorktreeParent(t.TempDir()); !ok || got != "" {
		t.Fatalf("non-repo parent = %q (ok=%v), want empty and cacheable", got, ok)
	}
}

// stubWorktreeGit points gitBinary at a POSIX shell script for one test and
// starts from an empty worktree-parent cache, so no earlier answer leaks in.
func stubWorktreeGit(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell stub for git")
	}
	bin := filepath.Join(t.TempDir(), "git-stub")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBin := gitBinary
	gitBinary = bin
	resetWorktreeParentCache()
	t.Cleanup(func() {
		gitBinary = oldBin
		resetWorktreeParentCache()
	})
}

func resetWorktreeParentCache() {
	worktreeParentMu.Lock()
	worktreeParentCache = map[string]worktreeParentEntry{}
	worktreeParentMu.Unlock()
}

// worktreeRunStub logs "start" and "end" around a sleep, so the number of
// simultaneous runs can be reconstructed from the log. It then reports a
// linked worktree whose parent is /main.
func worktreeRunStub(logPath, sleep string) string {
	return "#!/bin/sh\necho start >> '" + logPath + "'\nsleep " + sleep + "\necho end >> '" + logPath + "'\n" +
		"printf '%s\\n%s\\n' '/main/.git/worktrees/w' '/main/.git'\n"
}

func readRunLog(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	return strings.Fields(string(data))
}

func peakConcurrentRuns(events []string) int {
	running, peak := 0, 0
	for _, ev := range events {
		switch ev {
		case "start":
			running++
			if running > peak {
				peak = running
			}
		case "end":
			running--
		}
	}
	return peak
}

// A deleted project is answered from os.Stat: no git runs, the answer is
// definitive (ok=true, cacheable), and the second call is a cache hit.
func TestWorktreeParentMissingPathSkipsGitAndCaches(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "runs.log")
	stubWorktreeGit(t, worktreeRunStub(logPath, "0"))
	gone := filepath.Join(t.TempDir(), "deleted-project")

	if got, ok := probeWorktreeParent(gone); !ok || got != "" {
		t.Fatalf("missing path probe = %q (ok=%v), want empty and ok=true", got, ok)
	}
	if got := worktreeParent(gone); got != "" {
		t.Fatalf("missing path parent = %q, want empty", got)
	}
	worktreeParentMu.Lock()
	_, cached := worktreeParentCache[gone]
	worktreeParentMu.Unlock()
	if !cached {
		t.Fatalf("missing path answer was not cached")
	}
	if got := worktreeParent(gone); got != "" {
		t.Fatalf("missing path parent on second call = %q, want empty", got)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("git ran for a missing path (run log stat err = %v), want no run", err)
	}
}

// Probes across the whole process, including two list requests running at
// once, never exceed projectWorktreeProbeLimit git processes.
func TestWorktreeProbesBoundedConcurrency(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "runs.log")
	stubWorktreeGit(t, worktreeRunStub(logPath, "0.1"))

	base := t.TempDir()
	listOf := func(from, n int) []projects.Project {
		var list []projects.Project
		for i := from; i < from+n; i++ {
			dir := filepath.Join(base, fmt.Sprintf("p%02d", i))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			list = append(list, projects.Project{Path: dir})
		}
		return list
	}

	lists := [][]projects.Project{listOf(0, 30), listOf(30, 30)}
	results := make([][]projectDTO, len(lists))
	var wg sync.WaitGroup
	for c := range lists {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			results[c] = withWorktreeParents(lists[c])
		}(c)
	}
	wg.Wait()

	if peak := peakConcurrentRuns(readRunLog(t, logPath)); peak > projectWorktreeProbeLimit {
		t.Fatalf("peak concurrent git probes = %d, want <= %d", peak, projectWorktreeProbeLimit)
	}
	for c, res := range results {
		for i, dto := range res {
			if dto.WorktreeOf != "/main" {
				t.Fatalf("list %d project %d WorktreeOf = %q, want /main", c, i, dto.WorktreeOf)
			}
		}
	}
}

// Two callers asking for the same uncached path share one git run.
func TestWorktreeParentDedupesConcurrentProbes(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "runs.log")
	stubWorktreeGit(t, worktreeRunStub(logPath, "0.3"))
	dir := t.TempDir()

	start := make(chan struct{})
	got := make([]string, 2)
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i] = worktreeParent(dir)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, g := range got {
		if g != "/main" {
			t.Fatalf("caller %d parent = %q, want /main", i, g)
		}
	}
	runs := 0
	for _, ev := range readRunLog(t, logPath) {
		if ev == "start" {
			runs++
		}
	}
	if runs != 1 {
		t.Fatalf("git ran %d times for one path, want 1", runs)
	}
}

// A failed probe is not cached as "no parent": the next call probes again.
func TestWorktreeParentDoesNotCacheFailedProbe(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "failed-once")
	stubWorktreeGit(t, "#!/bin/sh\n"+
		"if [ ! -e '"+marker+"' ]; then touch '"+marker+"'; echo 'transient failure' >&2; exit 1; fi\n"+
		"printf '%s\\n%s\\n' '/main/.git/worktrees/w' '/main/.git'\n")
	dir := t.TempDir()

	if got := worktreeParent(dir); got != "" {
		t.Fatalf("failing probe parent = %q, want empty", got)
	}
	if got := worktreeParent(dir); got != "/main" {
		t.Fatalf("parent after failed probe = %q, want /main (failure was cached)", got)
	}
}
