package remote

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// errNoListener means the port inspection ran and found nothing listening. It
// is an expected state, not a failure, so reclaimStaleTunnelPort tests for it
// with errors.Is and returns nil instead of an error.
var errNoListener = errors.New("no process listening")

// reclaimStaleTunnelPort kills an ocode-managed ssh tunnel that is holding the
// given local TCP port, left behind by a previous ocode instance that exited
// without gracefully shutting down (SIGKILL, force quit, crash, power loss).
//
// The tunnel ssh is started in its own session (Setsid), so when ocode dies
// ungracefully the child is reparented to PID 1 and keeps listening on its
// local forward port forever. The next connect cannot bind that port and fails
// with "connection refused". This function is called before starting a new
// tunnel so a stale port holder cannot block reconnect.
//
// Only tunnels whose parent process is dead (PPID == 1, i.e. reparented to
// init) are reaped, so a live ocode instance's tunnel is never killed.
//
// It returns nil if the port is free or no ocode tunnel was found holding it.
// It returns an error only when inspecting or killing the port holder fails;
// callers log that error. A process that is not an ocode-managed ssh tunnel is
// never touched.
func reclaimStaleTunnelPort(port int) error {
	if port <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), listenerProbeTimeout)
	defer cancel()
	pids, err := findListeningProcesses(ctx, port)
	if errors.Is(err, errNoListener) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find listener on port %d: %w", port, err)
	}
	// An ocode tunnel is a single ssh process, so its first listener is the one to judge.
	pid := pids[0]
	if !isOcodeSSHTunnel(pid) {
		return nil // not ours; leave it alone
	}
	// Ownership check: only reap orphans (reparented to PID 1).
	ppid, err := parentPID(pid)
	if err != nil {
		return fmt.Errorf("read parent of pid %d on port %d: %w", pid, port, err)
	}
	if ppid != 1 {
		log.Printf("remote: port %d held by pid %d (ppid %d, not orphaned) — not reaping", port, pid, ppid)
		return nil
	}
	cmdline, err := processCmdline(pid)
	if err != nil {
		return fmt.Errorf("read cmdline of pid %d on port %d: %w", pid, port, err)
	}
	lower := strings.ToLower(cmdline)
	// Port-specific check: the cmdline must contain -L <port>: (local forward spec).
	if !strings.Contains(lower, fmt.Sprintf("-l %d:", port)) {
		return nil
	}
	if err := killProcess(pid); err != nil {
		return fmt.Errorf("reclaim port %d: kill pid %d: %w", port, pid, err)
	}
	log.Printf("remote: reclaimed port %d from stale tunnel pid %d: %s", port, pid, cmdline)
	// Give the kernel a moment to release the port.
	time.Sleep(100 * time.Millisecond)
	return nil
}

// isOcodeSSHTunnel reports whether the process with the given PID is an
// ocode-managed ssh tunnel. It requires the binary name to be "ssh" and the
// distinctive ocode marker "ExitOnForwardFailure" (set by StartTunnel's
// tunnelArgs) to avoid killing a user's own ssh tunnel.
func isOcodeSSHTunnel(pid int) bool {
	cmdline, err := processCmdline(pid)
	if err != nil {
		log.Printf("remote: inspect pid %d for ocode ssh tunnel: %v", pid, err)
		return false // cannot prove it is ours; leave it alone
	}
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return false
	}
	if filepath.Base(fields[0]) != "ssh" {
		return false
	}
	lower := strings.ToLower(cmdline)
	return strings.Contains(lower, "exitonforwardfailure")
}

// parentPID returns the parent process ID of the given PID. An error means the
// parent could not be read (inspection failed or the PID is gone); it is never
// reported as a parent of 0.
func parentPID(pid int) (int, error) {
	switch runtime.GOOS {
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return 0, fmt.Errorf("ps ppid of pid %d: %w", pid, err)
		}
		ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			return 0, fmt.Errorf("parse ps ppid of pid %d %q: %w", pid, out, err)
		}
		return ppid, nil
	case "linux":
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return 0, fmt.Errorf("read proc stat of pid %d: %w", pid, err)
		}
		ppid, err := parseProcStatPPID(string(data))
		if err != nil {
			return 0, fmt.Errorf("parse proc stat of pid %d: %w", pid, err)
		}
		return ppid, nil
	default:
		return 0, fmt.Errorf("parent process lookup not supported on %s", runtime.GOOS)
	}
}

// parseProcStatPPID extracts the ppid from /proc/<pid>/stat content.
func parseProcStatPPID(s string) (int, error) {
	// /proc/pid/stat: pid (comm) state ppid ... The comm field may
	// contain spaces and parentheses, so find the last ')'.
	idx := strings.LastIndex(s, ")")
	if idx < 0 {
		return 0, fmt.Errorf("no comm terminator in %q", s)
	}
	fields := strings.Fields(s[idx+1:])
	if len(fields) < 2 {
		return 0, fmt.Errorf("too few fields after comm in %q", s)
	}
	// fields[0] is state, fields[1] is ppid.
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("ppid field %q: %w", fields[1], err)
	}
	return ppid, nil
}

