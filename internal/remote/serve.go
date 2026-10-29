package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// ServeState mirrors internal/server's remoteServeState JSON shape — the
// wire contract for ~/.ocode/remote/serve.json. Field names/JSON tags must
// stay in sync with internal/server/server.go's remoteServeState; the two
// packages don't share a type (server can't import remote without a cycle:
// remote's CLI-side code is what drives server.Run in the first place).
type ServeState struct {
	PID  int `json:"pid"`
	Port int `json:"port"`
	// BrowsePort is the browse-origin port (embedded browser panel); 0 when
	// the remote server has no browse origin. Tunneled on the same local
	// port number so the SPA's /api/browse/config base URL stays valid.
	BrowsePort int       `json:"browsePort"`
	Token      string    `json:"token"`
	Version    string    `json:"version"`
	StartedAt  time.Time `json:"startedAt"`
	// Outdated reports that a reused server runs a different version than the
	// connecting client. It is derived at discovery time and never persisted to
	// the state file (json:"-"), so a state file written by an older server is
	// unaffected by the field.
	Outdated bool `json:"-"`
}

// remoteStateDir holds one state file and one log per server slot.
const remoteStateDir = "~/.ocode/remote"

// ServeSlot names one remote server instance. The empty slot is the single
// per-host server (`ocode remote --web`); a non-empty slot is a per-project
// server whose state file is serve-<slot>.json, so projects on one host never
// share a process, a token or a failure.
type ServeSlot string

var safeServeSlot = regexp.MustCompile(`^[0-9a-f]{0,16}$`)

// SlotForProject derives the stable slot for a project root on the remote.
func SlotForProject(projectPath string) ServeSlot {
	sum := sha256.Sum256([]byte(projectPath))
	return ServeSlot(hex.EncodeToString(sum[:6]))
}

func (s ServeSlot) valid() error {
	if !safeServeSlot.MatchString(string(s)) {
		return fmt.Errorf("unsafe serve slot %q", string(s))
	}
	return nil
}

func (s ServeSlot) stateFile() string {
	if s == "" {
		return remoteStateDir + "/serve.json"
	}
	return remoteStateDir + "/serve-" + string(s) + ".json"
}

func (s ServeSlot) logFile() string {
	if s == "" {
		return remoteStateDir + "/serve.log"
	}
	return remoteStateDir + "/serve-" + string(s) + ".log"
}

// serveArgs is the slot's extra `serve` argument string (leading space).
func (s ServeSlot) serveArgs() string {
	if s == "" {
		return ""
	}
	return " --state-slot " + string(s)
}

func stateCatCmd(slot ServeSlot) string {
	return "cat " + shellQuotePath(slot.stateFile()) + " 2>/dev/null || true"
}

// DiscoverServer reads and parses the remote state file. A missing file (no
// stdout), a read error, or a corrupt/partial JSON body all return
// ok=false — every one of those is "no reusable server," never a fatal
// connect error.
func DiscoverServer(t Transport, slot ServeSlot) (ServeState, bool) {
	state, ok, _ := discoverServer(t, slot)
	return state, ok
}

// sshExitCode is the exit status ssh itself returns for a connection-level
// failure (as opposed to the remote command's own status).
const sshExitCode = 255

// transportFailed reports whether an Exec error means the command never got a
// verdict: ssh could not reach the host (exit 255), or the run failed outright
// (a context timeout, or a process killed by a signal, which
// exec.ExitError.ExitCode reports as -1). A remote command that ran and exited
// non-zero is NOT a transport failure; that is an answer.
func transportFailed(res ExecResult, err error) bool {
	return err != nil && (res.ExitCode <= 0 || res.ExitCode == sshExitCode)
}

// discoverServer is DiscoverServer plus the transport error, so a caller can
// tell "the host did not answer" from "there is no server". The state-file cat
// ends in `|| true`, so any Exec error here is a transport failure.
func discoverServer(t Transport, slot ServeSlot) (ServeState, bool, error) {
	if slot.valid() != nil {
		return ServeState{}, false, nil
	}
	res, err := t.Exec(stateCatCmd(slot))
	if err != nil {
		return ServeState{}, false, fmt.Errorf("read remote state file: %w", err)
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return ServeState{}, false, nil
	}
	var state ServeState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		return ServeState{}, false, nil
	}
	return state, true, nil
}

