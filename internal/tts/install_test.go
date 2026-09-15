package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

func TestInstallerPersistsStateAcrossRestart(t *testing.T) {
	root := t.TempDir()
	first := NewInstaller(root)
	first.SetState("piper", EngineInstall{EngineID: "piper", State: InstallAccepted, LicenseName: "x"})
	second := NewInstaller(root)
	if got := second.State("piper"); got.State != InstallAccepted || got.LicenseName != "x" {
		t.Fatalf("state not persisted: %#v", got)
	}
}

func TestPinRejectsForeignManifestVersion(t *testing.T) {
	s := NewSupervisor(DefaultConfig(), Options{Root: t.TempDir()})
	if _, ok := piperManifest.HostRuntime(); !ok {
		t.Skip("piper has no runtime for this host")
	}
	if err := s.AcceptLicense("piper", piperManifest.LicenseHash(), piperManifest.LicenseName); err != nil {
		t.Fatal(err)
	}
	if err := s.Pin("piper", "something-else"); err == nil {
		t.Fatal("pin accepted a version that is not the manifest's")
	}
	if err := s.Pin("piper", piperManifest.Version); err != nil {
		t.Fatal(err)
	}
	if st := s.InstallStates()["piper"]; st.State != InstallPinned || !st.Pinned {
		t.Fatalf("unexpected state after pin: %#v", st)
	}
}

func TestInstallStepsRefuseUnavailableEngines(t *testing.T) {
	s := NewSupervisor(DefaultConfig(), Options{Root: t.TempDir()})
	// Engines without authoritative license metadata cannot record consent.
	if err := s.AcceptLicense("fish-audio", "h", "n"); err == nil {
		t.Fatal("accepted a license for an engine without authoritative metadata")
	}
	// Pin/Download are also blocked: no manifest exists.
	if err := s.Pin("fish-audio", "v"); err == nil {
		t.Fatal("pinned an engine with no manifest")
	}
	if err := s.Download("fish-audio"); err == nil {
		t.Fatal("downloaded an engine with no manifest")
	}
}

func TestAcceptLicenseRejectsMismatchedMetadata(t *testing.T) {
	s := NewSupervisor(DefaultConfig(), Options{Root: t.TempDir()})
	if err := s.AcceptLicense("piper", "manifest:piper", piperManifest.LicenseName); err == nil {
		t.Fatal("accepted a synthetic license hash")
	}
	if err := s.AcceptLicense("piper", piperManifest.LicenseHash(), "changed license"); err == nil {
		t.Fatal("accepted a mismatched license name")
	}
	if got := s.InstallStates()["piper"].State; got != "" {
		t.Fatalf("mismatched acceptance changed state to %q", got)
	}
}

func TestKokoroManifestIsInstallable(t *testing.T) {
	if _, ok := kokoroManifest.HostRuntime(); !ok {
		t.Skip("kokoro has no runtime for this host")
	}
	m, ok := ManifestFor(EngineKokoro)
	if !ok {
		t.Fatal("kokoro manifest missing")
	}
	for _, a := range m.VoiceFiles {
		if a.URL == "" || a.Size <= 0 {
			t.Fatalf("artifact %q is not fully pinned: %#v", a.Name, a)
		}
		if len(a.SHA256) > 0 && len(a.SHA256) != 64 {
			t.Fatalf("artifact %q has invalid checksum length: %d", a.Name, len(a.SHA256))
		}
	}
	for host, rt := range m.Runtime {
		if len(rt.Requirements) == 0 || rt.MinPython[0] == 0 {
			t.Fatalf("runtime for %s is not pinned: %#v", host, rt)
		}
		for _, req := range rt.Requirements {
			if !strings.Contains(req, "==") {
				t.Fatalf("runtime for %s has an unpinned requirement %q", host, req)
			}
		}
	}
}

func TestManifestPythonRequirementsMatchMinimums(t *testing.T) {
	for _, manifest := range []Manifest{piperManifest, kokoroManifest} {
		for host, runtime := range manifest.Runtime {
			required := "onnxruntime==1.30.0"
			if host == "darwin/amd64" {
				if manifest.Engine == EnginePiper {
					required = "onnxruntime==1.22.1"
				} else {
					required = "onnxruntime==1.22.0"
				}
			}
			for _, requirement := range runtime.Requirements {
				if strings.HasPrefix(requirement, "onnxruntime==") && requirement != required {
					t.Errorf("%s %s pins %q, want %q", manifest.Engine, host, requirement, required)
				}
			}
			if required == "onnxruntime==1.30.0" && runtime.MinPython != [2]int{3, 11} {
				t.Errorf("%s %s has Python minimum %v, want 3.11 for %s", manifest.Engine, host, runtime.MinPython, required)
			}
		}
	}
}

// fakeInstalledPiper lays out a cache directory that Verify accepts, with a
// stand-in "venv python" script that writes its --output_file.
func fakeInstalledPiper(t *testing.T, root string, script string) Manifest {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake venv script needs a POSIX shell")
	}
	m := piperManifest
	m.Runtime = map[string]PythonRuntime{Host(): {Requirements: []string{"x==1"}, MinPython: [2]int{3, 11}}}
	m.VoiceFiles = nil
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "venv", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "venv", "bin", "python"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, installedMarker), []byte(`{"version":"`+m.Version+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return m
}

