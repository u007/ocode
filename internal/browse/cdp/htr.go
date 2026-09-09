package cdp

// HTR NControl companion: supervised `htrcli serve` daemon + extension-dir
// resolution. Cross-platform (darwin/linux/windows), no new dependencies.
//
// Architecture:
//
//	ocode server (supervisor owner)
//	  └─ `htrcli serve --no-tray` (ProcessKindHTR, ID "htr-serve")
//	       ├─ HTTP API on 127.0.0.1:<port> (default 3846, /api/health)
//	       └─ native-messaging relay to the preloaded extension
//	ocode headless Chrome --load-extension=<HTRExtensionDir> (see launch.go)
//
// Lifetime: the daemon is retained across individual ocode shutdowns while
// any cross-process lease is alive. Heartbeats expire after leaseTTL; the last
// explicit release terminates only the daemon recorded in ocode's own marker.
// A standalone htrcli daemon is on port 3845 and has no ocode marker, so it is
// never adopted or stopped.
//
// Limits (documented, not silently degraded):
//   - The Chrome profile stays ephemeral (fresh tmpDir per launch), so
//     extension storage does not persist across Chrome restarts.
//   - Unpacked loads without a pinned manifest `key` get a random extension
//     ID per profile. Release builds should provide
//     OCODE_HTR_EXTENSION_ORIGIN; direct `htrcli --cdp` remains available.
//   - Branded Google Chrome 137+ ignores --load-extension; Chromium, Canary,
//     Edge and Brave still honor it. A warning is logged at launch.
//   - Windows has the HTR daemon and asset path; Chrome mode remains gated by
//     its existing browser support policy.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/filelock"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/tool"
)

// DefaultHTRPort is the ocode-managed port. Standalone htrcli keeps 3845;
// using a separate default is what prevents ocode from attaching to or
// stopping a user's existing daemon.
const DefaultHTRPort = 3846

const (
	DefaultHTRNativeHostName = "com.ocode.htrcontrol"
	managedHTRDir            = "htr"
	leaseHeartbeat           = 5 * time.Second
	leaseTTL                 = 20 * time.Second
)

// htrServeID is the supervisor registration ID for the daemon.
const htrServeID = "htr-serve"

// htrOwnerFileName is the ownership record for multi-process takeover.
const htrOwnerFileName = "ocode-owner.json"

// HTROptions configures the companion. Zero value = disabled.
type HTROptions struct {
	Enabled        bool
	CliPath        string // "" = resolve via env/PATH
	ExtensionDir   string // "" = no extension preload (Chrome default)
	Port           int    // 0 = DefaultHTRPort
	SocketPath     string // "" = ocode-managed socket path
	NativeHostName string // "" = DefaultHTRNativeHostName
	BrowserPath    string // selected browser, used for native-host registration
}

// HTRStatus describes the daemon state after EnsureHTRServe.
type HTRStatus struct {
	Running bool   // health probe passed
	Owned   bool   // this process spawned it (retained across supervisor shutdown while leases exist)
	Addr    string // 127.0.0.1:port
	Binary  string // resolved htrcli binary ("" when reused and unknown)
	Socket  string
	Notice  string
	Release func()
}

// NormalizeHTRPort maps 0 to the default and validates the range.
func NormalizeHTRPort(port int) (int, error) {
	if port == 0 {
		return DefaultHTRPort, nil
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("htr port must be 1-65535, got %d", port)
	}
	return port, nil
}

// htrPortEnv returns the effective port honoring HTR_PORT (htrcli's own env).
func htrPortEnv(configured int) int {
	if p := strings.TrimSpace(os.Getenv("HTR_PORT")); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n >= 1 && n <= 65535 {
			return n
		}
	}
	if configured <= 0 {
		return DefaultHTRPort
	}
	return configured
}

// ResolveHTRCliBinary locates the htrcli binary cross-platform.
// Order: configured path → OCODE_HTRCLI_PATH → HTRCLI_PATH → PATH.
// The configured path may be ~-relative. exec.LookPath handles the Windows
// .exe/PATHEXT resolution, so no OS-specific suffix logic is needed.
func ResolveHTRCliBinary(configured string) (string, error) {
	candidates := []string{}
	if configured != "" {
		candidates = append(candidates, expandUser(configured))
	}
	for _, env := range []string{"OCODE_HTRCLI_PATH", "HTRCLI_PATH"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			candidates = append(candidates, expandUser(v))
		}
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	if p, err := exec.LookPath("htrcli"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("htrcli not found — set browser.htrcli_path or HTRCLI_PATH, or install htrcli on PATH")
}

// resolveExtensionDir expands ~ and returns "" when empty. Validation
// (manifest.json presence) happens at launch with a loud log.
func resolveExtensionDir(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	return expandUser(dir)
}

// expandUser expands a leading ~ to the user home dir (cross-platform).
func expandUser(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if p[1] == '/' || p[1] == '\\' {
		return filepath.Join(home, p[2:])
	}
	return p
}

// htrAddr returns the loopback dial address (127.0.0.1, never "localhost" to
// avoid IPv6/dual-stack surprises on Windows).
func htrAddr(port int) string { return net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) }

