package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
)

// saveSessionToDir persists a session into the storage dir for wd, so tests
// can plant sessions under arbitrary project roots without touching the
// process-global workdir (the override is reset on test cleanup).
func saveSessionToDir(t *testing.T, wd, id string) {
	t.Helper()
	session.SetWorkDir(wd)
	t.Cleanup(func() { session.SetWorkDir("") })
	if err := session.Save(id, "project session", []agent.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatalf("save session %s under %s: %v", id, wd, err)
	}
}

// removeSessionFileForDir deletes the on-disk session file for id under wd's
// storage dir (used to prove Resolve's cache answers without the disk).
func removeSessionFileForDir(t *testing.T, wd, id string) error {
	t.Helper()
	dir, err := session.GetStorageDirForPath(wd)
	if err != nil {
		return err
	}
	for _, suffix := range []string{".ojsonl", ".json"} {
		if err := os.Remove(filepath.Join(dir, id+suffix)); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no session file found for %s under %s", id, wd)
}

// TestSessionManagerResolveAcrossProjects is the core cross-project test:
// two project roots each hold a session on disk; Resolve finds each session's
// correct root; unknown IDs error; a second Resolve of the same ID hits the
// cache (the file is gone by then, so only the cache could answer).
func TestSessionManagerResolveAcrossProjects(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	idA := session.NewSessionID()
	idB := session.NewSessionID()
	saveSessionToDir(t, rootA, idA)
	saveSessionToDir(t, rootB, idB)

	mgr := NewSessionManager(30*time.Minute, func() []string { return []string{rootA, rootB} }, nil)

	eA, err := mgr.Resolve(idA)
	if err != nil {
		t.Fatalf("resolve %s: %v", idA, err)
	}
	if eA.ProjectRoot != rootA {
		t.Fatalf("resolve %s: root = %q, want %q", idA, eA.ProjectRoot, rootA)
	}
	eB, err := mgr.Resolve(idB)
	if err != nil {
		t.Fatalf("resolve %s: %v", idB, err)
	}
	if eB.ProjectRoot != rootB {
		t.Fatalf("resolve %s: root = %q, want %q", idB, eB.ProjectRoot, rootB)
	}

	if _, err := mgr.Resolve("ses_missing"); err == nil {
		t.Fatal("expected error for unknown session id")
	}

	// Cache hit: the file is removed, so only the cached mapping can answer.
	if err := removeSessionFileForDir(t, rootA, idA); err != nil {
		t.Fatal(err)
	}
	if e, err := mgr.Resolve(idA); err != nil || e.ProjectRoot != rootA {
		t.Fatalf("cached resolve: entry=%+v err=%v, want root %q", e, err, rootA)
	}
}

// TestSessionManagerRegisterBindsExplicitRoot verifies explicit registration
// and that it wins over an earlier implicit resolution.
func TestSessionManagerRegisterBindsExplicitRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	id := session.NewSessionID()
	saveSessionToDir(t, rootA, id)

	mgr := NewSessionManager(30*time.Minute, func() []string { return []string{rootA, rootB} }, nil)

	// Implicit resolution finds rootA (only place the file exists).
	e, err := mgr.Resolve(id)
	if err != nil {
		t.Fatal(err)
	}
	if e.ProjectRoot != rootA {
		t.Fatalf("implicit root = %q, want %q", e.ProjectRoot, rootA)
	}

	// Explicit Register to rootB (client-supplied project_path) wins.
	mgr.Register(id, rootB)
	if got := mgr.Lookup(id).ProjectRoot; got != rootB {
		t.Fatalf("after register: root = %q, want %q", got, rootB)
	}
}

// TestEvictIdleReleasesStaleAgents covers Task 4: an entry with a built agent
// idle past the threshold is released (agent nil afterwards, entry still
// resolvable); an entry with an active turn is not evicted.
func TestEvictIdleReleasesStaleAgents(t *testing.T) {
	mgr := NewSessionManager(time.Minute, func() []string { return nil }, nil)

	staleID := session.NewSessionID()
	staleEntry := mgr.Register(staleID, t.TempDir())
	mgr.setAgent(staleID, &agentSession{})
	staleEntry.lastActivity = time.Now().Add(-2 * time.Minute)

	activeID := session.NewSessionID()
	mgr.Register(activeID, t.TempDir())
	mgr.setAgent(activeID, &agentSession{})
	mgr.setTurnActive(activeID, true)
	mgr.Lookup(activeID).lastActivity = time.Now().Add(-2 * time.Minute)

	evicted := mgr.EvictIdle()
	if len(evicted) != 1 || evicted[0] != staleID {
		t.Fatalf("evicted = %v, want [%s]", evicted, staleID)
	}

	if got := mgr.Lookup(staleID); got == nil || got.agent != nil {
		t.Fatalf("stale entry should survive eviction without an agent, got %+v", got)
	}
	// Entry remains resolvable even though the agent is gone.
	if e, err := mgr.Resolve(staleID); err != nil || e == nil {
		t.Fatalf("evicted entry must stay resolvable: %v", err)
	}
	if got := mgr.Lookup(activeID); got == nil || got.agent == nil {
		t.Fatalf("active-turn entry must keep its agent, got %+v", got)
	}
}

// TestEvictIdleNotifiesHandler verifies the onEvict callback fires for
// evicted sessions (used to drop the h.agents mirror entry).
func TestEvictIdleNotifiesHandler(t *testing.T) {
	var evicted []string
	mgr := NewSessionManager(time.Minute, func() []string { return nil }, func(id string) {
		evicted = append(evicted, id)
	})
	id := session.NewSessionID()
	mgr.Register(id, t.TempDir())
	mgr.setAgent(id, &agentSession{})
	mgr.Lookup(id).lastActivity = time.Now().Add(-2 * time.Minute)

	mgr.EvictIdle()
	if len(evicted) != 1 || evicted[0] != id {
		t.Fatalf("onEvict got %v, want [%s]", evicted, id)
	}
}
