package agent

import (
	"reflect"
	"sync"
	"testing"
)

// TestAgentRegistryConcurrentReloadAndRead pins the invariant that a registry
// reload and a concurrent read are safe.
//
// A reload REPLACES the whole agent list (it nils defs/diagnostic, re-registers
// the built-ins, then re-adds the markdown entries), while readers walk the same
// fields. Without synchronization this is a plain data race, and the observable
// failure in production is not a corrupted list but a torn read: a background
// title-generation goroutine resolving a hidden agent while a config reload
// repopulates the registry.
//
// LoadMarkdownAgents is the real entry point (it performs the filesystem scan);
// ReloadMarkdownAgents(nil) reaches the same reloadMarkdownAgents body and is
// used here so the test does not depend on the user's agent directories.
func TestAgentRegistryConcurrentReloadAndRead(t *testing.T) {
	r := NewAgentRegistry()

	const (
		reloads = 40
		readers = 8
		perRead = 40
	)
	var wg sync.WaitGroup
	// start is a barrier so the writer and the readers provably overlap: without
	// it the writer could finish all 40 reloads before the first reader starts
	// and the test would pass vacuously. No sleeps anywhere in this test.
	start := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < reloads; i++ {
			r.ReloadMarkdownAgents(nil)
		}
	}()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < perRead; j++ {
				_ = r.All()
				_ = r.SubAgents()
				_ = r.PrimaryAgents()
				_ = r.Diagnostics()
				_ = r.Get("build")
			}
		}()
	}
	close(start)
	wg.Wait()

	// The registry must still be coherent afterwards: the built-in "build"
	// agent is re-registered by every reload, so it must be present exactly once.
	all := r.All()
	seen := 0
	for _, d := range all {
		if d.Name == "build" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("built-in \"build\" appears %d times after %d reloads, want 1", seen, reloads)
	}
}

// Get must hand back a stable SNAPSHOT. Callers keep the pointer and read its
// fields after the call returns (title.go's lookupHiddenAgent reads def.Hidden
// from a background goroutine), so what they hold must not be mutated out from
// under them by a later in-place update.
//
// The hazard is addLoaded, which rewrites r.defs[i] in place. A reload cannot
// trigger it against an already-handed-out pointer (each reload clears defs
// first, so it builds a fresh array), which is exactly why this asserts the
// update path directly rather than relying on a reload: the contract is "you
// get your own value", and the first post-publication update API added later
// must not quietly break it.
func TestAgentRegistryGetIsStableAcrossUpdate(t *testing.T) {
	r := NewAgentRegistry()

	def := r.Get("build")
	if def == nil {
		t.Fatal(`Get("build") = nil, want the built-in`)
	}
	before := *def

	// In-place update of the same name, as addLoaded performs.
	updated := before
	updated.Description = "description written by the test"
	r.addLoaded(updated)

	// DeepEqual, not ==: AgentDefinition holds a []string (Tools) and so is not
	// comparable.
	if !reflect.DeepEqual(*def, before) {
		t.Fatalf("value handed out by Get changed under the caller: before %+v, now %+v", before, *def)
	}
	// The registry itself must reflect the update.
	if got := r.Get("build"); got == nil || got.Description != "description written by the test" {
		t.Fatalf("registry did not apply the update: %+v", got)
	}
}
