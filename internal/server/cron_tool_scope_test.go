package server

import (
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/scheduler"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// This file covers TODO.md item 1: the LLM-facing `cron` tool must operate on the
// SESSION'S project, not the server's boot project.
//
// Before this, `buildAgentSession` injected `h.scheduler` — one process-wide
// service keyed on the server's boot directory — into every agent. So a chat on
// project B that asked the agent to "schedule this nightly" created the job in
// project A, where the user never sees it. The REST surface had already been made
// per project (cron_scope.go); this closes the last unscoped path into the same
// data.
//
// The assertions are deliberately behavioural — a job created by the tool is
// looked up in the project's own store FILE — rather than a check on which
// service pointer the tool captured. Both services are live in this process, so
// asking a service "what do you have" would answer from the right place even when
// the tool wrote to the wrong one.

// newCronToolHandler builds a Handler with the given projects registered and a
// ready MCP cache, so `buildAgentSession` does not stall enumerating MCP servers.
func newCronToolHandler(t *testing.T, roots ...string) *Handler {
	t.Helper()
	h := NewHandler()
	h.projects = newTestProjectStore(t, roots...)
	h.SetWorkDir(roots[0])
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}
	return h
}

// buildCronToolAgent builds an agent session in `root` and returns it. An empty
// root means "the server's own project dir" and is passed through unchanged, so
// the fallback under test is the production one.
func buildCronToolAgent(t *testing.T, h *Handler, root string) *agentSession {
	t.Helper()
	id := session.NewSessionID()
	saveSessionToDir(t, h.workDir, id)
	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, root)
	if err != nil {
		t.Fatalf("buildAgentSession(root=%q): %v (stage %s)", root, err, stage)
	}
	t.Cleanup(func() { as.agent.Shutdown() })
	return as
}

func cronToolAddArgs(t *testing.T, name string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"action":   "add",
		"name":     name,
		"message":  "prompt for " + name,
		"schedule": map[string]any{"kind": "every", "every_ms": 60000},
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return raw
}

func cronToolIn(t *testing.T, as *agentSession) tool.Tool {
	t.Helper()
	ct, ok := as.agent.GetTool("cron")
	if !ok {
		t.Fatal("the session has no `cron` tool; a scheduler is attached, so it should have one")
	}
	return ct
}

func cronToolAdd(t *testing.T, as *agentSession, name string) {
	t.Helper()
	if _, err := cronToolIn(t, as).Execute(cronToolAddArgs(t, name)); err != nil {
		t.Fatalf("cron add %q: %v", name, err)
	}
}

