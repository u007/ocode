package stt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeWorkerPython writes a Python stand-in that speaks the worker protocol.
// It stands in for onnx-asr so the protocol, the crash recovery and the
// request/response correlation can be tested without the real runtime.
//
// The script echoes every request it receives to a log file, so a test can
// assert the model was loaded exactly once and the requests reached the
// worker rather than a fresh process.
func fakeWorkerPython(t *testing.T, logPath string, opts fakeWorkerOptions) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake worker scripts need a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "python3")
	body := `#!/bin/sh
# The interpreter probe runs the version flag and then tries to import
# onnx_asr, so the fake must answer both before it serves the protocol.
case "$1" in
  --version) echo "Python 3.12.0"; exit 0 ;;
esac
# The module-import probe is run as "<python> -I -c \"import onnx_asr\"", so
# the -c flag is NOT the first argument. Scan the arguments for it.
for arg in "$@"; do
  if [ "$arg" = "-c" ]; then
    # A module import probe. Answer with success so the probe accepts this
    # interpreter, without touching the protocol log.
    echo "ok"
    exit 0
  fi
done
LOG="LOGPATH"
# Every process logs its own load, so a restarted worker is visible in the
# log as a second MODEL_LOADED line. A shared marker file would hide it.
echo "MODEL_LOADED" >> "$LOG"
# The handshake, exactly as the real worker sends it.
printf '{"op":"ready","pid":%s,"load_count":1}\n' "$$"
while IFS= read -r line; do
  echo "$line" >> "$LOG"
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  op=$(printf '%s' "$line" | sed -n 's/.*"op":"\([a-z]*\)".*/\1/p')
  if [ "$op" = "quit" ]; then
    printf '{"id":%s,"ok":true}\n' "$id"
    exit 0
  fi
  if [ "$op" = "recognize" ]; then
    printf '{"id":%s,"ok":true,"text":"fake transcript","pid":%s,"load_count":1}\n' "$id" "$$"
  else
    printf '{"id":%s,"ok":true,"ping":true,"pid":%s,"load_count":1}\n' "$id" "$$"
  fi
done
`
	body = strings.ReplaceAll(body, "LOGPATH", logPath)
	if opts.dieAfterFirst {
		marker := `    printf '{"id":%s,"ok":true,"text":"fake transcript","pid":%s,"load_count":1}\n' "$id" "$$"`
		if !strings.Contains(body, marker) {
			t.Fatalf("dieAfterFirst marker not found in the fake body")
		}
		body = strings.ReplaceAll(body, marker, marker+`
    exit 1`)
	}
	if opts.silentHandshake {
		body = strings.ReplaceAll(body,
			`printf '{"op":"ready","pid":%s,"load_count":1}\n' "$$"`,
			`exit 3`)
	}
	if opts.stallHandshake {
		body = strings.ReplaceAll(body,
			`printf '{"op":"ready","pid":%s,"load_count":1}\n' "$$"`,
			`sleep 10
printf '{"op":"ready","pid":%s,"load_count":1}\n' "$$"`)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeWorkerOptions tunes the fake worker's behaviour.
type fakeWorkerOptions struct {
	// dieAfterFirst makes the worker exit after answering one request, which
	// exercises the crash-recovery path.
	dieAfterFirst bool
	// silentHandshake makes the worker exit before answering the handshake.
	silentHandshake bool
	// stallHandshake makes the worker sleep before answering the handshake, so
	// a test can cancel a prewarm while the load is still in flight.
	stallHandshake bool
}

// TestWorkerReusesOneProcessForManyCalls is the protocol-level check that
// repeated requests go to ONE worker process that loaded the model once.
func TestWorkerReusesOneProcessForManyCalls(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	interp := []string{py}
	key := workerKey{upstream: "m", repo: "r", modelDir: t.TempDir(), provider: workerProvider}

	for i := 0; i < 3; i++ {
		w, err := acquireWorker(context.Background(), key, interp)
		if err != nil {
			t.Fatalf("acquire %d: %v", i+1, err)
		}
		text, err := w.recognize(context.Background(), "/tmp/clip.wav")
		if err != nil {
			t.Fatalf("recognize %d: %v", i+1, err)
		}
		if text != "fake transcript" {
			t.Fatalf("recognize %d: text = %q, want %q", i+1, text, "fake transcript")
		}
		releaseWorker(w)
	}

	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	loads := 0
	requests := 0
	for _, line := range lines {
		if line == "MODEL_LOADED" {
			loads++
			continue
		}
		if strings.Contains(line, `"op":"recognize"`) {
			requests++
		}
	}
	if loads != 1 {
		t.Fatalf("model loaded %d times, want 1", loads)
	}
	if requests != 3 {
		t.Fatalf("saw %d recognize requests, want 3", requests)
	}
}

// TestWorkerRestartedAfterModelChange makes sure a different model does not
// reuse the old worker.
func TestWorkerRestartedAfterModelChange(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	interp := []string{py}
	keyA := workerKey{upstream: "m", repo: "r", modelDir: t.TempDir(), provider: workerProvider}
	keyB := keyA
	keyB.modelDir = t.TempDir()

	wA, err := acquireWorker(context.Background(), keyA, interp)
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	releaseWorker(wA)
	wB, err := acquireWorker(context.Background(), keyB, interp)
	if err != nil {
		t.Fatalf("acquire B: %v", err)
	}
	releaseWorker(wB)

	if wA == wB {
		t.Fatal("a different model reused the previous worker")
	}
	if wA.isDead() == false {
		t.Error("the superseded worker was not stopped")
	}
}

// TestWorkerRespawnsAfterCrash makes sure a worker that dies is replaced on
// the next call instead of poisoning every later call.
func TestWorkerRespawnsAfterCrash(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{dieAfterFirst: true})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	interp := []string{py}
	key := workerKey{upstream: "m", repo: "r", modelDir: t.TempDir(), provider: workerProvider}

	w, err := acquireWorker(context.Background(), key, interp)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if _, err := w.recognize(context.Background(), "/tmp/clip.wav"); err != nil {
		t.Fatalf("first recognize: %v", err)
	}
	// The fake worker exits after the first answer, so the next call must
	// observe a dead worker and start a new one. Wait for the child to be
	// reaped by the OS before checking.
	releaseWorker(w)
	waitForWorkerExit(t, w)

	w2, err := acquireWorker(context.Background(), key, interp)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	defer releaseWorker(w2)
	if w2 == w {
		t.Fatal("a dead worker was handed back")
	}
	// The replacement must be a genuinely new process, and the new worker
	// must serve the request. Asserting only that the pointer differs would
	// pass even if the dead worker were still handed out.
	if w2.pid == w.pid {
		t.Fatalf("the replacement reused pid %d", w2.pid)
	}
	text, err := w2.recognize(context.Background(), "/tmp/clip.wav")
	if err != nil {
		t.Fatalf("recognize on the replacement: %v", err)
	}
	if text != "fake transcript" {
		t.Fatalf("replacement transcript = %q, want %q", text, "fake transcript")
	}
}

