package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/sysperm"
)

func TestSystemPermissionsConfigRoundTrip(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Seed an unrelated field so we can prove the targeted save preserves it.
	seed := defaultOcodeConfig()
	seed.Ocr.Enabled = true
	if err := SaveOcodeConfig(&seed); err != nil {
		t.Fatalf("SaveOcodeConfig: %v", err)
	}

	cfg := sysperm.DefaultConfig()
	cfg.Set("macos.files.documents", sysperm.EntryConfig{Enabled: true, Requested: true})
	cfg.Set(sysperm.PathID("/tmp/proj"), sysperm.EntryConfig{Enabled: true, Path: "/tmp/proj", Label: "proj"})
	if err := SaveSystemPermissionsConfig(cfg); err != nil {
		t.Fatalf("SaveSystemPermissionsConfig: %v", err)
	}

	// The raw key lands on disk.
	configPath := filepath.Join(tmp, ".config", "opencode", "ocodeconfig.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "system_permissions") {
		t.Fatalf("system_permissions key missing from disk config")
	}

	// And reloads with the unrelated field intact.
	got, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if !got.Ocr.Enabled {
		t.Fatal("targeted save dropped an unrelated field (ocr.enabled)")
	}
	if ec := got.SystemPermissions.Get("macos.files.documents"); !ec.Enabled || !ec.Requested {
		t.Fatalf("builtin entry after reload = %+v", ec)
	}
	if ec := got.SystemPermissions.Get(sysperm.PathID("/tmp/proj")); !ec.Enabled || ec.Path != "/tmp/proj" || ec.Label != "proj" {
		t.Fatalf("path entry after reload = %+v", ec)
	}
}

func TestDefaultOcodeConfigHasEmptySystemPermissions(t *testing.T) {
	cfg := defaultOcodeConfig()
	if cfg.SystemPermissions.Entries == nil {
		t.Fatal("default SystemPermissions.Entries is nil; want an initialized empty map")
	}
	if len(cfg.SystemPermissions.Entries) != 0 {
		t.Fatalf("default SystemPermissions.Entries = %v, want empty", cfg.SystemPermissions.Entries)
	}
}
