package computer

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/u007/ocode/internal/tool"
)

const stdoutLimit = 1 << 20 // 1 MiB

// limitWriter caps writes to a maximum number of bytes.
// Excess bytes are silently discarded.
type limitWriter struct {
	buf   *bytes.Buffer
	limit int64
	size  int64
}

func (w *limitWriter) Write(p []byte) (int, error) {
	room := w.limit - w.size
	if room <= 0 {
		return len(p), nil
	}
	n := len(p)
	if int64(n) > room {
		n = int(room)
	}
	w.buf.Write(p[:n])
	w.size += int64(n)
	return len(p), nil
}

// commandRunner abstracts command execution so drivers (and their
// tests) can substitute a recording stub.
type commandRunner interface {
	run(ctx context.Context, name string, args ...string) (stdout string, err error)
	runStdin(ctx context.Context, stdin string, name string, args ...string) (string, error)
}

// execRunner implements commandRunner by spawning real processes
// under the shared process supervisor. Every call is registered as
// a computer-<name> process so it survives shutdown and appears in
// snapshots.
type execRunner struct {
	sup *tool.ProcessSupervisor
}

// run executes name with args, captures stdout (capped at 1 MiB)
// and stderr, registers the process via StartSupervised, waits for
// completion, and marks it exited or killed.
func (r *execRunner) run(ctx context.Context, name string, args ...string) (string, error) {
	return r.runStdin(ctx, "", name, args...)
}

// runStdin executes name with args, feeding stdin, capturing stdout
// (capped at 1 MiB) and stderr, registering via StartSupervised,
// and waiting for completion.
func (r *execRunner) runStdin(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	cmd.Stdout = &limitWriter{buf: stdoutBuf, limit: stdoutLimit}
	cmd.Stderr = stderrBuf
	cmd.Stdin = strings.NewReader(stdin)

	id := fmt.Sprintf("computer-%s-%d", name, time.Now().UnixNano())
	_, err := tool.StartSupervised(r.sup, cmd, tool.ProcessRegistration{
		ID:      id,
		Name:    "computer " + name,
		Command: name + " " + strings.Join(args, " "),
		Kind:    tool.ProcessKindComputer,
	})
	if err != nil {
		// Preserve exec.ErrNotFound through wrapping so callers can detect missing binaries.
		return "", fmt.Errorf("computer %s: %w: %s", name, err, strings.TrimSpace(stderrBuf.String()))
	}

	err = cmd.Wait()
	code := 0
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		r.sup.MarkKilled(id, code)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("computer %s: %w: %s", name, err, strings.TrimSpace(stderrBuf.String()))
	}
	r.sup.MarkExited(id, code)
	return stdoutBuf.String(), nil
}