func pidAliveCmd(pid int) string {
	return fmt.Sprintf("kill -0 %d 2>/dev/null", pid)
}

// probeCurlMissing is what healthProbeCmd prints when the host has no usable
// curl. It is deliberately NOT an HTTP-ish value: "the probe could not run" and
// "the server did not answer" are different answers, and only the second one
// means the server is wedged.
const probeCurlMissing = "no-curl"

func healthProbeCmd(port int) string {
	// Best-effort liveness probe. Two properties matter here:
	//
	//   - It must always produce a VERDICT, so curl carries a timeout. Without
	//     one, a server that accepts TCP but never answers hangs the probe until
	//     connectBounded's 10-minute backstop, which is indistinguishable from a
	//     wedged connect.
	//   - It must distinguish "curl is not installed" from "nothing answered".
	//     Conflating them makes every reconnect on a curl-less host fail: the
	//     pid is alive, so the server is never relaunched, and the health check
	//     can never pass either. The explicit probeCurlMissing marker lets
	//     probeServer treat an unmeasurable server as reusable instead of
	//     replacing (and thereby killing) a live one.
	//
	// No Authorization header: /api/health is deliberately unauthenticated
	// (see internal/server/handler_health.go), and putting the token into
	// this command string would land it in the remote host's process
	// listing (ps) for the probe's duration — the same class of leak
	// Transport.ExecStdin's doc warns against.
	return fmt.Sprintf(
		"if command -v curl >/dev/null 2>&1; then "+
			"curl -s -m 5 --connect-timeout 3 -o /dev/null -w '%%{http_code}' http://127.0.0.1:%d/api/health; "+
			"else echo %s; fi",
		port, probeCurlMissing,
	)
}

// serverHealthy reports whether a discovered ServeState describes a server
// this client can reuse: the process must still be running and it must either
// answer /api/health with 200 or be unmeasurable on this host (no curl — see
// probeServer). Version is deliberately not part of this decision — a
// version-mismatched but healthy server is reused (EnsureRemoteServer flags it
// Outdated) rather than replaced, so its terminals and in-flight turns are not
// orphaned.
func serverHealthy(t Transport, state ServeState) bool {
	alive, healthy, _ := probeServer(t, state)
	return alive && healthy
}

// probeServer reports whether the state file's server pid is alive and whether
// it answers /api/health. err is non-nil only when the host could not be asked
// at all (transport failure, including a probe killed by a signal), because in
// that case the server's fate is unknown and the caller must not replace it: a
// replaced server takes every terminal on it down.
//
// healthy is true when the server answered 200, and ALSO when its health could
// not be measured on this host (no usable curl). An unmeasurable server is
// reused rather than replaced — the same "never destroy what you cannot verify"
// rule, resolved the safe way. A probe that ran and got a non-200 answer is
// NOT healthy: that is a wedged server, which only the explicit Restart retires.
func probeServer(t Transport, state ServeState) (alive, healthy bool, err error) {
	res, execErr := t.Exec(pidAliveCmd(state.PID))
	if transportFailed(res, execErr) {
		return false, false, fmt.Errorf("check remote server pid %d: %w", state.PID, execErr)
	}
	if execErr != nil || res.ExitCode != 0 {
		return false, false, nil
	}
	res, execErr = t.Exec(healthProbeCmd(state.Port))
	if transportFailed(res, execErr) {
		return true, false, fmt.Errorf("probe remote server pid %d health: %w", state.PID, execErr)
	}
	switch out := strings.TrimSpace(res.Stdout); out {
	case "200":
		return true, true, nil
	case probeCurlMissing, "":
		// The probe could not run (missing or unusable curl) or produced no
		// output at all. Liveness is proven by the pid check, so keep the server
		// instead of guessing that it is wedged and killing its terminals.
		return true, true, nil
	default:
		// A completed probe that is not 200: the server is running but not
		// serving. An answer, not an error — EnsureRemoteServer turns it into
		// the actionable "restart it explicitly".
		return true, false, nil
	}
}

