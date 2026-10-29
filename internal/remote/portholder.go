package remote

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// errRemotePortFree is returned by FindPortHolder when nothing visible to the
// remote user listens on the port.
var errRemotePortFree = errors.New("no visible listener on remote port")

// PortBusyError marks a reverse forward that sshd refused because the remote
// port is already bound. It keeps the underlying message verbatim, so callers
// that only print the error see no change; callers that can act on it (offer
// to kill the holder) match it with errors.As.
type PortBusyError struct{ Err error }

func (e *PortBusyError) Error() string { return e.Err.Error() }
func (e *PortBusyError) Unwrap() error { return e.Err }

// PortHolder is the remote process listening on a port.
type PortHolder struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`

	// pids is every process listening on the port, PID first. A socket shared by
	// a master and its workers is held by all of them, and SIGTERM to one leaves
	// the port bound.
	pids []int
}

// portHolderPolls and portHolderInterval bound how often the kill paths re-check
// that the port has freed. The overall deadline is the caller's context.
const (
	portHolderPolls    = 10
	portHolderInterval = 300 * time.Millisecond
)

// FindPortHolder returns the process listening on the remote TCP port. Only
// processes of the ssh user are visible, so a port held by another user reports
// errRemotePortFree even though it is bound; the caller treats "unknown holder"
// as not killable. ctx bounds every remote exec.
//
// `ss` is tried first (Linux); `lsof` answers on hosts without it (macOS). The
// `|| true` keeps a no-match exit status from reading as a transport failure.
func FindPortHolder(ctx context.Context, tr BoundedTransport, port int) (*PortHolder, error) {
	res, err := tr.ExecContext(ctx, "ss -ltnp 2>/dev/null || true")
	if err != nil {
		return nil, fmt.Errorf("list listeners on remote: %w", err)
	}
	pids, perr := parseSsOutput(res.Stdout, port)
	if perr != nil {
		res, err = tr.ExecContext(ctx, "lsof -nP -iTCP:"+strconv.Itoa(port)+" -sTCP:LISTEN 2>/dev/null || true")
		if err != nil {
			return nil, fmt.Errorf("list listeners on remote: %w", err)
		}
		pids, perr = parseLsofOutput(res.Stdout, port)
		if perr != nil {
			return nil, fmt.Errorf("%w %d", errRemotePortFree, port)
		}
	}
	pid := pids[0]
	cmd, err := tr.ExecContext(ctx, "ps -o args= -p "+strconv.Itoa(pid)+" 2>/dev/null || true")
	if err != nil {
		return nil, fmt.Errorf("read command of remote pid %d: %w", pid, err)
	}
	return &PortHolder{PID: pid, Command: strings.TrimSpace(cmd.Stdout), pids: pids}, nil
}

// IsPortFree reports whether err from FindPortHolder means "nothing listening".
func IsPortFree(err error) bool { return errors.Is(err, errRemotePortFree) }

// KillPortHolder terminates every process holding the remote port, but only if
// pid is still one of them (the pid may have been recycled since the user was
// asked). SIGTERM only: a holder that ignores it is reported, never -9'd. ctx is
// the overall deadline for the verify, kill and wait-for-free sequence.
func KillPortHolder(ctx context.Context, tr BoundedTransport, port, pid int) error {
	if pid <= 1 {
		return fmt.Errorf("refusing to kill remote pid %d", pid)
	}
	cur, err := FindPortHolder(ctx, tr, port)
	if err != nil {
		return fmt.Errorf("verify holder of remote port %d: %w", port, err)
	}
	if !slices.Contains(cur.pids, pid) {
		return fmt.Errorf("remote port %d is now held by pid %d, not pid %d", port, cur.PID, pid)
	}
	for _, p := range cur.pids {
		if p <= 1 {
			return fmt.Errorf("refusing to kill remote pid %d on port %d", p, port)
		}
	}
	if _, err := tr.ExecContext(ctx, "kill "+pidList(cur.pids)); err != nil {
		return fmt.Errorf("kill remote pid %d: %w", pid, err)
	}
	for i := 0; i < portHolderPolls; i++ {
		_, err := FindPortHolder(ctx, tr, port)
		if IsPortFree(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("confirm remote port %d freed: %w", port, err)
		}
		if err := sleepCtx(ctx, portHolderInterval); err != nil {
			return fmt.Errorf("remote pid %d did not release port %d before the deadline: %w", pid, port, err)
		}
	}
	return fmt.Errorf("remote pid %d did not release port %d after SIGTERM", pid, port)
}

// LocalPortBusyError marks a -L forward whose LOCAL port ssh could not bind
// because another process on this machine holds it. Holder is nil when the
// owner could not be identified. The message is the underlying error verbatim.
type LocalPortBusyError struct {
	Port   int
	Holder *PortHolder
	Err    error
}

func (e *LocalPortBusyError) Error() string { return e.Err.Error() }
func (e *LocalPortBusyError) Unwrap() error { return e.Err }

// Seams for the local inspection and signalling, replaced by tests.
var (
	findLocalListener = findListeningProcesses
	localCmdline      = processCmdline
	signalLocalTerm   = func(pid int) error {
		proc, err := os.FindProcess(pid)
		if err != nil {
			return err
		}
		return proc.Signal(syscall.SIGTERM)
	}
)

// FindLocalPortHolder returns the process on THIS machine listening on port. ctx
// bounds the listener inspection.
func FindLocalPortHolder(ctx context.Context, port int) (*PortHolder, error) {
	pids, err := findLocalListener(ctx, port)
	if err != nil {
		return nil, fmt.Errorf("find listener on local port %d: %w", port, err)
	}
	pid := pids[0]
	cmd, err := localCmdline(pid)
	if err != nil {
		return nil, fmt.Errorf("read command of local pid %d: %w", pid, err)
	}
	return &PortHolder{PID: pid, Command: strings.TrimSpace(cmd), pids: pids}, nil
}

// KillLocalPortHolder terminates every process on THIS machine listening on
// port, but only if pid is still one of them. SIGTERM only. pid <= 1 and ocode
// itself are refused, and so is any listener set that contains either: nothing
// is signalled unless every member is safe. ctx is the overall deadline.
func KillLocalPortHolder(ctx context.Context, port, pid int) error {
	if pid <= 1 || pid == os.Getpid() {
		return fmt.Errorf("refusing to kill local pid %d", pid)
	}
	pids, err := findLocalListener(ctx, port)
	if err != nil {
		return fmt.Errorf("verify holder of local port %d: %w", port, err)
	}
	if !slices.Contains(pids, pid) {
		return fmt.Errorf("local port %d is now held by pid %d, not pid %d", port, pids[0], pid)
	}
	for _, p := range pids {
		if p <= 1 || p == os.Getpid() {
			return fmt.Errorf("refusing to kill local pid %d: it shares local port %d", p, port)
		}
	}
	for _, p := range pids {
		if err := signalLocalTerm(p); err != nil {
			// A master reaping its workers can exit between the listing and this
			// signal. The wait-for-free loop below is the real success check.
			if errors.Is(err, syscall.ESRCH) {
				log.Printf("remote: local pid %d on port %d exited before SIGTERM", p, port)
				continue
			}
			return fmt.Errorf("kill local pid %d: %w", p, err)
		}
	}
	for i := 0; i < portHolderPolls; i++ {
		if _, err := findLocalListener(ctx, port); errors.Is(err, errNoListener) {
			return nil
		} else if err != nil {
			return fmt.Errorf("confirm local port %d freed: %w", port, err)
		}
		if err := sleepCtx(ctx, portHolderInterval); err != nil {
			return fmt.Errorf("local pid %d did not release port %d before the deadline: %w", pid, port, err)
		}
	}
	return fmt.Errorf("local pid %d did not release port %d after SIGTERM", pid, port)
}

// sleepCtx waits d, or returns ctx.Err() as soon as ctx ends first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// pidList renders pids as space-separated shell arguments.
func pidList(pids []int) string {
	parts := make([]string, len(pids))
	for i, p := range pids {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, " ")
}
