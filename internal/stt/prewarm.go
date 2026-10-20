package stt

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// PrewarmTimeout bounds a prewarm. It is generous because the first load may
// also materialise the model, but it must not outlive the user's patience:
// a prewarm that is still running when the transcription starts is harmless
// (the transcription waits for the same worker), it just means the prewarm
// did not save any time.
const PrewarmTimeout = 5 * time.Minute

// prewarmToken identifies one in-flight prewarm, so a cancellation cannot
// stop a newer prewarm that replaced it.
type prewarmToken struct {
	cancel context.CancelFunc
}

// prewarmGuard tracks the in-flight prewarm.
//
// The worker is a per-process singleton, so this is process-wide: cancelling
// stops the load in this process, and the next transcription starts a worker
// itself. A prewarm is best-effort, so losing one costs latency, never
// correctness.
var prewarmGuard struct {
	mu    sync.Mutex
	token *prewarmToken
}

// CancelPrewarm aborts an in-flight prewarm and reports whether one was
// running.
//
// A recording can be discarded before its model finishes loading, and there is
// no reason to keep roughly 2 GB resident for a clip that will never be
// transcribed. A prewarm that has already finished is unaffected: its token is
// cleared before Prewarm returns, so a cancellation arriving after the load is
// a no-op and the warm worker is kept for the next utterance.
//
// Only a LOAD in progress can be interrupted. A worker that is already serving
// a transcription is never touched.
func CancelPrewarm() bool {
	prewarmGuard.mu.Lock()
	t := prewarmGuard.token
	prewarmGuard.token = nil
	prewarmGuard.mu.Unlock()
	if t == nil {
		return false
	}
	t.cancel()
	return true
}

func registerPrewarm(cancel context.CancelFunc) *prewarmToken {
	t := &prewarmToken{cancel: cancel}
	prewarmGuard.mu.Lock()
	prewarmGuard.token = t
	prewarmGuard.mu.Unlock()
	return t
}

func unregisterPrewarm(t *prewarmToken) {
	prewarmGuard.mu.Lock()
	if prewarmGuard.token == t {
		prewarmGuard.token = nil
	}
	prewarmGuard.mu.Unlock()
}

// Prewarm starts the resident worker for the selected local model so the
// model is already loaded by the time the user finishes speaking.
//
// Without it, the first utterance after an idle unload pays the model load
// (~1.3 s warm, more on a cold page cache) while the user waits. Prewarming
// overlaps that cost with the recording.
//
// Prewarm is best-effort and never fails the caller: a prewarm failure is
// reported, but the recording continues and the first transcription will
// start the worker itself. A prewarm stopped through CancelPrewarm returns nil
// — the caller asked for it to stop, so it is not a failure.
//
// It must never prompt for an install: the install-approval contract belongs
// to the transcription path, and a prewarm that silently started a pip install
// would be a surprise.
//
// Prewarm is a no-op for a hosted (OpenAI) model: there is nothing local to
// load.
func Prewarm(ctx context.Context, opts Options) error {
	id := ResolveModel(opts.Model)
	s, _ := lookup(id)
	if s.Engine != EngineLocal {
		return nil
	}

	// Wrapped so CancelPrewarm can abort a load the user no longer needs. The
	// cancellation is registered before the interpreter scan, which can take a
	// second on a cold probe cache.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	token := registerPrewarm(cancel)
	defer unregisterPrewarm(token)

	// The interpreter must already exist. Prewarm must not install anything:
	// the transcription path owns the install-approval flow.
	py, err := localInterpreter(opts.Python, s.Extra)
	if err != nil {
		return fmt.Errorf("speech-to-text: prewarm: %w", err)
	}
	// A cancel during the scan stops the prewarm before a worker is spawned.
	// (The scan does not take this context: a successful scan is shared with
	// the transcription path, which wants it cached.)
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("speech-to-text: prewarm: %w", err)
	}
	modelDir, err := opts.modelRoot()
	if err != nil {
		return err
	}
	key := localWorkerKey(s.Upstream, s.Repo, filepath.Join(modelDir, id))

	w, err := acquireWorker(ctx, key, py.Command)
	if err != nil {
		// A cancelled start is the requested outcome, not a failure. The
		// worker was killed before it could be registered as active, so the
		// next transcription starts its own.
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("speech-to-text: prewarm: %w", err)
	}
	// The worker is now running with the model loaded. Touch it so the idle
	// timer does not expire while the user is still speaking.
	releaseWorker(w)
	return nil
}
