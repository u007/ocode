package stt

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The local engine used to spawn a fresh Python process per utterance and
// reloaded the model every time. Loading the Parakeet model costs about a
// second even on the CPU provider, and the live partial preview asks for a
// decode every three seconds, so a single dictation paid that load several
// times over.
//
// The worker keeps one Python process alive with the model already loaded and
// serves line-delimited JSON over stdin/stdout. The protocol is deliberately
// trivial: one request per line, one response per line, correlated by id.
//
// Ownership: the worker is a package-level singleton per process. The TUI and
// the web server are separate processes and each owns its own worker, which
// matches how a remote project's STT already runs on the host
// (`serve --remote`). Nothing is shared across processes, so there is no
// port to collide on and no socket to authenticate.

const (
	// workerIdleTimeout unloads the worker after this long without a
	// request. A resident model costs real memory (a couple of GB for the
	// fp32 Parakeet export), so it must not outlive the user's session.
	workerIdleTimeout = 2 * time.Minute
	// workerLoadTimeout bounds the handshake, which includes downloading the
	// model on a first run and can therefore take minutes.
	workerLoadTimeout = 10 * time.Minute
	// workerRequestTimeout bounds one recognition.
	workerRequestTimeout = 3 * time.Minute
	// workerStderrCap bounds the captured stderr buffer.
	workerStderrCap = 8 << 10
)

// workerRequest is one line sent to the worker.
type workerRequest struct {
	Op   string `json:"op"`
	ID   int    `json:"id"`
	Path string `json:"path,omitempty"`
}

// workerResponse is one line read back from the worker.
type workerResponse struct {
	Op        string `json:"op"`
	ID        int    `json:"id"`
	OK        bool   `json:"ok"`
	Text      string `json:"text"`
	Error     string `json:"error"`
	PID       int    `json:"pid"`
	LoadCount int    `json:"load_count"`
}

// worker is a resident Python process holding the loaded model.
type worker struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *ringBuffer

	// key is the model identity this worker was started for.
	key workerKey

	mu      sync.Mutex
	nextID  int
	lastUse time.Time
	pid     int
	loads   int
	dead    bool

	// exited is set by the reaper goroutine once the child has been waited
	// on. A crashed worker is a zombie until it is reaped, and a zombie
	// still answers a signal probe, so liveness must come from the wait.
	exitedMu sync.Mutex
	exited   bool
}

// workerState is the package-level singleton plus the lock that guards it.
var workerState struct {
	mu     sync.Mutex
	active *worker
}

// workerKey identifies the model a worker was started for. Changing the model
// (or its directory) invalidates the worker: the old process is stopped and a
// new one loads the new model.
type workerKey struct {
	upstream string
	repo     string
	modelDir string
	// provider is part of the key because it changes what the worker loads.
	provider string
}

// workerError is a failure reported by the worker process itself.
type workerError struct {
	msg string
}

func (e *workerError) Error() string { return e.msg }

// ErrWorkerBusy is returned when a partial decode is requested while another
// request holds the worker. A live preview that is a moment stale is
// worthless, so the caller skips it rather than queueing behind the final
// transcription.
var ErrWorkerBusy = errors.New("speech-to-text: worker busy")

// workerScript is the embedded Python worker.
//
//go:embed stt_worker.py
var workerScript string

// workerScriptPath writes the embedded worker script to a stable per-user
// location and returns its path. The file is rewritten whenever the embedded
// content changes, so an upgrade picks up a new worker without a cache purge.
func workerScriptPath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("speech-to-text: no cache dir: %w", err)
	}
	dir := filepath.Join(base, "ocode", "stt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("speech-to-text: create worker dir: %w", err)
	}
	path := filepath.Join(dir, "stt_worker.py")
	want := []byte(workerScript)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(want) {
		return path, nil
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		return "", fmt.Errorf("speech-to-text: write worker script: %w", err)
	}
	return path, nil
}

// acquireWorker returns a live worker for key, starting one if needed. The
// caller must call releaseWorker with the returned worker when done.
//
// The returned worker is exclusively owned by the caller until it is released.
func acquireWorker(ctx context.Context, key workerKey, interpreter []string) (*worker, error) {
	workerState.mu.Lock()
	defer workerState.mu.Unlock()

	if w := workerState.active; w != nil {
		if w.modelKey() == key && !w.isDead() {
			w.touch()
			return w, nil
		}
		// A different model, or a worker that died: stop the old one.
		stopWorkerLocked(w)
	}

	w, err := startWorker(ctx, key, interpreter)
	if err != nil {
		return nil, err
	}
	workerState.active = w
	return w, nil
}