const fakePiperOK = `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--output_file" ]; then out="$2"; shift; fi
  shift
done
cat >/dev/null
printf 'RIFFfakewav' > "$out"
`

const fakePiperFail = `#!/bin/sh
echo "boom" >&2
exit 3
`

func waitPlayback(t *testing.T, s *Supervisor, want string) Playback {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p := s.Status().Playback
		if p.Status == want {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("playback never reached %q: %#v", want, s.Status().Playback)
	return Playback{}
}

func TestReplaceSynthesizesAndServesOnlyActiveAudio(t *testing.T) {
	root := t.TempDir()
	m := fakeInstalledPiper(t, root, fakePiperOK)
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	s := NewSupervisor(DefaultConfig(), Options{Root: root, ProcSup: sup})
	s.installer.SetState("piper", EngineInstall{EngineID: "piper", State: InstallInstalled, ManifestVer: m.Version})
	if _, err := s.Enable("piper"); err != nil {
		t.Fatal(err)
	}
	// The supervisor verifies against the real manifest's voice files, which
	// the fake does not have; point it at the fake by swapping the manifest.
	pb, err := s.replaceWith(m, "hello there")
	if err != nil {
		t.Fatal(err)
	}
	if pb.Status != PlaybackStatusSynthesizing || pb.AudioID == "" {
		t.Fatalf("unexpected initial playback: %#v", pb)
	}
	ready := waitPlayback(t, s, PlaybackStatusReady)
	path, err := s.AudioPath(ready.AudioID)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "RIFFfakewav" {
		t.Fatalf("audio not written: %v %q", err, data)
	}
	if _, err := s.AudioPath("999"); err == nil {
		t.Fatal("served a non-active audio id")
	}
	s.Stop()
	if _, err := s.AudioPath(ready.AudioID); err == nil {
		t.Fatal("served audio after stop")
	}
}

func TestReplaceReportsSynthesisFailure(t *testing.T) {
	root := t.TempDir()
	m := fakeInstalledPiper(t, root, fakePiperFail)
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	s := NewSupervisor(DefaultConfig(), Options{Root: root, ProcSup: sup})
	if _, err := s.replaceWith(m, "hello"); err != nil {
		t.Fatal(err)
	}
	p := waitPlayback(t, s, PlaybackStatusError)
	if p.Error == "" {
		t.Fatalf("error status without message: %#v", p)
	}
}

func TestReconcileMarksMissingCacheFailed(t *testing.T) {
	root := t.TempDir()
	NewInstaller(root).SetState("piper", EngineInstall{EngineID: "piper", State: InstallEnabled})
	s := NewSupervisor(Config{Engine: EnginePiper, Mode: PlaybackManual}, Options{Root: root})
	if st := s.InstallStates()["piper"]; st.State != InstallFailed {
		t.Fatalf("stale installed state survived reconcile: %#v", st)
	}
	if e, _ := s.Engine(EnginePiper); e.Availability == AvailabilityReady {
		t.Fatal("piper advertised ready without verified cache")
	}
}

func TestDownloadRetriesTransientFailuresThenVerifies(t *testing.T) {
	payload := []byte("voice-bytes")
	sum := sha256.Sum256(payload)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			// First attempt: truncated body simulates a reset mid-transfer.
			w.Header().Set("Content-Length", "11")
			_, _ = w.Write(payload[:3])
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	a := Artifact{Name: "v.onnx", URL: srv.URL + "/v.onnx", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(payload))}
	inst := &piperInstaller{client: srv.Client(), progress: func(int, string) {}}
	dst := filepath.Join(t.TempDir(), "v.onnx")
	if err := inst.downloadWithRetry(t.Context(), a, dst, func(int64) {}); err != nil {
		t.Fatal(err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
	if err := verifyArtifact(dst, a); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRejectsChecksumMismatchAfterAllAttempts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("wrong"))
	}))
	defer srv.Close()
	a := Artifact{Name: "v.onnx", URL: srv.URL, SHA256: "", Size: 5}
	inst := &piperInstaller{client: srv.Client(), progress: func(int, string) {}}
	dst := filepath.Join(t.TempDir(), "v.onnx")
	err := inst.downloadWithRetry(t.Context(), a, dst, func(int64) {})
	if err != nil {
		t.Fatalf("expected download to succeed without checksum, got %v", err)
	}
	if _, statErr := os.Stat(dst); statErr != nil {
		t.Fatal("verified artifact was not installed")
	}
}

func TestDownloadRejectsTruncatedDownloadAfterAllAttempts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()
	a := Artifact{Name: "v.onnx", URL: srv.URL, SHA256: "", Size: 10}
	inst := &piperInstaller{client: srv.Client(), progress: func(int, string) {}}
	dst := filepath.Join(t.TempDir(), "v.onnx")
	err := inst.downloadWithRetry(t.Context(), a, dst, func(int64) {})
	if err == nil || !strings.Contains(err.Error(), "truncated download") {
		t.Fatalf("expected truncated download error, got %v", err)
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		t.Fatal("truncated artifact should not be installed")
	}
}
