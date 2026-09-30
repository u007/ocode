package server

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/reminders"
	"github.com/u007/ocode/internal/scheduler"
)

// This file makes the WHOLE cron surface project-scoped: jobs, the delivery
// outbox, run history, the Telegram targets registry, reminders, and tasks.
//
// The stores were ALREADY per project — `scheduler.DefaultStorePath(workDir)`
// resolves to `<GlobalDataDir>/scheduler/<base>-<slug>/jobs.json`, and
// `reminders.DefaultStorePath` puts `reminders.json` in that same directory. What
// was process-wide was the running SERVICE: one `scheduler.Service` and one
// `reminders.Service`, both started from the server's boot directory. So the Cron
// tab showed the boot project's jobs while the sidebar sat on a different project.
//
// The design deliberately mirrors the two per-project patterns already in this
// package: `Handler.lspManagerFor` (a canonical-root-keyed map) and
// `Handler.resolveTerminalHistoryProject` (the trust boundary for a project param).

// cronProjectIdleTimeout is how long a project's cron engines may sit untouched
// before the sweeper stops them. It matches `defaultSessionIdleTimeout` (30 min) so
// the two caches expire on the same timescale, and it is long enough that a
// reminder an hour out keeps firing: the engines are STOPPED, not discarded, and
// the next request for that project reloads the store from disk. Nothing is lost.
const cronProjectIdleTimeout = 30 * time.Minute

// cronProjectEntry is one project's pair of engines.
//
// The per-entry mutex (rather than a single lock held across starts) is what lets
// a slow start for project A not block a request for project B, while still
// guaranteeing that concurrent first touches of the SAME project produce exactly
// one pair of engines. It is a plain mutex and not a `sync.Once` on purpose: a
// `sync.Once` would make a FAILED start permanently sticky, so a transient error
// (a corrupt store, a full disk) would disable that project's cron for the life of
// the process. On error the entry is dropped from the map instead, so the next
// request retries.
type cronProjectEntry struct {
	mu         sync.Mutex
	svcs       *cronProjectServices
	lastUsedNs atomic.Int64
}

// cronProjectServices is everything one project owns. The three leaf stores are
// kept alongside the two engines because the outbox, the run history and the
// Telegram targets registry are all keyed off the SAME per-project store path —
// giving each request its own throwaway instance of them would work, but then a
// write from one request and a read from the next would go through different
// in-memory caches.
type cronProjectServices struct {
	cron      *scheduler.Service
	reminders *reminders.Service
	outbox    *scheduler.Outbox
	runs      *scheduler.RunHistory
	targets   *scheduler.Targets
}

// stop halts both engines. Called by the idle sweeper and by Shutdown, and only
// while the owning entry's mutex is held, so a request cannot be mid-start.
func (p *cronProjectServices) stop() {
	if p == nil {
		return
	}
	if p.cron != nil {
		p.cron.Stop()
	}
	if p.reminders != nil {
		p.reminders.Stop()
	}
}

func (e *cronProjectEntry) touch() { e.lastUsedNs.Store(time.Now().UnixNano()) }

func (e *cronProjectEntry) idleFor(now time.Time) time.Duration {
	return now.Sub(time.Unix(0, e.lastUsedNs.Load()))
}

// cronScope holds both registries. One mutex for map access only — never held
// while an engine starts, and never while an agent turn runs, so a cron request
// cannot stall an unrelated project (the same rule `Handler.mu` follows).
type cronScope struct {
	mu      sync.Mutex
	entries map[string]*cronProjectEntry
	// cfg is the host config used to build agents for both engines' AgentRunner.
	cfg *config.Config
	// notifier is the optional plain-notification sink for reminder firings.
	notifier reminders.Notifier
	// stopEvict stops the idle sweeper.
	stopEvict chan struct{}
	evictOnce sync.Once
}

func (s *Server) cronScopeOrInit() *cronScope {
	s.cronScopeMu.Lock()
	defer s.cronScopeMu.Unlock()
	if s.cron == nil {
		s.cron = &cronScope{
			entries:   make(map[string]*cronProjectEntry),
			stopEvict: make(chan struct{}),
		}
	}
	return s.cron
}

// canonicalCronProject normalises a project path into a cache key. The terminal
// trust boundary compares the EXPANDED path against the saved roots, so the key
// must be the expanded path too — otherwise `/Users/x/p` and `~/p` would be two
// engines writing the same store file.
func canonicalCronProject(root string) string {
	if root == "" {
		return ""
	}
	if expanded, err := projects.ExpandHome(root); err == nil && expanded != "" {
		root = expanded
	}
	abs, err := filepath.Abs(root)
	if err == nil {
		root = abs
	}
	return filepath.Clean(root)
}

