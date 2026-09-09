package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserConfigHTRFields(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initial := `{"browser":{"htr_enabled":true,"htr_extension_path":"/x/ext","htrcli_path":"/x/htrcli","htr_port":4845,"htr_socket_path":"/x/socket","htr_native_host_name":"com.ocode.test.htr"}}`
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	b := cfg.Ocode.Browser
	if !b.HTREnabled {
		t.Fatal("HTREnabled = false, want true")
	}
	if b.HTRExtensionPath != "/x/ext" {
		t.Fatalf("HTRExtensionPath = %q, want /x/ext", b.HTRExtensionPath)
	}
	if b.HTRCliPath != "/x/htrcli" {
		t.Fatalf("HTRCliPath = %q, want /x/htrcli", b.HTRCliPath)
	}
	if b.HTRPort != 4845 {
		t.Fatalf("HTRPort = %d, want 4845", b.HTRPort)
	}
	if b.HTRSocketPath != "/x/socket" || b.HTRNativeHostName != "com.ocode.test.htr" {
		t.Fatalf("runtime overrides = (%q, %q)", b.HTRSocketPath, b.HTRNativeHostName)
	}
	if _, ok := cfg.Ocode.Extra["browser"]; ok {
		t.Fatalf("browser leaked into Extra: %v", cfg.Ocode.Extra["browser"])
	}
}

func TestBrowserConfigHTRPortRejected(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(`{"browser":{"htr_port":99999}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err == nil {
		t.Fatal("expected error for htr_port 99999")
	}
}

func TestBrowserConfigRejectsStandaloneNativeHostName(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(`{"browser":{"htr_native_host_name":"com.htrcontrol.host"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err == nil {
		t.Fatal("expected standalone native host name to be rejected")
	}
}

func TestBrowserConfigHTRDefaultsAbsent(t *testing.T) {
	chdirTempForConfigTest(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	configDir := filepath.Join(tmp, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ocodeconfig.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if err := LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("LoadOcodeConfig: %v", err)
	}
	b := cfg.Ocode.Browser
	if !b.HTREnabled {
		t.Fatal("default HTREnabled must be true for managed Chrome")
	}
	if b.HTRPort != 3846 {
		t.Fatalf("default HTRPort = %d, want 3846", b.HTRPort)
	}
}
