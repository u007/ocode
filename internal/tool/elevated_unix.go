//go:build !windows

package tool

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// elevatedSecretRequired reports whether this platform needs a collected
// password to elevate. Unix types it into sudo/su; Windows uses UAC and needs
// none, so a missing secret must NOT fail closed there.
func elevatedSecretRequired() bool { return true }

// executeElevated runs an already-built command on a new pseudo-terminal and
// types secret into it once, without ever placing the secret in argv, env, a
// log line, or the returned output. It is used only for human-approved
// sudo/su tool calls (the context carries the single-use secret getter).
//
// Echo is disabled on the pty master with term.MakeRaw before the secret is
// written, so the password is consumed by the child exactly once and never
// reflected into the captured stream. All output and error strings are
// additionally scrubbed of the exact secret bytes as defence in depth, and
// every transient buffer holding the secret is zeroed before returning.
func executeElevated(ctx context.Context, cmd *exec.Cmd, secret []byte) (string, error) {
	// Fail closed: an approved elevation with no secret may never fall through
	// to running the command unprivileged or partially.
	if len(secret) == 0 {
		return "", fmt.Errorf("%s", elevationNoSecretMessage)
	}
	// The secret is single-use: destroy our copy on every exit path. The return
	// paths below scrub output/errors before this deferred zero runs.
	defer clear(secret)

	// exec.CommandContext's default Cancel signals only the leader; the child
	// is in its own process group (setProcGroup / pty.Start Setsid), so kill the
	// whole group on timeout or cancellation instead.
	killGroup := func() error {
		if cmd.Process != nil {
			return killProcessGroup(cmd.Process)
		}
		return nil
	}
	cmd.Cancel = killGroup

	// pty.Start assigns the tty to stdin/stdout/stderr only when they are nil.
	// Drop any sinks the normal path attached so the whole stream flows through
	// the master and is captured in one bounded buffer.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", scrubElevationError(fmt.Errorf("elevated: start command: %w", err), secret)
	}
	defer func() { _ = ptmx.Close() }()

	fd := int(ptmx.Fd())
	saved, err := term.MakeRaw(fd)
	if err != nil {
		_ = killGroup()
		_ = cmd.Wait()
		return "", scrubElevationError(fmt.Errorf("elevated: cannot disable terminal echo: %w", err), secret)
	}
	defer func() { _ = term.Restore(fd, saved) }()

	// Compose the typed line in a transient buffer, write it immediately (no
	// prompt detection needed with echo off), then erase that buffer.
	line := append(append([]byte(nil), secret...), '\n')
	_, writeErr := ptmx.Write(line)
	clear(line)
	if writeErr != nil {
		_ = killGroup()
		_ = cmd.Wait()
		return "", scrubElevationError(fmt.Errorf("elevated: send password: %w", writeErr), secret)
	}

	// One stream: with the tty on all three fds, stdout and stderr are merged.
	// The reader goroutine OWNS captured (and its own copy of the secret): it
	// scrubs and zeroes the buffer itself and delivers only the final string
	// over readResult. The main goroutine never reads or clears captured.buf,
	// so the timeout escape path cannot race with an in-flight io.Copy write.
	// snapshotWriter lets that same reader publish a scrubbed snapshot of the
	// bytes received so far; the escape path reads the latest snapshot instead
	// of losing the partial output when io.Copy never sees EOF.
	captured := newBoundedBuffer(bashMaxRetainedBytes)
	readDone := make(chan struct{})
	readResult := make(chan string, 1)
	partial := make(chan string, 1)
	readerSecret := bytes.Clone(secret)
	snap := &snapshotWriter{buf: captured, secret: readerSecret, partial: partial}
	go func() {
		_, _ = io.Copy(snap, ptmx)
		close(readDone)
		readResult <- scrubElevationBuffer(captured.buf, readerSecret)
		clear(readerSecret)
	}()

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var cmdErr error
	select {
	case cmdErr = <-waitDone:
	case <-ctx.Done():
		_ = killGroup() // CommandContext also calls cmd.Cancel; be explicit.
		cmdErr = <-waitDone
	}

	// cmd.Wait returns as soon as the child exits; the kernel-buffered tail of
	// pty output may still be draining. Bound the wait and force the read to
	// unblock by closing the master if a stray holder keeps the slave open.
	var out string
	select {
	case <-readDone:
		out = <-readResult
	case <-time.After(2 * time.Second):
		_ = ptmx.Close()
		// A leaked slave fd can keep io.Copy blocked even after Close. Never
		// wait forever on it: take the latest snapshot the reader published and
		// ABANDON the reader — only the goroutine that owns the buffer may
		// touch it, so the main goroutine must never scrub captured.buf here.
		select {
		case <-readDone:
			out = <-readResult
		case <-time.After(500 * time.Millisecond):
			select {
			case out = <-partial:
			default:
			}
		}
	}

	return out, scrubElevationError(cmdErr, secret)
}

// snapshotWriter publishes a scrubbed snapshot of the bytes captured so far
// into partial (non-blocking, at most once per snapshotInterval) as io.Copy
// feeds it. It runs only on the reader goroutine that owns buf, so reading
// buf.buf here cannot race the main goroutine's escape path. The secret is
// scrubbed out of every snapshot before it leaves this goroutine.
const snapshotInterval = 100 * time.Millisecond

type snapshotWriter struct {
	buf     *boundedBuffer
	secret  []byte
	partial chan string
	last    time.Time
}

func (w *snapshotWriter) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	if now := time.Now(); now.Sub(w.last) >= snapshotInterval {
		w.last = now
		// Keep only the newest snapshot queued; the escape path takes whatever
		// is here when it gives up on the reader.
		select {
		case <-w.partial:
		default:
		}
		select {
		case w.partial <- scrubElevationSecret(string(w.buf.buf), w.secret):
		default:
		}
	}
	return n, err
}

// scrubElevationBuffer removes every occurrence of secret from buf and returns
// the scrubbed string, zeroing the entire backing buffer before returning. The
// repeated ReplaceAll matches scrubElevationSecret's overlap semantics.
func scrubElevationBuffer(buf []byte, secret []byte) string {
	if len(secret) == 0 {
		out := string(buf)
		clear(buf)
		return out
	}
	b := buf
	for {
		next := bytes.ReplaceAll(b, secret, nil)
		if len(next) == len(b) {
			break
		}
		copy(buf, next)
		b = buf[:len(next)]
	}
	out := string(b)
	clear(buf)
	return out
}

// scrubElevationSecret removes every occurrence of secret from s. Replacing
// with the empty string strictly shortens s, so the loop terminates; it also
// catches reconstructed overlaps (e.g. "abab" in "ababab") that a single
// pass would leave as a partial remnant.
func scrubElevationSecret(s string, secret []byte) string {
	if len(secret) == 0 {
		return s
	}
	b := []byte(s)
	for bytes.Contains(b, secret) {
		b = bytes.ReplaceAll(b, secret, nil)
	}
	out := string(b)
	clear(b)
	return out
}

// scrubElevationError scrubs the secret from an error string without changing
// the error's identity contract to callers (they only inspect message text).
func scrubElevationError(err error, secret []byte) error {
	if err == nil || len(secret) == 0 {
		return err
	}
	msg := err.Error()
	scrubbed := scrubElevationSecret(msg, secret)
	if scrubbed == msg {
		// Common case: nothing to scrub. Keep the original error so *exec.ExitError
		// survives and commandExitCode reports the real exit status, not 1.
		return err
	}
	return fmt.Errorf("%s", scrubbed)
}