func launchServerCmd(ver string, slot ServeSlot) string {
	remoteOcode := shellQuotePath(RemoteBinaryPath(ver))
	logPath := shellQuotePath(slot.logFile())
	statePath := shellQuotePath(slot.stateFile())
	// Delete any existing state file before launching: StartFreshServer only
	// gets here because the previously discovered state was unusable (dead
	// pid, version mismatch, unhealthy) — not because the file was missing —
	// so it's often still present and still parses. Without this delete, the
	// poll loop below could re-read that stale file on its very first
	// iteration and return it as if it were the new server's, silently
	// reconnecting to the wrong (old/mismatched-version) server. `rm -f` runs
	// synchronously (`;`, not part of the backgrounded `&&` chain) so it has
	// completed by the time this Exec call returns, making that stale read
	// structurally impossible: the poll can only ever see nothing (keep
	// polling) or the genuinely new server's fresh state.
	//
	// Redirect all three standard fds and detach the child in its own
	// backgrounded subshell: a backgrounded process that still holds the ssh
	// session's stdout/stderr pipe open makes the outer non-interactive
	// `ssh host cmd` hang until that process exits, even after this shell
	// returns. `&&` before a bare `nohup … &` is the trap: `mkdir && nohup … &`
	// backgrounds the WHOLE `mkdir && nohup` list, so the forked subshell waits
	// on the (long-lived) server before exiting and keeps the ssh channel open —
	// StartFreshServer's Exec then blocks until the server dies, which the
	// connect backstop reports as a 502 after its timeout. Wrapping the nohup in
	// `( … & )` keeps the mkdir guard while letting the subshell return
	// immediately, so `ssh` sees EOF as soon as this command's shell exits.
	//
	// The version dir goes first on PATH so the server's agent shell resolves a
	// bare `htrcli` to the version-matched install (see EnsureHTRCli).
	versionDir := shellQuotePath(RemoteBinDir + "/" + ver)
	return fmt.Sprintf(
		"rm -f %s; mkdir -p %s && (PATH=%s:\"$PATH\" nohup %s serve --remote --host 127.0.0.1 --port 0%s </dev/null >%s 2>&1 &); echo launched",
		statePath, shellQuotePath(remoteStateDir), versionDir, remoteOcode, slot.serveArgs(), logPath,
	)
}

// serveStatePollInterval/Attempts bound how long StartFreshServer waits for
// the just-launched server to write its state file. Package-level vars
// (not consts) so tests can shrink them instead of taking seconds per run.
var (
	serveStatePollInterval = 250 * time.Millisecond
	serveStatePollAttempts = 40 // 10s total at the default interval
)

// StartFreshServer launches a detached `ocode serve --remote` on t and
// blocks until its state file appears (bounded by
// serveStatePollAttempts × serveStatePollInterval), returning the parsed
// state. Used when DiscoverServer finds nothing reusable.
func StartFreshServer(t Transport, ver string, slot ServeSlot) (ServeState, error) {
	if err := slot.valid(); err != nil {
		return ServeState{}, err
	}
	if res, err := t.Exec(launchServerCmd(ver, slot)); err != nil {
		return ServeState{}, fmt.Errorf("launch remote server: %w: %s", err, res.Stderr)
	}
	for i := 0; i < serveStatePollAttempts; i++ {
		if state, ok := DiscoverServer(t, slot); ok {
			return state, nil
		}
		time.Sleep(serveStatePollInterval)
	}
	return ServeState{}, fmt.Errorf("remote server did not write its state file within %s — check %s on the remote", time.Duration(serveStatePollAttempts)*serveStatePollInterval, slot.logFile())
}

// EnsureRemoteServer implements the reuse-vs-fresh decision table: discover →
// alive and healthy → reuse (flagging Outdated when the version differs);
// anything else (missing, dead, unhealthy) → start fresh at the local version.
// reused reports which path was taken, for progress reporting.
func EnsureRemoteServer(t Transport, ver string, slot ServeSlot) (state ServeState, reused bool, err error) {
	if err := slot.valid(); err != nil {
		return ServeState{}, false, err
	}
	existing, ok, err := discoverServer(t, slot)
	if err != nil {
		return ServeState{}, false, err
	}
	if ok {
		alive, healthy, err := probeServer(t, existing)
		if err != nil {
			return ServeState{}, false, err
		}
		if alive && !healthy {
			// A live pid that does not answer /api/health is wedged, or just slow
			// behind a flaky link. Replacing it would kill its terminals; the
			// explicit Restart action is the way to retire it.
			return ServeState{}, false, fmt.Errorf("remote server pid %d is running but not answering /api/health; restart it explicitly", existing.PID)
		}
		if alive {
			return retireOrReuse(t, ver, slot, existing)
		}
	}
	return startAndSettle(t, ver, slot)
}

