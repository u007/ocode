package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestDeleteBashPrefixRuleRemovesOnlyTheNamedEntry pins the delete contract the
// Settings rule editor depends on: removing one prefix deletes THAT key (it is
// not downgraded to "ask") and leaves every sibling rule and permissions field
// untouched on disk.
func TestDeleteBashPrefixRuleRemovesOnlyTheNamedEntry(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := SaveSingleBashPrefixRule("git push", "deny"); err != nil {
		t.Fatalf("SaveSingleBashPrefixRule(git push) error = %v", err)
	}
	if err := SaveSingleBashPrefixRule("git status", "allow"); err != nil {
		t.Fatalf("SaveSingleBashPrefixRule(git status) error = %v", err)
	}
	if err := SaveSingleToolRule("bash", "ask"); err != nil {
		t.Fatalf("SaveSingleToolRule error = %v", err)
	}

	if err := DeleteBashPrefixRule("git push"); err != nil {
		t.Fatalf("DeleteBashPrefixRule error = %v", err)
	}

	cfg, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() error = %v", err)
	}
	if _, still := cfg.Permissions.Bash.Prefixes["git push"]; still {
		t.Fatalf("git push survived the delete: %#v", cfg.Permissions.Bash.Prefixes)
	}
	if got := cfg.Permissions.Bash.Prefixes["git status"]; got != "allow" {
		t.Fatalf("sibling rule = %q, want allow (delete must not touch others)", got)
	}
	if got := cfg.Permissions.Tools["bash"]; got != "ask" {
		t.Fatalf("permissions.tools.bash = %q, want ask (unrelated field must survive)", got)
	}
}

// TestDeleteBashPrefixRuleMissingKeyIsNoOp: Remove is idempotent — the editor
// can send a delete for a rule another surface already deleted without the
// write failing.
func TestDeleteBashPrefixRuleMissingKeyIsNoOp(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := DeleteBashPrefixRule("never-existed"); err != nil {
		t.Fatalf("deleting an absent prefix = %v, want no error", err)
	}
	if err := DeleteBashPrefixRule("  "); err != nil {
		t.Fatalf("deleting a blank prefix = %v, want no error", err)
	}
	if err := DeleteBashPrefixRule("never-existed"); err != nil {
		t.Fatalf("second delete = %v, want no error", err)
	}
}

// TestDeleteBashPrefixRuleConcurrentWritersAllLand guards the load-modify-write
// path: parallel writers of DIFFERENT prefixes must not clobber each other (a
// naive read-modify-write of the whole map would drop all but the last).
func TestDeleteBashPrefixRuleConcurrentWritersAllLand(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			prefix := "prefix" + string(rune('a'+i))
			if err := SaveSingleBashPrefixRule(prefix, "deny"); err != nil {
				t.Errorf("SaveSingleBashPrefixRule(%s) error = %v", prefix, err)
			}
		}(i)
	}
	wg.Wait()

	cfg, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() error = %v", err)
	}
	if len(cfg.Permissions.Bash.Prefixes) != n {
		t.Fatalf("prefixes = %#v, want %d concurrent writes all present", cfg.Permissions.Bash.Prefixes, n)
	}

	// Now delete them all concurrently and assert the map ends up empty rather
	// than partially deleted.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if err := DeleteBashPrefixRule("prefix" + string(rune('a'+i))); err != nil {
				t.Errorf("DeleteBashPrefixRule error = %v", err)
			}
		}(i)
	}
	wg.Wait()

	cfg, err = LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy() after deletes error = %v", err)
	}
	if len(cfg.Permissions.Bash.Prefixes) != 0 {
		t.Fatalf("prefixes = %#v, want empty after deleting every key", cfg.Permissions.Bash.Prefixes)
	}
}

// TestSaveSingleBashPrefixRuleRoundTripsThroughDisk guards against a saver that
// reports success while writing nothing (the silent-discard bug class this
// feature's delete path sits next to): the key must exist in the FILE, not just
// in memory.
func TestSaveSingleBashPrefixRuleRoundTripsThroughDisk(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := SaveSingleBashPrefixRule("sed", "deny"); err != nil {
		t.Fatalf("SaveSingleBashPrefixRule error = %v", err)
	}
	path, err := ActiveOcodeConfigPath()
	if err != nil {
		t.Fatalf("ActiveOcodeConfigPath() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), `"sed"`) {
		t.Fatalf("config file does not contain the sed rule:\n%s", raw)
	}
}
