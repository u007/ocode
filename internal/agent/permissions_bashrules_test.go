package agent

import (
	"encoding/json"
	"sync"
	"testing"
)

// TestRemoveBashPrefixRuleDeletesInsteadOfDowngrading pins the semantic the
// Settings Remove button depends on: the rule disappears from the manager (it is
// NOT left behind as an "ask" rule, which would still show up in /permissions
// and /ban list output as cruft).
func TestRemoveBashPrefixRuleDeletesInsteadOfDowngrading(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetBashPrefixRule("sed", PermissionDeny)
	pm.SetBashPrefixRule("rm -rf", PermissionDeny)

	if !pm.RemoveBashPrefixRule("sed") {
		t.Fatal("RemoveBashPrefixRule(sed) = false, want true (the rule existed)")
	}
	if _, still := pm.BashPrefixRules()["sed"]; still {
		t.Fatalf("sed rule survived removal: %#v", pm.BashPrefixRules())
	}
	if got := pm.BashPrefixRules()["rm -rf"]; got != PermissionDeny {
		t.Fatalf("sibling rule = %q, want deny (remove must not touch others)", got)
	}
	if banned := pm.BashBannedPrefixes(); len(banned) != 1 || banned[0] != "rm -rf" {
		t.Fatalf("banned = %#v, want only rm -rf", banned)
	}
	// And the command is decided as if the rule never existed.
	if dec := pm.Decide("bash", json.RawMessage(`{"command":"sed -n '1,3p' /etc/hosts"}`)); dec.Level == PermissionDeny {
		t.Fatalf("sed still denied after removal: %s", dec.Level)
	}
}

// TestRemoveBashPrefixRuleIdempotentAndGuarded: removing an absent rule, a
// blank prefix or an internal in-root key is a no-op that reports false, so the
// editor's Remove cannot wipe a machine-written key or fail on a rule another
// surface already deleted.
func TestRemoveBashPrefixRuleIdempotentAndGuarded(t *testing.T) {
	pm := NewPermissionManager()
	if pm.RemoveBashPrefixRule("never-set") {
		t.Fatal("RemoveBashPrefixRule(absent) = true, want false")
	}
	if pm.RemoveBashPrefixRule("") {
		t.Fatal("RemoveBashPrefixRule(blank) = true, want false")
	}
	pm.SetBashPrefixRule("awk", PermissionAllow)
	inRoot := bashInRootKey("awk", t.TempDir())
	pm.mutateBashPrefixes(func(rules map[string]PermissionLevel) { rules[inRoot] = PermissionAllow })
	if pm.RemoveBashPrefixRule(inRoot) {
		t.Fatal("RemoveBashPrefixRule(internal in-root key) = true, want false")
	}
	if _, ok := pm.bashPrefixSnapshot()[inRoot]; !ok {
		t.Fatal("remove deleted an internal in-root key")
	}
}

// TestValidateBashPrefixRuleRejectsWhatSetBashPrefixRuleSilentlyDrops pins the
// silent-discard bug this feature had to close: the setter takes no return
// value, so an HTTP write that skipped validation returned 200 while the rule
// was never stored. Every write path must be able to report the rejection.
func TestValidateBashPrefixRuleRejectsWhatSetBashPrefixRuleSilentlyDrops(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		level  PermissionLevel
	}{
		{"empty prefix", "", PermissionDeny},
		{"blank prefix", "   ", PermissionDeny},
		{"unknown level", "sed", PermissionLevel("maybe")},
		{"blanket git allow", "git", PermissionAllow},
		{"reserved in-root key", bashInRootPersistPrefix + "cat:/tmp", PermissionAllow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateBashPrefixRule(tc.prefix, tc.level); err == nil {
				t.Fatalf("ValidateBashPrefixRule(%q, %q) = nil, want an error", tc.prefix, tc.level)
			}
		})
	}

	ok := []struct {
		prefix string
		level  PermissionLevel
	}{
		{"git push", PermissionDeny},
		{"git status", PermissionAllow},
		{"  git push  ", PermissionAsk},
		{"git", PermissionDeny},
	}
	for _, tc := range ok {
		if err := ValidateBashPrefixRule(tc.prefix, tc.level); err != nil {
			t.Fatalf("ValidateBashPrefixRule(%q, %q) = %v, want nil", tc.prefix, tc.level, err)
		}
	}

	// The setter must agree with the validator: nothing the validator rejects
	// may reach the rule map.
	pm := NewPermissionManager()
	for _, tc := range cases {
		pm.SetBashPrefixRule(tc.prefix, tc.level)
	}
	if got := len(pm.BashPrefixRules()); got != 0 {
		t.Fatalf("rejected rules were stored anyway: %#v", pm.BashPrefixRules())
	}
	// And SetBashPrefixRule trims, so a rule the validator accepted on its
	// trimmed form is stored under the trimmed key (a " git push" entry would
	// never match a real command).
	pm.SetBashPrefixRule("  git push  ", PermissionDeny)
	if _, ok := pm.BashPrefixRules()["git push"]; !ok {
		t.Fatalf("trimmed prefix was not stored under its trimmed key: %#v", pm.BashPrefixRules())
	}
}