// retireOrReuse handles a live, healthy server: reuse it when it matches the
// local version, otherwise retire it and start a fresh one.
func retireOrReuse(t Transport, ver string, slot ServeSlot, existing ServeState) (ServeState, bool, error) {
	existing.Outdated = existing.Version != ver
	if !existing.Outdated {
		return existing, true, nil
	}
	// A patch update leaves the previous version's server running and
	// holding memory; retire it and start one at the local version.
	if err := KillServer(t, existing.PID, slot); err != nil {
		return ServeState{}, false, fmt.Errorf("retire outdated remote server v%s: %w", existing.Version, err)
	}
	return startAndSettle(t, ver, slot)
}

// startAndSettle launches a fresh server and reaps leftovers around it.
func startAndSettle(t Transport, ver string, slot ServeSlot) (ServeState, bool, error) {
	fresh, err := StartFreshServer(t, ver, slot)
	if err != nil {
		return ServeState{}, false, err
	}
	if err := killOtherVersionServers(t, ver); err != nil {
		log.Printf("remote: reap old-version servers on %s: %v", t, err)
	}
	fresh, reap := settleOnStateFileServer(t, ver, slot, fresh)
	if reap {
		if err := killOrphanSlotServers(t, ver, slot, fresh.PID); err != nil {
			log.Printf("remote: reap orphaned %q servers on %s: %v", string(slot), t, err)
		}
	}
	// A freshly launched server always reports the local version; a stale
	// Outdated value can never leak in from the state file (json:"-").
	fresh.Outdated = false
	return fresh, false, nil
}

// settleOnStateFileServer re-reads the state file immediately before the orphan
// reap and returns the server this client should converge on, plus whether the
// reap may run. Concurrent connects each relaunch (launchServerCmd deletes the
// state file first), so the server a sibling client discovered is the one the
// state file names now, not necessarily the one this client launched. Outcomes:
//   - the file names our own server: keep it and reap around it;
//   - it names a different, healthy, same-version server: a sibling won, so
//     adopt it and reap around it. Every client converges on the state-file
//     server, so the reaper never kills the server a client is returning;
//   - the file is missing or unreadable, or names an unhealthy or other-version
//     server: a sibling launch is in flight (its rm has run, its server has not
//     written yet) or the named server is unusable. Skip the reap, because a
//     server about to be discovered is indistinguishable from an orphan.
//     Leftovers are reaped by a later launch.
//
// Residual window: one Exec round trip between this read and the kill. A launch
// that starts inside it can lose a server nobody has discovered yet; that
// client's state-file poll fails loudly rather than using a dead server.
func settleOnStateFileServer(t Transport, ver string, slot ServeSlot, fresh ServeState) (ServeState, bool) {
	current, ok := DiscoverServer(t, slot)
	if !ok {
		log.Printf("remote: state file for slot %q missing or unreadable on %s; skipping orphan reap", string(slot), t.Describe())
		return fresh, false
	}
	if current.PID == fresh.PID {
		return fresh, true
	}
	if current.Version != ver || !serverHealthy(t, current) {
		log.Printf("remote: state file on %s names pid %d (version %q, not reusable); skipping orphan reap", t.Describe(), current.PID, current.Version)
		return fresh, false
	}
	return current, true
}

// otherVersionServersCmd signals (with sig) every
// `<RemoteBinDir>/<version>/ocode serve` process whose version dir is not ver.
// KillServer only knows the single pid in the state file, so servers orphaned
// by earlier connects (a state file overwritten by a newer launch) stay alive
// and hold memory until reaped here. The awk script never matches its own
// command line: the ssh shell's $2 is "-c", not an ocode binary path.
func otherVersionServersCmd(ver, sig string) string {
	return fmt.Sprintf(`ps -eo pid=,args= | awk -v keep="$HOME/.ocode/bin/%s/ocode" '$2 ~ /\/\.ocode\/bin\/[^\/]+\/ocode$/ && $3 == "serve" && $2 != keep {print $1}' | xargs -r kill %s 2>/dev/null; true`, ver, sig)
}