// resolveCronProject reads the project a cron request is for and applies the same
// trust boundary as the terminal endpoints: an empty param falls back to the
// server's default project (which is what every pre-project-scope caller and the
// in-repo tests rely on), a leading `~` is expanded against THIS host's home, and
// anything else must be an EXACT member of `allowedProjectRoots()` or the request
// is refused with 403.
//
// It deliberately does NOT read `?host=`. A remote project's traffic is
// reverse-proxied to that host's own `serve --remote`, which runs this same code
// against ITS saved roots, so the local server never resolves a remote project's
// cron itself.
func (s *Server) resolveCronProject(r *http.Request) (string, int, string) {
	project := r.URL.Query().Get("project")
	if project == "" {
		project = r.URL.Query().Get("project_path")
	}
	if expanded, err := projects.ExpandHome(project); err == nil {
		project = expanded
	}
	if project == "" {
		project = s.workDir
	}
	if project == "" {
		project = "."
	}
	roots := s.allowedCronProjectRoots()
	if len(roots) == 0 {
		// This server has not been told about ANY project — no workDir and no
		// projects store, which is the shape of an embedded server and of most
		// package tests. A boundary with nothing in it cannot refuse anything,
		// so the resolved default is accepted. Refusing here instead would break
		// every such host outright.
		return project, 0, ""
	}
	for _, root := range roots {
		if canonicalCronProject(project) == canonicalCronProject(root) {
			return project, 0, ""
		}
	}
	return "", http.StatusForbidden, "project is not a project registered with this server"
}

// allowedCronProjectRoots is the trust boundary: the default project plus every
// saved LOCAL project. It mirrors `Handler.allowedProjectRoots` (which skips
// remote entries, because their path is interpreted on the other machine) and
// falls back to the server's own workDir when there is no Handler, which is the
// case in tests that build a `Server` literal.
func (s *Server) allowedCronProjectRoots() []string {
	if s.handler != nil {
		return s.handler.allowedProjectRoots()
	}
	if s.workDir != "" {
		return []string{s.workDir}
	}
	return nil
}

// cronProjectEntryFor returns the per-project entry, creating it if needed. The
// returned entry is NOT locked; the caller locks it.
func (s *Server) cronProjectEntryFor(root string) *cronProjectEntry {
	scope := s.cronScopeOrInit()
	key := canonicalCronProject(root)
	scope.mu.Lock()
	defer scope.mu.Unlock()
	e, ok := scope.entries[key]
	if !ok {
		e = &cronProjectEntry{}
		e.touch()
		scope.entries[key] = e
	}
	return e
}

// servicesFor returns the project's cron and reminders engines, starting them on
// first use.
//
// The start happens under the ENTRY's mutex, never the registry's, so a slow start
// for one project does not block any other. A failure removes the entry from the
// map so the next request retries rather than inheriting a sticky error.
func (s *Server) servicesFor(root string) (*cronProjectServices, error) {
	if !s.cronEnabled() {
		return nil, errCronNotAttached
	}
	entry := s.cronProjectEntryFor(root)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.touch()

	if entry.svcs != nil {
		return entry.svcs, nil
	}

	scope := s.cronScopeOrInit()
	svcs, err := s.startProjectServices(root, scope.cfg, scope.notifier)
	if err != nil {
		// Drop the entry. Note that a failed start is ALREADY retryable without
		// this delete — `entry.svcs` is only set on success, so the next call
		// re-enters the start path. (Deleting the delete is a semantically
		// equivalent mutant; it was verified as one rather than counted as a
		// missed test.) What the delete buys is bounded memory: without it, every
		// project with a permanently unreadable store leaves an entry behind
		// forever, and a server that has failed to open one directory would grow
		// its registry for the life of the process.
		key := canonicalCronProject(root)
		scope.mu.Lock()
		if scope.entries[key] == entry {
			delete(scope.entries, key)
		}
		scope.mu.Unlock()
		return nil, err
	}
	entry.svcs = svcs
	return svcs, nil
}

// cronEnabled reports whether any cron engine was attached at all. Without this
// the per-project machinery would try to start engines on a server that was never
// given a scheduler (the TUI-hosted case, and most tests), which is exactly the
// "restores a file the host never opened" failure mode.
func (s *Server) cronEnabled() bool {
	if s.scheduler != nil {
		return true
	}
	return s.cronHasReminders
}

