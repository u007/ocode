package stt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolveModelFallsBackToDefault(t *testing.T) {
	if got := ResolveModel(""); got != DefaultModel {
		t.Fatalf("empty = %q, want %q", got, DefaultModel)
	}
	if got := ResolveModel("no-such-model"); got != DefaultModel {
		t.Fatalf("unknown = %q, want default", got)
	}
	if got := ResolveModel("whisper-1"); got != "whisper-1" {
		t.Fatalf("known id changed to %q", got)
	}
}

func TestStatusMarksOpenAIModelsUnavailableWithoutKey(t *testing.T) {
	st := StatusFor(Options{OpenAIKey: func() string { return "" }, Python: "/nonexistent/python"})
	if st.Selected != DefaultModel {
		t.Fatalf("selected = %q", st.Selected)
	}
	for _, m := range st.Models {
		if m.Engine == EngineOpenAI && m.Available {
			t.Fatalf("%s available without a key", m.ID)
		}
		if m.Engine == EngineOpenAI && m.Reason == "" {
			t.Fatalf("%s has no reason", m.ID)
		}
		if m.Engine == EngineLocal && m.Available {
			t.Fatalf("%s available with a missing interpreter", m.ID)
		}
	}
	if st.ModelAvailable(DefaultModel) {
		t.Fatalf("default model reported ready with no onnx-asr")
	}
}

func TestStatusOpenAIAvailableWithKey(t *testing.T) {
	st := StatusFor(Options{OpenAIKey: func() string { return "sk-test" }})
	if !st.ModelAvailable("gpt-4o-transcribe") || !st.ModelAvailable("whisper-1") {
		t.Fatalf("openai models should be ready with a key: %+v", st.Models)
	}
}

func writeAudio(t *testing.T, name string, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTranscribeOpenAISendsMultipartAndParsesText(t *testing.T) {
	var gotAuth, gotModel, gotFile string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		gotModel = r.FormValue("model")
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("file field: %v", err)
		}
		b, _ := io.ReadAll(f)
		gotFile = string(b)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "  hello world  "})
	}))
	defer srv.Close()

	audio := writeAudio(t, "clip.webm", "RIFFfake-audio")
	res, err := Transcribe(context.Background(), Options{
		Model:         "whisper-1",
		OpenAIKey:     func() string { return "sk-test" },
		OpenAIBaseURL: srv.URL + "/v1",
	}, audio)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "hello world" || res.Model != "whisper-1" {
		t.Fatalf("result = %+v", res)
	}
	if gotAuth != "Bearer sk-test" || gotModel != "whisper-1" || gotFile != "RIFFfake-audio" {
		t.Fatalf("auth=%q model=%q file=%q", gotAuth, gotModel, gotFile)
	}
}

func TestTranscribeOpenAIUpstreamErrorIsSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	audio := writeAudio(t, "clip.wav", "x")
	_, err := Transcribe(context.Background(), Options{
		Model: "whisper-1", OpenAIKey: func() string { return "sk-bad" }, OpenAIBaseURL: srv.URL,
	}, audio)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want 401", err)
	}
}

func TestTranscribeRejectsEmptyAndMissingKey(t *testing.T) {
	empty := writeAudio(t, "empty.wav", "")
	if _, err := Transcribe(context.Background(), Options{Model: "whisper-1", OpenAIKey: func() string { return "k" }}, empty); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty recording err = %v", err)
	}
	audio := writeAudio(t, "a.wav", "x")
	if _, err := Transcribe(context.Background(), Options{Model: "whisper-1", OpenAIKey: func() string { return "" }}, audio); err == nil {
		t.Fatalf("missing key should fail")
	}
}

func TestCaptureCommandSelection(t *testing.T) {
	name, args, interrupt, err := captureCommand("linux", "/tmp/x.wav", true)
	if err != nil || name != "arecord" || !interrupt || args[len(args)-1] != "/tmp/x.wav" {
		t.Fatalf("linux+arecord = %q %v %v %v", name, args, interrupt, err)
	}
	name, _, interrupt, err = captureCommand("linux", "/tmp/x.wav", false)
	if err != nil || name != "ffmpeg" || interrupt {
		t.Fatalf("linux ffmpeg fallback = %q interrupt=%v err=%v", name, interrupt, err)
	}
	if name, _, _, _ = captureCommand("darwin", "o.wav", false); name != "ffmpeg" {
		t.Fatalf("darwin = %q", name)
	}
	if _, _, _, err := captureCommand("plan9", "o.wav", false); err == nil {
		t.Fatalf("unsupported OS should error")
	}
}

func TestNormaliseSkipsWAVInput(t *testing.T) {
	in := writeAudio(t, "in.WAV", "x")
	out, cleanup, err := normaliseToWAV(context.Background(), in, "")
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	defer cleanup()
	if out != in {
		t.Fatalf("WAV input should pass through, got %q", out)
	}
}

func TestEmbeddedOnnxScriptIsPresent(t *testing.T) {
	if !strings.Contains(transcribeOnnxScript, "import onnx_asr") {
		t.Fatalf("embedded helper missing onnx_asr import")
	}
}