// orphanSlotServersCmd signals every same-version server of ONE slot except
// keepPID. Concurrent connects each relaunch (launchServerCmd deletes the state
// file first), so all but the last launch lose their state file and would live
// forever; the slot match (`--state-slot <slot>`, or no such flag for the
// legacy slot) keeps a sibling project's live server out of the blast radius.
func orphanSlotServersCmd(ver string, slot ServeSlot, keepPID int, sig string) string {
	want := "0"
	if slot != "" {
		want = "1"
	}
	return fmt.Sprintf(`ps -eo pid=,args= | awk -v bin="$HOME/.ocode/bin/%s/ocode" -v slot=%q -v want=%s -v keep=%d '$2 == bin && $3 == "serve" && $1 != keep { has = 0; for (i = 4; i < NF; i++) if ($i == "--state-slot" && $(i+1) == slot) has = 1; if (has == want) print $1 }' | xargs -r kill %s 2>/dev/null; true`, ver, string(slot), want, keepPID, sig)
}

// killOrphanSlotServers terminates same-version, same-slot servers other than
// keepPID (SIGTERM, then SIGKILL for survivors).
func killOrphanSlotServers(t Transport, ver string, slot ServeSlot, keepPID int) error {
	if !safeVersion.MatchString(ver) {
		return fmt.Errorf("unsafe version string %q", ver)
	}
	if res, err := t.Exec(orphanSlotServersCmd(ver, slot, keepPID, "-TERM")); err != nil {
		return fmt.Errorf("terminate orphaned servers: %w: %s", err, res.Stderr)
	}
	if res, err := t.Exec(orphanSlotServersCmd(ver, slot, keepPID, "-KILL")); err != nil {
		return fmt.Errorf("kill orphaned servers: %w: %s", err, res.Stderr)
	}
	return nil
}

var safeVersion = regexp.MustCompile(`^[0-9A-Za-z._+-]+$`)

// killOtherVersionServers terminates remote servers left over from other
// ocode versions (SIGTERM, then SIGKILL for survivors).
func killOtherVersionServers(t Transport, ver string) error {
	if !safeVersion.MatchString(ver) {
		return fmt.Errorf("unsafe version string %q", ver)
	}
	if res, err := t.Exec(otherVersionServersCmd(ver, "-TERM")); err != nil {
		return fmt.Errorf("terminate old servers: %w: %s", err, res.Stderr)
	}
	time.Sleep(2 * time.Second)
	if res, err := t.Exec(otherVersionServersCmd(ver, "-KILL")); err != nil {
		return fmt.Errorf("kill old servers: %w: %s", err, res.Stderr)
	}
	return nil
}

// killTERMCmd / killKILLCmd are the two signal commands KillServer issues.
// Package-level funcs (not inline fmt.Sprintf calls) so tests can assert the
// exact command sequence.
func killTERMCmd(pid int) string { return fmt.Sprintf("kill %d 2>/dev/null", pid) }
func killKILLCmd(pid int) string { return fmt.Sprintf("kill -9 %d 2>/dev/null", pid) }

// killServerPoll* bound how long KillServer waits for a terminated process to
// disappear. Package-level vars so tests can shrink them.
var (
	killServerPollAttempts = 20
	killServerPollInterval = 100 * time.Millisecond
)

// KillServer terminates the remote server process pid with SIGTERM, waits
// (bounded) for it to exit, escalates to SIGKILL once, and finally removes the
// remote state file so a later discovery cannot reuse the dead pid. It returns
// an error if the process is still alive after SIGKILL.
func KillServer(t Transport, pid int, slot ServeSlot) error {
	if err := slot.valid(); err != nil {
		return err
	}
	if pid <= 0 {
		return fmt.Errorf("kill remote server: invalid pid %d", pid)
	}
	if res, err := t.Exec(killTERMCmd(pid)); err != nil {
		return fmt.Errorf("kill remote server pid %d: %w: %s", pid, err, res.Stderr)
	}
	alive := true
	for i := 0; i < killServerPollAttempts; i++ {
		res, err := t.Exec(pidAliveCmd(pid))
		if err != nil || res.ExitCode != 0 {
			alive = false
			break
		}
		time.Sleep(killServerPollInterval)
	}
	if alive {
		if res, err := t.Exec(killKILLCmd(pid)); err != nil {
			return fmt.Errorf("kill -9 remote server pid %d: %w: %s", pid, err, res.Stderr)
		}
		if res, err := t.Exec(pidAliveCmd(pid)); err == nil && res.ExitCode == 0 {
			return fmt.Errorf("remote server pid %d still alive after SIGKILL", pid)
		}
	}
	if res, err := t.Exec("rm -f " + shellQuotePath(slot.stateFile())); err != nil {
		return fmt.Errorf("remove remote state file: %w: %s", err, res.Stderr)
	}
	return nil
}

