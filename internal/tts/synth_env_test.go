package tts

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestORTTelemetryDisabledEnvDropsInheritedValue(t *testing.T) {
	got := ortTelemetryDisabledEnv([]string{"PATH=/bin", "ORT_DISABLE_TELEMETRY=0", "HOME=/home/x"})
	seen := 0
	for _, kv := range got {
		if strings.HasPrefix(kv, ortDisableTelemetryKey+"=") {
			seen++
			if kv != ortDisableTelemetryKey+"=1" {
				t.Fatalf("inherited value not overridden: %q", kv)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("want exactly one %s entry, got %d in %v", ortDisableTelemetryKey, seen, got)
	}
	for _, want := range []string{"PATH=/bin", "HOME=/home/x"} {
		found := false
		for _, kv := range got {
			if kv == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("unrelated env entry %q was dropped: %v", want, got)
		}
	}
}

func TestApplySynthProcessEnvPinsCwdAndDisablesTelemetry(t *testing.T) {
	cmd := exec.Command("true")
	applySynthProcessEnv(cmd, "/cache/engine")
	if cmd.Dir != "/cache/engine" {
		t.Fatalf("cmd.Dir = %q, want /cache/engine", cmd.Dir)
	}
	found := false
	for _, kv := range cmd.Env {
		if kv == ortDisableTelemetryKey+"=1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s=1 missing from child env", ortDisableTelemetryKey)
	}
}

// TestKokoroSynthDisablesORTTelemetry proves the synth child actually receives
// the telemetry opt-out. ONNX Runtime >= 1.29 writes its device-id/session
// sidecar (":memory:.ses") into the process cwd on OrtEnv init, which litters
// whatever directory ocode was launched from; the fake interpreter reproduces
// that contract by refusing to synthesize without ORT_DISABLE_TELEMETRY=1.
func TestKokoroSynthDisablesORTTelemetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake venv script needs a POSIX shell")
	}
	root := shortTempDir(t)
	m := kokoroManifest
	m.Runtime = map[string]PythonRuntime{Host(): {Requirements: []string{"x==1"}, MinPython: [2]int{3, 11}}}
	m.VoiceFiles = nil
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		t.Fatal(err)
	}
	venv := filepath.Join(dir, "venv")
	if err := os.MkdirAll(filepath.Join(venv, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "espeak-ng-data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "phontab"), []byte("phontab"), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(data) >= espeakDataPathLimit() {
		t.Fatalf("test setup: data path %d is over the %d-byte limit", len(data), espeakDataPathLimit())
	}

	fake := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-c" ]; then
  printf '%%s\n' '%s'
  exit 0
fi
if [ "$ORT_DISABLE_TELEMETRY" != "1" ]; then
  echo "ORT_DISABLE_TELEMETRY not set for synth child" >&2
  exit 11
fi
cat >/dev/null
printf 'RIFFfakewav' > "$5"
`, data)
	if err := os.WriteFile(filepath.Join(venv, "bin", "python"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "out.wav")
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	if err := kokoroSynth(context.Background(), sup, root, m, "test", "hello", outPath, m.Voice); err != nil {
		t.Fatalf("kokoroSynth: %v", err)
	}
	if b, err := os.ReadFile(outPath); err != nil || !strings.HasPrefix(string(b), "RIFF") {
		t.Fatalf("no WAV produced: err %v content %q", err, b)
	}
}