// ResolveHTRSocketPath returns the ocode-managed socket when configured is
// empty. It is exported so the Chrome launcher and daemon share exactly one
// value, including on Windows where no Unix socket is assumed by callers.
func ResolveHTRSocketPath(configured string) (string, error) {
	if strings.TrimSpace(configured) != "" {
		return expandUser(configured), nil
	}
	if runtime.GOOS == "windows" {
		// Windows has no Unix-domain socket in the supported htrcli transport;
		// use a private loopback endpoint in the same namespace instead.
		return "127.0.0.1:3847", nil
	}
	root, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", fmt.Errorf("resolve HTR socket path: %w", err)
	}
	dir := filepath.Join(root, managedHTRDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create HTR runtime dir: %w", err)
	}
	return filepath.Join(dir, "daemon.sock"), nil
}

func effectiveHTRNativeHostName(configured string) string {
	if strings.TrimSpace(configured) == "" {
		return DefaultHTRNativeHostName
	}
	return strings.TrimSpace(configured)
}

func validateHTRNativeHostName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("native host name is empty")
	}
	if name == "com.htrcontrol.host" || !strings.HasPrefix(name, "com.ocode.") || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("native host name %q must use the com.ocode.* namespace", name)
	}
	return nil
}

func newHTRIdentity() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate HTR daemon identity: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

type htrHealthResponse struct {
	Service  string `json:"service"`
	Managed  bool   `json:"managed"`
	Identity string `json:"identity"`
	Port     int    `json:"port"`
	Socket   string `json:"socket"`
}

func htrHealthyForInstance(port int, socket, identity string) bool {
	p, err := NormalizeHTRPort(port)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + htrAddr(p) + "/api/health")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	var health htrHealthResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&health); err != nil {
		return false
	}
	return health.Service == "htrcli" && health.Managed && health.Identity == identity && health.Port == p && health.Socket == socket
}

// HTRHealthy probes the daemon HTTP API and accepts only the managed daemon
// described by the ocode owner marker. A random service on the configured
// loopback port is never adopted.
func HTRHealthy(port int, lg *log.Logger) bool {
	owner, err := readHTROwner()
	if err != nil || owner.Port != port || owner.Identity == "" {
		return false
	}
	return htrHealthyForInstance(port, owner.Socket, owner.Identity)
}

// htrOwner is the multi-process ownership record.
type htrOwner struct {
	Identity   string    `json:"identity"`
	PID        int       `json:"daemon_pid"`
	OwnerPID   int       `json:"owner_pid"`
	Port       int       `json:"port"`
	Socket     string    `json:"socket"`
	StartedAt  time.Time `json:"started_at"`
	Executable string    `json:"executable"`
	StartToken string    `json:"process_start_token"`
}

type htrLease struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Socket    string    `json:"socket"`
	Identity  string    `json:"identity"`
	Heartbeat time.Time `json:"heartbeat"`
}

type htrLeaseHandle struct {
	mu       sync.RWMutex
	path     string
	port     int
	socket   string
	identity string
	stop     chan struct{}
	once     sync.Once
	logger   *log.Logger
}

func htrLeaseDir() (string, error) {
	root, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, managedHTRDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func htrLeaseLockPath() (string, error) {
	dir, err := htrLeaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(dir), "leases.lock"), nil
}

func withHTRStartLock(fn func() error) error {
	root, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return err
	}
	lockPath := filepath.Join(root, managedHTRDir, "start.lock")
	return filelock.WithFileLock(lockPath, fn)
}

func cleanupStaleHTRLeases() error {
	dir, err := htrLeaseDir()
	if err != nil {
		return err
	}
	lockPath, err := htrLeaseLockPath()
	if err != nil {
		return err
	}
	return filelock.WithFileLock(lockPath, func() error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		cutoff := time.Now().Add(-leaseTTL)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "lease-") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, readErr := os.ReadFile(path)
			var lease htrLease
			if readErr != nil || json.Unmarshal(data, &lease) != nil || lease.Heartbeat.Before(cutoff) || !pidAlive(lease.PID) || lease.Port <= 0 || lease.Identity == "" {
				_ = os.Remove(path)
			}
		}
		return nil
	})
}

