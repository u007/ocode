package tts

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// fakeKyutaiWorker wires a kyutaiWorker to in-memory pipes. The test drives the
// far end of the protocol with reqRead (requests the worker received) and
// repWrite (replies it sends back), so the slot and reply-matching logic run
// without a Python process.
type fakeKyutaiWorker struct {
	w        *kyutaiWorker
	reqRead  *bufio.Reader
	repWrite io.WriteCloser
}

func newFakeKyutaiWorker(t *testing.T) fakeKyutaiWorker {
	t.Helper()
	reqR, reqW := io.Pipe()
	repR, repW := io.Pipe()
	t.Cleanup(func() {
		_ = reqW.Close()
		_ = repW.Close()
	})
	return fakeKyutaiWorker{
		w: &kyutaiWorker{
			stdin:  reqW,
			stdout: bufio.NewReader(repR),
			stderr: &tailBuffer{cap: 1024},
			slot:   make(chan struct{}, 1),
			exited: make(chan struct{}),
		},
		reqRead:  bufio.NewReader(reqR),
		repWrite: repW,
	}
}

// readRequest reads and decodes one request line from the fake worker's stdin.
func (f fakeKyutaiWorker) readRequest(t *testing.T) kyutaiWorkerRequest {
	t.Helper()
	line, err := f.reqRead.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	var req kyutaiWorkerRequest
	if err := json.Unmarshal(line, &req); err != nil {
		t.Fatalf("decode request %q: %v", line, err)
	}
	return req
}

// reply writes one reply line for id.
func (f fakeKyutaiWorker) reply(t *testing.T, id int, ok bool, errText string) {
	t.Helper()
	line, err := json.Marshal(kyutaiWorkerReply{ID: id, OK: ok, Error: errText})
	if err != nil {
		t.Fatalf("encode reply: %v", err)
	}
	if _, err := f.repWrite.Write(append(line, '\n')); err != nil {
		t.Fatalf("write reply: %v", err)
	}
}

func TestKyutaiWorkerSynthRoundTrip(t *testing.T) {
	f := newFakeKyutaiWorker(t)
	go func() {
		req := f.readRequest(t)
		f.reply(t, req.ID, true, "")
	}()
	err := f.w.synth(context.Background(), kyutaiWorkerRequest{Op: "synth", Voice: "alba", Text: "hi", Out: "/clip.wav"})
	if err != nil {
		t.Fatalf("synth: %v", err)
	}
}

func TestKyutaiWorkerSendsOpVoiceTextAndOut(t *testing.T) {
	f := newFakeKyutaiWorker(t)
	got := make(chan kyutaiWorkerRequest, 1)
	go func() {
		req := f.readRequest(t)
		got <- req
		f.reply(t, req.ID, true, "")
	}()
	if err := f.w.synth(context.Background(), kyutaiWorkerRequest{Op: "synth", Voice: "marius", Text: "Hello.", Out: "/out.wav"}); err != nil {
		t.Fatalf("synth: %v", err)
	}
	req := <-got
	if req.Op != "synth" || req.Voice != "marius" || req.Text != "Hello." || req.Out != "/out.wav" || req.ID != 1 {
		t.Fatalf("request fields = %+v", req)
	}
}

func TestKyutaiWorkerReturnsReplyError(t *testing.T) {
	f := newFakeKyutaiWorker(t)
	go func() {
		req := f.readRequest(t)
		f.reply(t, req.ID, false, "ValueError: unknown voice")
	}()
	err := f.w.synth(context.Background(), kyutaiWorkerRequest{Op: "synth", Voice: "nobody", Text: "x", Out: "/x.wav"})
	if err == nil || !strings.Contains(err.Error(), "unknown voice") {
		t.Fatalf("synth error = %v, want the worker's message", err)
	}
	if f.w.dead.Load() {
		t.Fatal("a reply that reports a synthesis error must not mark the worker dead")
	}
}

