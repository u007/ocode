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
	token := man.ExpectedServeID()
	if token == "" {
		return 0, "", false
	}
	return findServerMatching(port, func(args []string) bool {
		return strayServerArgsMatch(args, token, port)
	})
}

// findOcodeEmbedServer locates a running process that owns port AND looks like
// an ocode-spawned local model server REGARDLESS of which model it serves — a
// wrong-model squat is precisely what reapStrayEmbedServer exists to reclaim,
// so matching only the requested manifest's ExpectedServeID would protect
// exactly the squatter we need to evict.
func findOcodeEmbedServer(port int, cacheDir string) (pid int, cmdline string, found bool) {
	return findServerMatching(port, func(args []string) bool {
		return embedServerArgsMatch(args, port, cacheDir)
	})
}

// findServerMatching returns the first non-self process that both owns a
// listener on port and satisfies match. Identification is by command line plus
// listening-socket ownership, so it also catches servers orphaned by an ocode
// process that died before it could record anything.
func findServerMatching(port int, match func([]string) bool) (pid int, cmdline string, found bool) {
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
		if match(args) {
			return int(p.Pid), strings.Join(args, " "), true
		}
	}
	return 0, "", false
}

// embedServerArgsMatch identifies an ocode-owned local model server on port
// independent of the served model: EITHER the served-model token of ANY known
// manifest (catches squatters spawned by older ocode versions whose layout or
// default model differed), OR any argument referencing an artifact under
// cacheDir's local model tree (<cacheDir>/local-<os>-<arch>/... GGUF/binary
// paths, or the bundled mlx_embed_server.py). Both forms always ride on top
// of the exact "--port N" flag check, and the caller additionally requires
// listening-socket ownership. Foreign servers that merely bind the same fixed
// port (user LM Studio etc.) carry neither signature and are reported, never
// killed.
func embedServerArgsMatch(args []string, port int, cacheDir string) bool {
	if !hasExactPortArg(args, port) {
		return false
	}
	for _, m := range localManifests {
		if strayServerArgsMatch(args, m.ExpectedServeID(), port) {
			return true
		}
	}
	return referencesDiscoveryArtifact(args, cacheDir)
}

// referencesDiscoveryArtifact reports whether any argument points into
// cacheDir's local-model artifact tree (llama-server binaries/GGUFs under
// local-<os>-<arch>/, or the bundled MLX script written at the tree root).
func referencesDiscoveryArtifact(args []string, cacheDir string) bool {
	if cacheDir == "" {
		return false
	}
	prefix := filepath.Join(cacheDir, "local-")
	script := filepath.Join(cacheDir, mlxEmbedServerScriptName)
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) || arg == script {
			return true
		}
	}
	return false
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
	return strayProcessStillMatchesFunc(pid, port, func(args []string) bool {
		return strayServerArgsMatch(args, man.ExpectedServeID(), port)
	})
}

// strayProcessStillMatchesFunc is strayProcessStillMatches with the command
// line predicate parameterized, so embed-server reaping can revalidate against
// model-independent identification (see embedServerArgsMatch).
func strayProcessStillMatchesFunc(pid int32, port int, match func([]string) bool) bool {
	if !pidOwnsListener(pid, port) {
		return false
	}
	proc, err := gopsprocess.NewProcess(pid)
	if err != nil {
		return false
	}
	args, err := proc.CmdlineSlice()
	return err == nil && match(args)
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

// reapStrayEmbedServer reclaims port when it is held by a wedged OR wrong-model
// ocode embed/local-model server. Mirrors reapStrayChatServer but for the
// singleton embed port, with one deliberate difference: identification does
// NOT require the holder to serve the manifest we are about to launch. A
// stale server from a previous version or model switch serving a DIFFERENT
// model is exactly the squat this function exists to reclaim — matching only
// the expected serve id would protect the squatter and keep the port hostage.
// Evidence required instead: listening-socket ownership on port AND command-
// line evidence of being ocode-spawned (embedServerArgsMatch). Like chat, it
// re-probes before killing, SIGTERM->SIGKILLs with strayReapGrace, and a port
// held by an unidentifiable process is reported, never blindly killed
// (protects user's LM Studio if they manually set it to 11457).
// Caller must hold acquireEmbedStartLock. cacheDir scopes artifact evidence.
func reapStrayEmbedServer(man ServerManifest, cacheDir string, port int) (bool, error) {
	matchArgs := func(args []string) bool {
		return embedServerArgsMatch(args, port, cacheDir)
	}
	if !chatPortHeld(port) {
		return false, nil
	}
	if healthy, served := probeLocalServerModel(fmt.Sprintf("http://localhost:%d", port), man.HealthPath, man.ExpectedServeID()); healthy && modelMatches(served, man.ExpectedServeID()) {
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("port %d answered on re-probe — not a stray, leaving %s embed server alone", port, man.ModelID), false)
		return false, nil
	}
	pid, cmdline, found := findOcodeEmbedServer(port, cacheDir)
	if !found {
		return false, fmt.Errorf("port %d is held by another process (not an ocode embed server) — free the port, then restart ocode", port)
	}
	emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("port %d is held by an unresponsive ocode embed server (pid %d) — reclaiming it: %s", port, pid, cmdline))
	proc, findErr := os.FindProcess(pid)
	if findErr != nil {
		return false, fmt.Errorf("found unresponsive ocode embed server on port %d (pid %d) but could not address it: %w", port, pid, findErr)
	}
	if !strayProcessStillMatchesFunc(int32(pid), port, matchArgs) {
		return false, fmt.Errorf("found an unresponsive ocode embed server candidate on port %d (pid %d), but could not verify its command line and listening-socket ownership — refusing to signal it", port, pid)
	}
	if sigErr := proc.Signal(syscall.SIGTERM); sigErr != nil {
		emitDiscoveryDebugAt("DISCOVERY", fmt.Sprintf("SIGTERM to stray ocode embed server pid %d failed (%v) — escalating to kill", pid, sigErr), false)
	}
	if waitForPortFree(port) {
		emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("reclaimed port %d from ocode embed server pid %d", port, pid))
		return true, nil
	}
	if !strayProcessStillMatchesFunc(int32(pid), port, matchArgs) {
		return false, fmt.Errorf("unresponsive ocode embed server candidate (pid %d) no longer matches or owns port %d — refusing to SIGKILL it", pid, port)
	}
	if killErr := proc.Kill(); killErr != nil {
		return false, fmt.Errorf("unresponsive ocode embed server (pid %d) is holding port %d and could not be killed: %w — kill it manually, then restart ocode", pid, port, killErr)
	}
	if waitForPortFree(port) {
		emitUserDiscoveryDebug("DISCOVERY", fmt.Sprintf("reclaimed port %d from ocode embed server pid %d (required SIGKILL)", port, pid))
		return true, nil
	}
	return false, fmt.Errorf("unresponsive ocode embed server (pid %d) still holds port %d after SIGKILL — kill it manually, then restart ocode", pid, port)
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
