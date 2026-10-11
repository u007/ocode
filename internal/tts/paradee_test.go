package tts

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

func TestParadeeManifestIsRegisteredAndFullyPinned(t *testing.T) {
	m, ok := ManifestFor(EngineParadee)
	if !ok {
		t.Fatal("paradee manifest is not registered")
	}
	if m.Voice != "af_heart" || len(m.Voices) != 1 || m.Voices[0] != "af_heart" {
		t.Fatalf("paradee voices = %q (default %q), want the single af_heart voice", m.Voices, m.Voice)
	}
	for _, a := range m.VoiceFiles {
		if !strings.HasPrefix(a.URL, "https://") {
			t.Errorf("artifact %q is not an https URL: %q", a.Name, a.URL)
		}
		if a.Size <= 0 {
			t.Errorf("artifact %q has no pinned size", a.Name)
		}
		if len(a.SHA256) != 64 {
			t.Errorf("artifact %q has no pinned SHA-256: %q", a.Name, a.SHA256)
		}
	}
	rt, ok := m.Runtime["darwin/arm64"]
	if !ok {
		t.Fatal("paradee declares no darwin/arm64 runtime")
	}
	if rt.MinPython != [2]int{3, 11} || rt.MaxPython != [2]int{3, 12} {
		t.Fatalf("paradee python range = %v-%v, want 3.11-3.12 (misaki<3.13, onnxruntime 1.30 needs >=3.11)", rt.MinPython, rt.MaxPython)
	}
	for _, req := range rt.Requirements {
		if !strings.Contains(req, "==") {
			t.Errorf("requirement %q is not pinned", req)
		}
	}
	artifacts := map[string]bool{}
	for _, a := range m.VoiceFiles {
		artifacts[a.Name] = true
	}
	for _, wheel := range rt.Wheels {
		if !artifacts[wheel] {
			t.Errorf("wheel %q is not a pinned artifact, so it cannot be installed from the verified cache", wheel)
		}
	}
}

// The English G2P loads en_core_web_sm itself and calls spacy.cli.download when
// it is missing, so the model must be an installed, pinned artifact and must
// match the spaCy minor line it was built against.
func TestParadeeInstallsTheSpacyModelAsAPinnedWheel(t *testing.T) {
	rt := paradeeManifest.Runtime["darwin/arm64"]
	if len(rt.Wheels) != 1 || rt.Wheels[0] != paradeeSpacyModelWheel {
		t.Fatalf("wheels = %v, want [%s]", rt.Wheels, paradeeSpacyModelWheel)
	}
	if !strings.HasPrefix(paradeeSpacyModelWheel, "en_core_web_sm-3.8.") {
		t.Fatalf("spaCy model %q is not on the 3.8 line", paradeeSpacyModelWheel)
	}
	if !contains(rt.Requirements, "spacy==3.8.16") {
		t.Fatalf("requirements %v do not pin spacy on the model's 3.8 line", rt.Requirements)
	}
}

// misaki's [en] extra adds spacy-curated-transformers, which drags PyTorch into
// a venv that only needs lexicon-mode G2P. Keep the extra off.
func TestParadeeRequirementsKeepTorchOut(t *testing.T) {
	for _, req := range paradeeManifest.Runtime["darwin/arm64"].Requirements {
		if strings.Contains(req, "[") || strings.Contains(req, "torch") || strings.Contains(req, "curated") {
			t.Fatalf("requirement %q pulls in the transformer G2P stack", req)
		}
	}
}

func TestParadeeCatalogEntryIsInstallableOnlyWhereVerified(t *testing.T) {
	e, ok := EngineForHost(EngineParadee)
	if !ok {
		t.Fatal("paradee is missing from the catalog")
	}
	_, pinned := paradeeManifest.HostRuntime()
	if pinned && e.Availability != AvailabilityInstallable {
		t.Fatalf("availability = %q on %s, want installable", e.Availability, Host())
	}
	if !pinned && e.Availability != AvailabilityUnavailable {
		t.Fatalf("availability = %q on unpinned host %s, want unavailable", e.Availability, Host())
	}
	if e.LicenseHash != paradeeManifest.LicenseHash() {
		t.Fatalf("catalog license hash does not cover the manifest disclosure")
	}
}

// The synth child must run the cached model and config with the espeak data
// path, write to the requested output, take the text on stdin, and inherit the
// cache dir as its cwd.
func TestParadeeSynthInvokesTheCachedArtifacts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter is a POSIX shell script")
	}
	root := t.TempDir()
	dir, err := CacheDirChecked(root, EngineParadee, paradeeManifest.Voice, paradeeManifest.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "venv", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, "record")
	// bundledEspeakDataPath calls the interpreter with -c; answer that with a
	// path short enough to pass espeak's length check unchanged, so the test
	// does not depend on the length of t.TempDir(). Any other call is the synth
	// driver: record argv and stdin, then write a placeholder WAV to the output
	// path (argv[5]).
	fake := `#!/bin/sh
if [ "$1" = "-c" ]; then echo /short/espeak-data; exit 0; fi
{
  echo "ARGV_COUNT=$#"
  i=1
  for a in "$@"; do echo "ARGV_$i=$a"; i=$((i+1)); done
  echo "STDIN=$(cat)"
  echo "CWD=$(pwd)"
} > ` + record + `
printf 'RIFFfakewav' > "$5"
`
	if err := os.WriteFile(filepath.Join(dir, "venv", "bin", "python"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.wav")
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	if err := paradeeSynth(context.Background(), sup, root, paradeeManifest, "test", "hello there", outPath); err != nil {
		t.Fatalf("paradeeSynth: %v", err)
	}
	if b, err := os.ReadFile(outPath); err != nil || !bytes.HasPrefix(b, []byte("RIFF")) {
		t.Fatalf("no WAV produced: err %v content %q", err, b)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	got := parseRecord(string(data))
	want := map[string]string{
		"ARGV_COUNT": "5",
		"ARGV_1":     filepath.Join(dir, paradeeScriptName),
		"ARGV_2":     filepath.Join(dir, paradeeModelName),
		"ARGV_3":     filepath.Join(dir, paradeeConfigName),
		"ARGV_4":     "/short/espeak-data",
		"ARGV_5":     outPath,
		"STDIN":      "hello there",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q (full record: %s)", k, got[k], v, data)
		}
	}
	if cwd := got["CWD"]; resolvePath(t, cwd) != resolvePath(t, dir) {
		t.Fatalf("child cwd = %q, want the engine cache dir %q", cwd, dir)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
