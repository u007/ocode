package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/discovery"
)

// blockingEmbedder wraps FakeEmbedder but blocks the first Embed until released,
// so a test can observe a warm that is genuinely in-flight.
type blockingEmbedder struct {
	discovery.FakeEmbedder
	calls   int32
	started chan struct{}
	release chan struct{}
}

func (b *blockingEmbedder) Embed(ctx context.Context, texts []string, kind discovery.EmbedKind) ([][]float32, error) {
	if atomic.AddInt32(&b.calls, 1) == 1 {
		close(b.started)
		select {
		case <-b.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return b.FakeEmbedder.Embed(ctx, texts, kind)
}

// TestStartBackgroundWarm_singleFlight guards the fix for the cold-warm deadlock:
// a slow (local) embedder is warmed off the turn's critical path, exactly once at
// a time, and once it lands the engine is Ready so later turns rank off the cache.
func TestStartBackgroundWarm_singleFlight(t *testing.T) {
	emb := &blockingEmbedder{
		FakeEmbedder: discovery.FakeEmbedder{Dimension: 8},
		started:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	eng := discovery.NewEngine(emb, t.TempDir())
	a := &Agent{disco: &discoveryState{enabled: true, engine: eng, session: discovery.NewSession(eng)}}
	docs := []discovery.Doc{{ID: "skill:a", Kind: "skill", Name: "a", Text: "alpha one"}}

	// First call launches the background warm; wait until it is actually running.
	a.startBackgroundWarm(docs)
	select {
	case <-emb.started:
	case <-time.After(2 * time.Second):
		t.Fatal("background warm never started")
	}
	if !a.disco.warming.Load() {
		t.Fatal("warming flag not set while warm in flight")
	}

	// A second call while in-flight must be a no-op (single-flight): no extra Embed.
	a.startBackgroundWarm(docs)
	if got := atomic.LoadInt32(&emb.calls); got != 1 {
		t.Fatalf("single-flight violated: Embed called %d times, want 1", got)
	}

	// Release and wait for the warm to finish; the flag must clear and the engine
	// must become Ready so subsequent turns rank instead of failing open.
	close(emb.release)
	deadline := time.Now().Add(2 * time.Second)
	for a.disco.warming.Load() {
		if time.Now().After(deadline) {
			t.Fatal("warming flag never cleared after completion")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !eng.Ready() {
		t.Fatal("engine not Ready after background warm completed")
	}
	if got := atomic.LoadInt32(&emb.calls); got != 1 {
		t.Fatalf("Embed called %d times total, want 1", got)
	}
}
