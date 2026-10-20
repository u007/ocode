package stt

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWorkerRealModelLoadsOnce is the non-vacuous check that the resident
// worker actually holds the model across calls.
//
// It runs only when OCODE_STT_LIVE points at a model directory that has the
// Parakeet export, because it needs the real onnx-asr runtime. A fake worker
// would prove the protocol but not the claim that the model is loaded once.
func TestWorkerRealModelLoadsOnce(t *testing.T) {
	dir := os.Getenv("OCODE_STT_LIVE")
	if dir == "" {
		t.Skip("set OCODE_STT_LIVE to a model directory to run the live worker test")
	}
	wav := absTestWav(t)
	if _, err := os.Stat(wav); err != nil {
		t.Skip("no test wav")
	}
	// The worker is a process-wide singleton; make sure this test does not
	// inherit one from another test.
	stopActiveWorker()

	modelDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Model: "parakeet-tdt-0.6b-v3", ModelDir: modelDir + "/ocode/stt-models"}

	const runs = 4
	var pids []int
	var loads []int
	var durations []time.Duration
	for i := 0; i < runs; i++ {
		start := time.Now()
		res, err := Transcribe(context.Background(), opts, wav)
		durations = append(durations, time.Since(start))
		if err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
		if !containsFold(res.Text, "hello world") {
			t.Fatalf("run %d: transcript = %q, want it to contain \"hello world\"", i+1, res.Text)
		}
		pids = append(pids, workerPID())
		loads = append(loads, workerLoadCount())
		t.Logf("run %d: %.2fs pid=%d loads=%d text=%q", i+1, durations[i].Seconds(), pids[i], loads[i], res.Text)
	}
	stopActiveWorker()

	for i := 1; i < runs; i++ {
		if pids[i] != pids[0] {
			t.Fatalf("run %d used pid %d, want the resident worker pid %d: the model was reloaded", i+1, pids[i], pids[0])
		}
	}
	for i := range loads {
		if loads[i] != 1 {
			t.Fatalf("run %d reported load_count=%d, want 1: the model was loaded more than once", i+1, loads[i])
		}
	}
}

// TestWorkerKeepsModelLoadedAcrossCalls is the same claim measured on the
// warm path: once the model is loaded, later calls must not pay the load.
func TestWorkerKeepsModelLoadedAcrossCalls(t *testing.T) {
	dir := os.Getenv("OCODE_STT_LIVE")
	if dir == "" {
		t.Skip("set OCODE_STT_LIVE to a model directory to run the live worker test")
	}
	wav := absTestWav(t)
	stopActiveWorker()
	modelDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Model: "parakeet-tdt-0.6b-v3", ModelDir: modelDir + "/ocode/stt-models"}

	// First call pays the load.
	start := time.Now()
	if _, err := Transcribe(context.Background(), opts, wav); err != nil {
		t.Fatalf("first: %v", err)
	}
	cold := time.Since(start)

	// Later calls reuse the loaded model.
	var warm time.Duration
	for i := 0; i < 3; i++ {
		start = time.Now()
		if _, err := Transcribe(context.Background(), opts, wav); err != nil {
			t.Fatalf("warm %d: %v", i+1, err)
		}
		warm = time.Since(start)
	}
	stopActiveWorker()

	t.Logf("cold=%.2fs warm=%.2fs", cold.Seconds(), warm.Seconds())
	if warm >= cold {
		t.Fatalf("warm call (%.2fs) was not faster than the cold call (%.2fs): the model is being reloaded", warm.Seconds(), cold.Seconds())
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && foldContains(s, sub)
}

func foldContains(s, sub string) bool {
	ls, lsub := lower(s), lower(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if ls[i:i+len(lsub)] == lsub {
			return true
		}
	}
	return false
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// absTestWav returns the absolute path of the test WAV. The worker runs with
// its working directory set to the model directory, so a relative path would
// not resolve inside the child.
func absTestWav(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "speech-16k.wav"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
