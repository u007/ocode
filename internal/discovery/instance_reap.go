package discovery

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	gopsnet "github.com/shirou/gopsutil/v4/net"
	gopsprocess "github.com/shirou/gopsutil/v4/process"
)

// strayReapGrace bounds how long a stray server gets to exit after each of
// SIGTERM and SIGKILL before the port is declared unreclaimable. The process
// being reaped is by definition already unresponsive, so this is a courtesy
// window for an orderly exit, not a shutdown budget.
const strayReapGrace = 5 * time.Second

// chatPortHeld reports whether port is currently bound by some process. It
// answers by attempting the same bind the server spawn is about to attempt, so
// a "free" result means the spawn can actually succeed — a probe of the health
// endpoint cannot distinguish "nothing listening" from "listening but wedged",
// which is exactly the case this file exists to handle.
func chatPortHeld(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	ln.Close()
	return false
}

// findStrayChatServer locates a running process that is a local chat server for
// man on port. Identification is by command line rather than a recorded pid, so
// it also catches servers orphaned by an ocode process that died before it could
// record anything (and by ocode versions that never recorded pids at all).
//
// Both matches are required — the served model identity (the MLX repo id or the
// GGUF basename, whichever this manifest launches with) AND the exact "--port N"
// flag every chat manifest passes. A process must also own the listening socket
// for this port. Command lines alone are not sufficient evidence: an unrelated
// process can contain the same model and port text in its arguments.
func findStrayChatServer(man ServerManifest, port int) (pid int, cmdline string, found bool) {
	listenerPIDs, err := listeningPIDs(port)
	if err != nil {
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("could not identify the listener for port %d: %v", port, err), false)
		return 0, "", false
	}
	procs, err := gopsprocess.Processes()
	if err != nil {
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("could not enumerate processes to look for a stray server on port %d: %v", port, err), false)
		return 0, "", false
	}
	token := man.ExpectedServeID()
	if token == "" {
		return 0, "", false
	}
	self := os.Getpid()
	for _, p := range procs {
		if int(p.Pid) == self {
			continue
		}
		if _, ok := listenerPIDs[p.Pid]; !ok {
			continue
		}
		args, err := p.CmdlineSlice()
		// intentionally not logged: Cmdline fails for every process owned by
		// another user, which is most of them — logging would flood the log
		// with expected permission errors on each scan.
		if err != nil || len(args) == 0 {
			continue
		}
		if strayServerArgsMatch(args, token, port) {
			return int(p.Pid), strings.Join(args, " "), true
		}
	}
	return 0, "", false
}

// listeningPIDs returns only processes that currently own a TCP listener that
// can accept a connection to 127.0.0.1:port. If the OS cannot report the
// listener owner, the caller must treat that as failure to prove ownership.
func listeningPIDs(port int) (map[int32]struct{}, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid TCP port %d", port)
	}
	connections, err := gopsnet.Connections("tcp")
	if err != nil {
		return nil, err
	}
	listeners := make(map[int32]struct{})
	for _, conn := range connections {
		if conn.Status != "LISTEN" || conn.Laddr.Port != uint32(port) || !listenerAcceptsLoopback(conn.Laddr.IP) || conn.Pid <= 0 {
			continue
		}
		listeners[conn.Pid] = struct{}{}
	}
	return listeners, nil
}

func listenerAcceptsLoopback(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.IsLoopback() || v4.IsUnspecified()
	}
	return ip.IsLoopback() || ip.IsUnspecified()
}

// strayServerArgsMatch validates the argument structure emitted by the chat
// manifests. It intentionally does not use substring matching: --port 11458
// must not match --port 114580, and the model token must be the value of a
// model flag (or an exact positional token used by the test helper).
func strayServerArgsMatch(args []string, token string, port int) bool {
	if token == "" || !hasExactPortArg(args, port) {
		return false
	}
	for i, arg := range args {
		if arg == "--model" || arg == "-m" {
			if i+1 < len(args) && modelArgMatches(args[i+1], token) {
				return true
			}
			continue
		}
		if strings.HasPrefix(arg, "--model=") && modelArgMatches(strings.TrimPrefix(arg, "--model="), token) {
			return true
		}
		if strings.HasPrefix(arg, "-m=") && modelArgMatches(strings.TrimPrefix(arg, "-m="), token) {
			return true
		}
		// This is used only by the controlled test helper. Real chat manifests
		// put the model behind --model or -m.
		if arg == token {
			return true
		}
	}
	return false
}

func hasExactPortArg(args []string, port int) bool {
	want := strconv.Itoa(port)
	for i, arg := range args {
		if arg == "--port" {
			return i+1 < len(args) && args[i+1] == want
		}
		if strings.HasPrefix(arg, "--port=") {
			return strings.TrimPrefix(arg, "--port=") == want
		}
	}
	return false
}

func modelArgMatches(value, token string) bool {
	return value == token || filepath.Base(value) == token
}

func pidOwnsListener(pid int32, port int) bool {
	listeners, err := listeningPIDs(port)
	if err != nil {
		return false
	}
	_, ok := listeners[pid]
	return ok
}