// releaseWorker marks the worker idle and stops it if it has been unused for
// longer than workerIdleTimeout.
func releaseWorker(w *worker) {
	if w == nil {
		return
	}
	workerState.mu.Lock()
	defer workerState.mu.Unlock()
	if workerState.active != w {
		return
	}
	// The idle clock is NOT reset here: activity was already recorded when
	// the request was made. Resetting it on release would make the timeout
	// unreachable, and the resident model would never be unloaded.
	w.mu.Lock()
	idle := time.Since(w.lastUse)
	w.mu.Unlock()
	if idle >= workerIdleTimeout {
		stopWorkerLocked(w)
	}
}

// stopWorkerLocked stops a worker and clears it as the active one. The caller
// holds workerState.mu.
func stopWorkerLocked(w *worker) {
	if w == nil {
		return
	}
	w.dead = true
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		// The reaper goroutine owns the Wait, so the child is collected
		// there and does not linger as a zombie.
	}
	if workerState.active == w {
		workerState.active = nil
	}
}

// startWorker spawns the Python worker and waits for its handshake.
func startWorker(ctx context.Context, key workerKey, interpreter []string) (*worker, error) {
	script, err := workerScriptPath()
	if err != nil {
		return nil, err
	}
	args := append(append([]string{}, interpreter[1:]...), "-I", script, key.upstream, key.repo, key.modelDir)
	cmd := exec.Command(interpreter[0], args...)
	// Pin the working directory to the model directory: ONNX Runtime writes a
	// telemetry sidecar into the process working directory when it initialises
	// its environment, and the working directory here is the user's project.
	//
	// The directory may not exist yet (a first run, or a model whose files
	// were removed). exec fails with a bare ENOENT when cmd.Dir is missing,
	// so fall back to the parent directory, which the worker itself creates.
	// Either way the child never runs in the user's project directory.
	cmd.Dir = key.modelDir
	if fi, err := os.Stat(key.modelDir); err != nil || !fi.IsDir() {
		cmd.Dir = filepath.Dir(key.modelDir)
	}
	cmd.Env = append(os.Environ(), "ORT_DISABLE_TELEMETRY=1", "HF_HUB_OFFLINE=1")
	// The child must not inherit the terminal: the TUI runs in the alt-screen
	// and a raw write there corrupts the frame.
	cmd.SysProcAttr = workerProcAttr()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("speech-to-text: worker stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("speech-to-text: worker stdout: %w", err)
	}
	stderr := &ringBuffer{cap: workerStderrCap}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("speech-to-text: start worker: %w", err)
	}

	w := &worker{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewReader(stdout),
		stderr:  stderr,
		key:     key,
		pid:     cmd.Process.Pid,
		lastUse: time.Now(),
	}
	// Reap the child in the background. Without this the worker becomes a
	// zombie when it exits, and a zombie cannot be distinguished from a live
	// process by a signal probe.
	go w.reap()

	loadCtx, cancel := context.WithTimeout(ctx, workerLoadTimeout)
	defer cancel()
	ready, err := w.readResponse(loadCtx)
	if err != nil {
		stopWorkerLocked(w)
		return nil, fmt.Errorf("speech-to-text: worker failed to start: %w%s", err, w.stderrSuffix())
	}
	if ready.Op != "ready" {
		stopWorkerLocked(w)
		return nil, fmt.Errorf("speech-to-text: worker handshake: unexpected response %q%s", ready.Op, w.stderrSuffix())
	}
	w.loads = ready.LoadCount
	return w, nil
}

// recognizeIfIdle asks the worker to transcribe path, but gives up at once if
// another request is already in flight.
//
// A live partial preview uses this. The preview is display-only and is
// superseded by the next one a moment later, so a stale preview is worth
// nothing: it must never queue behind the final transcription, which is the
// one the user is waiting for.
func (w *worker) recognizeIfIdle(ctx context.Context, path string) (string, error) {
	if !w.mu.TryLock() {
		return "", ErrWorkerBusy
	}
	defer w.mu.Unlock()
	return w.recognizeLocked(ctx, path)
}

