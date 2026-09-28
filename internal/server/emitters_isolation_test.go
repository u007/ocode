package server

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestForEachGitStatusConcurrentlyYieldsFastBeforeSlow is the deterministic
// unit-level guard: results are yielded as they arrive, not in input order, so
// a blocked project cannot hold back a ready one.
func TestForEachGitStatusConcurrentlyYieldsFastBeforeSlow(t *testing.T) {
	old := gitStatusFn
	defer func() { gitStatusFn = old }()

	release := make(chan struct{})
	var once sync.Once
	releaseSlow := func() { once.Do(func() { close(release) }) }
	defer releaseSlow()
	go func() { time.Sleep(2 * time.Second); releaseSlow() }()

	gitStatusFn = func(project string) (GitStatus, error) {
		if project == "slow" {
			<-release
		}
		return GitStatus{Branch: project}, nil
	}

	var order []string
	forEachGitStatusConcurrently([]string{"slow", "fast"}, func(project string, _ GitStatus, _ error) {
		order = append(order, project)
	})

	if len(order) != 2 || order[0] != "fast" {
		t.Fatalf("yield order = %v, want fast yielded before slow", order)
	}
}

// TestGitWatcherSlowProjectDoesNotDelayAnother is the integration-level guard
// for the user requirement: while one viewed project's git status is blocked,
// another viewed project must still receive its git_status event. Before the
// fix the emitter loop computed projects sequentially, so the blocked project
// came first and every later project's envelope waited on it.
func TestGitWatcherSlowProjectDoesNotDelayAnother(t *testing.T) {
	h := NewHandler()

	old := gitStatusFn
	defer func() { gitStatusFn = old }()
	release := make(chan struct{})
	var once sync.Once
	releaseSlow := func() { once.Do(func() { close(release) }) }
	defer releaseSlow()
	go func() { time.Sleep(10 * time.Second); releaseSlow() }()

	const slow = "/proj/slow"
	const fast = "/proj/fast"
	gitStatusFn = func(project string) (GitStatus, error) {
		if project == slow {
			<-release
			return GitStatus{}, nil
		}
		return GitStatus{Branch: "fast"}, nil
	}

	ch := h.bus.Subscribe([]string{slow, fast})
	defer h.bus.Unsubscribe(ch)

	h.startWatchEmitters()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case env := <-ch:
			if env.Event != "git_status" {
				continue
			}
			if env.Project == slow {
				t.Fatal("slow project's git_status was published before the fast project's; the emitter is serialised")
			}
			if env.Project == fast {
				return // fast published while slow is still blocked
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("no git_status for the fast project within 5s — a slow project delayed it")
}

// A project whose status fails (wedged repo, timeout) must publish nothing:
// an empty git_status would tell every viewer the repository is clean. A
// healthy project viewed alongside it still gets its event.
func TestGitWatcherFailedStatusPublishesNothing(t *testing.T) {
	h := NewHandler()

	old := gitStatusFn
	defer func() { gitStatusFn = old }()
	const broken = "/proj/broken"
	const ok = "/proj/ok"
	gitStatusFn = func(project string) (GitStatus, error) {
		if project == broken {
			return GitStatus{}, errors.New("git status for /proj/broken timed out after 10s")
		}
		return GitStatus{Branch: "main", IsRepo: true}, nil
	}

	ch := h.bus.Subscribe([]string{broken, ok})
	defer h.bus.Unsubscribe(ch)
	h.startWatchEmitters()

	sawOK := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case env := <-ch:
			if env.Event != "git_status" {
				continue
			}
			if env.Project == broken {
				t.Fatalf("failed project published git_status %+v", env.Data)
			}
			if env.Project == ok {
				sawOK = true
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !sawOK {
		t.Fatal("healthy project got no git_status")
	}
}
