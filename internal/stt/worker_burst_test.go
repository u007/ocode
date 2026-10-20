package stt

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestWorkerPartialBurstIsCheap measures the case that motivated the worker:
// a dictation that triggers several live previews and then a final
// transcription.
//
// Before the worker, each of those was a separate process that reloaded the
// model. With the worker, only the first one pays the load.
func TestWorkerPartialBurstIsCheap(t *testing.T) {
	dir := os.Getenv("OCODE_STT_LIVE")
	if dir == "" {
		t.Skip("set OCODE_STT_LIVE to a model directory to run the live worker test")
	}
	wav := absTestWav(t)
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	modelDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Model: "parakeet-tdt-0.6b-v3", ModelDir: modelDir + "/ocode/stt-models"}

	// One cold call, then the burst a 12-second dictation produces:
	// a preview every three seconds, then the final transcription.
	if _, err := Transcribe(context.Background(), opts, wav); err != nil {
		t.Fatalf("cold: %v", err)
	}
	var total time.Duration
	const partials = 4
	for i := 0; i < partials; i++ {
		start := time.Now()
		if _, err := Transcribe(context.Background(), opts, wav); err != nil {
			t.Fatalf("burst %d: %v", i+1, err)
		}
		total += time.Since(start)
	}
	start := time.Now()
	if _, err := Transcribe(context.Background(), opts, wav); err != nil {
		t.Fatalf("final: %v", err)
	}
	total += time.Since(start)
	stopActiveWorker()

	t.Logf("%d calls took %.2fs total (%.2fs each)", partials+1, total.Seconds(), total.Seconds()/float64(partials+1))
}