// recognize asks the worker to transcribe path.
func (w *worker) recognize(ctx context.Context, path string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.recognizeLocked(ctx, path)
}

// recognizeLocked performs one request. The caller holds w.mu.
func (w *worker) recognizeLocked(ctx context.Context, path string) (string, error) {
	if w.dead {
		return "", errors.New("speech-to-text: worker is not running")
	}
	w.nextID++
	id := w.nextID
	line, err := json.Marshal(workerRequest{Op: "recognize", ID: id, Path: path})
	if err != nil {
		return "", err
	}
	if _, err := w.stdin.Write(append(line, '\n')); err != nil {
		w.dead = true
		return "", fmt.Errorf("speech-to-text: worker write: %w%s", err, w.stderrSuffix())
	}
	resp, err := w.readResponse(ctx)
	if err != nil {
		w.dead = true
		return "", fmt.Errorf("speech-to-text: worker: %w%s", err, w.stderrSuffix())
	}
	if !resp.OK {
		return "", &workerError{msg: resp.Error}
	}
	return resp.Text, nil
}

// readResponse reads one JSON line, honouring ctx for the wait.
func (w *worker) readResponse(ctx context.Context) (workerResponse, error) {
	type result struct {
		resp workerResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := w.stdout.ReadBytes('\n')
		if err != nil {
			ch <- result{err: err}
			return
		}
		var resp workerResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			ch <- result{err: fmt.Errorf("bad worker response: %w", err)}
			return
		}
		ch <- result{resp: resp}
	}()
	select {
	case <-ctx.Done():
		return workerResponse{}, ctx.Err()
	case res := <-ch:
		return res.resp, res.err
	}
}

// reap waits for the worker process and records that it exited. It is the
// only reliable liveness signal: a child that has exited but has not been
// waited on stays in the process table as a zombie and still answers a
// signal probe.
func (w *worker) reap() {
	if w.cmd == nil {
		return
	}
	_ = w.cmd.Wait()
	w.exitedMu.Lock()
	w.exited = true
	w.exitedMu.Unlock()
}

// modelKey returns the worker's model identity.
func (w *worker) modelKey() workerKey {
	return w.key
}

// isDead reports whether the worker process has exited.
func (w *worker) isDead() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dead {
		return true
	}
	if w.cmd == nil {
		return true
	}
	// A signal probe is not a liveness test: an exited-but-unreaped child is
	// a zombie and still answers signal 0. Liveness comes from the reaper
	// goroutine, which Waits the child and records the exit.
	//
	// cmd.ProcessState is deliberately NOT read here: the reaper's Wait
	// writes it concurrently, which is a data race.
	w.exitedMu.Lock()
	exited := w.exited
	w.exitedMu.Unlock()
	return exited
}

// touch records activity.
func (w *worker) touch() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastUse = time.Now()
}

// stderrSuffix returns the captured stderr, for error messages.
func (w *worker) stderrSuffix() string {
	msg := strings.TrimSpace(w.stderr.String())
	if msg == "" {
		return ""
	}
	if len(msg) > 400 {
		msg = msg[len(msg)-400:]
	}
	return ": " + msg
}

// ringBuffer is a bounded buffer for a child's stderr. It keeps the tail,
// which is where a Python traceback puts the actual error.
type ringBuffer struct {
	mu  sync.Mutex
	buf []byte
	cap int
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.cap {
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

// stopActiveWorker stops the resident worker, if any. It exists for tests and
// for a process that is shutting down.
func stopActiveWorker() {
	workerState.mu.Lock()
	defer workerState.mu.Unlock()
	if workerState.active != nil {
		stopWorkerLocked(workerState.active)
	}
}

// workerPID returns the resident worker's process id, or 0 when no worker is
// running. It is used by the live test to prove the same process served every
// call.
func workerPID() int {
	workerState.mu.Lock()
	defer workerState.mu.Unlock()
	if workerState.active == nil {
		return 0
	}
	return workerState.active.pid
}

// workerLoadCount returns the number of times the resident worker loaded the
// model. A healthy worker loads exactly once, no matter how many requests it
// serves.
func workerLoadCount() int {
	workerState.mu.Lock()
	defer workerState.mu.Unlock()
	if workerState.active == nil {
		return 0
	}
	return workerState.active.loads
}