// TestWorkerProtocolLinesAreJSON makes sure the request encoding matches the
// worker's expectations.
func TestWorkerProtocolLinesAreJSON(t *testing.T) {
	line, err := json.Marshal(workerRequest{Op: "recognize", ID: 7, Path: "/tmp/a.wav"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(line); got != `{"op":"recognize","id":7,"path":"/tmp/a.wav"}` {
		t.Fatalf("request = %s", got)
	}
}

// TestWorkerIdleTimeoutStopsWorker makes sure a resident model does not
// outlive the user's session.
func TestWorkerIdleTimeoutStopsWorker(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	interp := []string{py}
	key := workerKey{upstream: "m", repo: "r", modelDir: t.TempDir(), provider: workerProvider}
	w, err := acquireWorker(context.Background(), key, interp)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// Pretend the worker has been idle well past the timeout.
	w.touch()
	w.lastUse.Store(time.Now().Add(-2 * workerIdleTimeout).UnixNano())
	releaseWorker(w)

	if workerPID() != 0 {
		t.Fatal("an idle worker was kept alive past the timeout")
	}
}

// TestWorkerIdleTimerUnloadsWithoutFurtherRequests proves an unused worker is
// unloaded by the timer, not only when a later request is released.
func TestWorkerIdleTimerUnloadsWithoutFurtherRequests(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	key := workerKey{upstream: "m", repo: "r", modelDir: t.TempDir(), provider: workerProvider}
	w, err := acquireWorker(context.Background(), key, []string{py})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// Idle for all but 50 ms of the timeout, then released: the timer must fire.
	w.lastUse.Store(time.Now().Add(-workerIdleTimeout + 50*time.Millisecond).UnixNano())
	releaseWorker(w)
	if workerPID() == 0 {
		t.Fatal("worker stopped before the idle timeout elapsed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for workerPID() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("idle timer never unloaded the worker")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForWorkerExit waits for the worker process to disappear. It watches
// the reaper's exit flag directly, not isDead(), so the wait does not depend
// on the liveness logic under test.
func waitForWorkerExit(t *testing.T, w *worker) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w.exitedMu.Lock()
		exited := w.exited
		w.exitedMu.Unlock()
		if exited {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the worker process did not exit")
}

// TestWorkerBusySkipsPartial makes sure a live preview gives up at once when
// the worker is already serving a request, instead of queueing behind it.
// The final transcription is the one the user is waiting for; a stale
// preview must not delay it.
func TestWorkerBusySkipsPartial(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	interp := []string{py}
	key := workerKey{upstream: "m", repo: "r", modelDir: t.TempDir(), provider: workerProvider}
	w, err := acquireWorker(context.Background(), key, interp)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer releaseWorker(w)

	// Hold the worker's request lock, as an in-flight decode would.
	w.mu.Lock()
	_, err = w.recognizeIfIdle(context.Background(), "/tmp/clip.wav")
	w.mu.Unlock()
	if !errors.Is(err, ErrWorkerBusy) {
		t.Fatalf("err = %v, want ErrWorkerBusy", err)
	}

	// A blocking request would wait for the lock; the preview must not.
	w.mu.Lock()
	start := time.Now()
	_, err = w.recognizeIfIdle(context.Background(), "/tmp/clip.wav")
	w.mu.Unlock()
	if !errors.Is(err, ErrWorkerBusy) {
		t.Fatalf("err = %v, want ErrWorkerBusy", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the busy check took %s, want an immediate give-up", elapsed)
	}
}

// TestWorkerExitsWhenStdinCloses covers the orphan guard on platforms without
// a parent-death signal (macOS, the BSDs).
//
// Go's syscall package only exposes Pdeathsig on Linux, so on macOS an
// abruptly killed ocode could leave the worker running, holding a couple of
// gigabytes of model weights. The worker reads its requests from stdin, and a
// pipe whose write end closes reports EOF, which ends the request loop and
// exits the process. This test proves that path works: closing stdin must
// terminate the worker.
func TestWorkerExitsWhenStdinCloses(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	interp := []string{py}
	key := workerKey{upstream: "m", repo: "r", modelDir: t.TempDir(), provider: workerProvider}
	w, err := acquireWorker(context.Background(), key, interp)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Close the request channel, exactly as a dead parent would.
	if err := w.stdin.Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		w.exitedMu.Lock()
		exited := w.exited
		w.exitedMu.Unlock()
		if exited {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the worker did not exit after its stdin closed: it would be orphaned when ocode dies")
}

// TestRealWorkerScriptExitsOnStdinEOF runs the REAL embedded worker script
// (with its dependencies stubbed) and proves it exits when stdin closes.
//
// This is the orphan guard on macOS and the BSDs, where Go's syscall package
// exposes no parent-death signal. The fake-worker test above cannot prove it:
// a shell `while read` loop ends at EOF by shell semantics, so it would pass
// even if the production Python loop ignored EOF. This test runs the actual
// script, so the loop under test is the one that ships.
func TestRealWorkerScriptExitsOnStdinEOF(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub script is POSIX-only")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found")
	}
	script := filepath.Join("testdata", "worker_eof_check.py")
	out, err := exec.Command(python, script).CombinedOutput()
	if err != nil {
		t.Fatalf("the worker did not exit on stdin EOF: %v\n%s", err, out)
	}
}

// TestWorkerStartsWhenTheModelDirectoryIsMissing covers the working
// directory fallback.
//
// The worker pins its working directory to the model directory so ONNX
// Runtime's telemetry sidecar cannot land in the user's project. That
// directory does not exist before the model's first download, and exec fails
// with a bare ENOENT when cmd.Dir is missing — so the worker must fall back
// to the parent directory rather than refusing to start.
func TestWorkerStartsWhenTheModelDirectoryIsMissing(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	// A model directory that does not exist, under a parent that does.
	parent := t.TempDir()
	missing := filepath.Join(parent, "parakeet-not-downloaded-yet")
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("the test's model directory unexpectedly exists")
	}

	key := workerKey{upstream: "m", repo: "r", modelDir: missing, provider: workerProvider}
	w, err := acquireWorker(context.Background(), key, []string{py})
	if err != nil {
		t.Fatalf("the worker did not start with a missing model directory: %v", err)
	}
	defer releaseWorker(w)

	text, err := w.recognize(context.Background(), "/tmp/clip.wav")
	if err != nil {
		t.Fatalf("recognize: %v", err)
	}
	if text != "fake transcript" {
		t.Fatalf("text = %q, want %q", text, "fake transcript")
	}
}

// TestWorkerWorkingDirectoryIsNeverTheCallersDirectory checks the other half
// of the contract: the fallback must not let the child run in the user's
// project directory.
func TestWorkerWorkingDirectoryIsNeverTheCallersDirectory(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	// A model directory that exists, to exercise the normal path.
	modelDir := t.TempDir()
	key := workerKey{upstream: "m", repo: "r", modelDir: modelDir, provider: workerProvider}
	w, err := acquireWorker(context.Background(), key, []string{py})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer releaseWorker(w)
	if w.cmd.Dir != modelDir {
		t.Fatalf("working directory = %q, want the model directory %q", w.cmd.Dir, modelDir)
	}
}

// TestIdleTimerDoesNotMakePreviewBusy proves the idle timer never takes the
// request lock. An idle worker must answer every live preview: a timer that
// briefly held the lock made recognizeIfIdle return ErrWorkerBusy while the
// worker was idle.
func TestIdleTimerDoesNotMakePreviewBusy(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.log")
	py := fakeWorkerPython(t, log, fakeWorkerOptions{})
	stopActiveWorker()
	t.Cleanup(stopActiveWorker)

	key := workerKey{upstream: "m", repo: "r", modelDir: t.TempDir(), provider: workerProvider}
	w, err := acquireWorker(context.Background(), key, []string{py})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	releaseWorker(w)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				unloadIfIdle(w)
				time.Sleep(50 * time.Microsecond)
			}
		}
	}()
	for i := 0; i < 200; i++ {
		if _, err := w.recognizeIfIdle(context.Background(), "/tmp/clip.wav"); errors.Is(err, ErrWorkerBusy) {
			close(stop)
			<-done
			t.Fatalf("preview %d refused as busy while the worker was idle", i)
		} else if err != nil {
			close(stop)
			<-done
			t.Fatalf("preview %d: %v", i, err)
		}
	}
	close(stop)
	<-done
	if workerPID() == 0 {
		t.Fatal("worker was unloaded by the timer although it was not idle for the timeout")
	}
}