// TestLiveParakeetTranscribes runs the real local engine (onnx-asr + Parakeet
// v3). It downloads ~3 GB on first run, so it only runs when OCODE_STT_LIVE
// names a model directory to use (e.g. OCODE_STT_LIVE=$HOME/.cache/ocode/stt-models).
func TestLiveParakeetTranscribes(t *testing.T) {
	dir := os.Getenv("OCODE_STT_LIVE")
	if dir == "" {
		t.Skip("set OCODE_STT_LIVE to a model directory to run the live Parakeet test")
	}
	res, err := Transcribe(context.Background(), Options{Model: "parakeet-tdt-0.6b-v3", ModelDir: dir},
		filepath.Join("testdata", "speech-16k.wav"))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if !strings.Contains(strings.ToLower(res.Text), "hello world") {
		t.Fatalf("transcript = %q, want it to contain \"hello world\"", res.Text)
	}
}

// TestLiveParakeetUnifiedTranscribes is the same check for Parakeet Unified EN
// (RNNT). Gated by OCODE_STT_LIVE like the v3 test; it shares the model dir.
func TestLiveParakeetUnifiedTranscribes(t *testing.T) {
	dir := os.Getenv("OCODE_STT_LIVE")
	if dir == "" {
		t.Skip("set OCODE_STT_LIVE to a model directory to run the live Parakeet test")
	}
	res, err := Transcribe(context.Background(), Options{Model: "parakeet-unified-en-0.6b", ModelDir: dir},
		filepath.Join("testdata", "speech-16k.wav"))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if !strings.Contains(strings.ToLower(res.Text), "hello world") {
		t.Fatalf("transcript = %q, want it to contain \"hello world\"", res.Text)
	}
}

// writeFakeInterpreter writes a POSIX stand-in for a Python interpreter. It
// reports version and fails every import with a ModuleNotFoundError that names
// the probed modules, so probes run against it never touch the host's Python.
func writeFakeInterpreter(t *testing.T, version string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "python3")
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  --version) echo 'Python " + version + "' ;;\n" +
		"  *) echo \"ModuleNotFoundError: $3\" >&2; exit 1 ;;\n" +
		"esac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// resetProbeCache empties the package-global probe cache when the test ends, so
// a fake interpreter's result never outlives the test that created it.
func resetProbeCache(t *testing.T) {
	t.Helper()
	forgetProbes()
	t.Cleanup(forgetProbes)
}

// A model's extra modules are part of its availability: an interpreter that
// has onnx_asr but lacks sentencepiece must not report the unified model ready.
func TestProbeLocalChecksExtraModules(t *testing.T) {
	resetProbeCache(t)
	python := writeFakeInterpreter(t, "3.12.0")
	ok, reason := probeLocal(python, []string{"ocode_no_such_module_xyz"})
	if ok {
		t.Fatalf("probe passed with a module that does not exist")
	}
	if !strings.Contains(reason, "ocode_no_such_module_xyz") {
		t.Errorf("reason %q does not name the missing module", reason)
	}
	st := StatusFor(Options{Python: python})
	for _, m := range st.Models {
		if m.ID == "parakeet-unified-en-0.6b" && !strings.Contains(m.Reason, "sentencepiece") {
			t.Fatalf("unified reason %q does not name sentencepiece", m.Reason)
		}
	}
}

// A pinned python that does not exist must be named in the reason. Select
// skips missing candidate names silently, so without the explicit check the
// user's configured path would vanish from the error.
func TestLocalInterpreterNamesMissingPinnedPython(t *testing.T) {
	resetProbeCache(t)
	missing := filepath.Join(t.TempDir(), "no-such-python")
	_, err := localInterpreter(missing, nil)
	if err == nil {
		t.Fatal("localInterpreter accepted a python that does not exist")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the configured python %q", err, missing)
	}
}

// A slow scan for one key must not hold the cache for the others: status polling
// and Transcribe for a different interpreter return while it is still running.
func TestProbeDoesNotBlockOtherKeys(t *testing.T) {
	resetProbeCache(t)
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	slow := filepath.Join(dir, "slow-python")
	script := "#!/bin/sh\ntouch " + started + "\nsleep 2\necho 'Python 3.12.0'\n"
	if err := os.WriteFile(slow, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		probeLocal(slow, nil)
	}()
	defer func() { <-done }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow scan never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	begin := time.Now()
	probeLocal(filepath.Join(dir, "no-such-python"), nil)
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("probe for another key took %v while a 2s scan ran; cache lock held across the scan", elapsed)
	}
}

// StatusFor probes the local models' distinct extras sets concurrently, so a
// status request waits for the slowest scan, not for the sum of them.
func TestStatusForProbesLocalModelsConcurrently(t *testing.T) {
	resetProbeCache(t)
	if runtime.GOOS == "windows" {
		t.Skip("fake interpreter scripts need a POSIX shell")
	}
	// Every invocation sleeps 1 s: the version check and each import probe. Two
	// distinct keys (no extras, sentencepiece) each cost 2 s; serial is 4 s.
	slow := filepath.Join(t.TempDir(), "slow-python")
	script := "#!/bin/sh\nsleep 1\necho 'Python 3.12.0'\n"
	if err := os.WriteFile(slow, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	begin := time.Now()
	st := StatusFor(Options{Python: slow})
	elapsed := time.Since(begin)
	if elapsed > 3500*time.Millisecond {
		t.Fatalf("StatusFor took %v; the local probes ran one after another", elapsed)
	}
	for i, s := range catalog {
		if st.Models[i].ID != s.ID {
			t.Fatalf("Models[%d] = %q, want catalog order %q", i, st.Models[i].ID, s.ID)
		}
		if s.Engine == EngineLocal && !st.Models[i].Available {
			t.Fatalf("%s unavailable: %s", s.ID, st.Models[i].Reason)
		}
	}
}
