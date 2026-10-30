package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFillerCleanupEnabledDefaultsOn(t *testing.T) {
	// An install that has never written the setting must still get the
	// cleanup: the pointer exists so the zero value cannot mean "off".
	if !(STTConfig{}).FillerCleanupEnabled() {
		t.Fatal("unset remove_fillers should mean cleanup on")
	}
	off := false
	if (STTConfig{RemoveFillers: &off}).FillerCleanupEnabled() {
		t.Fatal("remove_fillers=false should disable cleanup")
	}
	on := true
	if !(STTConfig{RemoveFillers: &on}).FillerCleanupEnabled() {
		t.Fatal("remove_fillers=true should enable cleanup")
	}
}

func TestSaveOcodeSTTConfigRoundTripsFillerSetting(t *testing.T) {
	chdirTempForConfigTest(t)
	home := t.TempDir()
	setHomeTree(t, home)

	// An unset flag writes no key, so upgrading an install does not change the
	// meaning of its config file.
	if err := SaveOcodeSTTConfig(STTConfig{Model: "whisper-1"}); err != nil {
		t.Fatalf("SaveOcodeSTTConfig(default) failed: %v", err)
	}
	path := filepath.Join(home, ".config", "opencode", "ocodeconfig.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse config failed: %v", err)
	}
	if stt, ok := raw["stt"].(map[string]any); ok {
		if _, present := stt["remove_fillers"]; present {
			t.Fatalf("unset remove_fillers was written: %s", data)
		}
	}

	off := false
	if err := SaveOcodeSTTConfig(STTConfig{Model: "whisper-1", RemoveFillers: &off}); err != nil {
		t.Fatalf("SaveOcodeSTTConfig(off) failed: %v", err)
	}
	loaded, err := LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy failed: %v", err)
	}
	if loaded.STT.Model != "whisper-1" {
		t.Fatalf("loaded model = %q, want whisper-1", loaded.STT.Model)
	}
	if loaded.STT.FillerCleanupEnabled() {
		t.Fatal("loaded remove_fillers=false should disable cleanup")
	}
}
