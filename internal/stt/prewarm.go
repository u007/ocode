package stt

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// PrewarmTimeout bounds a prewarm. It is generous because the first load may
// also materialise the model, but it must not outlive the user's patience:
// a prewarm that is still running when the transcription starts is harmless
// (the transcription waits for the same worker), it just means the prewarm
// did not save any time.
const PrewarmTimeout = 5 * time.Minute

// Prewarm starts the resident worker for the selected local model so the
// model is already loaded by the time the user finishes speaking.
//
// Without it, the first utterance after an idle unload pays the model load
// (~1.3 s warm, more on a cold page cache) while the user waits. Prewarming
// overlaps that cost with the recording.
//
// Prewarm is best-effort and never fails the caller: a prewarm failure is
// reported, but the recording continues and the first transcription will
// start the worker itself. It must never prompt for an install — the
// install-approval contract belongs to the transcription path, and a prewarm
// that silently started a pip install would be a surprise.
//
// Prewarm is a no-op for a hosted (OpenAI) model: there is nothing local to
// load.
func Prewarm(ctx context.Context, opts Options) error {
	id := ResolveModel(opts.Model)
	s, _ := lookup(id)
	if s.Engine != EngineLocal {
		return nil
	}
	// The interpreter must already exist. Prewarm must not install anything:
	// the transcription path owns the install-approval flow.
	py, err := localInterpreter(opts.Python, s.Extra)
	if err != nil {
		return fmt.Errorf("speech-to-text: prewarm: %w", err)
	}
	modelDir, err := opts.modelRoot()
	if err != nil {
		return err
	}
	key := localWorkerKey(s.Upstream, s.Repo, filepath.Join(modelDir, id))

	w, err := acquireWorker(ctx, key, py.Command)
	if err != nil {
		return fmt.Errorf("speech-to-text: prewarm: %w", err)
	}
	// The worker is now running with the model loaded. Touch it so the idle
	// timer does not expire while the user is still speaking.
	releaseWorker(w)
	return nil
}