// A cancelled caller returns at once, but the abandoned request keeps the slot
// until its reply is read. The next request must wait for that reply and then
// succeed with its own id, never read the stale one.
func TestKyutaiWorkerCancelHoldsSlotUntilReplyIsRead(t *testing.T) {
	f := newFakeKyutaiWorker(t)
	release := make(chan struct{})
	firstSeen := make(chan struct{})
	go func() {
		first := f.readRequest(t)
		close(firstSeen)
		<-release
		f.reply(t, first.ID, true, "")
		second := f.readRequest(t)
		f.reply(t, second.ID, true, "")
	}()

	ctx, cancel := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- f.w.synth(ctx, kyutaiWorkerRequest{Op: "synth", Voice: "alba", Text: "one", Out: "/one.wav"})
	}()
	<-firstSeen
	cancel()
	if err := <-firstErr; err != context.Canceled {
		t.Fatalf("cancelled synth error = %v, want context.Canceled", err)
	}

	secondErr := make(chan error, 1)
	go func() {
		secondErr <- f.w.synth(context.Background(), kyutaiWorkerRequest{Op: "synth", Voice: "alba", Text: "two", Out: "/two.wav"})
	}()
	select {
	case err := <-secondErr:
		t.Fatalf("second synth returned before the abandoned reply was read: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-secondErr; err != nil {
		t.Fatalf("second synth: %v", err)
	}
}

func TestKyutaiWorkerRejectsMismatchedReplyID(t *testing.T) {
	f := newFakeKyutaiWorker(t)
	go func() {
		req := f.readRequest(t)
		f.reply(t, req.ID+7, true, "")
	}()
	err := f.w.synth(context.Background(), kyutaiWorkerRequest{Op: "synth", Voice: "alba", Text: "x", Out: "/x.wav"})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("synth error = %v, want a reply-id mismatch", err)
	}
	if !f.w.dead.Load() {
		t.Fatal("pipes out of step: the worker must be marked dead")
	}
}

func TestTailBufferKeepsOnlyTheLastBytes(t *testing.T) {
	b := &tailBuffer{cap: 4}
	if _, err := b.Write([]byte("abcdefgh")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := b.String(); got != "efgh" {
		t.Fatalf("tail = %q, want efgh", got)
	}
}

// TestKyutaiWorkerSynthesizesWithInstalledRuntime is the real end-to-end check.
// It runs the actual worker against a pocket-tts venv and asserts that two
// utterances produce audio through one resident process.
//
//	KYUTAI_TTS_VENV=/path/to/venv go test ./internal/tts/ -run TestKyutaiWorkerSynthesizesWithInstalledRuntime -v
func TestKyutaiWorkerSynthesizesWithInstalledRuntime(t *testing.T) {
	venv := os.Getenv("KYUTAI_TTS_VENV")
	if venv == "" {
		t.Skip("set KYUTAI_TTS_VENV to a venv with the pinned pocket-tts runtime")
	}
	m, ok := ManifestFor(EngineKyutai)
	if !ok {
		t.Fatal("kyutai manifest is not registered")
	}
	root := t.TempDir()
	dir, err := CacheDirChecked(root, m.Engine, m.Voice, m.Version)
	if err != nil {
		t.Fatalf("cache dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(venv, filepath.Join(dir, "venv")); err != nil {
		t.Fatalf("link venv: %v", err)
	}
	t.Cleanup(func() {
		kyutaiWorkerState.mu.Lock()
		defer kyutaiWorkerState.mu.Unlock()
		if kyutaiWorkerState.active != nil {
			kyutaiWorkerState.active.stop()
			kyutaiWorkerState.active = nil
		}
	})
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	for i, text := range []string{"The quick brown fox jumps over the lazy dog.", "Second utterance, same worker."} {
		out := filepath.Join(root, fmt.Sprintf("clip%d.wav", i))
		start := time.Now()
		if err := kyutaiSynth(context.Background(), sup, root, m, text, out, m.Voice); err != nil {
			t.Fatalf("utterance %d: %v", i, err)
		}
		info, err := os.Stat(out)
		if err != nil || info.Size() == 0 {
			t.Fatalf("utterance %d produced no audio: %v", i, err)
		}
		t.Logf("utterance %d: %s", i, time.Since(start).Round(time.Millisecond))
	}
	kyutaiWorkerState.mu.Lock()
	active := kyutaiWorkerState.active
	kyutaiWorkerState.mu.Unlock()
	if active == nil || active.dead.Load() {
		t.Fatal("both utterances should share one live resident worker")
	}
}
