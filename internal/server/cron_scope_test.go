package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/reminders"
	"github.com/u007/ocode/internal/scheduler"
)

// This file covers the property the per-project change exists for: EVERY part of
// the cron surface — jobs, outbox, run history, targets, reminders and tasks —
// is isolated to the project it was created under. Before this, all of it was one
// process-wide service keyed on the server's boot directory, so opening project B
// showed project A's jobs and reminders.

func newScopedServer(t *testing.T, roots ...string) *Server {
	t.Helper()
	srv := &Server{mux: http.NewServeMux(), workDir: roots[0]}
	// A real projects store with every root registered, so the trust boundary has
	// something in it. Without this the extra roots would be refused — correctly.
	if len(roots) > 1 {
		h := testProjectHandler(t)
		for _, r := range roots {
			if err := h.projects.Add(r); err != nil {
				t.Fatalf("register project %s: %v", r, err)
			}
		}
		srv.handler = h
	}
	svc := scheduler.NewService(mustStorePath(t, roots[0]))
	if err := svc.Start(); err != nil {
		t.Fatalf("start default scheduler: %v", err)
	}
	// SetScheduler (not attachScheduler): it is the path that registers the
	// outbox/targets/runs routes AND builds the per-project leaf stores. Using
	// the lower-level function would leave those routes unregistered and the
	// harness would silently assert against 404s.
	srv.SetScheduler(svc)
	rem := reminders.NewService(mustRemindersPath(t, roots[0]))
	if err := rem.Start(); err != nil {
		t.Fatalf("start default reminders: %v", err)
	}
	rem.SetOutbox(srv.schedulerOutbox)
	rem.SetRunHistory(srv.schedulerRuns)
	srv.attachReminders(rem)
	srv.markRemindersAttached()
	srv.setCronScopeConfig(&config.Config{}, nil, svc, rem, roots[0],
		srv.schedulerOutbox, srv.schedulerRuns, srv.schedulerTargets)
	t.Cleanup(srv.stopAllCronServices)
	return srv
}

func mustStorePath(t *testing.T, root string) string {
	t.Helper()
	p, err := scheduler.DefaultStorePath(root)
	if err != nil {
		t.Fatalf("DefaultStorePath(%s): %v", root, err)
	}
	return p
}

func mustRemindersPath(t *testing.T, root string) string {
	t.Helper()
	p, err := reminders.DefaultStorePath(root)
	if err != nil {
		t.Fatalf("reminders.DefaultStorePath(%s): %v", root, err)
	}
	return p
}

func call2(t *testing.T, srv *Server, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, httptest.NewRequest(method, path, rdr))
	var out map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &out)
	}
	return w, out
}

func post2(t *testing.T, srv *Server, path string, body map[string]any) map[string]any {
	t.Helper()
	w, out := call2(t, srv, "POST", path, body)
	if w.Code != http.StatusOK {
		t.Fatalf("POST %s: %d %s", path, w.Code, w.Body.String())
	}
	return out
}

