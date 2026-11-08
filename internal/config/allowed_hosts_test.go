package config

import (
	"reflect"
	"testing"
)

// TestAllowedHostsRoundTrip proves permissions.allowed_hosts survives a
// save/load cycle, is normalised on save, and that saving an empty list
// clears it on disk.
func TestAllowedHostsRoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	setHomeTree(t, tmpHome)
	chdirTempForConfigTest(t)

	if err := SaveOcodeAllowedHosts([]string{"  API.example.com ", "*.Example.org", "api.example.com", "", "a.test"}); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	want := []string{"*.example.org", "a.test", "api.example.com"}
	if got := cfg.Ocode.Permissions.AllowedHosts; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed_hosts round trip: got %#v want %#v", got, want)
	}

	if err := SaveOcodeAllowedHosts(nil); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	var cleared Config
	if err := LoadOcodeConfig(&cleared); err != nil {
		t.Fatalf("load after clear failed: %v", err)
	}
	if got := cleared.Ocode.Permissions.AllowedHosts; len(got) != 0 {
		t.Fatalf("empty save did not clear allowed_hosts: %#v", got)
	}
}

// TestAllowedHostsSaveLeavesOtherPermissionsAlone pins that the targeted saver
// touches only allowed_hosts: the mode, tool rules, bash prefixes and the auto
// block written before it must all survive.
func TestAllowedHostsSaveLeavesOtherPermissionsAlone(t *testing.T) {
	tmpHome := t.TempDir()
	setHomeTree(t, tmpHome)
	chdirTempForConfigTest(t)

	if err := SavePermissionModeSwitch("locked"); err != nil {
		t.Fatalf("mode save failed: %v", err)
	}
	if err := SaveSingleToolRule("webfetch", "allow"); err != nil {
		t.Fatalf("tool rule save failed: %v", err)
	}
	if err := SaveOcodeAutoPermissionConfig(AutoPermissionConfig{
		Enabled:         true,
		RelaxedConcerns: []string{"network"},
	}); err != nil {
		t.Fatalf("auto save failed: %v", err)
	}

	if err := SaveOcodeAllowedHosts([]string{"docs.example.com"}); err != nil {
		t.Fatalf("allowed hosts save failed: %v", err)
	}

	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	p := cfg.Ocode.Permissions
	if p.Mode != "locked" {
		t.Errorf("mode clobbered: got %q", p.Mode)
	}
	if p.Tools["webfetch"] != "allow" {
		t.Errorf("tool rule clobbered: got %#v", p.Tools)
	}
	if p.Auto == nil || !p.Auto.Enabled || !p.Auto.ConcernRelaxed("network") {
		t.Errorf("auto block clobbered: %#v", p.Auto)
	}
	if want := []string{"docs.example.com"}; !reflect.DeepEqual(p.AllowedHosts, want) {
		t.Errorf("allowed_hosts: got %#v want %#v", p.AllowedHosts, want)
	}
}
