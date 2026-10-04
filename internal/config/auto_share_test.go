package config

import (
	"encoding/json"
	"os"
	"testing"
)

// TestAutoShareOnStartDefaultsOff pins the safe default: a fresh install must
// NOT auto-publish the instance. This is the whole point of the flag being
// opt-in, so the default is asserted explicitly rather than trusted to Go's
// zero value.
func TestAutoShareOnStartDefaultsOff(t *testing.T) {
	var cfg OcodeConfig
	if err := loadOcodeConfigFile(writeAbsentConfigPath(t), &cfg); err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	if cfg.AutoShareOnStart {
		t.Fatal("AutoShareOnStart defaulted to true; auto-share must be opt-in")
	}
}

// TestSaveAutoShareOnStartRoundTrips is the regression guard for the explicit
// `payload[...]` entry in writeOcodeConfigFile. That map is built by hand, so a
// resolved struct field that is never added to it silently fails to persist —
// the save "succeeds" and the flag reverts to false on next launch, with no
// error anywhere. Reading the raw JSON back (rather than trusting the in-memory
// struct) is what makes this test able to catch that class of bug.
func TestSaveAutoShareOnStartRoundTrips(t *testing.T) {
	for _, want := range []bool{true, false} {
		if err := SaveAutoShareOnStart(want); err != nil {
			t.Fatalf("SaveAutoShareOnStart(%v): %v", want, err)
		}

		path, err := getGlobalOcodeConfigPath()
		if err != nil {
			t.Fatalf("config path: %v", err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read config file: %v", err)
		}
		var onDisk map[string]any
		if err := json.Unmarshal(raw, &onDisk); err != nil {
			t.Fatalf("parse config file: %v", err)
		}
		got, ok := onDisk["auto_share_on_start"]
		if !ok {
			t.Fatalf("auto_share_on_start missing from persisted config after saving %v; keys=%v", want, keysOf(onDisk))
		}
		if got != want {
			t.Fatalf("persisted auto_share_on_start = %v, want %v", got, want)
		}

		// And it must survive a real reload, not just the raw key check.
		fresh, err := LoadOcodeConfigCopy()
		if err != nil {
			t.Fatalf("reload config: %v", err)
		}
		if fresh.AutoShareOnStart != want {
			t.Fatalf("reloaded AutoShareOnStart = %v, want %v", fresh.AutoShareOnStart, want)
		}
	}
}

// writeAbsentConfigPath returns a path inside the test temp dir that does not
// exist, so loadOcodeConfigFile exercises the "no file yet" branch.
func writeAbsentConfigPath(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/absent-ocodeconfig.json"
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}