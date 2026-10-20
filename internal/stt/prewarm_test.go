package stt

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPrewarmStartsTheWorker checks that Prewarm starts the resident worker,
// so the model is already loaded by the time the user finishes speaking.
func TestPrewarmStartsTheWorker(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	// A pinned interpreter makes the worker start with the fake python, so
	// the test does not need onnx-asr.
	opts := Options{
		Model:    "parakeet-tdt-0.6b-v3",
		Python:   py,
		ModelDir: t.TempDir(),
	}
	if err := Prewarm(context.Background(), opts); err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	if workerPID() == 0 {
		t.Fatal("Prewarm did not start a worker")
	}
	// The worker must be the one a transcription would use, not a second
	// worker with a different key.
	key := localWorkerKey("nemo-parakeet-tdt-0.6b-v3", "istupakov/parakeet-tdt-0.6b-v3-onnx",
		filepath.Join(opts.ModelDir, "parakeet-tdt-0.6b-v3"))
	w, err := acquireWorker(context.Background(), key, []string{py})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer releaseWorker(w)
	if w.pid != workerPID() {
		t.Fatalf("the prewarmed worker (pid %d) is not the one a transcription uses (pid %d)",
			workerPID(), w.pid)
	}
}

// TestPrewarmIsIdempotent checks that prewarming twice does not start a
// second worker or reload the model.
func TestPrewarmIsIdempotent(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	opts := Options{Model: "parakeet-tdt-0.6b-v3", Python: py, ModelDir: t.TempDir()}
	if err := Prewarm(context.Background(), opts); err != nil {
		t.Fatalf("first Prewarm: %v", err)
	}
	first := workerPID()
	if first == 0 {
		t.Fatal("the first Prewarm did not start a worker")
	}
	if err := Prewarm(context.Background(), opts); err != nil {
		t.Fatalf("second Prewarm: %v", err)
	}
	if second := workerPID(); second != first {
		t.Fatalf("the second Prewarm started a new worker (pid %d, was %d)", second, first)
	}

	// The model must have been loaded once, not twice.
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	loads := 0
	for _, line := range splitLines(string(data)) {
		if line == "MODEL_LOADED" {
			loads++
		}
	}
	if loads != 1 {
		t.Fatalf("the model was loaded %d times, want 1", loads)
	}
}

// TestPrewarmIsANoOpForHostedModels checks that a hosted model has nothing to
// prewarm, and that Prewarm does not fail.
func TestPrewarmIsANoOpForHostedModels(t *testing.T) {
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)
	opts := Options{
		Model:     "gpt-4o-transcribe",
		OpenAIKey: func() string { return "sk-test" },
	}
	if err := Prewarm(context.Background(), opts); err != nil {
		t.Fatalf("Prewarm on a hosted model: %v", err)
	}
	if workerPID() != 0 {
		t.Fatal("Prewarm started a worker for a hosted model")
	}
}

// TestPrewarmDoesNotInstall checks that Prewarm never triggers a dependency
// install. The install-approval flow belongs to the transcription path; a
// prewarm that silently started a pip install would be a surprise.
//
// The fake interpreter has no onnx_asr, so the interpreter probe fails. With
// no approval, the probe must fail rather than install.
func TestPrewarmDoesNotInstall(t *testing.T) {
	resetProbeCache(t)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	log := filepath.Join(t.TempDir(), "calls.log")
	base := writeInstallablePython(t, log)
	t.Setenv("PATH", filepath.Dir(base)+string(os.PathListSeparator)+os.Getenv("PATH"))

	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	opts := Options{Model: "parakeet-tdt-0.6b-v3", ModelDir: t.TempDir()}
	// InstallApproved is deliberately false: a prewarm must not install.
	err := Prewarm(context.Background(), opts)
	if err == nil {
		t.Fatal("Prewarm succeeded without an interpreter that has onnx_asr")
	}
	if calls, _ := os.ReadFile(log); containsAny(string(calls), "pip", "-m venv") {
		t.Errorf("a prewarm installed:\n%s", calls)
	}
}

// TestPrewarmMakesTheFirstTranscriptionFast is the end-to-end claim: after a
// prewarm, the first transcription does not pay the model load.
//
// It needs the real model, so it is gated on OCODE_STT_LIVE.
func TestPrewarmMakesTheFirstTranscriptionFast(t *testing.T) {
	dir := os.Getenv("OCODE_STT_LIVE")
	if dir == "" {
		t.Skip("set OCODE_STT_LIVE to a model directory to run the live prewarm test")
	}
	wav := absTestWav(t)
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	modelDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Model: "parakeet-tdt-0.6b-v3", ModelDir: modelDir + "/ocode/stt-models"}

	// Cold: the first transcription pays the load.
	start := time.Now()
	if _, err := Transcribe(context.Background(), opts, wav); err != nil {
		t.Fatalf("cold transcribe: %v", err)
	}
	cold := time.Since(start)

	// Prewarm after an idle unload, then measure the first transcription.
	stopActiveWorker()
	prewarmStart := time.Now()
	if err := Prewarm(context.Background(), opts); err != nil {
		t.Fatalf("Prewarm: %v", err)
	}
	prewarm := time.Since(prewarmStart)

	start = time.Now()
	if _, err := Transcribe(context.Background(), opts, wav); err != nil {
		t.Fatalf("warm transcribe: %v", err)
	}
	warm := time.Since(start)
	stopActiveWorker()

	t.Logf("cold=%.2fs  prewarm=%.2fs  first-transcribe-after-prewarm=%.2fs",
		cold.Seconds(), prewarm.Seconds(), warm.Seconds())
	if warm >= cold {
		t.Fatalf("the first transcription after a prewarm took %.2fs, not less than the cold %.2fs: "+
			"the prewarm did not help", warm.Seconds(), cold.Seconds())
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
