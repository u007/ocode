package agent

import (
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// captureAgentDebug redirects the AGENT debug sink for one test and returns a
// getter for the collected lines. t.Cleanup restores the previous sink, so a
// failing assertion cannot leak a global into another test.
func captureAgentDebug(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	prev := DebugAppend
	DebugAppend = func(kind, msg string) {
		if kind == "AGENT" {
			mu.Lock()
			lines = append(lines, msg)
			mu.Unlock()
		}
	}
	t.Cleanup(func() { DebugAppend = prev })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(lines))
		copy(out, lines)
		return out
	}
}

// TestDiscoveryJudgeClient_KeylessDoesNotSpamDebug is the regression guard for
// the reason the discovery judge caches at all. Before the dedup, a keyless user
// got one "no API key ... refusing to build client" line per turn and per
// /discovery status read — for every judge — which buried everything else in the
// debug panel.
//
// The dedup lives in NewClient rather than in a cached nil, because caching a nil
// would pin "no credential" for the life of the session. So this asserts the log
// is quiet on the SOURCE, not that the caller avoided calling.
func TestDiscoveryJudgeClient_KeylessDoesNotSpamDebug(t *testing.T) {
	warnedNoAPIKey.Range(func(k, _ any) bool {
		warnedNoAPIKey.Delete(k)
		return true
	})

	a := &Agent{config: &config.Config{}}
	a.disco = &discoveryState{}

	lines := captureAgentDebug(t)
	const calls = 5
	for i := 0; i < calls; i++ {
		if got := a.discoveryJudgeClient(); got != nil {
			t.Fatalf("call %d: expected nil for an unconfigured provider, got %T", i, got)
		}
	}

	var noKeyLines int
	for _, l := range lines() {
		if strings.Contains(l, "no API key") {
			noKeyLines++
		}
	}
	if noKeyLines != 1 {
		t.Errorf("got %d \"no API key\" debug lines across %d calls, want exactly 1\nlines: %v",
			noKeyLines, calls, lines())
	}
}

// TestDiscoveryJudgeClient_NilIsNotCached pins the /connect liveness property.
// The old implementation memoised with a sync.Once, so a nil resolution was
// pinned for the whole session and /connect typesafe only took effect after a
// /discovery toggle or a restart. A keyless call must leave nothing behind that a
// later credentialed call could read.
func TestDiscoveryJudgeClient_NilIsNotCached(t *testing.T) {
	warnedNoAPIKey.Range(func(k, _ any) bool {
		warnedNoAPIKey.Delete(k)
		return true
	})

	a := &Agent{config: &config.Config{}}
	a.disco = &discoveryState{}

	if got := a.discoveryJudgeClient(); got != nil {
		t.Fatalf("expected nil before any credential, got %T", got)
	}
	if a.disco.judge != nil {
		t.Error("a nil client was cached; /connect could not go live without a reset")
	}

	// Simulate the credential arriving mid-session: a config that now carries a
	// key for the slot's provider. The NEXT call must see it.
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Cleanup(func() { warnedNoAPIKey.Delete("typesafe\x00typesafe/jev-latest") })
	warnedNoAPIKey.Delete("typesafe\x00typesafe/jev-latest")

	got := a.discoveryJudgeClient()
	if got == nil {
		t.Fatal("still nil after a credential appeared mid-session — nil was pinned")
	}
	if lbl := deciderLabel(got); !strings.HasPrefix(lbl, "typesafe/") {
		t.Errorf("label = %q, want a typesafe/ prefix", lbl)
	}
}

// TestDeciderLabel_UsesClientProvider pins the attribution fix. The judges used
// to read the concrete TypesafeClient's Model field and hardcode a "typesafe/"
// prefix, which books every decision backend's tokens to TypeSafe in the usage
// ledger as soon as a second backend exists.
func TestDeciderLabel_UsesClientProvider(t *testing.T) {
	ts := newTypesafeClient("k", "jev-1.13.0", "https://api.typesafe.ai/v1")
	if got, want := deciderLabel(ts), "typesafe/jev-1.13.0"; got != want {
		t.Errorf("deciderLabel(typesafe) = %q, want %q", got, want)
	}
	if got := deciderLabel(nil); got != "" {
		t.Errorf("deciderLabel(nil) = %q, want empty", got)
	}
}