func activeHTRLeases(port int, socket, identity string) (bool, error) {
	dir, err := htrLeaseDir()
	if err != nil {
		return false, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "lease-") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var lease htrLease
		if json.Unmarshal(data, &lease) == nil && lease.Port == port && lease.Socket == socket && lease.Identity == identity {
			return true, nil
		}
	}
	return false, nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".htr-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceHTRFile(tmpName, path)
}

func acquireHTRLease(port int, socket, identity string, lg *log.Logger) (*htrLeaseHandle, error) {
	dir, err := htrLeaseDir()
	if err != nil {
		return nil, err
	}
	if err := cleanupStaleHTRLeases(); err != nil {
		return nil, err
	}
	lockPath, err := htrLeaseLockPath()
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("lease-%d-%d.json", os.Getpid(), time.Now().UnixNano())
	path := filepath.Join(dir, name)
	write := func() error {
		data, marshalErr := json.Marshal(htrLease{PID: os.Getpid(), Port: port, Socket: socket, Identity: identity, Heartbeat: time.Now()})
		if marshalErr != nil {
			return marshalErr
		}
		return atomicWriteFile(path, data, 0o600)
	}
	if err := filelock.WithFileLock(lockPath, write); err != nil {
		return nil, fmt.Errorf("create HTR lease: %w", err)
	}
	h := &htrLeaseHandle{path: path, port: port, socket: socket, identity: identity, stop: make(chan struct{}), logger: lg}
	go h.refresh()
	return h, nil
}

func (h *htrLeaseHandle) refresh() {
	ticker := time.NewTicker(leaseHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.mu.RLock()
			leasePath, port, socket, identity := h.path, h.port, h.socket, h.identity
			h.mu.RUnlock()
			data, err := json.Marshal(htrLease{PID: os.Getpid(), Port: port, Socket: socket, Identity: identity, Heartbeat: time.Now()})
			if err == nil {
				if lockPath, lockErr := htrLeaseLockPath(); lockErr == nil {
					_ = filelock.WithFileLock(lockPath, func() error {
						return atomicWriteFile(leasePath, data, 0o600)
					})
				}
			}
		case <-h.stop:
			return
		}
	}
}

func (h *htrLeaseHandle) updateIdentity(identity string) error {
	h.mu.RLock()
	oldIdentity := h.identity
	h.mu.RUnlock()
	if identity == "" || identity == oldIdentity {
		return nil
	}
	lockPath, err := htrLeaseLockPath()
	if err != nil {
		return err
	}
	err = filelock.WithFileLock(lockPath, func() error {
		data, err := json.Marshal(htrLease{PID: os.Getpid(), Port: h.port, Socket: h.socket, Identity: identity, Heartbeat: time.Now()})
		if err != nil {
			return err
		}
		return atomicWriteFile(h.path, data, 0o600)
	})
	if err == nil {
		h.mu.Lock()
		h.identity = identity
		h.mu.Unlock()
	}
	return err
}

func (h *htrLeaseHandle) release() {
	h.once.Do(func() {
		close(h.stop)
		h.mu.RLock()
		leasePath, port, socket, identity := h.path, h.port, h.socket, h.identity
		h.mu.RUnlock()
		if lockPath, err := htrLeaseLockPath(); err == nil {
			_ = filelock.WithFileLock(lockPath, func() error {
				_ = os.Remove(leasePath)
				active, err := activeHTRLeases(port, socket, identity)
				if err == nil && !active {
					terminateManagedHTR(port, socket, identity, h.logger)
				}
				return err
			})
		}
	})
}

// htrOwnerPath returns the ocode-only managed daemon marker. It is deliberately
// outside ~/.htrcli so a standalone htrcli installation is never adopted or
// removed by ocode.
func htrOwnerPath() (string, error) {
	root, err := paths.OcodeGlobalDataDir()
	if err != nil {
		return "", fmt.Errorf("resolving ocode data dir: %w", err)
	}
	dir := filepath.Join(root, managedHTRDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, htrOwnerFileName), nil
}