// TestBashPrefixRulesConcurrentWriteDuringDecide is the regression guard for
// the copy-on-write rule store. Before it, bashPrefixes was a plain map that
// the Settings UI (PUT /api/permissions/bash-rules), POST
// /api/permissions/bash-rule and the TUI /ban handler all wrote while a turn
// goroutine read it inside Decide — a concurrent map read/write, which is a Go
// runtime FATAL, not just a race-detector warning. Run with -race.
func TestBashPrefixRulesConcurrentWriteDuringDecide(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetWorkDir(t.TempDir())

	commands := []string{
		`{"command":"sed -n '1,3p' /etc/hosts"}`,
		`{"command":"git status --short"}`,
		`{"command":"git push origin main"}`,
		`{"command":"ls -la /etc/hosts"}`,
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writers: add, remove and re-add while decisions run.
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
				prefix := []string{"sed", "git status", "git push", "ls"}[i%4]
				pm.SetBashPrefixRule(prefix, []PermissionLevel{PermissionDeny, PermissionAsk, PermissionAllow}[i%3])
				pm.RemoveBashPrefixRule(prefix)
				_ = pm.BashBannedPrefixes()
				_ = pm.BashPrefixRules()
			}
		}(w)
	}

	// Readers: the decision path itself.
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
			}
		}()
	}

	// Let the goroutines interleave, then stop and join.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			for _, raw := range commands {
				pm.Decide("bash", json.RawMessage(raw))
			}
		}
	}()
	<-done
	close(stop)
	wg.Wait()

	// The store must still be coherent (no torn/lost writes) after all of that.
	if _, ok := pm.bashPrefixSnapshot()["git push"]; ok {
		t.Log("git push present at the end (timing-dependent); map remains readable")
	}
}

// TestCloneBashPrefixRulesAreIndependent: Clone must give the copy its own
// rule map. Sharing one map would mean two managers mutating the same map —
// precisely what the copy-on-write swap exists to prevent.
func TestCloneBashPrefixRulesAreIndependent(t *testing.T) {
	pm := NewPermissionManager()
	pm.SetBashPrefixRule("sed", PermissionDeny)

	clone := pm.Clone()
	clone.SetBashPrefixRule("awk", PermissionAllow)
	pm.RemoveBashPrefixRule("sed")

	// Independence is one-directional by design: Clone is a POINT-IN-TIME
	// snapshot, so the clone keeps the rules it was built with even after the
	// original changes, and the clone's own writes never reach the original.
	if _, ok := pm.BashPrefixRules()["awk"]; ok {
		t.Fatal("clone's write leaked into the original")
	}
	if got := clone.BashPrefixRules()["awk"]; got != PermissionAllow {
		t.Fatalf("clone rule = %q, want allow", got)
	}
	if got := clone.BashPrefixRules()["sed"]; got != PermissionDeny {
		t.Fatalf("clone sed = %q, want the snapshot value deny", got)
	}
	if _, ok := pm.BashPrefixRules()["sed"]; ok {
		t.Fatal("original's delete did not apply to the original")
	}
}
