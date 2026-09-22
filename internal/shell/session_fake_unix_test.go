//go:build !windows

package shell

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The pty integration tests skip on a host that denies /dev/ptmx (notably a
// process confined by ocode's own sandbox). This file drives Session.Run
// against an os.Pipe instead, so the run state machine — framing on the wire,
// marker waiting, timeout recovery, close, serialisation, death detection — is
// still exercised in-sandbox. It does not test the shell protocol (prelude and
// prompt hook); that is what the pty tests cover.

type fakeShell struct {
	sess  *Session
	wire  *os.File
	onCmd func(self *fakeShell, command string)
}

// newFakeSession wires a Session to one end of an os.Pipe and runs a goroutine
// that decodes the framed command units Run writes, handing each to onCmd.
func newFakeSession(t *testing.T, onCmd func(self *fakeShell, command string)) *fakeShell {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	nonce := "fakenonce"
	s := &Session{
		nonce:   nonce,
		marker:  doneMarker(nonce),
		delim:   commandDelimiter(nonce),
		timeout: 2 * time.Second,
		grace:   500 * time.Millisecond,
		closeCh: make(chan struct{}),
		ptmx:    w,
		notify:  make(chan struct{}, 1),
		dead:    make(chan struct{}),
		cmd:     nil,
	}
	s.aliveFlag.Store(true)
	f := &fakeShell{sess: s, wire: r, onCmd: onCmd}
	go f.serve()
	t.Cleanup(func() {
		_ = s.Close()
		_ = r.Close()
		_ = w.Close()
	})
	return f
}

// emit plays the part of the shell plus the readLoop: it appends the command's
// output and then the prompt-hook marker to the session buffer.
func (f *fakeShell) emit(output string, status int, cwd string) {
	f.sess.outMu.Lock()
	f.sess.out.WriteString(output)
	if output != "" && !strings.HasSuffix(output, "\n") {
		f.sess.out.WriteString("\n")
	}
	f.sess.out.WriteString("\n")
	fmt.Fprintf(&f.sess.out, "%s %d %s\n", f.sess.marker, status, cwd)
	f.sess.outMu.Unlock()
	select {
	case f.sess.notify <- struct{}{}:
	default:
	}
}

// serve decodes framed units: `eval "$(cat <<'DELIM'` / body / `DELIM` / `)"`.
func (f *fakeShell) serve() {
	reader := bufio.NewReader(f.wire)
	for {
		first, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		start := strings.Index(first, "<<'")
		if start < 0 {
			continue
		}
		delim := strings.TrimSuffix(first[start+3:], "'\n")
		var body strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.TrimRight(line, "\n") == delim {
				break
			}
			body.WriteString(line)
		}
		// consume the closing `)"` line
		if _, err := reader.ReadString('\n'); err != nil {
			return
		}
		f.onCmd(f, strings.TrimSuffix(body.String(), "\n"))
	}
}

func TestFakeSessionRunParsesResult(t *testing.T) {
	f := newFakeSession(t, func(f *fakeShell, command string) {
		switch command {
		case "fail":
			f.emit("boom", 3, "/work")
		case "multi":
			f.emit("one\ntwo", 0, "/work")
		default:
			f.emit("ok:"+command, 0, "/work")
		}
	})

	res, cwd, err := f.sess.Run(t.Context(), "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Output != "ok:hello" || res.ExitCode != 0 || cwd != "/work" {
		t.Errorf("got output=%q exit=%d cwd=%q", res.Output, res.ExitCode, cwd)
	}

	res, cwd, err = f.sess.Run(t.Context(), "fail")
	if err != nil {
		t.Fatalf("Run(fail): %v", err)
	}
	if res.ExitCode != 3 || cwd != "/work" || res.Output != "boom" {
		t.Errorf("got output=%q exit=%d cwd=%q", res.Output, res.ExitCode, cwd)
	}

	res, _, err = f.sess.Run(t.Context(), "multi")
	if err != nil {
		t.Fatalf("Run(multi): %v", err)
	}
	if res.Output != "one\ntwo" {
		t.Errorf("multi output = %q, want %q", res.Output, "one\ntwo")
	}
}

func TestFakeSessionRejectsDelimiterBeforeWriting(t *testing.T) {
	called := false
	f := newFakeSession(t, func(*fakeShell, string) { called = true })
	if _, _, err := f.sess.Run(t.Context(), "x\n"+f.sess.delim+"\ny"); err == nil {
		t.Fatal("expected the delimiter-bearing command to be rejected")
	}
	// Nothing should have reached the shell.
	time.Sleep(50 * time.Millisecond)
	if called {
		t.Error("a rejected command was written to the shell")
	}
}

func TestFakeSessionTimeoutRecoveryAfterInterrupt(t *testing.T) {
	f := newFakeSession(t, func(f *fakeShell, command string) {
		if command == "hang" {
			// Simulate a command that ignores SIGINT briefly and then finishes:
			// the marker arrives inside the grace window, so the session lives.
			go func() {
				time.Sleep(200 * time.Millisecond)
				f.emit("late", 0, "/work")
			}()
			return
		}
		f.emit("ok", 0, "/work")
	})
	f.sess.timeout = 100 * time.Millisecond

	start := time.Now()
	res, _, err := f.sess.Run(t.Context(), "hang")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout error, got %v", err)
	}
	if res.ExitCode != 1 {
		t.Errorf("exit = %d, want 1", res.ExitCode)
	}
	if !strings.Contains(err.Error(), "preserved") {
		t.Errorf("error = %v, want the recovered variant", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("recovery took %s", elapsed)
	}
	if !f.sess.Alive() {
		t.Error("session should still be alive after an interrupted command")
	}

	res, _, err = f.sess.Run(t.Context(), "again")
	if err != nil || res.Output != "ok" {
		t.Errorf("next Run: output=%q err=%v", res.Output, err)
	}
}

func TestFakeSessionCloseInterruptsRun(t *testing.T) {
	f := newFakeSession(t, func(*fakeShell, string) { /* never emits a marker */ })
	f.sess.timeout = 10 * time.Second

	done := make(chan error, 1)
	go func() {
		_, _, err := f.sess.Run(t.Context(), "hang")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	if err := f.sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil after Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after Close")
	}
	if f.sess.Alive() {
		t.Error("session should not be alive after Close")
	}
}

func TestFakeSessionConcurrentRunsSerialize(t *testing.T) {
	f := newFakeSession(t, func(f *fakeShell, command string) {
		// The delay makes an interleaving bug (two commands in flight) show up
		// as the wrong run's output.
		time.Sleep(20 * time.Millisecond)
		f.emit("out:"+command, 0, "/work")
	})

	var wg sync.WaitGroup
	outputs := make([]string, 4)
	for i := range 4 {
		wg.Go(func() {
			res, _, err := f.sess.Run(t.Context(), fmt.Sprintf("c%d", i))
			if err != nil {
				outputs[i] = "ERR:" + err.Error()
				return
			}
			outputs[i] = res.Output
		})
	}
	wg.Wait()
	for i := range 4 {
		want := fmt.Sprintf("out:c%d", i)
		if outputs[i] != want {
			t.Errorf("run %d = %q, want %q", i, outputs[i], want)
		}
	}
}

func TestFakeSessionDetectsDeadShell(t *testing.T) {
	f := newFakeSession(t, func(f *fakeShell, _ string) { close(f.sess.dead) })
	_, _, err := f.sess.Run(t.Context(), "die")
	if err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("expected a dead-shell error, got %v", err)
	}
}