// strayProcessStillMatches revalidates both pieces of evidence immediately
// before a signal. This closes the PID-reuse/exec race between process
// enumeration and signalling: owning the port alone is not enough if the PID
// has since become an unrelated listener.
func strayProcessStillMatches(pid int32, man ServerManifest, port int) bool {
	if !pidOwnsListener(pid, port) {
		return false
	}
	proc, err := gopsprocess.NewProcess(pid)
	if err != nil {
		return false
	}
	args, err := proc.CmdlineSlice()
	return err == nil && strayServerArgsMatch(args, man.ExpectedServeID(), port)
}

// reapStrayChatServer reclaims port for modelID when it is held by a chat
// server that is not answering its health endpoint.
//
// A previous ocode process can exit (quit, crash, SIGKILL) while the server it
// spawned is still booting. The server survives as an orphan that holds the
// port but never serves a request, and StartLocalModelInstance's kill-on-failure
// cleanup never runs because the process that would have run it is gone. Every
// later start then failed its probe, failed to bind, and burned the full
// health-poll budget before the circuit breaker took over — the observed
// bonsai-8b-1bit symptom of an auto-permission model that could never come up.
//
// Callers must hold the cross-process start lock: that is what makes a
// bound-but-unhealthy port a stray rather than a sibling ocode's legitimate
// in-flight spawn. Returns reaped=true when a stray was killed and the port is
// free again, (false, nil) when the port was not held at all, and an error when
// the port is held but could not be reclaimed.
func reapStrayChatServer(modelID string, port int, man ServerManifest) (reaped bool, err error) {
	if !chatPortHeld(port) {
		return false, nil
	}

	// Re-probe before doing anything violent. The caller's probe uses a 2s
	// timeout, which a healthy server can miss under load (queued requests
	// behind the concurrency limiter, disk pressure) — and the cmdline of a
	// working server is indistinguishable from a wedged one. A genuinely
	// stuck server never starts answering, so this second look costs 2s and
	// removes the entire class of "SIGKILLed a server another session was
	// using".
	if healthy, served := probeLocalServerModel(fmt.Sprintf("http://localhost:%d", port), man.HealthPath, man.ExpectedServeID()); healthy && modelMatches(served, man.ExpectedServeID()) {
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("port %d answered on re-probe — not a stray, leaving %s server alone", port, modelID), false)
		return false, nil
	}

	pid, cmdline, found := findStrayChatServer(man, port)
	if !found {
		return false, fmt.Errorf("port %d is held by another process (not a %s server) — free the port, then restart the model via /localmodel", port, modelID)
	}

	emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("port %d is held by an unresponsive %s server (pid %d) — reclaiming it: %s", port, modelID, pid, cmdline))

	proc, findErr := os.FindProcess(pid)
	if findErr != nil {
		return false, fmt.Errorf("found unresponsive %s server on port %d (pid %d) but could not address it: %w", modelID, port, pid, findErr)
	}
	if !strayProcessStillMatches(int32(pid), man, port) {
		return false, fmt.Errorf("found an unresponsive %s server candidate on port %d (pid %d), but could not verify its command line and listening-socket ownership — refusing to signal it", modelID, port, pid)
	}
	if sigErr := proc.Signal(syscall.SIGTERM); sigErr != nil {
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("SIGTERM to stray %s server pid %d failed (%v) — escalating to kill", modelID, pid, sigErr), false)
	}
	if waitForPortFree(port) {
		emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("reclaimed port %d from %s server pid %d", port, modelID, pid))
		return true, nil
	}

	if !strayProcessStillMatches(int32(pid), man, port) {
		return false, fmt.Errorf("unresponsive %s server candidate (pid %d) no longer matches or owns port %d — refusing to SIGKILL it", modelID, pid, port)
	}
	if killErr := proc.Kill(); killErr != nil {
		return false, fmt.Errorf("unresponsive %s server (pid %d) is holding port %d and could not be killed: %w — kill it manually, then restart the model via /localmodel", modelID, pid, port, killErr)
	}
	if waitForPortFree(port) {
		emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("reclaimed port %d from %s server pid %d (required SIGKILL)", port, modelID, pid))
		return true, nil
	}
	return false, fmt.Errorf("unresponsive %s server (pid %d) still holds port %d after SIGKILL — kill it manually, then restart the model via /localmodel", modelID, pid, port)
}

// chatStartLockOwnerDead reports whether lockPath records an owner pid that is
// no longer running. A lock whose owner is gone was orphaned by a process that
// died before releasing it, and blocking on it only delays the stray-server
// reap that the same dead process's leftovers require. Locks written by an
// older ocode (no pid recorded) return false and fall back to mtime staleness.
//
// PID reuse can make a dead owner look alive; that errs toward waiting, which
// is the safe direction.
func chatStartLockOwnerDead(lockPath string) bool {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		// intentionally not logged: the lock vanishing between the failed
		// create and this read is the normal race with a holder releasing it.
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false // pre-pid lock file, or a partial write — use mtime instead
	}
	alive, err := gopsprocess.PidExists(int32(pid))
	if err != nil {
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("could not check whether chat start lock owner pid %d is alive: %v", pid, err), false)
		return false
	}
	return !alive
}

// waitForPortFree polls until the port is bindable or strayReapGrace elapses.
func waitForPortFree(port int) bool {
	deadline := time.Now().Add(strayReapGrace)
	for {
		if !chatPortHeld(port) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}
