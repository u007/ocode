package agent

import (
	"sync"
	"testing"
)

// TestCowMapConcurrentWriteDuringRead is the guard for the primitive every
// permission table now sits on. Before it, each of those tables was a plain map
// written by a handler goroutine while a turn goroutine read it inside Decide —
// a Go runtime fatal ("concurrent map read and map write"), which a race
// detector reports as a data race but which in production kills the process.
// Run with -race.
func TestCowMapConcurrentWriteDuringRead(t *testing.T) {
	var c cowMap[string, int]
	c.set(map[string]int{"seed": 1})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				key := []string{"a", "b", "c"}[(i+w)%3]
				c.mutate(func(m map[string]int) { m[key] = i })
				c.mutate(func(m map[string]int) { delete(m, key) })
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap := c.load()
				total := 0
				for _, v := range snap {
					total += v
				}
				_, _ = c.get("a")
			}
		}()
	}
	for i := 0; i < 2000; i++ {
		c.mutate(func(m map[string]int) { m["seed"] = i })
		_ = len(c.load())
	}
	close(stop)
	wg.Wait()

	if _, ok := c.get("seed"); !ok {
		t.Fatal("seed key vanished")
	}
}

// TestCowMapMutateNeverMutatesALiveSnapshot is the subtlety a map-of-slices
// depends on: a reader may hold a snapshot for as long as it likes, so a writer
// cloning and mutating in place would corrupt a range the reader is still doing.
func TestCowMapMutateNeverMutatesALiveSnapshot(t *testing.T) {
	var c cowMap[string, []string]
	c.set(map[string][]string{"tool": {"a"}})

	held := c.load()
	heldList := held["tool"]

	c.mutate(func(m map[string][]string) { m["tool"] = append(m["tool"], "b") })

	if len(heldList) != 1 || heldList[0] != "a" {
		t.Fatalf("a held snapshot changed under the writer: %#v", held)
	}
	if got := c.load()["tool"]; len(got) != 2 {
		t.Fatalf("new version = %#v, want 2 entries", got)
	}
	// A writer that appends to a slice with spare capacity would write into the
	// array the previous version still points at. That is why SetPathRule copies
	// its inner slice first — the guard itself is pinned by
	// TestSetPathRuleDoesNotMutateALiveSnapshot.
	spare := []string{"a", "b", "c", "d"}[:2:4] // len 2, cap 4
	var c2 cowMap[string, []string]
	c2.set(map[string][]string{"tool": spare})
	c2.mutate(func(m map[string][]string) {
		// The hazard: no inner copy, so this append lands in the shared array.
		m["tool"] = append(m["tool"], "c")
	})
	if len(c2.load()["tool"]) != 3 {
		t.Fatalf("version = %#v, want 3 entries", c2.load()["tool"])
	}
}

// TestCowMapConcurrentWritersDoNotLoseEachOther: mutate serialises writers, so
// two independent keys written in parallel both survive.
func TestCowMapConcurrentWritersDoNotLoseEachOther(t *testing.T) {
	var c cowMap[string, int]
	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			c.mutate(func(m map[string]int) { m[string(rune('a'+i))] = i })
		}(i)
	}
	wg.Wait()
	if got := len(c.load()); got != n {
		t.Fatalf("len = %d, want %d (a writer's change was lost)", got, n)
	}
}

// TestCowSliceAppendDoesNotMutateLiveSnapshot: cowSlice exists for the
// append-in-place pattern (tool glob patterns); an append must always land in a
// fresh array.
func TestCowSliceAppendDoesNotMutateLiveSnapshot(t *testing.T) {
	var s cowSlice[patternRule]
	s.mutate(func(cur []patternRule) []patternRule {
		return append(cur, patternRule{pattern: "a*", level: PermissionAllow})
	})

	held := s.load()
	if len(held) != 1 {
		t.Fatalf("initial = %d entries, want 1", len(held))
	}
	capBefore := cap(held)

	s.mutate(func(cur []patternRule) []patternRule {
		return append(cur, patternRule{pattern: "b*", level: PermissionDeny})
	})

	if len(s.load()) != 2 {
		t.Fatalf("after append = %d entries, want 2", len(s.load()))
	}
	if len(held) != 1 {
		t.Fatalf("a live snapshot grew to %d entries", len(held))
	}
	if cap(held) != capBefore {
		t.Fatalf("published slice had spare capacity (%d → %d), so the next append would write into an array a live reader may hold", capBefore, cap(held))
	}
}

// TestCowMapZeroValueIsSafe: a PermissionManager built without the constructor
// must not panic on a read.
func TestCowMapZeroValueIsSafe(t *testing.T) {
	var c cowMap[string, int]
	if got := len(c.load()); got != 0 {
		t.Fatalf("len = %d, want 0", got)
	}
	if _, ok := c.get("missing"); ok {
		t.Fatal("zero-value cowMap reported a key")
	}
	c.mutate(func(m map[string]int) { m["x"] = 1 })
	if v, ok := c.get("x"); !ok || v != 1 {
		t.Fatalf("after mutate: %v %v, want 1 true", v, ok)
	}
}
