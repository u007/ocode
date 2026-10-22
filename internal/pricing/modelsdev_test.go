package pricing

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"
)

func TestLookupReturnsBundledPricing(t *testing.T) {
	got, ok := Lookup("gpt-4o")
	if !ok {
		t.Fatal("expected gpt-4o pricing to exist")
	}

	if got.InputPerMillion != 5 || got.OutputPerMillion != 15 {
		t.Fatalf("unexpected gpt-4o pricing: %+v", got)
	}
}

func TestLookupRejectsUnknownModel(t *testing.T) {
	if _, ok := Lookup("does-not-exist"); ok {
		t.Fatal("expected unknown model lookup to fail")
	}
}

// TestLookupWarnsOncePerUnknownModel guards against the unknown-model warning
// firing on every LLM call for the same model.
func TestLookupWarnsOncePerUnknownModel(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	const model = "warn-once-test-model"
	for range 3 {
		if _, ok := Lookup(model); ok {
			t.Fatal("expected unknown model lookup to fail")
		}
	}

	if got := strings.Count(buf.String(), model); got != 1 {
		t.Fatalf("unknown model warning logged %d times, want 1; log:\n%s", got, buf.String())
	}
}

func TestLookupNormalizesPrefixedAndVersionedModels(t *testing.T) {
	got, ok := Lookup("openai/gpt-4o-2024-05-13")
	if !ok {
		t.Fatal("expected normalized model lookup to succeed")
	}

	if got.InputPerMillion != 5 || got.OutputPerMillion != 15 {
		t.Fatalf("unexpected normalized pricing: %+v", got)
	}
}

func TestLookupFallsBackToMiniMaxM3Pricing(t *testing.T) {
	got, ok := Lookup("minimax/minimax-m3-20260531")
	if !ok {
		t.Fatal("expected minimax m3 pricing to fall back")
	}

	if got.InputPerMillion != 0.30 || got.OutputPerMillion != 1.20 {
		t.Fatalf("unexpected minimax m3 pricing: %+v", got)
	}
}

func TestLookupNormalizesCaseInsensitiveModelNames(t *testing.T) {
	got, ok := Lookup("MiniMax/MiniMax-M3")
	if !ok {
		t.Fatal("expected case-insensitive minimax lookup to succeed")
	}

	if got.InputPerMillion != 0.30 || got.OutputPerMillion != 1.20 {
		t.Fatalf("unexpected case-insensitive pricing: %+v", got)
	}
}

func TestLookupFallsBackToCodexGPT56Pricing(t *testing.T) {
	// The GPT-5.6 codex family (gpt-5.6-luna/sol/terra) must resolve even when
	// the models.dev snapshot is stale, so codex sessions don't silently bill $0.
	got, ok := Lookup("gpt-5.6-luna")
	if !ok {
		t.Fatal("expected gpt-5.6-luna pricing to exist")
	}
	if got.InputPerMillion != 0.20 || got.OutputPerMillion != 1.20 {
		t.Fatalf("unexpected gpt-5.6-luna pricing: %+v", got)
	}

	// Provider-prefixed form must normalize the same way.
	if _, ok := Lookup("openai/gpt-5.6-terra"); !ok {
		t.Fatal("expected openai/gpt-5.6-terra pricing to resolve")
	}
}

func resetUnknownWarned(t *testing.T) {
	t.Helper()
	unknownWarnMu.Lock()
	clear(unknownWarned)
	unknownWarnMu.Unlock()
}

func TestShouldWarnUnknownModelRewarnsAfterTTL(t *testing.T) {
	resetUnknownWarned(t)
	t0 := time.Now()
	if !shouldWarnUnknownModel("m", t0) {
		t.Fatal("first sighting must warn")
	}
	if shouldWarnUnknownModel("m", t0.Add(unknownWarnTTL-time.Second)) {
		t.Fatal("repeat inside the TTL must not warn")
	}
	if !shouldWarnUnknownModel("m", t0.Add(unknownWarnTTL)) {
		t.Fatal("a model still unknown after the TTL must be reported again")
	}
}

func TestShouldWarnUnknownModelIsBounded(t *testing.T) {
	resetUnknownWarned(t)
	now := time.Now()
	for i := 0; i < unknownWarnMax*4; i++ {
		shouldWarnUnknownModel(fmt.Sprintf("model-%d", i), now)
		unknownWarnMu.Lock()
		n := len(unknownWarned)
		unknownWarnMu.Unlock()
		if n > unknownWarnMax {
			t.Fatalf("warned set grew to %d, cap is %d", n, unknownWarnMax)
		}
	}
	// Expired entries are evicted first, so a still-fresh name survives a sweep.
	resetUnknownWarned(t)
	shouldWarnUnknownModel("old", now.Add(-2*unknownWarnTTL))
	for i := 0; i < unknownWarnMax-1; i++ {
		shouldWarnUnknownModel(fmt.Sprintf("fresh-%d", i), now)
	}
	shouldWarnUnknownModel("trigger", now) // at capacity: sweeps "old" only
	unknownWarnMu.Lock()
	_, oldKept := unknownWarned["old"]
	_, freshKept := unknownWarned["fresh-0"]
	unknownWarnMu.Unlock()
	if oldKept || !freshKept {
		t.Fatalf("sweep evicted wrongly: old kept=%v, fresh kept=%v", oldKept, freshKept)
	}
}
