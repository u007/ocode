package config

import (
	"testing"
)

// TestRelaxedConcernsRoundTrip proves the negative enforcement set survives a
// save/load cycle through the pointer-mirror file struct, and that the loader
// REPLACES rather than merges it (re-ticking a box in the UI writes the full
// remaining opt-out list, so a merge would resurrect the unticked category).
func TestRelaxedConcernsRoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	chdirTempForConfigTest(t)

	if err := SaveOcodeAutoPermissionConfig(AutoPermissionConfig{
		Enabled:         true,
		RelaxedConcerns: []string{"secrets", "network"},
	}); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	auto := cfg.Ocode.Permissions.Auto
	if auto == nil {
		t.Fatal("auto block missing after round trip")
	}
	if len(auto.RelaxedConcerns) != 2 || auto.RelaxedConcerns[0] != "secrets" || auto.RelaxedConcerns[1] != "network" {
		t.Fatalf("relaxed_concerns round trip: got %#v", auto.RelaxedConcerns)
	}
	if !auto.ConcernRelaxed("secrets") || auto.ConcernRelaxed("destructive") {
		t.Fatalf("ConcernRelaxed mismatch: %#v", auto.RelaxedConcerns)
	}

	// Shrinking the set must shrink it on disk too (replace, not merge).
	if err := SaveOcodeAutoPermissionConfig(AutoPermissionConfig{
		Enabled:         true,
		RelaxedConcerns: []string{"network"},
	}); err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	var cfg2 Config
	if err := LoadOcodeConfig(&cfg2); err != nil {
		t.Fatalf("second load failed: %v", err)
	}
	got := cfg2.Ocode.Permissions.Auto.RelaxedConcerns
	if len(got) != 1 || got[0] != "network" {
		t.Fatalf("relaxed_concerns did not replace on save: %#v", got)
	}
}

// TestApplyAutoPermissionConfigReplacesRelaxedConcerns pins the replace (not
// merge) semantics of the file→runtime mapping. The round-trip test cannot see
// the difference — a load always starts from defaults — so it has to be pinned
// where the mapping actually happens: a stale key must not survive a save that
// dropped it.
func TestApplyAutoPermissionConfigReplacesRelaxedConcerns(t *testing.T) {
	dst := &AutoPermissionConfig{Enabled: true, RelaxedConcerns: []string{"secrets", "network"}}
	applyAutoPermissionConfig(dst, &autoPermissionConfigFile{
		RelaxedConcerns: []string{"network"},
	})
	if len(dst.RelaxedConcerns) != 1 || dst.RelaxedConcerns[0] != "network" {
		t.Fatalf("relaxed_concerns must replace, got %#v", dst.RelaxedConcerns)
	}

	// An absent field (nil) leaves the in-memory set alone — the file is a
	// complete snapshot, but "field missing" is not "empty list".
	applyAutoPermissionConfig(dst, &autoPermissionConfigFile{})
	if len(dst.RelaxedConcerns) != 1 {
		t.Fatalf("nil field must not clear the set, got %#v", dst.RelaxedConcerns)
	}

	// An explicit empty list clears it.
	empty := []string{}
	applyAutoPermissionConfig(dst, &autoPermissionConfigFile{RelaxedConcerns: empty})
	if len(dst.RelaxedConcerns) != 0 {
		t.Fatalf("explicit empty list must clear the set, got %#v", dst.RelaxedConcerns)
	}
}

// TestConcernRelaxedNilSafe pins the nil-receiver contract: both judge paths read
// this before any nil check of their own.
func TestConcernRelaxedNilSafe(t *testing.T) {
	var auto *AutoPermissionConfig
	if auto.ConcernRelaxed("secrets") {
		t.Fatal("nil config must report nothing relaxed")
	}
	if (&AutoPermissionConfig{}).ConcernRelaxed("secrets") {
		t.Fatal("empty config must report nothing relaxed")
	}
	if (&AutoPermissionConfig{}).ConcernRelaxed("") {
		t.Fatal("empty key must never match")
	}
}