// errCronNotAttached is returned when a cron route is hit on a server that has no
// cron engine wired. It maps to 501, which is honest: the route exists, the
// capability does not.
var errCronNotAttached = fmt.Errorf("scheduler not attached to this server")

// startProjectServices builds and starts BOTH engines for one project. The
// reminders engine deliberately reuses the cron project's Outbox and RunHistory
// instances (one delivery log, one drainer, one Telegram fan-out per project) —
// the same sharing AttachReminders does for the default project.
func (s *Server) startProjectServices(root string, cfg *config.Config, notifier reminders.Notifier) (*cronProjectServices, error) {
	jobsPath, err := scheduler.DefaultStorePath(root)
	if err != nil {
		return nil, err
	}
	cronSvc, err := scheduler.StartForHost(cfg, root, schedulerAgentRunner(cfg))
	if err != nil {
		return nil, fmt.Errorf("start cron for %s: %w", root, err)
	}
	remSvc, err := reminders.StartForHost(root, reminderAgentRunner(cfg), notifier)
	if err != nil {
		// The cron engine is already running; do not leak it because the
		// reminders half failed.
		cronSvc.Stop()
		return nil, fmt.Errorf("start reminders for %s: %w", root, err)
	}
	outbox := scheduler.NewOutbox(jobsPath)
	runs := scheduler.NewRunHistory(jobsPath)
	remSvc.SetOutbox(outbox)
	remSvc.SetRunHistory(runs)
	return &cronProjectServices{
		cron:      cronSvc,
		reminders: remSvc,
		outbox:    outbox,
		runs:      runs,
		targets:   scheduler.NewTargets(jobsPath),
	}, nil
}

// setCronScopeConfig records the config and notifier the lazily-started per-project
// engines should use, and seeds the default project's entry with the engines the
// host already started. Seeding matters: the host's `SetScheduler` /
// `AttachReminders` services have the Telegram sink and RC-bridge fan-out already
// attached, and replacing them with a lazily started twin would silently drop that.
func (s *Server) setCronScopeConfig(cfg *config.Config, notifier reminders.Notifier, cronSvc *scheduler.Service, remSvc *reminders.Service, defaultRoot string, schedulerOutbox *scheduler.Outbox, schedulerRuns *scheduler.RunHistory, schedulerTargets *scheduler.Targets) {
	scope := s.cronScopeOrInit()
	scope.mu.Lock()
	scope.cfg = cfg
	scope.notifier = notifier
	scope.mu.Unlock()

	if cronSvc == nil && remSvc == nil && schedulerOutbox == nil && schedulerRuns == nil && schedulerTargets == nil {
		// Nothing to seed. This is the case where a host attached no cron
		// capability at all, and the registry must stay inert.
		return
	}
	entry := s.cronProjectEntryFor(defaultRoot)
	entry.mu.Lock()
	entry.svcs = &cronProjectServices{
		cron:      cronSvc,
		reminders: remSvc,
		outbox:    schedulerOutbox,
		runs:      schedulerRuns,
		targets:   schedulerTargets,
	}
	entry.touch()
	entry.mu.Unlock()
}

// markRemindersAttached records that a reminders engine exists, which is what makes
// the per-project machinery eligible to start anything at all.
func (s *Server) markRemindersAttached() {
	s.cronScopeMu.Lock()
	s.cronHasReminders = true
	s.cronScopeMu.Unlock()
}

// warmCronProjects starts both engines for every saved LOCAL project, so a
// reminder in a project nobody has opened still fires and still reaches the
// Telegram drainer. Without this the feature would only work once you looked at a
// project, which is not what anyone means by a reminder.
//
// Remote projects are skipped on purpose: their traffic is reverse-proxied to that
// host, and the host warms its own list. Failures are logged and skipped — one
// unreadable project must not stop the others from arming.
func (s *Server) warmCronProjects() {
	if !s.cronEnabled() {
		return
	}
	roots := s.allowedCronProjectRoots()
	for _, root := range roots {
		if root == "" {
			continue
		}
		if _, err := s.servicesFor(root); err != nil {
			// A project whose store is corrupt is reported, not fatal.
			log.Printf("serve: cron: could not arm project %s: %v", root, err)
		}
	}
	s.startCronEvictSweeper()
}

