package tts

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/u007/ocode/internal/tool"
)

const kyutaiScriptName = "kyutai_worker.py"

// kyutaiWorkerScript is the resident synthesis process. See kyutai_worker.py.
//
//go:embed kyutai_worker.py
var kyutaiWorkerScript string

// kyutaiWorkerIdleTimeout stops the resident worker after this long without a
// request, so the loaded model does not outlive the listening session. It
// mirrors the speech-to-text worker.
const kyutaiWorkerIdleTimeout = 2 * time.Minute

// kyutaiWorkerProcID is the stable supervisor id of the resident worker.
const kyutaiWorkerProcID = "tts-kyutai-worker"

// kyutaiWorkerStderrCap bounds the captured stderr. The worker lives for the
// whole idle window, so its stderr must not grow without bound.
const kyutaiWorkerStderrCap = 8 << 10

type kyutaiWorkerRequest struct {
	ID    int    `json:"id"`
	Op    string `json:"op"`
	Voice string `json:"voice"`
	Text  string `json:"text"`
	Out   string `json:"out"`
}

type kyutaiWorkerReply struct {
	ID    int    `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// kyutaiWorker is one resident Pocket TTS process. It loads the model once and
// synthesises one request at a time.
type kyutaiWorker struct {
	// key is the cache directory the worker was started from. A different
	// directory (another engine version) replaces the worker.
	key    string
	cmd    *exec.Cmd
	stdin  io.Writer
	stdout *bufio.Reader
	stderr *tailBuffer

	// slot is held from the moment a request is written until its reply has
	// been read. A caller that gives up early leaves the slot held, so the
	// next request cannot read the abandoned reply.
	slot   chan struct{}
	nextID int // guarded by slot
	dead   atomic.Bool
	// exited is closed by reap once the child has been waited on and its
	// supervisor record is terminal.
	exited chan struct{}

	// users and idle are guarded by kyutaiWorkerState.mu.
	users int
	idle  *time.Timer
}

// kyutaiWorkerState is the package singleton: at most one resident worker.
var kyutaiWorkerState struct {
	mu     sync.Mutex
	active *kyutaiWorker
}

// kyutaiSynth renders text through the resident Kyutai worker. The model and
// the voice prompt are loaded once per worker, not once per utterance.
func kyutaiSynth(ctx context.Context, sup *tool.ProcessSupervisor, root string, m Manifest, text, outPath, voice string) error {
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		return err
	}
	if voice == "" {
		voice = "alba"
	}
	w, err := checkoutKyutaiWorker(sup, dir)
	if err != nil {
		return err
	}
	defer checkinKyutaiWorker(w)
	if err := w.synth(ctx, kyutaiWorkerRequest{Op: "synth", Voice: voice, Text: text, Out: outPath}); err != nil {
		return err
	}
	info, err := os.Stat(outPath)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("kyutai produced no audio: %s", w.stderrText())
	}
	return nil
}

// checkoutKyutaiWorker returns the resident worker for dir, starting it when no
// worker runs for that directory. Every checkout is paired with a checkin.
func checkoutKyutaiWorker(sup *tool.ProcessSupervisor, dir string) (*kyutaiWorker, error) {
	kyutaiWorkerState.mu.Lock()
	defer kyutaiWorkerState.mu.Unlock()
	w := kyutaiWorkerState.active
	if w != nil && w.key == dir && !w.dead.Load() {
		w.users++
		return w, nil
	}
	if w != nil {
		w.stop()
		kyutaiWorkerState.active = nil
	}
	nw, err := startKyutaiWorker(sup, dir)
	if err != nil {
		return nil, err
	}
	nw.users = 1
	kyutaiWorkerState.active = nw
	return nw, nil
}

// checkinKyutaiWorker releases a checkout and restarts the idle clock.
func checkinKyutaiWorker(w *kyutaiWorker) {
	kyutaiWorkerState.mu.Lock()
	defer kyutaiWorkerState.mu.Unlock()
	w.users--
	if w.users == 0 {
		w.idle.Reset(kyutaiWorkerIdleTimeout)
	}
}

// stopIdleKyutaiWorker is the idle timer's callback. It stops the worker only
// if nothing has checked it out since the timer was armed.
func stopIdleKyutaiWorker(w *kyutaiWorker) {
	kyutaiWorkerState.mu.Lock()
	defer kyutaiWorkerState.mu.Unlock()
	if kyutaiWorkerState.active != w || w.users > 0 {
		return
	}
	w.stop()
	kyutaiWorkerState.active = nil
}

// startKyutaiWorker writes the worker script into dir and launches it under the
// process supervisor. The model loads on the first request, so the cost is
// paid by the first utterance and not by every one after it.
func startKyutaiWorker(sup *tool.ProcessSupervisor, dir string) (*kyutaiWorker, error) {
	scriptPath, err := writeKyutaiWorkerScript(dir)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(venvPython(filepath.Join(dir, "venv")), scriptPath)
	applySynthProcessEnv(cmd, dir)
	// Weights were fetched at install time (kyutaiPrefetchScript). Offline
	// loading skips the hub round-trip that otherwise stalls every start.
	cmd.Env = append(cmd.Env, "HF_HUB_OFFLINE=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("kyutai worker stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("kyutai worker stdout: %w", err)
	}
	stderr := &tailBuffer{cap: kyutaiWorkerStderrCap}
	cmd.Stderr = stderr
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:              kyutaiWorkerProcID,
		Name:            "Kyutai worker",
		Command:         "python kyutai worker",
		Kind:            tool.ProcessKindTTS,
		ReplaceTerminal: true,
	}); err != nil {
		return nil, fmt.Errorf("start kyutai worker: %w", err)
	}
	w := &kyutaiWorker{
		key:    dir,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		stderr: stderr,
		slot:   make(chan struct{}, 1),
		exited: make(chan struct{}),
	}
	w.idle = time.AfterFunc(kyutaiWorkerIdleTimeout, func() { stopIdleKyutaiWorker(w) })
	go w.reap(sup)
	return w, nil
}

// writeKyutaiWorkerScript writes the worker script via a unique temp file and a
// rename, so a concurrent start never reads a half-written script.
func writeKyutaiWorkerScript(dir string) (string, error) {
	scriptPath := filepath.Join(dir, kyutaiScriptName)
	tmp, err := os.CreateTemp(dir, kyutaiScriptName+".*")
	if err != nil {
		return "", fmt.Errorf("write kyutai worker script: %w", err)
	}
	_, werr := tmp.WriteString(kyutaiWorkerScript)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("write kyutai worker script: %w", errors.Join(werr, cerr))
	}
	if err := os.Rename(tmp.Name(), scriptPath); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("write kyutai worker script: %w", err)
	}
	return scriptPath, nil
}

// reap waits for the child so it never becomes a zombie, and records how it
// ended in the supervisor.
func (w *kyutaiWorker) reap(sup *tool.ProcessSupervisor) {
	defer close(w.exited)
	err := w.cmd.Wait()
	w.dead.Store(true)
	code := 0
	if w.cmd.ProcessState != nil {
		code = w.cmd.ProcessState.ExitCode()
	}
	if err != nil {
		sup.MarkKilled(kyutaiWorkerProcID, code)
		return
	}
	sup.MarkExited(kyutaiWorkerProcID, code)
}

// stop kills the process and waits until its supervisor record is terminal.
// The wait matters: the next worker registers under the same stable id, and a
// record that is still running would make that registration fail. The caller
// holds kyutaiWorkerState.mu; reap never takes it, so this cannot deadlock.
func (w *kyutaiWorker) stop() {
	w.idle.Stop()
	if err := w.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		log.Printf("tts: kill kyutai worker pid %d: %v", w.cmd.Process.Pid, err)
	}
	<-w.exited
}

type kyutaiReadResult struct {
	reply kyutaiWorkerReply
	err   error
}

// synth sends one request and waits for its reply. A cancelled ctx returns at
// once, but the worker still finishes the request in the background and the
// slot stays held until its reply is read, so the next request waits its turn
// instead of reading a stale reply.
func (w *kyutaiWorker) synth(ctx context.Context, req kyutaiWorkerRequest) error {
	select {
	case w.slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if w.dead.Load() {
		<-w.slot
		return errors.New("kyutai worker is not running")
	}
	w.nextID++
	req.ID = w.nextID
	line, err := json.Marshal(req)
	if err != nil {
		<-w.slot
		return fmt.Errorf("encode kyutai request: %w", err)
	}
	if _, err := w.stdin.Write(append(line, '\n')); err != nil {
		w.dead.Store(true)
		<-w.slot
		return fmt.Errorf("kyutai worker write: %w%s", err, w.stderrSuffix())
	}
	done := make(chan kyutaiReadResult, 1)
	go func() {
		defer func() { <-w.slot }()
		reply, err := w.readReply(req.ID)
		if err != nil {
			w.dead.Store(true)
		}
		done <- kyutaiReadResult{reply: reply, err: err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return fmt.Errorf("kyutai worker: %w%s", r.err, w.stderrSuffix())
		}
		if !r.reply.OK {
			return fmt.Errorf("kyutai synth failed: %s", r.reply.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// readReply reads one reply line. Any mismatch with the request ID means the
// pipes are out of step, so the caller marks the worker dead.
func (w *kyutaiWorker) readReply(wantID int) (kyutaiWorkerReply, error) {
	line, err := w.stdout.ReadBytes('\n')
	if err != nil {
		return kyutaiWorkerReply{}, err
	}
	var reply kyutaiWorkerReply
	if err := json.Unmarshal(line, &reply); err != nil {
		return kyutaiWorkerReply{}, fmt.Errorf("decode reply %q: %w", strings.TrimSpace(string(line)), err)
	}
	if reply.ID != wantID {
		return kyutaiWorkerReply{}, fmt.Errorf("reply id %d does not match request %d", reply.ID, wantID)
	}
	return reply, nil
}

func (w *kyutaiWorker) stderrSuffix() string {
	if s := w.stderrText(); s != "" {
		return " (stderr: " + s + ")"
	}
	return ""
}

func (w *kyutaiWorker) stderrText() string {
	return w.stderr.String()
}

// tailBuffer keeps the last cap bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	cap int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - b.cap; over > 0 {
		b.buf = b.buf[over:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}

// kyutaiPrefetchScript imports the runtime, loads the pinned model, and loads the
// voice prompt of every listed voice once, at install time. That fetches the
// weights into the Hugging Face cache. The resident worker runs offline
// (HF_HUB_OFFLINE=1), so a voice that was not prefetched fails its first
// utterance; every voice in kyutaiVoices must therefore be listed here.
func kyutaiPrefetchScript(voices []string) string {
	quoted := make([]string, len(voices))
	for i, voice := range voices {
		quoted[i] = fmt.Sprintf("%q", voice)
	}
	return "import pocket_tts, scipy\n" +
		"from pocket_tts import TTSModel\n" +
		"model = TTSModel.load_model()\n" +
		"for voice in [" + strings.Join(quoted, ", ") + "]:\n" +
		"    model.get_state_for_audio_prompt(voice)\n"
}

func kyutaiVerifyRuntime(ctx context.Context, venv string, voices []string) (string, error) {
	return runCmd(ctx, []string{venvPython(venv), "-c", kyutaiPrefetchScript(voices)})
}
