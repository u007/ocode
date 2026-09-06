package remote

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
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
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
}

const remoteStateFilePath = "~/.ocode/remote/serve.json"

var remoteStateCatCmd = "cat " + shellQuotePath(remoteStateFilePath) + " 2>/dev/null || true"

// DiscoverServer reads and parses the remote state file. A missing file (no
// stdout), a read error, or a corrupt/partial JSON body all return
// ok=false — every one of those is "no reusable server," never a fatal
// connect error.
func DiscoverServer(t Transport) (ServeState, bool) {
	res, err := t.Exec(remoteStateCatCmd)
	if err != nil {
		return ServeState{}, false
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return ServeState{}, false
	}
	var state ServeState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		return ServeState{}, false
	}
	return state, true
}

func pidAliveCmd(pid int) string {
	return fmt.Sprintf("kill -0 %d 2>/dev/null", pid)
}

func healthProbeCmd(port int, token string) string {
	// Best-effort: curl ships on the overwhelming majority of target
	// systems ocode already requires (git, go toolchain era Linux/macOS).
	// A missing curl makes this probe report "000" (curl's own placeholder
	// for "no response"), which ServerAlive correctly treats as not-alive —
	// degrading to a fresh server start rather than failing the connect.
	return fmt.Sprintf(
		"curl -s -o /dev/null -w '%%{http_code}' -H %s http://127.0.0.1:%d/api/health",
		shellQuote("Authorization: Bearer "+token), port,
	)
}

// ServerAlive decides whether a discovered ServeState describes a server
// this client can reuse: the process must still be running, its version
// must match the connecting client's version exactly (a stale binary is
// never reused — same rule as the TUI's ActivateAndVerify), and it must
// actually answer /api/health with 200 (catches "process alive but HTTP
// stack wedged," and degrades gracefully when curl is unavailable).
func ServerAlive(t Transport, state ServeState, localVersion string) bool {
	if state.Version != localVersion {
		return false
	}
	if res, err := t.Exec(pidAliveCmd(state.PID)); err != nil || res.ExitCode != 0 {
		return false
	}
	res, err := t.Exec(healthProbeCmd(state.Port, state.Token))
	if err != nil {
		return false
	}
	return strings.TrimSpace(res.Stdout) == "200"
}

func launchServerCmd(ver string) string {
	remoteOcode := shellQuotePath(RemoteBinaryPath(ver))
	logPath := shellQuotePath("~/.ocode/remote/serve.log")
	// Redirect all three standard fds explicitly and disown the child: a
	// backgrounded process that still holds the ssh session's stdout/stderr
	// pipe open makes the outer non-interactive `ssh host cmd` hang waiting
	// for those fds to close, even after this shell returns. </dev/null
	// plus explicit redirects plus `disown` fully detaches it so `ssh`
	// returns as soon as this command's own shell exits.
	return fmt.Sprintf(
		"mkdir -p %s && nohup %s serve --remote --host 127.0.0.1 --port 0 </dev/null >%s 2>&1 & disown; echo launched",
		shellQuotePath("~/.ocode/remote"), remoteOcode, logPath,
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
func StartFreshServer(t Transport, ver string) (ServeState, error) {
	if _, err := t.Exec(launchServerCmd(ver)); err != nil {
		return ServeState{}, fmt.Errorf("launch remote server: %w", err)
	}
	for i := 0; i < serveStatePollAttempts; i++ {
		if state, ok := DiscoverServer(t); ok {
			return state, nil
		}
		time.Sleep(serveStatePollInterval)
	}
	return ServeState{}, fmt.Errorf("remote server did not write its state file within %s — check ~/.ocode/remote/serve.log on the remote", time.Duration(serveStatePollAttempts)*serveStatePollInterval)
}

// EnsureRemoteServer implements the reuse-vs-fresh decision table: discover
// → alive+matching-version → reuse; anything else (missing, dead,
// version-mismatched, unhealthy) → start fresh. reused reports which path
// was taken, for progress reporting.
func EnsureRemoteServer(t Transport, ver string) (state ServeState, reused bool, err error) {
	if existing, ok := DiscoverServer(t); ok && ServerAlive(t, existing, ver) {
		return existing, true, nil
	}
	fresh, err := StartFreshServer(t, ver)
	if err != nil {
		return ServeState{}, false, err
	}
	return fresh, false, nil
}

// FreeLocalPort asks the OS for an ephemeral free TCP port on 127.0.0.1 by
// binding to :0 and immediately releasing it — standard technique, with the
// usual (accepted) TOCTOU caveat that something else could grab it before
// the tunnel binds; ssh reports that failure directly and the caller
// retries once with a new port (see ConnectWeb, Task 7).
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

// StartTunnel starts `ssh -N -L localPort:127.0.0.1:remotePort <target>`
// under sup's supervision. It does not wait for the tunnel to establish or
// for it to exit — see ConnectWeb for the foreground supervise loop. Only
// meaningful for KindSSH targets; WSL never tunnels (Task 13).
func StartTunnel(sup *tool.ProcessSupervisor, target Target, localPort, remotePort int) (*exec.Cmd, error) {
	cmd := exec.Command("ssh", "-N", "-L", fmt.Sprintf("%d:127.0.0.1:%d", localPort, remotePort), target.String())
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
		ID:      "remote-tunnel",
		Name:    "ssh-tunnel",
		Command: cmd.String(),
		Kind:    tool.ProcessKindRemote,
	}); err != nil {
		return nil, err
	}
	return cmd, nil
}
