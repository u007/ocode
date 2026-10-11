package agent

import (
	"encoding/json"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestPermissionTablesConcurrentWriteDuringDecide is the regression guard for the
// whole permission table set. Every table below used to be a plain map that an
// HTTP handler, a TUI command handler or a permission continuation mutated while
// a turn goroutine read it inside Decide:
//
//	rules               POST /api/permissions            SetRule
//	userConfirmedRules  permission continuation          SetUserConfirmedRule
//	webfetchDomains     permission continuation          SetWebfetchDomain
//	bashAutoAllow       TUI /permissions                SetBashAutoAllowPrefix
//	bashPrefixModes     TUI /permissions                SetBashPrefixMode
//	patterns            POST /api/permissions (glob)     SetRule
//	pathPatterns        agent permission maps           SetPathRule
//	bashPrefixes        /ban, both HTTP rule paths      SetBashPrefixRule
//
// A concurrent map read/write is a Go runtime FATAL, so this is a process-kill
// bug, not a subtle one. Run with -race.
func TestPermissionTablesConcurrentWriteDuringDecide(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())

	commands := []string{
		`{"command":"sed -n '1,3p' /etc/hosts"}`,
		`{"command":"git status --short"}`,
		`{"command":"git push origin main"}`,
		`{"command":"ls -la /etc/hosts"}`,
		`{"command":"cat /etc/hosts"}`,
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	writers := []struct {
		name string
		fn   func(i int)
	}{
		{"SetRule", func(i int) {
			pm.SetRule([]string{"write", "edit", "glob*", "read"}[i%4], []PermissionLevel{PermissionAllow, PermissionAsk}[i%2])
		}},
		{"SetUserConfirmedRule", func(i int) {
			pm.SetUserConfirmedRule([]string{"write", "read"}[i%2], PermissionAllow)
		}},
		{"SetWebfetchDomain", func(i int) {
			pm.SetWebfetchDomain([]string{"api.example.com", "other.example.org"}[i%2], []PermissionLevel{PermissionAllow, PermissionAsk}[i%2])
		}},
		{"SetBashAutoAllowPrefix", func(i int) {
			pm.SetBashAutoAllowPrefix([]string{"jq", "yq"}[i%2], i%2 == 0)
		}},
		{"SetBashPrefixMode", func(i int) {
			pm.SetBashPrefixMode([]string{"jq", "yq"}[i%2], []string{bashPrefixModeReadOnly, bashPrefixModeMutating, bashPrefixModeNever}[i%3])
		}},
		{"SetBashPrefixRule", func(i int) {
			pm.SetBashPrefixRule([]string{"git push", "sed", "curl"}[i%3], []PermissionLevel{PermissionDeny, PermissionAsk, PermissionAllow}[i%3])
		}},
		{"RemoveBashPrefixRule", func(i int) {
			pm.RemoveBashPrefixRule([]string{"git push", "sed", "curl"}[i%3])
		}},
		{"SetPathRule", func(i int) {
			pm.SetPathRule("edit", []string{"**/*.go", "src/**"}[i%2], PermissionAllow)
		}},
	}

	for w := range writers {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				writers[w].fn(i)
				// Readers of the same tables, on this goroutine, to catch a writer
				// that mutates in place instead of swapping a copy.
				_ = pm.Rules()
				_ = pm.BashPrefixRules()
				_ = pm.BashAutoAllowPrefixes()
				_ = pm.BashPrefixModes()
				_ = pm.BashBannedPrefixes()
				_ = pm.AllowedWebfetchDomains()
				_ = pm.IsUserConfirmedRule("write")
				_ = pm.CheckPathPatterns("edit", "/tmp/x.go")
				_ = pm.ExportConfig()
			}
		}()
	}

	// Decision-path readers.
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
				for _, raw := range commands {
					pm.Decide("bash", json.RawMessage(raw))
				}
				pm.Decide("write", json.RawMessage(`{"file_path":"/tmp/x.go","content":"x"}`))
				pm.Decide("webfetch", json.RawMessage(`{"url":"https://api.example.com/v1"}`))
			}
		}()
	}

	// Bounded by time, not iterations: eight writers spin on the same mutex,
	// so a fixed 400 rounds of Decide waited behind them for ~7 minutes here
	// and once tripped go test's 10-minute package timeout. A short window
	// gives the race detector the same interleavings in well under a second.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, raw := range commands {
			pm.Decide("bash", json.RawMessage(raw))
		}
	}
	close(stop)
	wg.Wait()

	// The tables must still answer coherently afterwards. "grep" is a default
	// allow that no writer above touches. This used to assert on "read", which
	// two writers set to different levels (SetRule → ask, SetUserConfirmedRule →
	// allow), so the result depended on which goroutine wrote last.
	if pm.Check("grep") != PermissionAllow {
		t.Fatalf("grep rule = %q, want the untouched default allow to survive", pm.Check("grep"))
	}
}