// listenerProbeTimeout bounds one local listener inspection (lsof or ss).
const listenerProbeTimeout = 5 * time.Second

// findListeningProcesses returns the PIDs of every process listening on the
// given local TCP port, in inspection order, or an error if none is found. A
// socket shared by a master and its workers is listed once per holder, so
// signalling only the first of them can leave the port bound.
//
// On macOS it uses lsof; on Linux it uses ss. The two formats are parsed
// separately to avoid misreading ss's Recv-Q column as a PID.
//
// These are short-lived inspection commands, not long-running managed
// processes, so they are invoked directly rather than through the process
// supervisor. ctx bounds each one.
//
// It returns errNoListener when nothing listens on the port.
func findListeningProcesses(ctx context.Context, port int) ([]int, error) {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.CommandContext(ctx, "lsof", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-P", "-n")
		out, err := cmd.Output()
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("lsof listeners on port %d: %w", port, ctxErr)
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
				// lsof exits 1 when no file matches the filter.
				return nil, errNoListener
			}
			return nil, err
		}
		return parseLsofOutput(string(out), port)
	case "linux":
		cmd := exec.CommandContext(ctx, "ss", "-ltnp")
		out, err := cmd.Output()
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("ss listeners on port %d: %w", port, ctxErr)
			}
			return nil, err
		}
		return parseSsOutput(string(out), port)
	default:
		return nil, fmt.Errorf("port inspection not supported on %s", runtime.GOOS)
	}
}

// appendPID adds pid to pids unless it is already there.
func appendPID(pids []int, pid int) []int {
	if slices.Contains(pids, pid) {
		return pids
	}
	return append(pids, pid)
}

// parseLsofOutput extracts every listener PID from lsof output for the given port.
func parseLsofOutput(out string, port int) ([]int, error) {
	var pids []int
	for _, line := range strings.Split(out, "\n") {
		if !containsPortAtBoundary(line, port) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		pids = appendPID(pids, pid)
	}
	if len(pids) == 0 {
		return nil, fmt.Errorf("%w on port %d", errNoListener, port)
	}
	return pids, nil
}

// parseSsOutput extracts every listener PID from ss output for the given port.
// One socket can list several holders: users:(("node",pid=1,fd=3),("node",pid=2,fd=3)).
func parseSsOutput(out string, port int) ([]int, error) {
	var pids []int
	for _, line := range strings.Split(out, "\n") {
		if !containsPortAtBoundary(line, port) {
			continue
		}
		rest := line
		for {
			idx := strings.Index(rest, "pid=")
			if idx < 0 {
				break
			}
			rest = rest[idx+4:]
			end := strings.IndexAny(rest, ",)")
			if end < 0 {
				end = len(rest)
			}
			if pid, err := strconv.Atoi(strings.TrimSpace(rest[:end])); err == nil {
				pids = appendPID(pids, pid)
			}
			rest = rest[end:]
		}
	}
	if len(pids) == 0 {
		return nil, fmt.Errorf("%w on port %d", errNoListener, port)
	}
	return pids, nil
}

// containsPortAtBoundary reports whether s contains the substring ":<port>"
// not immediately followed by another digit, to avoid matching 36655 inside
// :366550.
func containsPortAtBoundary(s string, port int) bool {
	spec := fmt.Sprintf(":%d", port)
	idx := 0
	for {
		i := strings.Index(s[idx:], spec)
		if i < 0 {
			return false
		}
		pos := idx + i + len(spec)
		if pos < len(s) && s[pos] >= '0' && s[pos] <= '9' {
			idx = pos
			continue
		}
		return true
	}
}

// processCmdline returns the command line of the process with the given PID.
func processCmdline(pid int) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if err != nil {
			return "", err
		}
		return string(out), nil
	case "linux":
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			return "", err
		}
		parts := strings.Split(string(data), "\x00")
		if len(parts) > 0 && parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		return strings.Join(parts, " "), nil
	default:
		return "", fmt.Errorf("process inspection not supported on %s", runtime.GOOS)
	}
}

// killProcess terminates the process with SIGTERM, then SIGKILL if it does not
// exit within a short grace period. It polls isProcessAlive rather than using
// a fixed sleep to avoid PID-reuse races.
func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	// Poll for exit with a bounded grace period.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Still alive — force kill.
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	return nil
}

// isProcessAlive reports whether the process with the given PID is still running.
func isProcessAlive(pid int) bool {
	if pid <= 0 || runtime.GOOS == "windows" {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}
