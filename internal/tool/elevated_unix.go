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
		return "", fmt.Errorf("elevation approved but no password is available; refusing to run the command unprivileged")
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
	captured := newBoundedBuffer(bashMaxRetainedBytes)
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(captured, ptmx)
		close(readDone)
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
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		_ = ptmx.Close()
		<-readDone
	}

	out := scrubElevationSecret(captured.String(), secret)
	return out, scrubElevationError(cmdErr, secret)
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
	return string(b)
}

// scrubElevationError scrubs the secret from an error string without changing
// the error's identity contract to callers (they only inspect message text).
func scrubElevationError(err error, secret []byte) error {
	if err == nil || len(secret) == 0 {
		return err
	}
	return fmt.Errorf("%s", scrubElevationSecret(err.Error(), secret))
}
