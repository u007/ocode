package server

import (
	"testing"
	"time"
)

// This file covers the lifetime rules for the per-project cron engines, which are
// separate from the per-project ISOLATION rules in cron_scope_test.go.
//
// Two engines per project means a server that outlives many projects would
// otherwise accumulate one run loop plus one drainer per project it ever saw, so
// `evictIdleCronProjects` stops the ones nobody has touched. That is safe for an
// engine this scope started itself — its store is on disk and the next request
// reloads it. It is NOT safe for the DEFAULT project's engine, which the host
// started and seeded with its Telegram drainer sink and RC-bridge fan-out.

// cronEntryServices reads one project's engines out of the registry WITHOUT
// creating the entry. `cronProjectEntryFor` inserts on a miss, so a test that
// wants to assert an entry was EVICTED must ask the map, not the accessor —
// otherwise the accessor re-creates an empty entry and the assertion passes
// vacuously.
func cronEntryServices(srv *Server, root string) (*cronProjectServices, bool) {
	scope := srv.cronScopeOrInit()
	key := canonicalCronProject(root)
	scope.mu.Lock()
	e, present := scope.entries[key]
	scope.mu.Unlock()
	if !present {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.svcs, true
}

// ageCronEntry backdates an entry's idle clock past the eviction threshold,
// standing in for a server left open with that project's Cron tab unvisited.
func ageCronEntry(srv *Server, root string) {
	scope := srv.cronScopeOrInit()
	key := canonicalCronProject(root)
	scope.mu.Lock()
	e := scope.entries[key]
	scope.mu.Unlock()
	if e == nil {
		return
	}
	e.lastUsedNs.Store(time.Now().Add(-2 * cronProjectIdleTimeout).UnixNano())
}

// TestCronSeededDefaultProjectSurvivesIdleEviction pins the contract stated where
// the default entry is seeded (SetScheduler): that entry's services carry the
// host's Telegram drainer sink and RC-bridge fan-out, so evicting it and lazily
// starting a replacement would silently drop that wiring — and because nothing
// re-seeds the entry after boot, delivery for the default project would never come
// back. This is the bug: a server left open past the idle timeout lost Telegram
// and RC delivery for the boot project for the rest of its life.
func TestCronSeededDefaultProjectSurvivesIdleEviction(t *testing.T) {
	def, other := t.TempDir(), t.TempDir()
	srv := newScopedServer(t, def, other)

	seeded, ok := cronEntryServices(srv, def)
	if !ok || seeded == nil || seeded.cron == nil {
		t.Fatal("harness did not seed the default project's entry with a cron service")
	}

	// Warm a second project so the eviction half below has something to reclaim.
	if _, err := srv.servicesFor(other); err != nil {
		t.Fatalf("servicesFor(%s): %v", other, err)
	}

	ageCronEntry(srv, def)
	ageCronEntry(srv, other)
	srv.evictIdleCronProjects()

	// The seeded entry must survive...
	after, present := cronEntryServices(srv, def)
	if !present {
		t.Fatal("idle eviction dropped the host-seeded default project entry; the next resolve " +
			"starts a twin with no Telegram/RC fan-out, and nothing re-seeds it after boot")
	}
	if after == nil {
		t.Fatal("the default project entry survived in the map but its services were nil'd out, " +
			"so the next resolve still starts a sinkless twin")
	}
	// ...as the SAME service, not a replacement.
	if after.cron != seeded.cron {
		t.Fatal("the default project's cron service was swapped for a different instance; " +
			"the host's Telegram drainer sink and RC-bridge fan-out were dropped")
	}

	// A resolve must hand back that same live service.
	got, err := srv.servicesFor(def)
	if err != nil {
		t.Fatalf("servicesFor(default): %v", err)
	}
	if got.cron != seeded.cron {
		t.Fatal("servicesFor(default) returned a different cron service than the host seeded")
	}

	// And every OTHER project must still be reclaimed — the sweeper still works.
	// This half is what stops "never evict anything" from passing this test.
	if _, present := cronEntryServices(srv, other); present {
		t.Fatal("a non-default project entry survived idle eviction; the sweeper stopped reclaiming engines")
	}
}

// TestCronShutdownStopsTheSeededDefaultProject is the companion to the exemption
// above: exempting the seeded entry from the IDLE sweeper must not exempt it from
// shutdown, or every server restart leaks a run loop and a drainer.
func TestCronShutdownStopsTheSeededDefaultProject(t *testing.T) {
	def := t.TempDir()
	srv := newScopedServer(t, def)

	seeded, ok := cronEntryServices(srv, def)
	if !ok || seeded == nil || seeded.cron == nil {
		t.Fatal("harness did not seed the default project's entry with a cron service")
	}
	if seeded.cron.Stopped() {
		t.Fatal("harness seeded an already-stopped cron service")
	}
	// Make the entry evictable-by-idle, so this test would ALSO pass if the
	// exemption were wrongly mirrored into the shutdown path.
	ageCronEntry(srv, def)

	srv.stopAllCronServices()

	if _, present := cronEntryServices(srv, def); present {
		t.Fatal("shutdown left the default project's entry in the registry")
	}
	// The registry being cleared is NOT evidence the engines stopped —
	// `stopAllCronServices` empties the map unconditionally, so an implementation
	// that skipped `stop()` for pinned entries would still pass the check above.
	// Assert the engine itself.
	if !seeded.cron.Stopped() {
		t.Fatal("shutdown did not stop the host-seeded cron engine; the idle exemption was " +
			"wrongly mirrored into the shutdown path, leaking a run loop and a drainer per restart")
	}
}