// storeJobNames reads a project's jobs.json straight off disk.
func storeJobNames(t *testing.T, root string) []string {
	t.Helper()
	raw, err := os.ReadFile(mustStorePath(t, root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read store for %s: %v", root, err)
	}
	var doc struct {
		Jobs []struct {
			Name string `json:"name"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse store for %s: %v", root, err)
	}
	out := make([]string, 0, len(doc.Jobs))
	for _, j := range doc.Jobs {
		out = append(out, j.Name)
	}
	return out
}

// TestCronToolWritesToTheSessionProject is the headline case: two sessions on two
// projects, each adding a job through the LLM tool, and each job lands only in its
// own project's store.
func TestCronToolWritesToTheSessionProject(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	srv := newScopedServer(t, a, b)
	// Own handler: `newScopedServer` only creates one when given 2+ roots, and
	// these tests are about the session->tool path, not about Server wiring (that
	// is TestSetSchedulerInstallsThePerProjectCronResolver). The REAL resolver is
	// wired in, not a stand-in.
	h := newCronToolHandler(t, a, b)
	h.cronServices = srv.cronServiceResolver()

	cronToolAdd(t, buildCronToolAgent(t, h, a), "job from A")
	cronToolAdd(t, buildCronToolAgent(t, h, b), "job from B")

	if got := storeJobNames(t, a); len(got) != 1 || got[0] != "job from A" {
		t.Fatalf("project A's store holds %v, want exactly [job from A]", got)
	}
	if got := storeJobNames(t, b); len(got) != 1 || got[0] != "job from B" {
		t.Fatalf("project B's store holds %v, want exactly [job from B]", got)
	}
}

// TestCronToolEmptyProjectRootUsesTheDefaultProject pins the "" -> h.workDir
// fallback, and pins WHERE it is applied. Resolving with the raw empty root would
// key the tool on "" — a different store directory from the default project's —
// so the job would land somewhere the Cron tab never looks.
func TestCronToolEmptyProjectRootUsesTheDefaultProject(t *testing.T) {
	def := t.TempDir()
	srv := newScopedServer(t, def)
	h := newCronToolHandler(t, def, t.TempDir())
	h.cronServices = srv.cronServiceResolver()

	cronToolAdd(t, buildCronToolAgent(t, h, ""), "job from the default project")

	if got := storeJobNames(t, def); len(got) != 1 || got[0] != "job from the default project" {
		t.Fatalf("the default project's store holds %v, want exactly [job from the default project]", got)
	}
}

// TestCronToolResolverErrorDoesNotFallBackToAnotherProject pins the deliberate
// no-fallback rule. If resolution fails the tool must fail the call loudly: the
// tempting "fall back to h.scheduler" would silently file the job in the WRONG
// project, which is the exact bug this change exists to remove.
func TestCronToolResolverErrorDoesNotFallBackToAnotherProject(t *testing.T) {
	a := t.TempDir()
	srv := newScopedServer(t, a)
	h := newCronToolHandler(t, a, t.TempDir())

	// A real server has BOTH wirings: the legacy boot-project service AND the
	// per-project resolver. Setting only the resolver would make this test blind to
	// a "helpful" implementation that quietly falls back to `h.scheduler` on
	// failure — the fallback would find nil and the mutant would behave exactly
	// like the correct code.
	seeded, ok := cronEntryServices(srv, a)
	if !ok || seeded == nil || seeded.cron == nil {
		t.Fatal("harness did not seed the default project's cron engine")
	}
	h.scheduler = seeded.cron

	// The real resolver, wrapped in a switch. Swapping `h.cronServices` after the
	// session is built would prove nothing: the tool captures the resolver when
	// the tool set is constructed, so a later field assignment is invisible to it.
	// Flipping the SWITCH instead is what actually pins the design claim — that
	// the tool resolves on every call rather than once at build time — and it is
	// also what a real resolution failure (a store that has become unreadable)
	// looks like from inside the tool.
	real := srv.cronServiceResolver()
	var failing atomic.Bool
	h.cronServices = func(root string) (*scheduler.Service, error) {
		if failing.Load() {
			return nil, errors.New("cron engines unavailable")
		}
		return real(root)
	}

	as := buildCronToolAgent(t, h, a)

	// Sanity: while healthy the tool writes to project A. Without this, the
	// assertions below would also pass if the tool were simply broken.
	cronToolAdd(t, as, "written while healthy")
	if got := storeJobNames(t, a); len(got) != 1 || got[0] != "written while healthy" {
		t.Fatalf("project A's store holds %v, want [written while healthy]", got)
	}

	// Now break resolution. The seeded engine is still live, so a fallback to it
	// would succeed and hide the failure.
	failing.Store(true)

	out, err := cronToolIn(t, as).Execute(cronToolAddArgs(t, "should not exist"))
	if err == nil {
		t.Fatalf("cron add succeeded (%q) despite the resolver failing; it must not fall back to another project", out)
	}
	// The only job present is the one written while healthy — the failed call
	// must not have appended anything, in this project or any other.
	if got := storeJobNames(t, a); len(got) != 1 || got[0] != "written while healthy" {
		t.Fatalf("project A's store holds %v; the failed call must not write, "+
			"and must not fall back to the boot project", got)
	}
}

// TestCronToolOmittedWhenNoSchedulerAttached preserves the pre-existing rule: a
// host with no cron engine (the TUI-hosted server, most tests) gets NO cron tool
// at all, rather than a tool that errors on every call.
func TestCronToolOmittedWhenNoSchedulerAttached(t *testing.T) {
	proj, spare := t.TempDir(), t.TempDir()
	h := newCronToolHandler(t, proj, spare)
	// No cronServices and no h.scheduler: nothing is attached.
	if _, ok := buildCronToolAgent(t, h, proj).agent.GetTool("cron"); ok {
		t.Fatal("a session on a server with no scheduler must not be given a `cron` tool")
	}
}

// TestCronToolReResolvesAfterItsEngineWasReclaimed pins why the tool holds a
// RESOLVER rather than a service pointer captured at build time.
//
// The per-project engines are stopped by the idle sweeper, so a tool holding a
// captured pointer would keep calling a stopped engine for the rest of the
// session's life. Re-resolving per call both keeps the entry warm and always
// returns a live service — which is what lets a project be reclaimed and then
// used again without rebuilding the agent.
func TestCronToolReResolvesAfterItsEngineWasReclaimed(t *testing.T) {
	def, other := t.TempDir(), t.TempDir()
	srv := newScopedServer(t, def, other)
	h := newCronToolHandler(t, def, other)
	h.cronServices = srv.cronServiceResolver()

	// `other` is not the default project, so its engines ARE reclaimable. Build
	// the session first, so the tool has already captured whatever it captures.
	as := buildCronToolAgent(t, h, other)
	ct := cronToolIn(t, as)

	ageCronEntry(srv, other)
	srv.evictIdleCronProjects()
	if _, present := cronEntryServices(srv, other); present {
		t.Fatal("the non-default project's entry was not reclaimed; this test needs a reclaimed engine")
	}

	if _, err := ct.Execute(cronToolAddArgs(t, "after reclamation")); err != nil {
		t.Fatalf("cron add after the engine was reclaimed: %v", err)
	}
	if got := storeJobNames(t, other); len(got) != 1 || got[0] != "after reclamation" {
		t.Fatalf("project store holds %v, want [after reclamation]", got)
	}

	// The store assertion above CANNOT tell a live engine from a stale one:
	// `AddJob` mutates the in-memory list and persists, and does not care whether
	// the run loop is still alive, so a tool holding a service captured at build
	// time writes the job just the same. What distinguishes them is whether the
	// call re-entered the registry and came back with a RUNNING engine.
	revived, present := cronEntryServices(srv, other)
	if !present || revived == nil {
		t.Fatal("after the cron call the project's engines were not re-resolved; the tool is " +
			"holding a service captured at build time, which the idle sweeper has since stopped")
	}
	if revived.cron.Stopped() {
		t.Fatal("the project's engine is back in the registry but STOPPED; the tool re-resolved " +
			"to a dead engine instead of starting a live one")
	}
}

// TestCronToolFallsBackToTheSingleServiceWithNoProjectScope covers the ONE
// fallback `cronToolService` is allowed to make: a host that has a scheduler but
// never installed a per-project scope. That is a TUI-hosted or embedded server, and
// a test that sets `h.scheduler` directly. Such a host must keep working exactly
// as it did before project scoping existed — the tool is present and its jobs go
// to the single service.
//
// The advisor checkpoint caught this: the fallback had no test at all. The other
// `h.scheduler` tests in this package (scheduler_rc_test.go, scheduler_resolver_test.go)
// set the field but never build an agent session, so they never reach
// `cronToolService`.
func TestCronToolFallsBackToTheSingleServiceWithNoProjectScope(t *testing.T) {
	def := t.TempDir()
	srv := newScopedServer(t, def)

	seeded, ok := cronEntryServices(srv, def)
	if !ok || seeded == nil || seeded.cron == nil {
		t.Fatal("harness did not seed the default project's cron engine")
	}

	// A host with a scheduler but NO per-project scope.
	h := newCronToolHandler(t, def, t.TempDir())
	h.scheduler = seeded.cron
	if h.cronServices != nil {
		t.Fatal("harness installed a per-project resolver; this test is about its absence")
	}

	ct, ok := buildCronToolAgent(t, h, def).agent.GetTool("cron")
	if !ok {
		t.Fatal("a host with a single scheduler and no project scope must still get a `cron` tool; " +
			"the fallback exists so those hosts keep working")
	}
	if _, err := ct.Execute(cronToolAddArgs(t, "single-service job")); err != nil {
		t.Fatalf("cron add via the single-service fallback: %v", err)
	}
	if got := storeJobNames(t, def); len(got) != 1 || got[0] != "single-service job" {
		t.Fatalf("store holds %v, want [single-service job]", got)
	}
}

// TestSetSchedulerInstallsThePerProjectCronResolverOnTheHandler covers the wiring
// the session->tool tests deliberately stub out. Without it the tool would fall
// back to the single boot-project service for every session, which is the bug.
func TestSetSchedulerInstallsThePerProjectCronResolverOnTheHandler(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	// Two roots, so the shared harness creates a handler before SetScheduler runs.
	srv := newScopedServer(t, a, b)
	h := srv.handler
	if h == nil {
		t.Fatal("harness did not create a handler for a two-root server")
	}
	if h.cronServices == nil {
		t.Fatal("SetScheduler did not install a per-project cron resolver on the handler; " +
			"every agent session would fall back to the boot project's engine")
	}

	svcA, err := h.cronServices(a)
	if err != nil {
		t.Fatalf("resolve %s: %v", a, err)
	}
	svcB, err := h.cronServices(b)
	if err != nil {
		t.Fatalf("resolve %s: %v", b, err)
	}
	if svcA == svcB {
		t.Fatal("two projects resolved to the same engine; the resolver is not per project")
	}

	// The default project must resolve to the HOST-SEEDED engine, not a lazily
	// started twin, or the Telegram/RC sinks are missing.
	seeded, ok := cronEntryServices(srv, a)
	if !ok || seeded == nil {
		t.Fatal("the default project's entry is not in the registry")
	}
	if svcA != seeded.cron {
		t.Fatal("the default project resolved to a different engine than the host seeded")
	}
}
