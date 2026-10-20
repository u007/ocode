package stt

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// localWorkerKey builds the worker identity for a model. Prewarm and
// Transcribe must agree on it, or prewarming would start one worker and the
// first transcription would start another.
func localWorkerKey(upstream, repo, modelDir string) workerKey {
	return workerKey{
		upstream: upstream,
		repo:     repo,
		modelDir: modelDir,
		provider: workerProvider,
	}
}

// transcribeLocal decodes a WAV with the local engine.
//
// It prefers the resident worker, which holds the model loaded, so a burst of
// utterances and the three-second live-partial ticks do not each pay the
// model load. The one-shot path stays as the fallback for the cases the
// worker cannot serve:
//
//   - the worker could not be started (no writable cache dir, an exhausted
//     process table), or
//   - the worker failed in a way a fresh process would not (a broken pipe
//     after the worker died).
//
// The fallback is a deliberate, narrow recovery: a worker failure is reported
// to the caller, and only the specific failures below retry one-shot. Every
// other error is surfaced as-is, so a broken model never silently becomes a
// different model's output.
//
// The install-approval contract is unaffected: the caller has already resolved
// the interpreter (and therefore already returned *InstallRequiredError when
// approval was missing) before this function runs.
//
// partial marks a live preview decode. A preview is display-only and is
// superseded moments later, so it never queues behind another request: it
// gives up at once when the worker is busy, and it never starts a worker of
// its own.
func transcribeLocal(ctx context.Context, interpreter []string, repo, upstream, modelDir, wavPath string, partial bool) (string, error) {
	key := localWorkerKey(upstream, repo, modelDir)

	// The worker runs with its working directory set to the model directory,
	// so a relative clip path would not resolve inside the child.
	if abs, err := filepath.Abs(wavPath); err == nil {
		wavPath = abs
	}

	w, err := acquireWorker(ctx, key, interpreter)
	if err == nil {
		var text string
		var werr error
		if partial {
			text, werr = w.recognizeIfIdle(ctx, wavPath)
		} else {
			text, werr = w.recognize(ctx, wavPath)
		}
		releaseWorker(w)
		if werr == nil {
			return text, nil
		}
		// A busy worker is not a failure for a preview: the next tick will
		// produce a fresher one.
		if partial && errors.Is(werr, ErrWorkerBusy) {
			return "", werr
		}
		// The worker died mid-request. A fresh process may still work (the
		// crash may have been the model file being replaced), so retry once
		// one-shot and report the original error if that also fails.
		if isWorkerGone(werr) {
			text, ferr := transcribeOnnx(ctx, interpreter, repo, upstream, modelDir, wavPath)
			if ferr == nil {
				return text, nil
			}
			return "", fmt.Errorf("%w (one-shot retry also failed: %v)", werr, ferr)
		}
		return "", werr
	}

	// The worker could not be started. Fall back to the one-shot path so the
	// feature keeps working; the worker failure is not fatal on its own.
	text, ferr := transcribeOnnx(ctx, interpreter, repo, upstream, modelDir, wavPath)
	if ferr != nil {
		return "", fmt.Errorf("%w (worker also unavailable: %v)", ferr, err)
	}
	return text, nil
}

// workerProvider is the onnxruntime execution provider the worker pins.
//
// The CPU provider is deliberate. onnx-asr otherwise selects every available
// provider, and CoreML compiles the model to the Apple Neural Engine on every
// fresh process, which cost about twelve seconds per start while supporting
// only part of the graph.
const workerProvider = "CPUExecutionProvider"

// isWorkerGone reports whether the error means the worker process died, as
// opposed to a request the worker answered with an error.
func isWorkerGone(err error) bool {
	if err == nil {
		return false
	}
	// A worker-reported error is a structured failure of the request, not a
	// dead process.
	var we *workerError
	if errors.As(err, &we) {
		return false
	}
	return true
}
