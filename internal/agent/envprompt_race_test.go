package agent

import (
	"sync"
	"testing"
)

// TestPrepareMessagesConcurrentWithCacheInvalidation is the regression guard for
// the unsynchronized env-prompt cache in (*Agent) — see TODO.md "Fix the
// CONFIRMED data race in Agent.SetWorkDir".
//
// Reachability: `MaybeCompactAsync` -> `startCompactAsync` spawns a
// crashguard.Go goroutine (agent.go:2315) that reaches
// `runInlineSummary` -> `a.PrepareMessages(messages, "")` (compact.go:1298),
// while the turn goroutine is in `Step` -> `PrepareMessages` ->
// `environmentPrompt()`. Nothing serializes them: `compactMu` is documented as
// serializing async compaction passes against EACH OTHER (agent.go:726), and
// neither `Step` nor `PrepareMessages` takes it.
//
// The test asserts nothing on purpose — `go test -race` IS the assertion. It
// reports a data race before the fix and is silent after, which is what makes it
// a regression guard rather than a probe that must be deleted once fixed.
func TestPrepareMessagesConcurrentWithCacheInvalidation(t *testing.T) {
	a := &Agent{}

	msgs := []Message{{Role: "user", Content: "hello"}}

	const readers = 4
	var wg sync.WaitGroup
	start := make(chan struct{})

	// The turn/compaction side: PrepareMessages reads a.envPromptDate and calls
	// environmentPrompt(), which reads AND writes the whole envPrompt* cluster.
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := 0; n < 40; n++ {
				_ = a.PrepareMessages(msgs, "")
			}
		}()
	}

	// The invalidation side: SetWorkDir/SetProjectHost -> clearEnvironmentPromptCache,
	// and SetWorkDir also writes a.workDir which environmentPrompt() reads.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for n := 0; n < 40; n++ {
			a.clearEnvironmentPromptCache()
			a.SetWorkDir("/tmp/race-regression")
		}
	}()

	close(start)
	wg.Wait()
}