// startCronEvictSweeper launches the single background goroutine that stops engines
// for projects nobody has touched in `cronProjectIdleTimeout`. One sweeper for the
// whole process, not one timer per project, so an idle server costs a single
// ticker.
func (s *Server) startCronEvictSweeper() {
	scope := s.cronScopeOrInit()
	scope.evictOnce.Do(func() {
		go func() {
			t := time.NewTicker(cronProjectIdleTimeout / 2)
			defer t.Stop()
			for {
				select {
				case <-scope.stopEvict:
					return
				case <-t.C:
					s.evictIdleCronProjects()
				}
			}
		}()
	})
}

// evictIdleCronProjects stops and forgets the engines of untouched projects. It is
// lossless: the store is on disk, and the next request for that project reloads it.
func (s *Server) evictIdleCronProjects() {
	scope := s.cronScopeOrInit()
	now := time.Now()

	type victim struct {
		key   string
		entry *cronProjectEntry
	}
	var victims []victim

	scope.mu.Lock()
	for key, e := range scope.entries {
		if e.idleFor(now) <= cronProjectIdleTimeout {
			continue
		}
		victims = append(victims, victim{key: key, entry: e})
		delete(scope.entries, key)
	}
	scope.mu.Unlock()

	for _, v := range victims {
		// Lock the entry so a request that just resolved this project cannot be
		// mid-start while we stop it.
		v.entry.mu.Lock()
		v.entry.svcs.stop()
		v.entry.svcs = nil
		v.entry.mu.Unlock()
	}
}

// stopAllCronServices stops every engine this server started. Wired into
// `Server.Shutdown`, which already stops the tts and tsShare subsystems — before
// this change a running scheduler was never stopped at all.
func (s *Server) stopAllCronServices() {
	s.cronScopeMu.Lock()
	scope := s.cron
	s.cronScopeMu.Unlock()
	if scope == nil {
		return
	}
	scope.evictOnce.Do(func() { close(scope.stopEvict) })

	scope.mu.Lock()
	entries := make([]*cronProjectEntry, 0, len(scope.entries))
	for _, e := range scope.entries {
		entries = append(entries, e)
	}
	scope.entries = make(map[string]*cronProjectEntry)
	scope.mu.Unlock()

	for _, e := range entries {
		e.mu.Lock()
		e.svcs.stop()
		e.svcs = nil
		e.mu.Unlock()
	}
}

// cronServicesFor resolves the project a cron request is for and returns THAT
// project's engines and stores, writing the refusal itself when the project is
// not allowed or its engines cannot start. Every cron route goes through this
// before touching any service, which is what keeps one project's jobs, outbox,
// targets, reminders and tasks invisible to another.
//
// It is the single place the project param is read for this surface, so there is
// no route that can forget it.
func (s *Server) cronServicesFor(w http.ResponseWriter, r *http.Request) (*cronProjectServices, bool) {
	root, code, msg := s.resolveCronProject(r)
	if code != 0 {
		writeError(w, code, msg)
		return nil, false
	}
	svcs, err := s.servicesFor(root)
	if err != nil {
		writeError(w, cronScopeStatus(err), err.Error())
		return nil, false
	}
	return svcs, true
}

// cronScopeStatus maps a per-project start failure onto an HTTP status. A server
// with no cron engine attached is 501, not 400: the route exists and the
// capability does not, and the web UI needs to be able to tell that apart from a
// bad request so it can show "cron is unavailable" rather than a field error.
func cronScopeStatus(err error) int {
	if errors.Is(err, errCronNotAttached) {
		return http.StatusNotImplemented
	}
	return http.StatusInternalServerError
}

// cronConfig returns the config the lazily-started per-project engines should
// build agents from. It lives on the Handler (the server reloads it per project
// via SetWorkDir), so a Server that has a Handler reads it from there; a bare
// `&Server{...}` in a test has neither, and the engines then run with a nil
// config — which `RunScheduledJob` already reports as an error rather than
// silently misbehaving.
func (s *Server) cronConfig() *config.Config {
	if s.handler != nil {
		return s.handler.cfg
	}
	return nil
}

// WarmCronProjects is the host-facing entry point: start both engines for every
// saved local project. See the method for why this is eager rather than lazy.
func (s *Server) WarmCronProjects() { s.warmCronProjects() }

// markCronEnabled declares that cron is available even when the host attached no
// service of its own. A test (or a host that only wants the leaf stores) can use
// it to opt in; without it `servicesFor` refuses to start an engine, because
// starting one unbidden is how a server ends up writing scheduler state the host
// never asked for.
func (s *Server) markCronEnabled() {
	s.cronScopeMu.Lock()
	s.cronHasReminders = true
	s.cronScopeMu.Unlock()
}
