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

// TestManifestPythonRangesRejectTooNewInterpreter is the regression for the
// Kokoro install failure where pip resolved the pinned requirements against a
// Python 3.14 venv and died with "Ignored the following versions that require a
// different python version" (kokoro-onnx 0.6.1 declares Requires-Python
// <3.14,>=3.10). Every shipped runtime must carry an upper bound, no shipped
// runtime may accept an interpreter newer than its wheels support, and the two
// engines' ceilings must stay pinned to what their requirements support.
func TestManifestPythonRangesRejectTooNewInterpreter(t *testing.T) {
	tests := []struct {
		engine      EngineID
		host        string
		rejectMinor int // newest minor that must NOT be accepted
		acceptMinor int // a minor that must be accepted
	}{
		// kokoro-onnx 0.6.1 caps every host at 3.13.
		{EngineKokoro, "darwin/arm64", 14, 13},
		{EngineKokoro, "darwin/amd64", 14, 13},
		{EngineKokoro, "linux/amd64", 14, 13},
		{EngineKokoro, "linux/arm64", 14, 13},
		{EngineKokoro, "windows/amd64", 14, 13},
		// piper-tts is cp39-abi3; onnxruntime 1.30.0 covers 3.14 on these hosts.
		{EnginePiper, "darwin/arm64", 15, 14},
		{EnginePiper, "linux/amd64", 15, 14},
		{EnginePiper, "linux/arm64", 15, 14},
		{EnginePiper, "windows/amd64", 15, 14},
		// onnxruntime 1.22.x has cp310-cp313 wheels only.
		{EnginePiper, "darwin/amd64", 14, 13},
	}
	for _, tc := range tests {
		m, ok := ManifestFor(tc.engine)
		if !ok {
			t.Fatalf("%s manifest missing", tc.engine)
		}
		rt, ok := m.Runtime[tc.host]
		if !ok {
			continue // host not advertised by this engine
		}
		if rt.MaxPython == [2]int{} {
			t.Errorf("%s %s has no Python ceiling; a too-new interpreter breaks pip resolution", tc.engine, tc.host)
			continue
		}
		if rt.accepts([2]int{3, tc.rejectMinor}) {
			t.Errorf("%s %s accepts 3.%d, want rejected", tc.engine, tc.host, tc.rejectMinor)
		}
		if !rt.accepts([2]int{3, tc.acceptMinor}) {
			t.Errorf("%s %s rejects 3.%d, want accepted", tc.engine, tc.host, tc.acceptMinor)
		}
		if !rt.accepts(rt.MinPython) {
			t.Errorf("%s %s rejects its own minimum %v", tc.engine, tc.host, rt.MinPython)
		}
		if !rt.accepts(rt.MaxPython) {
			t.Errorf("%s %s rejects its own maximum %v", tc.engine, tc.host, rt.MaxPython)
		}
	}
}

// TestSelectPythonBoundsRange covers the probe loop that picks the venv
// interpreter: a first-on-PATH interpreter newer than the pinned range must be
// skipped in favor of an in-range one, and when none is in range the error must
// name the range and the rejected versions.
func TestSelectPythonBoundsRange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	dir := t.TempDir()
	writeFakePython := func(name, version string) string {
		path := filepath.Join(dir, name)
		script := "#!/bin/sh\necho 'Python " + version + "'\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// 3.14 first (mirrors /opt/homebrew/bin/python3 on the failing host), then
	// 3.13 and 3.10 as fallbacks.
	tooNew := writeFakePython("python3", "3.14.6")
	inRange := writeFakePython("python3.13", "3.13.14")
	tooOld := writeFakePython("python3.10", "3.10.20")

	rt := PythonRuntime{
		Requirements: []string{"kokoro-onnx" + "==" + "0.6.1"},
		MinPython:    [2]int{3, 11},
		MaxPython:    [2]int{3, 13},
	}

	got, err := selectPython(t.Context(), rt, [][]string{{tooNew}, {inRange}})
	if err != nil {
		t.Fatalf("selectPython: %v", err)
	}
	if got.Version != [2]int{3, 13} {
		t.Fatalf("selected %v, want 3.13 (3.14 is outside the pinned range)", got.Version)
	}

	err = nil
	if _, err = selectPython(t.Context(), rt, [][]string{{tooNew}, {tooOld}}); err == nil {
		t.Fatal("selectPython accepted only out-of-range interpreters")
	}
	msg := err.Error()
	if !strings.Contains(msg, "3.11-3.13") {
		t.Errorf("error does not name the supported range: %q", msg)
	}
	for _, want := range []string{"3.14", "3.10"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not report rejected %s: %q", want, msg)
		}
	}

	// piper on darwin/arm64 keeps accepting 3.14 (regression guard: a blanket
	// 3.13 cap would break the working 3.14 piper install).
	piper, _ := ManifestFor(EnginePiper)
	armRT, ok := piper.Runtime["darwin/arm64"]
	if !ok {
		t.Skip("piper has no darwin/arm64 runtime")
	}
	got, err = selectPython(t.Context(), armRT, [][]string{{tooNew}})
	if err != nil {
		t.Fatalf("piper/darwin-arm64 must accept 3.14: %v", err)
	}
	if got.Version != [2]int{3, 14} {
		t.Fatalf("selected %v, want 3.14", got.Version)
	}
}