// pidAlive cross-platform: ps on unix, tasklist on windows. No new deps.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		// tasklist /FI "PID eq N" prints the PID row iff alive.
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").Output()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), strconv.Itoa(pid))
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func processStartToken(pid int) string {
	if pid <= 0 || runtime.GOOS == "windows" {
		return ""
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func processMatchesOwner(owner htrOwner) bool {
	if owner.Executable == "" {
		return false
	}
	base := strings.ToLower(filepath.Base(owner.Executable))
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", owner.PID), "/FO", "CSV", "/NH").Output()
		return err == nil && strings.Contains(strings.ToLower(string(out)), base)
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(owner.PID), "-o", "comm=").Output()
	if err != nil || !strings.Contains(strings.ToLower(strings.TrimSpace(string(out))), base) {
		return false
	}
	if owner.StartToken != "" {
		return processStartToken(owner.PID) == owner.StartToken
	}
	return true
}

func readHTROwner() (htrOwner, error) {
	path, err := htrOwnerPath()
	if err != nil {
		return htrOwner{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return htrOwner{}, err
	}
	var owner htrOwner
	if err := json.Unmarshal(data, &owner); err != nil {
		return htrOwner{}, err
	}
	return owner, nil
}

func htrWriteOwner(identity string, port, daemonPID int, socket, executable string, startedAt time.Time) error {
	path, err := htrOwnerPath()
	if err != nil {
		return err
	}
	o := htrOwner{
		Identity:   identity,
		PID:        daemonPID,
		OwnerPID:   os.Getpid(),
		Port:       port,
		Socket:     socket,
		StartedAt:  startedAt,
		Executable: executable,
		StartToken: processStartToken(daemonPID),
	}
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0o600)
}

func terminateManagedHTR(port int, socket, identity string, lg *log.Logger) {
	path, err := htrOwnerPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var owner htrOwner
	if json.Unmarshal(data, &owner) != nil || owner.Port != port || owner.Socket != socket || owner.Identity != identity || owner.PID <= 0 || !pidAlive(owner.PID) {
		return
	}
	if !htrHealthyForInstance(owner.Port, owner.Socket, owner.Identity) || !processMatchesOwner(owner) {
		return
	}
	proc, err := os.FindProcess(owner.PID)
	if err != nil {
		return
	}
	if lg != nil {
		lg.Printf("htr: final lease expired; stopping managed daemon pid %d", owner.PID)
	}
	_ = proc.Kill()
	_ = os.Remove(path)
}

// EnsureHTRServe guarantees the `htrcli serve` daemon: reuse the healthy
// instance when present, else spawn a supervisor-owned child that dies with
// Server.Shutdown (last ocode process). sup may be nil — then an already
// running daemon is reused but a missing one returns an error instead of
// spawning (callers without a supervisor cannot own the lifetime).
func EnsureHTRServe(sup *tool.ProcessSupervisor, opts HTROptions, lg *log.Logger) (HTRStatus, error) {
	if !opts.Enabled {
		return HTRStatus{}, fmt.Errorf("HTR is disabled")
	}
	port := htrPortEnv(opts.Port)
	addr := htrAddr(port)
	if lg == nil {
		lg = log.Default()
	}
	if err := validateHTRNativeHostName(effectiveHTRNativeHostName(opts.NativeHostName)); err != nil {
		return HTRStatus{}, err
	}
	socketPath, err := ResolveHTRSocketPath(opts.SocketPath)
	if err != nil {
		return HTRStatus{}, err
	}
	identity := ""
	if owner, ownerErr := readHTROwner(); ownerErr == nil && owner.Port == port && owner.Socket == socketPath {
		if owner.Identity != "" && htrHealthyForInstance(port, socketPath, owner.Identity) {
			identity = owner.Identity
		}
	}
	if identity == "" {
		identity, err = newHTRIdentity()
		if err != nil {
			return HTRStatus{}, err
		}
	}
	lease, err := acquireHTRLease(port, socketPath, identity, lg)
	if err != nil {
		return HTRStatus{}, err
	}
	fail := func(err error) (HTRStatus, error) {
		lease.release()
		return HTRStatus{}, err
	}

	if htrHealthyForInstance(port, socketPath, identity) {
		owned := false
		if sup != nil {
			if rec, ok := sup.Lookup(htrServeID); ok && rec.PID > 0 {
				owned = true
			}
		}
		if sup != nil {
			_ = sup.RegisterShutdownCallback(lease.release)
		}
		return HTRStatus{Running: true, Owned: owned, Addr: addr, Socket: socketPath, Release: lease.release}, nil
	}
	if sup == nil {
		return fail(fmt.Errorf("htr daemon not running on %s and no supervisor to start it (start `htrcli serve` manually)", addr))
	}
	assets, err := ResolveHTRAssetsForHost(opts.ExtensionDir, opts.CliPath, effectiveHTRNativeHostName(opts.NativeHostName))
	if err != nil {
		return fail(err)
	}
	bin := assets.CliPath
	if bin == "" {
		bin, err = ResolveHTRCliBinary(opts.CliPath)
		if err != nil {
			return fail(err)
		}
	}
	if err := ensureNativeHostManifest(effectiveHTRNativeHostName(opts.NativeHostName), bin, assets.ExtensionDir, opts.BrowserPath); err != nil {
		return fail(err)
	}
	cmd := exec.Command(bin, "serve", "--no-tray")
	cmd.Env = append(os.Environ(),
		"HTR_PORT="+strconv.Itoa(port),
		"HTRCLI_NO_TRAY=1",
		"HTR_SOCKET_PATH="+socketPath,
		"HTR_NATIVE_HOST_NAME="+effectiveHTRNativeHostName(opts.NativeHostName),
		"HTR_MANAGED_ID="+identity,
	)
	var rec tool.ProcessRecord
	started := false
	err = withHTRStartLock(func() error {
		if htrHealthyForInstance(port, socketPath, identity) {
			return nil
		}
		if owner, ownerErr := readHTROwner(); ownerErr == nil && owner.Port == port && owner.Socket == socketPath && owner.Identity != "" && htrHealthyForInstance(port, socketPath, owner.Identity) {
			if err := lease.updateIdentity(owner.Identity); err != nil {
				return err
			}
			identity = owner.Identity
			return nil
		}
		var startErr error
		rec, startErr = tool.StartSupervised(sup, cmd, tool.ProcessRegistration{
			ID:               htrServeID,
			Name:             "htrcli serve",
			Kind:             tool.ProcessKindHTR,
			RetainOnShutdown: true,
		})
		started = startErr == nil
		return startErr
	})
	if err != nil {
		// Port likely taken by a daemon we don't own — reuse if it answers.
		if owner, ownerErr := readHTROwner(); ownerErr == nil && owner.Port == port && owner.Socket == socketPath && owner.Identity != "" && htrHealthyForInstance(port, socketPath, owner.Identity) {
			_ = lease.updateIdentity(owner.Identity)
			lg.Printf("htr: port %d busy, reusing existing daemon", port)
			_ = sup.RegisterShutdownCallback(lease.release)
			return HTRStatus{Running: true, Owned: false, Addr: addr, Socket: socketPath, Binary: bin, Release: lease.release}, nil
		}
		return fail(fmt.Errorf("start htrcli serve: %w", err))
	}
	if !started {
		_ = sup.RegisterShutdownCallback(lease.release)
		return HTRStatus{Running: true, Owned: false, Addr: addr, Socket: socketPath, Binary: bin, Release: lease.release}, nil
	}
	go watchHTRExit(cmd, sup, rec, identity, lg)
	// Wait for readiness (bounded): the HTTP API must answer /api/health.
	ready := false
	for i := 0; i < 50; i++ {
		if htrHealthyForInstance(port, socketPath, identity) {
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		lg.Printf("htr: daemon pid %d did not answer /api/health on %s within 5s", rec.PID, addr)
		if rec.PID > 0 {
			if process, findErr := os.FindProcess(rec.PID); findErr == nil {
				_ = process.Kill()
			}
		}
		return fail(fmt.Errorf("htr daemon started (pid %d) but /api/health unreachable on %s", rec.PID, addr))
	}
	if err := htrWriteOwner(identity, port, rec.PID, socketPath, bin, rec.StartedAt); err != nil {
		return fail(fmt.Errorf("record managed HTR owner: %w", err))
	}
	// Release the cross-process lease before the supervisor shuts down. The HTR
	// child is retained by the supervisor while another ocode process owns a
	// lease; the final release terminates only the managed daemon.
	_ = sup.RegisterShutdownCallback(lease.release)
	lg.Printf("htr: daemon running pid %d on %s (owned, dies with ocode exit)", rec.PID, addr)
	return HTRStatus{Running: true, Owned: true, Addr: addr, Socket: socketPath, Binary: bin, Release: lease.release}, nil
}

func watchHTRExit(cmd *exec.Cmd, sup *tool.ProcessSupervisor, rec tool.ProcessRecord, identity string, lg *log.Logger) {
	err := cmd.Wait()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	if sup != nil {
		sup.MarkExitedPID(htrServeID, rec.PID, code)
	}
	if owner, ownerErr := readHTROwner(); ownerErr == nil && owner.PID == rec.PID && owner.Identity == identity {
		if path, pathErr := htrOwnerPath(); pathErr == nil {
			_ = os.Remove(path)
		}
	}
	if lg != nil {
		lg.Printf("htr: managed daemon pid %d exited with code %d", rec.PID, code)
	}
}
