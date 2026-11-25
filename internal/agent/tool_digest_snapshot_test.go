package agent

import (
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/crashguard"
)

// TestToolDigestSnapshot_ApplySpecModelSwapRace drives the real applySpecModel
// swap (through the newClientFn seam) against snapshotToolDigestState and the
// OpenCode identity accessors. Run it with -race: the swap and the snapshot
// read must be synchronized, and the lock order must never deadlock.
func TestToolDigestSnapshot_ApplySpecModelSwapRace(t *testing.T) {
	initial := &toolDigestStubClient{provider: "fake", model: "initial"}
	a := &Agent{client: initial, config: &config.Config{}}

	prevNewClient := newClientFn
	t.Cleanup(func() { newClientFn = prevNewClient })
	createdCh := make(chan LLMClient, 256)
	newClientFn = func(*config.Config, string) LLMClient {
		c := &toolDigestStubClient{provider: "fake", model: "swap"}
		createdCh <- c
		return c
	}

	const swaps = 100
	const snaps = 200
	spec := &AgentSpec{Name: "swap-race", Model: "fake/swap"}

	swapDone := make(chan struct{})
	snapDone := make(chan struct{})
	identDone := make(chan struct{})
	var snapped []LLMClient

	crashguard.Go(func() {
		defer close(swapDone)
		for i := 0; i < swaps; i++ {
			a.applySpecModel(spec)
		}
	})
	crashguard.Go(func() {
		defer close(snapDone)
		for i := 0; i < snaps; i++ {
			snapped = append(snapped, a.snapshotToolDigestState().Client)
		}
	})
	crashguard.Go(func() {
		defer close(identDone)
		for i := 0; i < swaps; i++ {
			a.SetOpenCodeSessionID("ses_swap_race")
			if a.OpenCodeSessionID() == "" {
				t.Errorf("OpenCodeSessionID returned empty during the swap race")
				return
			}
		}
	})

	for _, ch := range []chan struct{}{swapDone, snapDone, identDone} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("swap/snapshot/identity goroutines did not finish within 5s: possible deadlock")
		}
	}
	close(createdCh)

	created := make(map[LLMClient]bool, swaps)
	for c := range createdCh {
		created[c] = true
	}
	if len(created) != swaps {
		t.Fatalf("applySpecModel built %d clients, want %d: some swaps did not complete", len(created), swaps)
	}
	for i, c := range snapped {
		if c == nil {
			t.Fatalf("snapshot %d carried a nil client", i)
		}
		if c != LLMClient(initial) && !created[c] {
			t.Fatalf("snapshot %d carried a client that is neither the old nor a new pointer", i)
		}
	}
	if cur := a.clientSnapshot(); !created[cur] {
		t.Fatalf("final client is not one the swap installed: %#v", cur)
	}
}
