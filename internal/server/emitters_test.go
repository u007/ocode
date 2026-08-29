package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// TestBroadcastEventPublishesTaggedEnvelope: every headless SSE event also
// lands on the unified bus, tagged with the session's owning project from the
// registry (Part 02 Task 3).
func TestBroadcastEventPublishesTaggedEnvelope(t *testing.T) {
	h := NewHandler()
	ch := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(ch)

	h.sessions.Register("ses_tagged", "/proj/alpha")
	h.broadcastEvent(SSEEvent{SessionID: "ses_tagged", Event: "text", Data: TextDelta{Delta: "hi"}})

	select {
	case env := <-ch:
		if env.Event != "text" || env.SessionID != "ses_tagged" || env.Project != "/proj/alpha" {
			t.Fatalf("envelope = %+v, want event=text session=ses_tagged project=/proj/alpha", env)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no envelope reached the bus subscriber")
	}
}

// TestRunsEmitterTagsSessionEvent: with a session whose agent is live, the
// process-wide runs emitter publishes a `runs` envelope carrying the session id
// and project.
func TestRunsEmitterTagsSessionEvent(t *testing.T) {
	h := NewHandler()
	ch := h.bus.Subscribe([]string{"/proj/alpha"})
	defer h.bus.Unsubscribe(ch)

	// Register a session with a live agent in project alpha. NewAgent with a
	// nil client is fine here — runsSnapshot only touches the run registry.
	entry := h.sessions.Register("ses_runs", "/proj/alpha")
	as := &agentSession{agent: agent.NewAgent(nil, nil, nil, nil), model: "fake-model"}
	h.mu.Lock()
	h.agents["ses_runs"] = as
	h.mu.Unlock()
	h.sessions.setAgent("ses_runs", as)
	entry.lastActivity = time.Now()

	h.startRunsEmitter()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case env := <-ch:
			if env.Event == "runs" {
				if env.SessionID != "ses_runs" || env.Project != "/proj/alpha" {
					t.Fatalf("runs envelope = %+v, want session ses_runs project /proj/alpha", env)
				}
				return // emitted and correctly tagged
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("no runs envelope arrived within 5s")
}

// TestGitWatcherIsSubscriberAware: with one subscriber declaring project P, a
// git_status envelope for P arrives (initial snapshot); a registered-but-unviewed
// project Q never produces one.
func TestGitWatcherIsSubscriberAware(t *testing.T) {
	h := NewHandler()
	projectP := t.TempDir()
	makeGitRepo(t, projectP) // best-effort: real git repo with a staged change

	ch := h.bus.Subscribe([]string{projectP})
	defer h.bus.Unsubscribe(ch)

	h.startWatchEmitters()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case env := <-ch:
			if env.Event != "git_status" {
				continue
			}
			if env.Project != projectP {
				t.Fatalf("git_status for unviewed project %q", env.Project)
			}
			return // initial snapshot for the viewed project arrived
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("no git_status envelope for the viewed project within 5s")
}

// TestSpendingEmitterPublishesInitial: while a subscriber is connected the
// spending emitter publishes an initial spending envelope (no records yet).
func TestSpendingEmitterPublishesInitial(t *testing.T) {
	h := NewHandler()
	ch := h.bus.Subscribe(nil)
	defer h.bus.Unsubscribe(ch)

	h.startWatchEmitters()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case env := <-ch:
			if env.Event != "spending" {
				continue
			}
			if _, ok := env.Data.(map[string]any); !ok {
				t.Fatalf("spending data = %#v, want map", env.Data)
			}
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("no spending envelope within 5s")
}

// TestTerminalProcessesEmitterReportsRegisteredPID: with a terminal registered
// in h.terminalProcs, the emitter publishes a terminal_processes envelope for
// its project carrying that terminal's id, pid, and a positive memory
// reading (its own real process, so RSS is always > 0; CPU% can legitimately
// be 0 on the first sample).
func TestTerminalProcessesEmitterReportsRegisteredPID(t *testing.T) {
	h := NewHandler()
	ch := h.bus.Subscribe([]string{"/proj/alpha"})
	defer h.bus.Unsubscribe(ch)

	h.terminalProcs.register("term-1", terminalProcEntry{Project: "/proj/alpha", PID: int32(os.Getpid())})
	h.startTerminalProcessesEmitter()

	for i := 0; i < 50; i++ {
		select {
		case env := <-ch:
			if env.Event != "terminal_processes" {
				continue
			}
			stats, ok := env.Data.([]terminalProcessStat)
			if !ok || len(stats) != 1 {
				t.Fatalf("terminal_processes data = %#v, want one terminalProcessStat", env.Data)
			}
			if stats[0].ID != "term-1" || stats[0].PID != int32(os.Getpid()) {
				t.Fatalf("stat = %+v, want id=term-1 pid=%d", stats[0], os.Getpid())
			}
			if stats[0].MemBytes == 0 {
				t.Fatalf("stat.MemBytes = 0, want > 0 for the live test process")
			}
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("no terminal_processes envelope within 5s")
}

// TestTerminalRegistryUnregisterDropsEntry: register then unregister removes
// the entry from the snapshot, matching HandleTerminalWS's shutdown path.
func TestTerminalRegistryUnregisterDropsEntry(t *testing.T) {
	r := newTerminalRegistry()
	r.register("t1", terminalProcEntry{Project: "/proj", PID: 123})
	if len(r.snapshot()) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(r.snapshot()))
	}
	r.unregister("t1")
	if len(r.snapshot()) != 0 {
		t.Fatalf("snapshot len after unregister = %d, want 0", len(r.snapshot()))
	}
}

// makeGitRepo best-effort creates a git repo with a staged change at dir. A
// failure to init (no git binary, non-repo fs) is ignored — gitStatusForDir
// degrades to an empty no-changes status, which still exercises the
// subscriber-aware tagging.
func makeGitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		return
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		_ = cmd.Run()
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	_ = os.WriteFile(filepath.Join(dir, "file.txt"), []byte("one\n"), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "init")
	_ = os.WriteFile(filepath.Join(dir, "file.txt"), []byte("two\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644)
	run("add", "new.txt")
}

// TestTerminalProcessStatIncludesCommand: the emitted stat carries a non-empty
// Command derived from the live process tree (here, the test binary itself), so
// the Processes tab has something to render even when no child command runs.
func TestTerminalProcessStatIncludesCommand(t *testing.T) {
	h := NewHandler()
	ch := h.bus.Subscribe([]string{"/proj/cmd"})
	defer h.bus.Unsubscribe(ch)

	h.terminalProcs.register("term-cmd", terminalProcEntry{Project: "/proj/cmd", PID: int32(os.Getpid())})
	h.startTerminalProcessesEmitter()

	for i := 0; i < 50; i++ {
		select {
		case env := <-ch:
			if env.Event != "terminal_processes" {
				continue
			}
			stats, ok := env.Data.([]terminalProcessStat)
			if !ok || len(stats) != 1 {
				t.Fatalf("terminal_processes data = %#v, want one terminalProcessStat", env.Data)
			}
			if stats[0].Command == "" {
				t.Fatalf("stat.Command = %q, want non-empty command line", stats[0].Command)
			}
			if isInteractiveShell(stats[0].Command) {
				t.Fatalf("stat.Command = %q, must not be reported as an idle interactive shell", stats[0].Command)
			}
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("no terminal_processes envelope within 5s")
}

// TestIsInteractiveShell: only login/interactive shells are skipped when picking
// the "running command"; a shell actually executing a script is not.
func TestIsInteractiveShell(t *testing.T) {
	cases := map[string]bool{
		"zsh":                 true,
		"zsh -i -l":           true,
		"-zsh":                true,
		"bash -l":             true,
		"bash run.sh":         false,
		"npm run dev":         false,
		"node server.js":      false,
		"/usr/bin/fish":       true,
		"/usr/bin/fish -c ls": false,
	}
	for cl, want := range cases {
		if got := isInteractiveShell(cl); got != want {
			t.Errorf("isInteractiveShell(%q) = %v, want %v", cl, got, want)
		}
	}
}