// TestCronIsolatesJobsPerProject is the headline case: a job created under
// project A must be invisible under project B, and both must coexist.
func TestCronIsolatesJobsPerProject(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	srv := newScopedServer(t, a, b)

	idA := post2(t, srv, "/api/cron?project="+a, map[string]any{
		"name": "project A job", "message": "only A",
		"schedule": map[string]any{"kind": "every", "every_ms": 60000},
	})["id"].(string)
	idB := post2(t, srv, "/api/cron?project="+b, map[string]any{
		"name": "project B job", "message": "only B",
		"schedule": map[string]any{"kind": "every", "every_ms": 60000},
	})["id"].(string)

	w, listA := call2(t, srv, "GET", "/api/cron?project="+a, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list A: %d", w.Code)
	}
	names := jobNames(listA)
	if len(names) != 1 || names[0] != "project A job" {
		t.Fatalf("project A sees %v, want only its own job", names)
	}
	_, listB := call2(t, srv, "GET", "/api/cron?project="+b, nil)
	names = jobNames(listB)
	if len(names) != 1 || names[0] != "project B job" {
		t.Fatalf("project B sees %v, want only its own job", names)
	}

	// A job's id is not addressable from the other project. This is the check
	// that would catch a shared service, which is how a delete from B could
	// otherwise remove A's job.
	w, _ = call2(t, srv, "DELETE", "/api/cron/"+idA+"?project="+b, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-project DELETE of A's job from B = %d, want 404", w.Code)
	}
	w, _ = call2(t, srv, "PATCH", "/api/cron/"+idA+"?project="+b, map[string]any{"name": "hijack"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-project PATCH of A's job from B = %d, want 404", w.Code)
	}
	// And A's job is still there and unchanged.
	_, listA = call2(t, srv, "GET", "/api/cron?project="+a, nil)
	if names = jobNames(listA); len(names) != 1 || names[0] != "project A job" {
		t.Fatalf("after the cross-project attempts A sees %v", names)
	}
	_ = idB
}

func jobNames(list map[string]any) []string {
	out := []string{}
	raw, _ := list["jobs"].([]any)
	for _, j := range raw {
		m, _ := j.(map[string]any)
		if m == nil {
			continue
		}
		name, _ := m["name"].(string)
		out = append(out, name)
	}
	return out
}

// TestCronIsolatesRemindersAndTasksPerProject is the same property for the new
// surface, plus the outbox and the targets registry — the two shared stores that
// would otherwise be the leakiest.
func TestCronIsolatesRemindersAndTasksPerProject(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	srv := newScopedServer(t, a, b)

	remA := post2(t, srv, "/api/reminders?project="+a, map[string]any{
		"title": "A reminder", "due_at_ms": farFutureMs,
	})["id"].(string)
	taskB := post2(t, srv, "/api/tasks?project="+b, map[string]any{
		"title": "B task",
	})["id"].(string)

	_, l := call2(t, srv, "GET", "/api/reminders?project="+a, nil)
	if items, _ := l["items"].([]any); len(items) != 1 {
		t.Fatalf("A reminders = %d items, want 1", len(items))
	}
	_, l = call2(t, srv, "GET", "/api/reminders?project="+b, nil)
	if items, _ := l["items"].([]any); len(items) != 0 {
		t.Fatalf("B reminders = %d items, want 0 (A's must not bleed)", len(items))
	}
	_, l = call2(t, srv, "GET", "/api/tasks?project="+a, nil)
	if items, _ := l["items"].([]any); len(items) != 0 {
		t.Fatalf("A tasks = %d items, want 0", len(items))
	}
	_, l = call2(t, srv, "GET", "/api/tasks?project="+b, nil)
	if items, _ := l["items"].([]any); len(items) != 1 {
		t.Fatalf("B tasks = %d items, want 1", len(items))
	}

	// A status change in A must not touch B.
	call2(t, srv, "PATCH", "/api/tasks/"+taskB+"?project="+b, map[string]any{"status": "completed"})
	_, l = call2(t, srv, "GET", "/api/tasks?project="+b, nil)
	items, _ := l["items"].([]any)
	m, _ := items[0].(map[string]any)
	if m["status"] != "completed" {
		t.Fatalf("B task status = %v, want completed", m["status"])
	}

	// Cross-kind AND cross-project: A cannot see or touch B's task.
	w, _ := call2(t, srv, "GET", "/api/reminders/"+taskB+"?project="+a, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("A reading B's task = %d, want 404", w.Code)
	}
	w, _ = call2(t, srv, "PATCH", "/api/tasks/"+taskB+"?project="+a, map[string]any{"title": "hijack"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("A patching B's task = %d, want 404", w.Code)
	}

	// The outbox is per project too: an append under A is not readable under B.
	if err := scheduler.NewOutbox(mustStorePath(t, a)).Append(scheduler.Delivery{
		JobID: "a-only", JobName: "A only", Result: "x", At: nowUTC(),
	}); err != nil {
		t.Fatalf("append A delivery: %v", err)
	}
	_, ob := call2(t, srv, "GET", "/api/cron/outbox?project="+a, nil)
	if n := len(entriesOf(ob)); n != 1 {
		t.Fatalf("A outbox = %d entries, want 1", n)
	}
	_, ob = call2(t, srv, "GET", "/api/cron/outbox?project="+b, nil)
	if n := len(entriesOf(ob)); n != 0 {
		t.Fatalf("B outbox = %d entries, want 0 (A's delivery leaked)", n)
	}

	// Targets are per project as well.
	post2(t, srv, "/api/cron/targets?project="+a, map[string]any{"workdir": a, "chat_id": 42})
	_, tg := call2(t, srv, "GET", "/api/cron/targets?project="+a, nil)
	m2, _ := tg["targets"].(map[string]any)
	if len(m2) != 1 {
		t.Fatalf("A targets = %v, want one entry", m2)
	}
	_, tg = call2(t, srv, "GET", "/api/cron/targets?project="+b, nil)
	m2, _ = tg["targets"].(map[string]any)
	if len(m2) != 0 {
		t.Fatalf("B targets = %v, want none (A's leaked)", m2)
	}
	_ = remA
}

func entriesOf(ob map[string]any) []any {
	e, _ := ob["entries"].([]any)
	return e
}

// TestCronRefusesUnregisteredProject is the trust boundary: a project the server
// has never been told about is refused, exactly as the terminal endpoints do.
func TestCronRefusesUnregisteredProject(t *testing.T) {
	known := t.TempDir()
	unknown := t.TempDir()
	srv := newScopedServer(t, known)

	for _, path := range []string{
		"/api/cron?project=" + unknown,
		"/api/cron/outbox?project=" + unknown,
		"/api/cron/targets?project=" + unknown,
		"/api/reminders?project=" + unknown,
		"/api/tasks?project=" + unknown,
	} {
		w, body := call2(t, srv, "GET", path, nil)
		if w.Code != http.StatusForbidden {
			t.Errorf("GET %s = %d, want 403 (%s)", path, w.Code, w.Body.String())
		}
		if msg, _ := body["error"].(string); msg == "" {
			t.Errorf("GET %s returned no error message", path)
		}
	}
	// A POST is refused too — a refusal that only covered reads would still let
	// a caller create state in an arbitrary directory.
	w, _ := call2(t, srv, "POST", "/api/reminders?project="+unknown, map[string]any{
		"title": "x", "due_at_ms": farFutureMs,
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("POST into an unregistered project = %d, want 403", w.Code)
	}
}

// TestCronDefaultProjectWithoutParam keeps every pre-existing caller working: a
// request with no project param resolves to the server's own workDir.
func TestCronDefaultProjectWithoutParam(t *testing.T) {
	a := t.TempDir()
	srv := newScopedServer(t, a)

	post2(t, srv, "/api/reminders", map[string]any{"title": "no-param", "due_at_ms": farFutureMs})
	_, l := call2(t, srv, "GET", "/api/reminders", nil)
	if items, _ := l["items"].([]any); len(items) != 1 {
		t.Fatalf("no-param list = %d items, want 1", len(items))
	}
	// The no-param view and the explicit-default view are the SAME project.
	_, l = call2(t, srv, "GET", "/api/reminders?project="+a, nil)
	if items, _ := l["items"].([]any); len(items) != 1 {
		t.Fatalf("explicit-default list = %d items, want 1 (the two must be one project)", len(items))
	}
}

// TestCronEnginesAreOnePerProject guards the cache: asking for the same project
// twice must return the SAME engine pair, not a second one racing it on the same
// store file.
func TestCronEnginesAreOnePerProject(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	srv := newScopedServer(t, a, b)

	first, err := srv.servicesFor(a)
	if err != nil {
		t.Fatalf("servicesFor(A): %v", err)
	}
	again, err := srv.servicesFor(a)
	if err != nil {
		t.Fatalf("servicesFor(A) again: %v", err)
	}
	if first != again {
		t.Fatal("servicesFor(A) returned two different bundles; two engines would race one store file")
	}
	other, err := srv.servicesFor(b)
	if err != nil {
		t.Fatalf("servicesFor(B): %v", err)
	}
	if first == other {
		t.Fatal("A and B share one bundle; the whole point of the change is that they do not")
	}
}

// TestCronServicesRefusedWhenNothingAttached covers the "no cron engine" guard.
// It is asserted at the service level, not over HTTP, because it is unreachable
// over HTTP by construction: `attachScheduler` is what registers the routes AND
// what marks cron enabled, so a server with a cron route always has an engine.
// The guard exists for a host that attaches the reminders half alone.
func TestCronServicesRefusedWhenNothingAttached(t *testing.T) {
	srv := &Server{mux: http.NewServeMux(), workDir: t.TempDir()}
	if _, err := srv.servicesFor(srv.workDir); !errors.Is(err, errCronNotAttached) {
		t.Fatalf("servicesFor with nothing attached = %v, want errCronNotAttached", err)
	}
	if got := cronScopeStatus(errCronNotAttached); got != http.StatusNotImplemented {
		t.Fatalf("status for errCronNotAttached = %d, want 501", got)
	}
}

// farFutureMs is 2100-01-01 in epoch ms. Written as a constant rather than a
// bit-shift because `1<<40` MILLISECONDS is December 2004 — comfortably in the
// PAST, which made the "future" reminder fire on creation and put a second entry
// in the outbox.
const farFutureMs int64 = 4102444800000

// nowUTC is a fixed clock for the outbox append in the isolation test, so the
// assertion does not depend on when the suite runs.
func nowUTC() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }
