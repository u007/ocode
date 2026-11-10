package remote

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/crashguard"
)

// ErrTunnelInProgress is returned when a tunnel is still being established.
var ErrTunnelInProgress = fmt.Errorf("ssh tunnel is still being established")

// ErrTunnelCooldown is returned when a recent tunnel attempt failed.
var ErrTunnelCooldown = fmt.Errorf("ssh tunnel recently failed; retry shortly")

// errTunnelRetired means the state was stopped after Ensure fetched it. Ensure
// fetches a fresh state and tries again, so it never launches ssh on a state no
// Stop can reach.
var errTunnelRetired = errors.New("ssh tunnel was stopped")

const (
	tunnelConnectWait  = 2 * time.Second
	tunnelCooldown     = 5 * time.Second
	tunnelPortCheckGap = 100 * time.Millisecond
	tunnelMaxAttempts  = 3
	tunnelDeadTimeout  = 6 * time.Second
)

// tunnelState tracks one SSH tunnel.
type tunnelState struct {
	mu sync.Mutex
	// name is the saved connection this tunnel serves. It is set once, before
	// the state is published in DBTunnelManager.tunnels, and read under the
	// manager's lock.
	name      string
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	stderr    *bytes.Buffer
	localPort int
	ready     bool
	failed    bool
	failedAt  time.Time
	failErr   error
	lastTry   time.Time
	startedAt time.Time
	// credHash fingerprints the credentials the last attempt used. A cached
	// failure only blocks retries with those same credentials, so a corrected
	// password reaches ssh at once.
	credHash string
	// stopped is set under mu by Stop, StopAll and StopConnection. A state that
	// was fetched before the stop and locked after it must not launch ssh,
	// because no Stop can find a process it starts.
	stopped bool
}

// DBTunnelManager manages SSH tunnels for DB connections.
type DBTunnelManager struct {
	mu      sync.Mutex
	tunnels map[string]*tunnelState
}

func NewDBTunnelManager() *DBTunnelManager {
	return &DBTunnelManager{tunnels: map[string]*tunnelState{}}
}

var defaultDBTunnelManager = NewDBTunnelManager()

func DefaultDBTunnelManager() *DBTunnelManager { return defaultDBTunnelManager }

// sshBinary is the ssh executable; overridable in tests.
var sshBinary = "ssh"

// tunnelKey identifies one tunnel descriptor. The credentials are deliberately
// not part of it: Stop has only the descriptor, and a retry with a different
// password reuses the state (see tunnelState.credHash).
func tunnelKey(name string, t *config.DBTunnel) string {
	auth := t.AuthMethod
	if auth == "" {
		auth = "key" // add stores "" as "key"; both must find the same tunnel
	}
	return fmt.Sprintf("%s|%s|%d|%s|%d|%s|%d|%s", name, t.Host, t.Port, t.RemoteHost, t.RemotePort, t.IdentityFile, t.LocalPort, auth)
}

// tunnelCredHash fingerprints what ssh authenticates with. The digest is sha256
// hex, so the plaintext password never sits in the state or in a log.
func tunnelCredHash(t *config.DBTunnel, password string) string {
	sum := sha256.Sum256([]byte(t.AuthMethod + "\x00" + t.IdentityFile + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

// validateTunnelDescriptor checks the descriptor before use.
func ValidateTunnelDescriptor(t *config.DBTunnel) error {
	if t == nil || t.Host == "" || t.RemoteHost == "" || t.RemotePort == 0 {
		return fmt.Errorf("invalid tunnel descriptor")
	}
	tr := Target{Host: t.Host, Port: t.Port, User: ""}
	if err := tr.Validate(); err != nil {
		return fmt.Errorf("invalid tunnel host: %w", err)
	}
	// RemoteHost: hostname/IP, no whitespace/control chars; brackets allowed for IPv6.
	if strings.ContainsAny(t.RemoteHost, " \t\r\n") || strings.HasPrefix(t.RemoteHost, "-") {
		return fmt.Errorf("invalid remote host %q", t.RemoteHost)
	}
	if strings.Contains(t.RemoteHost, ":") && !strings.HasPrefix(t.RemoteHost, "[") {
		return fmt.Errorf("invalid remote host %q (use brackets for IPv6)", t.RemoteHost)
	}
	if t.RemotePort < 1 || t.RemotePort > 65535 {
		return fmt.Errorf("invalid remote port %d", t.RemotePort)
	}
	if t.LocalPort != 0 && (t.LocalPort < 1 || t.LocalPort > 65535) {
		return fmt.Errorf("invalid local port %d", t.LocalPort)
	}
	if t.IdentityFile != "" {
		if !strings.HasPrefix(t.IdentityFile, "/") {
			return fmt.Errorf("identity file must be an absolute path")
		}
		if strings.HasPrefix(t.IdentityFile, "-") || strings.ContainsAny(t.IdentityFile, "\r\n") {
			return fmt.Errorf("invalid identity file %q", t.IdentityFile)
		}
	}
	switch t.AuthMethod {
	case "", "key", "auto":
		if t.Password != "" {
			return fmt.Errorf("password is only allowed with auth_method \"password\"")
		}
	case "password":
	default:
		return fmt.Errorf("invalid auth_method %q", t.AuthMethod)
	}
	return nil
}

// stateFor returns the state for one connection, creating it on first use.
func (m *DBTunnelManager) stateFor(name string, t *config.DBTunnel) *tunnelState {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tunnelKey(name, t)
	ts, ok := m.tunnels[key]
	if !ok {
		ts = &tunnelState{name: name}
		m.tunnels[key] = ts
	}
	return ts
}

// Ensure establishes (or reuses) a tunnel and returns the local port. A tunnel
// whose ssh process has exited is started again.
func (m *DBTunnelManager) Ensure(ctx context.Context, name string, t *config.DBTunnel, password string) (int, error) {
	if err := ValidateTunnelDescriptor(t); err != nil {
		return 0, err
	}
	tr := Target{Host: t.Host, Port: t.Port, User: ""}

	for attempt := 0; attempt < tunnelMaxAttempts; attempt++ {
		ts := m.stateFor(name, t)
		port, err, retry := m.ensureOnce(ctx, ts, t, tr, password)
		if err == nil {
			return port, nil
		}
		if errors.Is(err, errTunnelRetired) {
			continue // stopped while we held the old state: take a fresh one
		}
		if !retry {
			return 0, err
		}
		// retryable: loop (possibly with a new port)
	}
	return 0, fmt.Errorf("ssh tunnel could not be established after %d attempts", tunnelMaxAttempts)
}

// ensureOnce performs a single establish/reuse attempt. retry=true means the
// caller may try again (e.g. port stolen, dead process detected at start).
func (m *DBTunnelManager) ensureOnce(ctx context.Context, ts *tunnelState, t *config.DBTunnel, tr Target, password string) (int, error, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.stopped {
		return 0, errTunnelRetired, true
	}
	// New credentials are a new attempt: they do not inherit the failure of the
	// old ones. An attempt still connecting under the old credentials is
	// abandoned. A ready tunnel is kept, because it already authenticated.
	if fp := tunnelCredHash(t, password); fp != ts.credHash {
		ts.credHash = fp
		ts.failed = false
		ts.failErr = nil
		if ts.cmd != nil && !ts.ready {
			killTunnelProcess(ts)
		}
	}

	// cooldown after recent failure
	if ts.failed && time.Since(ts.failedAt) < tunnelCooldown {
		if ts.failErr != nil {
			return 0, fmt.Errorf("%w: %v", ErrTunnelCooldown, ts.failErr), false
		}
		return 0, ErrTunnelCooldown, false
	}
	if ts.failed && time.Since(ts.failedAt) >= tunnelCooldown {
		ts.failed = false
		ts.failErr = nil
		ts.cmd = nil
		ts.ready = false
	}

	// already ready? The process exit handler clears ready and cmd, so a
	// tunnel whose ssh died falls through to a restart below.
	if ts.ready && ts.cmd != nil && ts.cmd.Process != nil {
		return ts.localPort, nil, false
	}

	// in progress?
	if ts.cmd != nil && !ts.ready && !ts.failed {
		// check if port now accepts
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", ts.localPort), 200*time.Millisecond)
		if err == nil {
			conn.Close()
			ts.ready = true
			return ts.localPort, nil, false
		}
		// give up waiting if too long → kill and fail
		if time.Since(ts.startedAt) > tunnelDeadTimeout {
			killTunnelProcess(ts)
			ts.failErr = fmt.Errorf("ssh tunnel did not become ready in time")
			ts.failed = true
			ts.failedAt = time.Now()
			return 0, ts.failErr, true
		}
		return 0, ErrTunnelInProgress, false
	}

	// allocate local port if needed
	localPort := t.LocalPort
	if localPort == 0 {
		// Keep the previous port when it is still free. A pooled DB handle has
		// the port in its URL, so a restart on a new one strands the pool.
		if ts.localPort != 0 && portIsFree(ts.localPort) {
			localPort = ts.localPort
		} else {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return 0, fmt.Errorf("allocate local port: %w", err), false
			}
			localPort = ln.Addr().(*net.TCPAddr).Port
			ln.Close()
		}
	}

	// Determine auth mode.
	usePassword := t.AuthMethod == "password" && password != ""

	// build ssh args
	args := []string{
		"-N",
		"-L", fmt.Sprintf("127.0.0.1:%d:%s:%d", localPort, t.RemoteHost, t.RemotePort),
		"-o", "ExitOnForwardFailure=yes",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if usePassword {
		args = append(args,
			"-o", "BatchMode=no",
			"-o", "PreferredAuthentications=password",
			"-o", "PubkeyAuthentication=no",
		)
	} else {
		args = append(args, "-o", "BatchMode=yes")
	}
	if t.IdentityFile != "" && !usePassword {
		args = append(args, "-i", t.IdentityFile)
	}
	if t.Port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d", t.Port))
	}
	args = append(args, "--", tr.String())

	cmdCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(cmdCtx, sshBinary, args...)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	cmd.Stdout = &bytes.Buffer{}

	// Password auth via SSH_ASKPASS (OpenSSH >= 8.4). The helper script reads
	// the password from a separate file so the password never touches the
	// shell command line or the process environment.
	var askpassCleanup func()
	if usePassword {
		scriptPath, passwordPath, cleanup, err := createAskpassFiles(password)
		if err != nil {
			ts.failed = true
			ts.failedAt = time.Now()
			ts.failErr = err
			cancel()
			return 0, err, false
		}
		askpassCleanup = cleanup
		cmd.Env = append(os.Environ(),
			"SSH_ASKPASS="+scriptPath,
			"SSH_ASKPASS_REQUIRE=force",
			"DISPLAY=:0",
		)
		_ = passwordPath
	}

	ts.cmd = cmd
	ts.cancel = cancel
	ts.stderr = stderr
	ts.localPort = localPort
	ts.lastTry = time.Now()
	ts.startedAt = time.Now()
	ts.ready = false
	ts.failed = false

	if err := cmd.Start(); err != nil {
		ts.failed = true
		ts.failedAt = time.Now()
		ts.failErr = err
		ts.cmd = nil
		cancel()
		if askpassCleanup != nil {
			askpassCleanup()
		}
		return 0, err, true
	}
	// Reap the process when it exits. The result goes to doneCh BEFORE the lock:
	// the connect loop below holds ts.mu while it polls doneCh, so taking the
	// lock first would hide the exit until the connect window ran out.
	doneCh := make(chan error, 1)
	crashguard.Go(func() {
		err := cmd.Wait()
		if askpassCleanup != nil {
			askpassCleanup()
		}
		doneCh <- err

		ts.mu.Lock()
		defer ts.mu.Unlock()
		if ts.cmd != cmd {
			return // already stopped, failed, or superseded by a newer process
		}
		if !ts.ready {
			// Died before it ever accepted: record why, so the caller gets the
			// real error and the cooldown applies. stderr is safe to read here:
			// Wait has returned, so its copy has finished.
			msg := strings.TrimSpace(stderr.String())
			if msg == "" && err != nil {
				msg = err.Error()
			}
			if msg == "" {
				msg = "ssh exited"
			}
			ts.failed = true
			ts.failedAt = time.Now()
			ts.failErr = fmt.Errorf("ssh tunnel exited: %s", msg)
		}
		// A ready tunnel that exits is only cleared, so the next Ensure restarts
		// it at once, without a cooldown.
		ts.cmd = nil
		ts.ready = false
		if ts.cancel != nil {
			ts.cancel()
		}
	})

	deadline := time.Now().Add(tunnelConnectWait)
	ready := false
loop:
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			ts.failed = true
			ts.failedAt = time.Now()
			ts.failErr = ctx.Err()
			killTunnelProcess(ts)
			return 0, ctx.Err(), false
		case err := <-doneCh:
			msg := strings.TrimSpace(stderr.String())
			if msg == "" && err != nil {
				msg = err.Error()
			}
			if msg == "" {
				msg = "ssh exited"
			}
			ts.failed = true
			ts.failedAt = time.Now()
			ts.failErr = fmt.Errorf("ssh tunnel exited: %s", msg)
			ts.cmd = nil
			ts.ready = false
			cancel()
			return 0, ts.failErr, true
		default:
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", localPort), 200*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break loop
		}
		time.Sleep(tunnelPortCheckGap)
	}

	if ready {
		ts.ready = true
		return localPort, nil, false
	}
	// still alive but not ready → in progress
	ts.lastTry = time.Now()
	return 0, ErrTunnelInProgress, false
}

// portIsFree reports whether 127.0.0.1:port can be bound right now.
func portIsFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// killTunnelProcess cancels and kills ts's ssh process and clears the state
// that described it. The caller holds ts.mu.
func killTunnelProcess(ts *tunnelState) {
	if ts.cancel != nil {
		ts.cancel()
	}
	if ts.cmd != nil && ts.cmd.Process != nil {
		_ = ts.cmd.Process.Kill()
	}
	ts.cmd = nil
	ts.ready = false
}

// retire marks the state stopped and kills its process. A racing Ensure that
// holds this state sees stopped and does not launch ssh.
func (ts *tunnelState) retire() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.stopped = true
	killTunnelProcess(ts)
}

// Stop tears down the tunnel for one connection descriptor.
func (m *DBTunnelManager) Stop(name string, t *config.DBTunnel) {
	key := tunnelKey(name, t)
	m.mu.Lock()
	ts, ok := m.tunnels[key]
	if ok {
		delete(m.tunnels, key)
	}
	m.mu.Unlock()
	if ok {
		ts.retire()
	}
}

// StopConnection tears down every tunnel saved for one connection name. Used
// when a connection is removed: the caller need not still hold its descriptor.
func (m *DBTunnelManager) StopConnection(name string) {
	m.mu.Lock()
	var victims []*tunnelState
	for key, ts := range m.tunnels {
		if ts.name == name {
			victims = append(victims, ts)
			delete(m.tunnels, key)
		}
	}
	m.mu.Unlock()
	for _, ts := range victims {
		ts.retire()
	}
}

// StopAll tears down all tunnels.
func (m *DBTunnelManager) StopAll() {
	m.mu.Lock()
	victims := make([]*tunnelState, 0, len(m.tunnels))
	for _, ts := range m.tunnels {
		victims = append(victims, ts)
	}
	m.tunnels = map[string]*tunnelState{}
	m.mu.Unlock()
	for _, ts := range victims {
		ts.retire()
	}
}

// createAskpassFiles writes a helper script + password file for SSH_ASKPASS.
// The password is never placed on a command line or in the environment.
// Returns paths and a cleanup function that removes both files.
func createAskpassFiles(password string) (string, string, func(), error) {
	dir, err := os.MkdirTemp("", "ocode-askpass-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create askpass dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return "", "", nil, err
	}
	passwordPath := dir + "/password"
	if err := os.WriteFile(passwordPath, []byte(password), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", "", nil, err
	}
	scriptPath := dir + "/askpass.sh"
	script := "#!/bin/sh\ncat " + shellQuote(passwordPath) + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		os.RemoveAll(dir)
		return "", "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	return scriptPath, passwordPath, cleanup, nil
}