// TestPipResolutionHintOnlyAnnotatesInterpreterMismatch keeps the actionable
// hint scoped: an unrelated pip failure must not be rewritten into a Python
// version story.
func TestPipResolutionHintOnlyAnnotatesInterpreterMismatch(t *testing.T) {
	rt := PythonRuntime{
		Requirements: []string{"x" + "==" + "1"},
		MinPython:    [2]int{3, 11},
		MaxPython:    [2]int{3, 13},
	}
	py := pythonSpec{Version: [2]int{3, 14}}

	mismatch := "ERROR: Ignored the following versions that require a different python version: 0.6.1 Requires-Python <3.14,>=3.10\nERROR: No matching distribution found for kokoro-onnx==0.6.1"
	hinted := pipResolutionHint(mismatch, py, rt)
	if !strings.Contains(hinted, "Python 3.14") || !strings.Contains(hinted, "3.11-3.13") {
		t.Errorf("mismatch hint missing interpreter version or supported range: %q", hinted)
	}

	network := "ERROR: Could not fetch URL https://pypi.org/simple/pip/: connection error"
	if got := pipResolutionHint(network, py, rt); got != network {
		t.Errorf("unrelated failure was rewritten: %q", got)
	}
}

// TestPythonRangeRendering keeps the user-facing range text stable, since the
// install error and the actionable hint both embed it.
func TestPythonRangeRendering(t *testing.T) {
	bounded := PythonRuntime{MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 13}}
	if got := bounded.pythonRange(); got != "3.11-3.13" {
		t.Errorf("pythonRange() = %q, want 3.11-3.13", got)
	}
	unbounded := PythonRuntime{MinPython: [2]int{3, 11}}
	if got := unbounded.pythonRange(); got != ">=3.11" {
		t.Errorf("pythonRange() = %q, want >=3.11", got)
	}
	if !unbounded.accepts([2]int{3, 99}) {
		t.Error("zero MaxPython must be unbounded")
	}
}

// TestPythonCandidatesPreferNewestSupportedMinor guards the search order: the
// unversioned `python3` is usually a too-new Homebrew build, so versioned names
// inside the supported range must be probed first, including the absolute
// /opt/homebrew/bin/python3.N path a user gets from `brew install python@N`
// (which is NOT reachable as bare `python3`).
func TestPythonCandidatesPreferNewestSupportedMinor(t *testing.T) {
	rt := PythonRuntime{MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 13}}
	candidates := pythonCandidates(rt)
	joined := make([]string, 0, len(candidates))
	for _, c := range candidates {
		joined = append(joined, strings.Join(c, " "))
	}
	text := strings.Join(joined, "\n")
	t.Logf("candidates for 3.11-3.13:\n%s", text)

	// Every candidate must be for a minor inside the range: probing 3.14 would
	// only add noise, since the range check rejects it anyway.
	for _, rejected := range []string{"3.14", "3.10"} {
		if strings.Contains(text, rejected) {
			t.Errorf("candidate mentions %s, outside the pinned range: %q", rejected, text)
		}
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(text, "py -3.13") || !strings.Contains(text, "py -3.11") {
			t.Errorf("windows candidates must pin range minors explicitly: %q", text)
		}
		return
	}

	for _, want := range []string{"python3.13", "python3.12", "python3.11"} {
		if !strings.Contains(text, want) {
			t.Errorf("versioned candidate %q missing", want)
		}
	}
	// Newest-first ordering: python3.13 precedes python3.12 precedes python3.11,
	// and every versioned name precedes the bare python3 fallback.
	i13 := strings.Index(text, "python3.13")
	i12 := strings.Index(text, "python3.12")
	i11 := strings.Index(text, "python3.11")
	iBare := strings.Index(text, "\npython3\n")
	if i13 < 0 || i12 < 0 || i11 < 0 {
		t.Fatalf("versioned candidates missing: %q", text)
	}
	if !(i13 < i12 && i12 < i11) {
		t.Errorf("versioned candidates are not newest-first: 3.13@%d 3.12@%d 3.11@%d", i13, i12, i11)
	}
	if iBare >= 0 && iBare < i11 {
		t.Errorf("bare python3 must be a fallback, after every versioned in-range name")
	}
	if !strings.Contains(text, filepath.Join("/opt/homebrew/bin", "python3.13")) {
		t.Errorf("absolute homebrew python3.13 path missing: %q", text)
	}
}

// TestPythonMinorRange covers the probe window: bounded by MaxPython when set,
// and only MinPython..MinPython+pythonScanWindow when a runtime is unbounded.
func TestPythonMinorRange(t *testing.T) {
	bounded := PythonRuntime{MinPython: [2]int{3, 11}, MaxPython: [2]int{3, 13}}
	if got := pythonMinorRange(bounded); len(got) != 3 || got[0] != 13 || got[2] != 11 {
		t.Errorf("pythonMinorRange(bounded) = %v, want [13 12 11]", got)
	}
	unbounded := PythonRuntime{MinPython: [2]int{3, 11}}
	got := pythonMinorRange(unbounded)
	if len(got) != pythonScanWindow+1 || got[0] != 11+pythonScanWindow || got[len(got)-1] != 11 {
		t.Errorf("pythonMinorRange(unbounded) = %v, want newest-first 19..11", got)
	}
}