// sshForwardBaseArgs is the option block shared by every port-forwarding ssh
// invocation (api/browse tunnel and project port maps): no remote command,
// keepalives, and exit when a forward cannot be bound.
func sshForwardBaseArgs() []string {
	args := append([]string{"-N"}, sshKeepaliveArgs()...)
	return append(args, "-o", "ExitOnForwardFailure=yes")
}

// tunnelArgs builds the ssh argument list for StartTunnel. It carries the same
// keepalive options as the interactive shell (sshKeepaliveArgs): a tunnel with
// no ServerAlive* never notices a silently-dropped transport, so the desktop
// proxy and the port forwards it carries hang forever instead of failing and
// being rebuilt.
// tunnelArgs builds the ssh argv for the api/browse tunnel. The browse forward
// comes BEFORE the api forward, and ExitOnForwardFailure=yes makes ssh exit
// on the first forward it cannot bind: the browse port is the remote's own
// number, so a local collision (another server's browse listener, a leftover
// forward) is possible, and without the exit ssh would keep running while a
// dial of that port reached the unrelated local service. With this order, the
// api listener (a fresh FreeLocalPort) exists only once the browse bind
// succeeded, so a successful dial of the api port proves both forwards.
func tunnelArgs(localPort, remotePort, browsePort int, target string) []string {
	args := sshForwardBaseArgs()
	if browsePort > 0 {
		args = append(args, "-L", fmt.Sprintf("%d:127.0.0.1:%d", browsePort, browsePort))
	}
	args = append(args, "-L", fmt.Sprintf("%d:127.0.0.1:%d", localPort, remotePort))
	return append(args, target)
}

// FreeLocalPort asks the OS for an ephemeral free TCP port on 127.0.0.1 by
// binding to :0 and immediately releasing it — standard technique, with the
// usual (accepted) TOCTOU caveat that something else could grab it before
// the tunnel binds; ssh reports that failure directly and the caller
// retries once with a new port (see ConnectWeb's startTunnelWithRetry).
func FreeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portStr)
}

// StartTunnel starts `ssh -N -L localPort:127.0.0.1:remotePort [-L
// browsePort:127.0.0.1:browsePort] <target>` under sup's supervision. The
// browse origin is forwarded on the same port number (skipped when
// browsePort is 0) because the SPA learns that port from the remote's
// /api/browse/config and cannot be told about a different local one. It does not wait for the tunnel to establish or
// for it to exit — see ConnectWeb for the foreground supervise loop. Only
// meaningful for KindSSH targets; WSL never tunnels — Windows forwards
// WSL2 localhost natively, so the browser can reach the WSL server's port
// directly with no tunnel process needed.
//
// The registration ID is keyed on localPort rather than a fixed string:
// ProcessSupervisor.Register rejects a duplicate ID outright, and
// StartSupervised's canReplace only allows replacing a terminal
// ProcessKindBrowser record — never ProcessKindRemote — so a retry (a new
// FreeLocalPort + a second StartTunnel call on the same supervisor, after a
// first bind failure) would otherwise always fail with "already
// registered." A fresh port on each retry makes the ID naturally unique.
func StartTunnel(sup *tool.ProcessSupervisor, target Target, localPort, remotePort, browsePort int) (*exec.Cmd, error) {
	args := tunnelArgs(localPort, remotePort, browsePort, target.String())
	if target.Port > 0 {
		args = append(args[:len(args)-1], "-p", strconv.Itoa(target.Port), args[len(args)-1])
	}
	cmd := exec.Command("ssh", args...)
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:      fmt.Sprintf("remote-tunnel-%d", localPort),
		Name:    "ssh-tunnel",
		Command: cmd.String(),
		Kind:    tool.ProcessKindRemote,
	}); err != nil {
		return nil, err
	}
	return cmd, nil
}