// TestRunWithTemporaryUserAllowRestoresBothTables pins the auto-permission
// judge's save/restore: the rule and the user-confirmed flag must both come back
// exactly as they were, whether or not they existed beforehand.
func TestRunWithTemporaryUserAllowRestoresBothTables(t *testing.T) {
	t.Run("restores an existing level and the confirmed flag", func(t *testing.T) {
		pm := NewPermissionManager()
		// SetUserConfirmedRule is SetRule + the confirmed flag, so this leaves
		// level=allow AND confirmed=true — the state a real "always allow" click
		// produces.
		pm.SetUserConfirmedRule("write", PermissionAllow)

		inside := PermissionLevel("")
		if err := pm.RunWithTemporaryUserAllow("write", func() error {
			inside = pm.Check("write")
			if !pm.IsUserConfirmedRule("write") {
				t.Error("tool is not user-confirmed inside the call")
			}
			return nil
		}); err != nil {
			t.Fatalf("RunWithTemporaryUserAllow: %v", err)
		}
		if inside != PermissionAllow {
			t.Fatalf("level inside = %q, want allow", inside)
		}
		if got := pm.Check("write"); got != PermissionAllow {
			t.Fatalf("level after = %q, want the original allow", got)
		}
		if !pm.IsUserConfirmedRule("write") {
			t.Fatal("the user-confirmed flag was not restored")
		}
	})

	t.Run("restores a level that is not allow", func(t *testing.T) {
		pm := NewPermissionManager()
		pm.SetRule("write", PermissionDeny) // exists, never user-confirmed

		if err := pm.RunWithTemporaryUserAllow("write", func() error {
			if pm.Check("write") != PermissionAllow {
				t.Error("the temporary allow was not visible inside the call")
			}
			return nil
		}); err != nil {
			t.Fatalf("RunWithTemporaryUserAllow: %v", err)
		}
		if got := pm.Check("write"); got != PermissionDeny {
			t.Fatalf("level after = %q, want the original deny", got)
		}
		if pm.IsUserConfirmedRule("write") {
			t.Fatal("a confirmed flag leaked onto a tool that never had one")
		}
	})

	t.Run("removes a rule that did not exist before", func(t *testing.T) {
		pm := NewPermissionManager()
		pm.SetRule("glob*", PermissionDeny) // a pattern, not a `rules` entry
		before := len(pm.Rules())

		if err := pm.RunWithTemporaryUserAllow("glob*", func() error { return nil }); err != nil {
			t.Fatalf("RunWithTemporaryUserAllow: %v", err)
		}
		if got := len(pm.Rules()); got != before {
			t.Fatalf("rule count = %d, want %d (the temporary entry leaked)", got, before)
		}
		if _, ok := pm.Rules()["glob*"]; !ok {
			t.Fatal("the original pattern entry was lost")
		}
		if pm.IsUserConfirmedRule("glob*") {
			t.Fatal("a user-confirmed flag leaked for a tool that had none")
		}
	})

	// Catches: a per-call snapshot. Two judge-approved calls for one tool
	// overlap; the second snapshotted the first's temporary allow as its
	// "previous" state and restored it after the first had already left, so the
	// tool stayed user-confirmed for the rest of the session.
	t.Run("overlapping calls do not leak the allow", func(t *testing.T) {
		pm := NewPermissionManager()
		pm.SetRule("webfetch", PermissionAsk)

		aInside, bInside := make(chan struct{}), make(chan struct{})
		releaseA, releaseB := make(chan struct{}), make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = pm.RunWithTemporaryUserAllow("webfetch", func() error {
				close(aInside)
				<-releaseA
				return nil
			})
		}()
		<-aInside
		go func() {
			defer wg.Done()
			_ = pm.RunWithTemporaryUserAllow("webfetch", func() error {
				close(bInside)
				<-releaseB
				return nil
			})
		}()
		<-bInside
		close(releaseA) // A leaves first, while B still holds the allow
		for pm.tempAllowHolders("webfetch") != 1 {
			runtime.Gosched()
		}
		if !pm.IsUserConfirmedRule("webfetch") {
			t.Fatal("the allow was withdrawn while a call still relied on it")
		}
		close(releaseB)
		wg.Wait()

		if got := pm.Check("webfetch"); got != PermissionAsk {
			t.Fatalf("level after = %q, want the original ask", got)
		}
		if pm.IsUserConfirmedRule("webfetch") {
			t.Fatal("the temporary allow leaked into a permanent user-confirmed rule")
		}
	})

	t.Run("propagates the callback error", func(t *testing.T) {
		pm := NewPermissionManager()
		want := error(temporaryAllowErr{})
		if got := pm.RunWithTemporaryUserAllow("write", func() error { return want }); got != want {
			t.Fatalf("error = %v, want %v", got, want)
		}
		// Restored even though the callback failed.
		if pm.IsUserConfirmedRule("write") {
			t.Fatal("state not restored after a failing callback")
		}
	})
}

type temporaryAllowErr struct{}

func (temporaryAllowErr) Error() string { return "boom" }

// TestSetPathRuleDoesNotMutateALiveSnapshot pins the copy inside SetPathRule: the
// path-pattern table is a map of SLICES, so a plain append would write into the
// backing array a concurrent CheckPathPatterns reader is still ranging over.
func TestSetPathRuleDoesNotMutateALiveSnapshot(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetPathRule("edit", "**/*.go", PermissionAllow)
	pm.SetPathRule("edit", "src/**", PermissionAsk)

	held := pm.pathPatterns.load()
	heldEntries := held["edit"]
	before := len(heldEntries)

	pm.SetPathRule("edit", "docs/**", PermissionDeny)

	if len(heldEntries) != before {
		t.Fatalf("a live snapshot grew from %d to %d entries — the inner slice was appended in place", before, len(heldEntries))
	}
	if got := len(pm.pathPatterns.load()["edit"]); got != before+1 {
		t.Fatalf("current table has %d entries, want %d", got, before+1)
	}
}

// tempAllowHolders reports how many RunWithTemporaryUserAllow calls currently
// hold tool's temporary allow.
func (pm *PermissionManager) tempAllowHolders(tool string) int {
	pm.tempAllowMu.Lock()
	defer pm.tempAllowMu.Unlock()
	if ta := pm.tempAllows[tool]; ta != nil {
		return ta.holders
	}
	return 0
}
