package cdp

// HTR NControl companion: supervised `htrcli serve` daemon + extension-dir
// resolution. Cross-platform (darwin/linux/windows), no new dependencies.
//
// Architecture:
//
//	ocode server (supervisor owner)
//	  └─ `htrcli serve --no-tray` (ProcessKindHTR, ID "htr-serve")
//	       ├─ HTTP API on 127.0.0.1:<port> (default 3846, /api/health)
//	       │  bearer-protected: HTR_BEARER_TOKEN = the managed identity
//	       └─ native-messaging relay to the preloaded extension
//	ocode headless Chrome --load-extension=<HTRExtensionDir> (see launch.go)
//
// Lifetime: the daemon is retained across individual ocode shutdowns while
// any cross-process lease is alive. Heartbeats expire after leaseTTL; the last
// explicit release terminates only the daemon recorded in ocode's own marker.
// A daemon on the shared port (3845) that ocode did not start is ADOPTED — it
// answers on the port and is reused. Adopting never grants ocode the right to
// stop it: the marker carries both OwnerPID (whoever last wrote it) and
// StartedByPID (whoever actually spawned the daemon), and only the latter can
// authorise a stop. See shouldStopSharedDaemon.
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
	"bytes"
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
	"sync/atomic"
	"time"

	gopsprocess "github.com/shirou/gopsutil/v4/process"

	"github.com/u007/ocode/internal/crashguard"
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

// htrWindowsEndpoint is the loopback endpoint used on Windows, where the
// supported htrcli transport has no Unix-domain socket. Shared by the managed
// daemon's socket resolution and shared-daemon resolution so the two cannot
// drift apart.
const htrWindowsEndpoint = "127.0.0.1:3847"

// htrServeID is the supervisor registration ID for the daemon.
const htrServeID = "htr-serve"

const (
	// sharedSpawnConfirmBudget bounds the BLOCKING half of readiness: how long
	// EnsureHTRServe waits for a freshly spawned daemon to die before accepting
	// it. Desktop boot calls StartBrowse synchronously, so this is paid directly
	// as startup time; it must stay well under a second.
	//
	// It is sized against measured reap latency, not guessed. A child that exits
	// immediately is not observed by the supervisor the instant it exits: on
	// macOS, exec of a freshly written binary plus cmd.Wait's teardown measured
	// 135-350ms here (6ms for an already-warm binary, which is what production
	// runs). 500ms clears that with margin and still costs half a second of
	// boot, once.
	sharedSpawnConfirmBudget = 500 * time.Millisecond

	// sharedVerifyBudget bounds BACKGROUND readiness verification: how long the
	// daemon is given to answer /api/health once it is known to be alive. It is
	// a ceiling, not a wait — verification runs off the boot path, so it may be
	// generous, but it must terminate so a daemon that never comes up is
	// reported once instead of polling forever.
	sharedVerifyBudget = 15 * time.Second
	// sharedVerifyPoll is the gap between probes inside that budget.
	sharedVerifyPoll = 250 * time.Millisecond
)

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
	// Shared is the resolved shared-daemon description behind Port/SocketPath.
	// Mode is "shared" when the coordinates came from htrcli's own config and
	// "private" for the legacy ocode-managed daemon, in which case Port and
	// SocketPath are still the ocode-managed defaults. Callers that must not
	// spawn a daemon (AdoptOnly) read this rather than re-deriving it.
	Shared SharedDaemon
}

// HTRStatus describes the daemon state after EnsureHTRServe.
type HTRStatus struct {
	Running bool // health probe passed
	Owned   bool // this process spawned it (retained across supervisor shutdown while leases exist)
	// StartedByOcode is true only when THIS call spawned the daemon. It stays
	// false whenever an already-running daemon is reused, whether ocode started
	// that one or the user did — which is what makes it the field to consult
	// before stopping anything: a daemon ocode did not start has no ocode owner
	// marker and must never be killed on ocode's initiative.
	StartedByOcode bool
	Addr           string // 127.0.0.1:port
	Binary         string // resolved htrcli binary ("" when reused and unknown)
	Socket         string
	Notice         string
	Release        func()
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
		return htrWindowsEndpoint, nil
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

// htrHealthResponse is the daemon's GET /api/health body. htrcli wraps every
// API reply as {"ok":bool,"data":...}.
type htrHealthResponse struct {
	OK   bool `json:"ok"`
	Data struct {
		Service  string `json:"service"`
		Managed  bool   `json:"managed"`
		Identity string `json:"identity"`
		Port     int    `json:"port"`
		Socket   string `json:"socket"`
	} `json:"data"`
}

// htrHealthyForInstance probes the daemon HTTP API. The managed daemon is
// started with HTR_BEARER_TOKEN set to its identity, and htrcli enforces the
// bearer on every route including /api/health, so the probe must present it.
func htrHealthyForInstance(port int, socket, identity string) bool {
	p, err := NormalizeHTRPort(port)
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+htrAddr(p)+"/api/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+identity)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
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
	h := health.Data
	return health.OK && h.Service == "htrcli" && h.Managed && h.Identity == identity && h.Port == p && h.Socket == socket
}

// htrHealthyForeign probes a daemon ocode did not start. It is deliberately
// laxer than htrHealthyForInstance: a daemon the user started reports
// managed:false, because HTR_MANAGED_ID is only set by ocode's own spawn.
// Requiring service+port+socket+authentication is enough to adopt it safely —
// those four cannot all coincide by accident on a loopback port, and the token
// proves the caller is entitled to drive this daemon at all.
func htrHealthyForeign(port int, socket, token string) bool {
	p, err := NormalizeHTRPort(port)
	if err != nil || strings.TrimSpace(token) == "" {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+htrAddr(p)+"/api/health", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
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
	h := health.Data
	return health.OK && h.Service == "htrcli" && h.Port == p && h.Socket == socket
}

// Probes are indirected through package vars so EnsureHTRServe's four-state
// resolution can be exercised against a branch matrix without a live daemon on
// a real port. Production code never reassigns them.
var (
	htrHealthyForInstanceFn = htrHealthyForInstance
	htrHealthyForeignFn     = htrHealthyForeign
)

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

// HTRTab is one connected browser tab as reported by the managed daemon's
// GET /api/tabs. Fields mirror htrcli's api.TabInfo.
type HTRTab struct {
	ID      int    `json:"id"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Active  bool   `json:"active"`
	Browser string `json:"browser,omitempty"`
}

// HTRDaemonInfo is a read-only snapshot of the managed daemon for the settings
// UI. Unlike EnsureHTRServe it takes no lease and never starts anything, and it
// only ever describes the daemon named by ocode's owner marker — a standalone
// htrcli daemon (port 3845, no marker) is never reported.
type HTRDaemonInfo struct {
	Running bool   `json:"running"`
	Managed bool   `json:"managed"`
	Addr    string `json:"addr"`
	Port    int    `json:"port"`
	Socket  string `json:"socket"`
	Binary  string `json:"binary"`
}

// htrTabsResponse is the daemon's GET /api/tabs body (htrcli wraps replies as
// {"ok":bool,"data":...}).
type htrTabsResponse struct {
	OK   bool     `json:"ok"`
	Data []HTRTab `json:"data"`
}

// HTRDaemonStatus reports whether the ocode-managed daemon is running. port is
// the configured HTR port (0 = managed default, honoring HTR_PORT); socketPath
// is the configured socket override ("" = managed default).
func HTRDaemonStatus(port int, socketPath string) HTRDaemonInfo {
	p := htrPortEnv(port)
	info := HTRDaemonInfo{Addr: htrAddr(p), Port: p}
	if resolved, err := ResolveHTRSocketPath(socketPath); err == nil {
		info.Socket = resolved
	}
	owner, err := readHTROwner()
	if err != nil || owner.Port != p || owner.Identity == "" {
		return info
	}
	info.Binary = owner.Executable
	if owner.Socket != "" {
		info.Socket = owner.Socket
	}
	if !pidAlive(owner.PID) || !htrHealthyForInstance(owner.Port, owner.Socket, owner.Identity) {
		return info
	}
	info.Running = true
	info.Managed = true
	return info
}

// StopHTRServe terminates the ocode-managed `htrcli serve` daemon recorded in
// ocode's owner marker for the configured port, but only when this process is
// entitled to: see shouldStopSharedDaemon. A daemon this process did not spawn
// — another ocode instance's, an external run's, an adopted one, or one another
// live instance still holds a lease on — is left running and reported as such
// with a nil error; that refusal is the contract, not a failure. sup may be
// nil; when non-nil the supervisor record is marked killed so an immediate
// restart can replace it. A standalone htrcli daemon is never touched. Stopping
// an already-stopped daemon is a no-op that reports Running:false.
func StopHTRServe(sup *tool.ProcessSupervisor, port int, lg *log.Logger) (HTRStatus, error) {
	p := htrPortEnv(port)
	addr := htrAddr(p)
	if lg == nil {
		lg = log.Default()
	}
	owner, err := readHTROwner()
	if err != nil || owner.Port != p || owner.PID <= 0 {
		return HTRStatus{Running: false, Addr: addr}, nil
	}
	markerPath, pathErr := htrOwnerPath()
	if !pidAlive(owner.PID) {
		if pathErr == nil {
			_ = os.Remove(markerPath)
		}
		return HTRStatus{Running: false, Addr: addr, Socket: owner.Socket}, nil
	}
	// Stop rights are decided before anything is signalled to the process. A
	// refusal is a normal outcome, not a failure: the daemon was started by
	// another ocode instance, adopted, or externally, or another instance is
	// still using it — and in every one of those cases it must survive this one
	// exiting. The marker is deliberately left in place; it is the only record
	// of who spawned the daemon, and deleting it would make the next run see "no
	// daemon" rather than "not mine". Both refusals are unreachable by then
	// (owner.PID > 0 above, and the process is alive), so a refusal here is
	// either provenance or a live foreign lease.
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil {
		return HTRStatus{Running: true, Addr: addr, Socket: owner.Socket}, err
	}
	if !stopped {
		reason := "this process did not spawn it"
		if owner.StartedByPID == os.Getpid() {
			reason = "another ocode instance still holds a lease on it"
		}
		lg.Printf("htr: leaving daemon pid %d on %s alone: %s", owner.PID, addr, reason)
		return HTRStatus{Running: pidAlive(owner.PID), Addr: addr, Socket: owner.Socket}, nil
	}
	proc, findErr := os.FindProcess(owner.PID)
	if findErr != nil {
		return HTRStatus{Running: true, Addr: addr, Socket: owner.Socket}, fmt.Errorf("find htr daemon pid %d: %w", owner.PID, findErr)
	}
	if err := proc.Kill(); err != nil {
		return HTRStatus{Running: true, Addr: addr, Socket: owner.Socket}, fmt.Errorf("stop htr daemon pid %d: %w", owner.PID, err)
	}
	if pathErr == nil {
		_ = os.Remove(markerPath)
	}
	if sup != nil {
		sup.MarkKilledPID(htrServeID, owner.PID, 0)
	}
	lg.Printf("htr: stopped managed daemon pid %d on %s", owner.PID, addr)
	return HTRStatus{Running: false, Addr: addr, Socket: owner.Socket}, nil
}

// ListHTRTabs returns the browser tabs connected to the managed daemon via its
// bearer-protected GET /api/tabs. It fails when no managed daemon is recorded
// in ocode's owner marker (a standalone daemon is never queried).
func ListHTRTabs(port int) ([]HTRTab, error) {
	p := htrPortEnv(port)
	owner, err := readHTROwner()
	if err != nil || owner.Port != p || owner.Identity == "" {
		return nil, fmt.Errorf("managed htr daemon is not running on %s", htrAddr(p))
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+htrAddr(p)+"/api/tabs", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+owner.Identity)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query htr tabs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("query htr tabs: daemon returned %s", resp.Status)
	}
	var body htrTabsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode htr tabs: %w", err)
	}
	if !body.OK {
		return nil, fmt.Errorf("query htr tabs: daemon reported an error")
	}
	return body.Data, nil
}

// htrOwner is the multi-process ownership record.
//
// OwnerPID and StartedByPID answer different questions and must not be collapsed
// into one:
//
//   - OwnerPID is "which ocode process wrote this file". It describes the write.
//   - StartedByPID is "which process spawned the daemon". It describes the
//     daemon, and it is the only field that can authorise stopping it.
//
// Today the spawn site is also the only writer, so the two hold the same value
// for every marker this build produces. They are kept separate anyway because
// the stop rule must survive the day they stop agreeing: a marker left on disk
// by one instance is read by whichever instance asks to stop next, and that
// instance is frequently an adopter rather than the spawner. Adopting writes no
// marker (adoptHTRServe deliberately leaves the file alone), so the marker keeps
// naming the spawner while a different process is the one asking — which is
// exactly the case that must be refused. A marker written before StartedByPID
// existed decodes to 0: "unknown", not "ours", and is therefore also never
// stopped. See shouldStopSharedDaemon.
//
// Known limit: StartedByPID is a bare pid with no start token, so a stale
// marker whose daemon died can name a pid that the OS later hands to some
// unrelated process, and that process would inherit stop rights. Closing that
// would need a spawner start token, which is a new field and out of scope here.
//
// Orphan note: if ocode is SIGKILLed the daemon outlives it. The next ocode run
// adopts that orphan without stop rights (StartedByPID names a dead process), so
// an orphan can outlive every ocode process. That is intended, not an oversight:
// a survivor is indistinguishable from a daemon the user started themselves, and
// the stop rule forbids killing those. The pid is surfaced in the Settings
// status so the user can end a survivor deliberately; there is no auto-reaping.
type htrOwner struct {
	Identity string `json:"identity"`
	PID      int    `json:"daemon_pid"`
	OwnerPID int    `json:"owner_pid"`
	// StartedByPID is 0 for a marker written before this field existed.
	StartedByPID int       `json:"started_by_pid,omitempty"`
	Port         int       `json:"port"`
	Socket       string    `json:"socket"`
	StartedAt    time.Time `json:"started_at"`
	Executable   string    `json:"executable"`
	StartToken   string    `json:"process_start_token"`
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

// activeHTRLeases reports whether a lease matching the daemon's coordinates is
// on disk. excludePID skips one process's own lease; 0 excludes nothing at all,
// which keeps the "is anyone else left" question byte-identical to before this
// parameter existed. The stop rule needs the exclusion because it asks whether
// ANOTHER ocode instance is still using this daemon, and counting our own lease
// would make the explicit Stop action a permanent no-op for a single-instance
// user, who is still holding the lease EnsureHTRServe took out for them.
func activeHTRLeases(port int, socket, identity string, excludePID int) (bool, error) {
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
		if json.Unmarshal(data, &lease) != nil {
			continue
		}
		// excludePID == 0 must skip nothing, including a lease whose pid field
		// is missing or zero — that was counted as active before this parameter
		// existed and must keep counting.
		if excludePID != 0 && lease.PID == excludePID {
			continue
		}
		if lease.Port == port && lease.Socket == socket && lease.Identity == identity {
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
				// 0 excludes nothing: this is the "is anyone left" question, and
				// our own lease file was just removed above.
				active, err := activeHTRLeases(port, socket, identity, 0)
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

// pidAlive reports whether pid names a running process. It uses gopsutil's
// PidExists (signal 0 on unix, OpenProcess on Windows) instead of shelling out
// to ps/tasklist: the browse/HTR paths run under ocode's own sandbox, where
// spawning `ps` is denied and a live Chrome/HTR owner would be misread as dead
// (removing a live SingletonLock, or stealing an active lease).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	exists, err := gopsprocess.PidExists(int32(pid))
	if err != nil {
		return false
	}
	return exists
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

// Liveness probes behind the stop rule are indirected so the rule can be tested
// against a recorded pid that is not a running process. Production code never
// reassigns them; only tests do, and only for the duration of one test.
var (
	pidAliveFn       = pidAlive
	processMatchesFn = processMatchesOwner
)

// shouldStopSharedDaemon reports whether THIS process may terminate the daemon
// recorded in owner. Every condition must hold:
//
//  1. The marker names a daemon at all.
//  2. This process is the one that spawned it (StartedByPID, never OwnerPID —
//     adopting rewrites OwnerPID and must not buy stop rights).
//  3. That process is still alive, so a dead marker is a cleanup problem rather
//     than a stop decision.
//  4. The daemon is still attributable to ocode — either the marker's executable
//     and start token match, or the bearer-protected managed health probe
//     answers. Two signals, either sufficient, exactly as before this rule
//     existed: a daemon verified only by the probe is still ocode's, so
//     demanding processMatchesOwner outright would refuse a stop the probe has
//     already positively attributed.
//  5. No OTHER ocode instance still holds a live lease on it.
//
// (false, nil) is a refusal, not a failure: the caller reports the daemon as
// still running and touches nothing. An error means the answer is unknown and
// the marker names a process ocode cannot account for.
func shouldStopSharedDaemon(owner htrOwner) (bool, error) {
	if owner.PID <= 0 {
		return false, nil
	}
	if owner.StartedByPID != os.Getpid() {
		return false, nil
	}
	if !pidAliveFn(owner.PID) {
		return false, nil // marker cleanup is the caller's job
	}
	if !processMatchesFn(owner) && !htrHealthyForInstanceFn(owner.Port, owner.Socket, owner.Identity) {
		return false, fmt.Errorf("managed htr daemon pid %d could not be verified; refusing to stop it", owner.PID)
	}
	active, err := activeHTRLeases(owner.Port, owner.Socket, owner.Identity, os.Getpid())
	if err != nil {
		return false, err
	}
	return !active, nil
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

// htrWriteOwner records the daemon in ocode's ownership marker. The spawn site is
// the only caller today, and it passes its own pid, but startedByPID is a
// parameter rather than an implicit os.Getpid() so the provenance being recorded
// is visible at the call site and so a future writer describing a daemon it
// merely attached to cannot accidentally grant itself stop rights — it would
// have to pass the original spawner, or 0 when it is unknown. OwnerPID is
// unconditionally this process, because writing the marker is what it means.
func htrWriteOwner(identity string, port, daemonPID int, socket, executable string, startedAt time.Time, startedByPID int) error {
	path, err := htrOwnerPath()
	if err != nil {
		return err
	}
	o := htrOwner{
		Identity:     identity,
		PID:          daemonPID,
		OwnerPID:     os.Getpid(),
		StartedByPID: startedByPID,
		Port:         port,
		Socket:       socket,
		StartedAt:    startedAt,
		Executable:   executable,
		StartToken:   processStartToken(daemonPID),
	}
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0o600)
}

// HTRProvenance is what ocode's owner marker says about the daemon on one
// HTR port. It is read-only, decision-free state for the settings API.
type HTRProvenance struct {
	// DaemonPID is the pid recorded in the owner marker, or 0 when no marker
	// names a daemon for this port.
	DaemonPID int
	// StartedByOcode is true only when THIS process may terminate that daemon —
	// it is shouldStopSharedDaemon's verdict, not a looser "the marker looks
	// like mine" guess. The settings UI gates its Stop button on it, and
	// StopHTRServe enforces exactly the same rule, so the button and the server
	// cannot disagree about who owns the daemon.
	StartedByOcode bool
}

// HTRProvenanceFor reports the provenance of the daemon recorded for the
// effective HTR port (0 = managed default, honoring HTR_PORT). A missing,
// foreign-port, or unparseable marker yields the zero value, and so does a
// marker whose daemon this process may not stop — the answer is always
// "ocode will not touch it", never a guess.
func HTRProvenanceFor(port int) HTRProvenance {
	p := htrPortEnv(port)
	owner, err := readHTROwner()
	if err != nil || owner.Port != p || owner.PID <= 0 {
		return HTRProvenance{}
	}
	prov := HTRProvenance{DaemonPID: owner.PID}
	// An error means the answer is unknown, which is a refusal like any other:
	// the caller must not offer to stop a daemon ocode cannot account for.
	if stoppable, stopErr := shouldStopSharedDaemon(owner); stopErr == nil {
		prov.StartedByOcode = stoppable
	}
	return prov
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
	// Stop rights are decided before anything is signalled to the process, and
	// the marker is deliberately left in place on a refusal: it is the only
	// record of who spawned the daemon, so deleting it would make the next run
	// see "no daemon" rather than "not mine". This is the real kill path — it is
	// what runs when the final lease is released — so the provenance clause is
	// what keeps an ADOPTER from killing the daemon that the process which
	// adopted it spawned. See shouldStopSharedDaemon.
	stopped, err := shouldStopSharedDaemon(owner)
	if err != nil || !stopped {
		logHTRStopRefusal(lg, owner, err)
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

// logHTRStopRefusal emits the single line explaining why a last-lease-release
// kill did not happen. A refusal is a normal outcome, not a failure, but it is
// silent in every other sense, so without this line a daemon that outlives its
// last lease looks exactly like one that was never noticed.
func logHTRStopRefusal(lg *log.Logger, owner htrOwner, err error) {
	if lg == nil {
		return
	}
	switch {
	case err != nil:
		lg.Printf("htr: final lease expired; leaving daemon pid %d on %s alone: %v", owner.PID, htrAddr(owner.Port), err)
	case owner.StartedByPID != os.Getpid():
		lg.Printf("htr: final lease expired; leaving daemon pid %d on %s alone: this process did not spawn it", owner.PID, htrAddr(owner.Port))
	default:
		lg.Printf("htr: final lease expired; leaving daemon pid %d on %s alone: another ocode instance still holds a lease on it", owner.PID, htrAddr(owner.Port))
	}
}

// htrOutputLimit caps what htrDaemonOutput keeps. The daemon is long-lived and
// its sink outlives the EnsureHTRServe call that built it, so an uncapped
// buffer would grow for the life of the ocode process.
const htrOutputLimit = 64 * 1024

// htrDaemonOutput collects the `htrcli serve` child's stdout and stderr.
//
// Capturing, rather than leaving Cmd.Stdout/Cmd.Stderr nil, is what keeps a
// daemon that never became healthy diagnosable: os/exec connects a nil stream
// to os.DevNull, so the child's own error text — the one line that says why it
// died — would be discarded and there would be nothing left to quote. The sink
// costs one bounded buffer and lets EnsureHTRServe put the daemon's own words
// into the readiness failure. Both streams share this single sink so their
// interleaving is preserved, which is what makes the captured text readable.
//
// The load-bearing part of this struct is the htrOutputLimit cap, not the
// mutex. The mutex is cheap insurance, not a fix: os/exec already serialises
// writes when Stdout and Stderr are the same writer and its type is comparable
// with ==, which bytes.Buffer is, so there is no race here to prevent. The lock
// only keeps that guarantee from resting on a subtlety of the standard library.
type htrDaemonOutput struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	dropped int
}

func (o *htrDaemonOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if room := htrOutputLimit - o.buf.Len(); room > 0 {
		if len(p) > room {
			o.buf.Write(p[:room])
			o.dropped += len(p) - room
		} else {
			o.buf.Write(p)
		}
	} else {
		o.dropped += len(p)
	}
	// Always report a full write: returning short would make os/exec surface a
	// spurious copy error for output we chose to drop.
	return len(p), nil
}

// String renders the captured output for a diagnostic, saying so when anything
// was dropped so a truncated log is never mistaken for a complete one.
func (o *htrDaemonOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := strings.TrimSpace(o.buf.String())
	if o.dropped > 0 {
		out += fmt.Sprintf("\n[%d further byte(s) of daemon output dropped]", o.dropped)
	}
	return out
}

// htrStatusFunc is the host UI's status sink, installed by SetStatusFunc.
//
// It has to be a package slot rather than a parameter because the two things
// that publish through it both run AFTER EnsureHTRServe has returned — the
// background verifier outlives the call that launched it, and the death watcher
// outlives the daemon — so neither has the caller's server to hand. The pointer
// is atomic: publishHTRStatus runs on a background goroutine while SetStatusFunc
// may be called from a later StartBrowse.
var htrStatusFunc atomic.Pointer[func(string)]

// SetStatusFunc installs the sink that receives shared-daemon status changes
// ("", or a reason the daemon is unusable) once EnsureHTRServe has already
// returned. Pass nil to remove it.
//
// internal/server installs one so a daemon that never becomes healthy — the
// failure this function used to report synchronously from the boot path — is
// still surfaced to the browser UI instead of being lost to a log line.
//
// One slot, last writer wins: a process that stands up a second server simply
// hands the sink over. Nothing is lost by the handover, and a sink left
// pointing at a server that has since shut down is inert, because publishing
// only writes a mutex-guarded string nobody reads.
func SetStatusFunc(fn func(string)) {
	if fn == nil {
		htrStatusFunc.Store(nil)
		return
	}
	htrStatusFunc.Store(&fn)
}

// publishHTRStatus hands a status change to the installed sink. An empty
// notice means "no longer claiming anything", which is what a dead daemon
// publishes: the UI must fall back to its own liveness read rather than keep
// showing a boot-time claim. A nil sink (the TUI bridge, or tests) is a no-op.
func publishHTRStatus(notice string) {
	if fn := htrStatusFunc.Load(); fn != nil {
		(*fn)(notice)
	}
}

// htrVerifyOutcome is the last background-verification result recorded for this
// process's daemon. It exists so a failure is reported once: without it every
// ensure would re-log and re-publish the same complaint, and the first one —
// the one that explains what happened — would be buried under the repeats.
// Immutable once published, so readers need no lock.
type htrVerifyOutcome struct {
	port  int
	ready bool
}

var lastHTRVerifyOutcome atomic.Pointer[htrVerifyOutcome]

// noteHTRVerifyOutcome records out and reports whether it CHANGED the answer
// for that port. A false return means the operator has already been told, so the
// caller must stay silent.
//
// What counts as the same answer is (port, ready) and nothing else — not the
// wording. Keying on the text instead would let a rewording, or a different
// budget in a test, turn one complaint into a stream of them, which is the very
// thing this exists to prevent.
func noteHTRVerifyOutcome(out htrVerifyOutcome) bool {
	for {
		prev := lastHTRVerifyOutcome.Load()
		if prev != nil && prev.port == out.port && prev.ready == out.ready {
			return false
		}
		if lastHTRVerifyOutcome.CompareAndSwap(prev, &out) {
			return true
		}
	}
}

// launchSharedVerifyFn starts background verification. Production goes through
// crashguard.Go: a panic in a raw goroutine kills the process before the TUI can
// restore the terminal, and the verifier probes the network from a goroutine
// that outlives the boot path, so it needs the same guard every other
// ocode-owned goroutine has. It is a var only so a test can join the goroutine
// instead of leaving it to race the next test's probe stubs.
var launchSharedVerifyFn = func(fn func()) { crashguard.Go(fn) }

// verifySharedDaemonAsync confirms in the background that a daemon which was
// just spawned and confirmed alive does actually answer /api/health.
//
// token is the identity the strict probe must present, which in shared mode IS
// htrcli's configured token (the daemon is spawned with HTR_MANAGED_ID set to
// it) and in private mode is the managed identity. Passing the same value to
// both probes is therefore correct in both modes: the laxer foreign probe can
// only be satisfied by a daemon holding that same bearer, which is precisely
// the daemon we are waiting for.
//
// On failure it logs once and publishes the reason to the host UI. It never
// touches a supervisor lock or a host lock, and it never kills the daemon: a
// slow start is not a failed start, and the stop rule (shouldStopSharedDaemon)
// owns termination.
func verifySharedDaemonAsync(port int, socket, token string, lg *log.Logger) {
	verifySharedDaemon(port, socket, token, lg, sharedVerifyBudget, sharedVerifyPoll)
}

// verifySharedDaemon is verifySharedDaemonAsync with the budget and poll
// interval as arguments. The production wrapper above passes the constants;
// keeping the parameters here is what lets the exhaustion path — the one that
// has to actually run to its deadline to be observed — be tested without a
// 15-second test.
func verifySharedDaemon(port int, socket, token string, lg *log.Logger, budget, poll time.Duration) bool {
	if lg == nil {
		lg = log.Default()
	}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if htrHealthyForInstanceFn(port, socket, token) || htrHealthyForeignFn(port, socket, token) {
			// Record the success: it is also what re-arms reporting, so a daemon
			// that dies later and is ensured again is complained about once more.
			noteHTRVerifyOutcome(htrVerifyOutcome{port: port, ready: true})
			return true
		}
		time.Sleep(poll)
	}
	notice := fmt.Sprintf("HTR automation is unavailable: the daemon on %s did not answer /api/health within %s. Browsing continues without the HTR extension — check the ocode log, or start `htrcli serve` yourself.", htrAddr(port), budget)
	if !noteHTRVerifyOutcome(htrVerifyOutcome{port: port, ready: false}) {
		return false
	}
	publishHTRStatus(notice)
	lg.Printf("htr: shared daemon on port %d did not become healthy within %s; run `htrcli serve` manually or check %s", port, budget, htrAddr(port))
	return false
}

// confirmSharedSpawnAlive is the BLOCKING half of readiness: it waits up to
// budget for the freshly spawned child to die, and reports whether it survived.
//
// It waits on died — the signal watchHTRExit raises after it has marked the
// supervisor record terminal, retracted the owner marker and logged — rather
// than polling the record. Polling cannot be made both cheap and reliable here:
// the child is not observable as gone until cmd.Wait reaps it, and reaping a
// process that exited immediately is not fast (see sharedSpawnConfirmBudget).
// Selecting on the event also means the FAILURE path costs one reap, not the
// whole window.
//
// The check buys exactly one thing: an exec that succeeded but produced a
// process that dies on the spot is reported here, loudly, with the daemon's own
// output, while the caller is still on the boot path. A daemon that dies later
// is not this function's business — that is watchHTRExit's, and it fails just
// as loudly without stalling anything.
func confirmSharedSpawnAlive(died <-chan struct{}, budget time.Duration) bool {
	if died == nil {
		return true
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-died:
		return false
	case <-timer.C:
		return true
	}
}

// sharedServeEnv builds the child environment for the SHARED daemon. It
// deliberately omits HTR_BEARER_TOKEN: htrcli resolves its own token from its
// own config, so passing one here would create a second source of truth that can
// silently disagree with the credential the daemon actually validates against —
// and with a disagreeing token every authenticated probe 401s and ocode would
// believe it may spawn a daemon that then never looks healthy.
func sharedServeEnv(token string, port int, socket, nativeHost string) []string {
	return append(os.Environ(),
		"HTR_PORT="+strconv.Itoa(port),
		"HTRCLI_NO_TRAY=1",
		"HTR_SOCKET_PATH="+socket,
		"HTR_NATIVE_HOST_NAME="+nativeHost,
		"HTR_MANAGED_ID="+token,
	)
}

// privateServeEnv is sharedServeEnv plus the bearer token, which only the
// ocode-managed (private) daemon gets: it was created by ocode precisely because
// there was no htrcli config for it to read a token from, so HTR_BEARER_TOKEN is
// its only credential.
func privateServeEnv(identity string, port int, socket, nativeHost string) []string {
	return append(sharedServeEnv(identity, port, socket, nativeHost),
		"HTR_BEARER_TOKEN="+identity)
}

// newServeCmd builds the `htrcli serve` child with its output captured rather
// than inherited; see htrDaemonOutput for why. Use htrDaemonOutput.String() via
// a type assertion on cmd.Stdout to recover what the daemon printed.
func newServeCmd(bin string, env []string) *exec.Cmd {
	cmd := exec.Command(bin, "serve", "--no-tray")
	cmd.Env = env
	out := &htrDaemonOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	return cmd
}

// newSharedServeCmd is newServeCmd for the shared daemon: HTR_MANAGED_ID is the
// shared token, never a fresh random identity, so the strict probe stays valid
// for a daemon ocode started itself.
func newSharedServeCmd(bin, token string, port int, socket, nativeHost string) *exec.Cmd {
	return newServeCmd(bin, sharedServeEnv(token, port, socket, nativeHost))
}

// adoptHTRServe builds the status for a daemon that was already running when
// EnsureHTRServe was called. StartedByOcode stays false because this call did
// not spawn it — that is the field a later stop consults before touching a
// daemon, and Owned reports only whether this process's supervisor registered
// one it launched earlier (an ocode daemon another process started is
// deliberately neither owned nor started-by-us).
func adoptHTRServe(sup *tool.ProcessSupervisor, addr, socket string, lease *htrLeaseHandle, lg *log.Logger) HTRStatus {
	owned := false
	if sup != nil {
		if rec, ok := sup.Lookup(htrServeID); ok && rec.PID > 0 {
			owned = true
		}
		_ = sup.RegisterShutdownCallback(lease.release)
	}
	if lg != nil {
		lg.Printf("htr: reusing the daemon already running on %s", addr)
	}
	return HTRStatus{
		Running:        true,
		Owned:          owned,
		StartedByOcode: false,
		Addr:           addr,
		Socket:         socket,
		Release:        lease.release,
	}
}

// sharedMode reports whether opts describes the single htrcli daemon shared with
// the user's browser extension. An empty Mode is private too: callers that
// predate shared mode leave the whole struct zero, and treating "" as shared
// would silently reroute every one of them onto the shared resolution.
func (o HTROptions) sharedMode() bool {
	return o.Shared.Mode != "" && o.Shared.Mode != "private"
}

// configuredSocket returns the socket path EnsureHTRServe should probe. A caller
// in shared mode that already copied Shared.Socket into SocketPath (see
// resolveManagedHTROptions in internal/server/htr.go) is unaffected; the
// fallback matters for a caller that set Shared alone, because SocketPath's
// empty default resolves into ocode's own private namespace — a directory a
// shared daemon never listens in, so probing there would always answer "dead"
// and ocode would spawn a second daemon onto a served port.
func (o HTROptions) configuredSocket() string {
	if o.SocketPath != "" || !o.sharedMode() {
		return o.SocketPath
	}
	return o.Shared.Socket
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
	socketPath, err := ResolveHTRSocketPath(opts.configuredSocket())
	if err != nil {
		return HTRStatus{}, err
	}
	identity := ""
	if owner, ownerErr := readHTROwner(); ownerErr == nil && owner.Port == port && owner.Socket == socketPath {
		if owner.Identity != "" && htrHealthyForInstanceFn(port, socketPath, owner.Identity) {
			identity = owner.Identity
		}
	}
	if identity == "" {
		if opts.sharedMode() && strings.TrimSpace(opts.Shared.Token) != "" {
			// Shared mode pins the identity to the token from htrcli's config.
			// A fresh random value would be wrong twice over: the daemon is
			// spawned with HTR_MANAGED_ID=<shared token>, so a random identity
			// could never satisfy the strict probe, and the lease would record an
			// identity no daemon has.
			identity = opts.Shared.Token
		} else {
			identity, err = newHTRIdentity()
			if err != nil {
				return HTRStatus{}, err
			}
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

	// foreignAlive reports a daemon on this port that answers with the shared
	// token but carries no ocode-managed identity. It is the only thing that
	// distinguishes "the user already runs htrcli serve" from "the port is
	// empty", and it is deliberately checked BEFORE the supervisor guard: a
	// foreign daemon is reusable with no supervisor, because ocode does not own
	// its lifetime.
	foreignAlive := func() bool {
		return opts.sharedMode() && !opts.Shared.AdoptOnly &&
			htrHealthyForeignFn(port, socketPath, opts.Shared.Token)
	}

	// The four states, in resolution order: own-alive, foreign-alive, adopt-only
	// refusal, spawn. Adoption MUST precede the `sup == nil` guard below — a
	// caller without a supervisor (the TUI bridge, the settings API) must still
	// attach to a daemon the user is already running.
	if opts.sharedMode() {
		if identity != "" && htrHealthyForInstanceFn(port, socketPath, identity) {
			return adoptHTRServe(sup, addr, socketPath, lease, lg), nil
		}
		if foreignAlive() {
			// No owner marker: that file is what authorises terminateManagedHTR
			// to kill a daemon, and this one is the user's. StopHTRServe reads
			// the same marker, so leaving it absent is what keeps a foreign
			// daemon out of ocode's stop path.
			lg.Printf("htr: adopting the running htrcli daemon on %s (not started by ocode)", addr)
			return adoptHTRServe(sup, addr, socketPath, lease, lg), nil
		}
		if opts.Shared.AdoptOnly {
			where := opts.Shared.ConfigPath
			if where == "" {
				where = "htrcli's config"
			}
			notice := opts.Shared.Notice
			if notice == "" {
				notice = "ocode is configured to adopt the shared htrcli daemon and never start one."
			}
			return fail(fmt.Errorf("no htrcli daemon on %s: %s Start `htrcli serve` yourself, or fix %s and retry", addr, notice, where))
		}
	}

	if htrHealthyForInstanceFn(port, socketPath, identity) {
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
	nativeHost := effectiveHTRNativeHostName(opts.NativeHostName)
	var cmd *exec.Cmd
	if opts.sharedMode() {
		cmd = newSharedServeCmd(bin, opts.Shared.Token, port, socketPath, nativeHost)
	} else {
		cmd = newServeCmd(bin, privateServeEnv(identity, port, socketPath, nativeHost))
	}
	var rec tool.ProcessRecord
	started := false
	// adoptedRace records that a daemon appeared between the resolution above
	// and this lock — another ocode process, or the user — so the spawn was
	// skipped. It must not be reported as started by ocode and must not write an
	// owner marker.
	adoptedRace := false
	err = withHTRStartLock(func() error {
		// Both probes are re-checked INSIDE the lock: two ocode processes can
		// pass the resolution above concurrently and then contend for this lock,
		// and the loser would spawn a second daemon onto a port already served.
		if htrHealthyForInstanceFn(port, socketPath, identity) {
			adoptedRace = true
			return nil
		}
		if foreignAlive() {
			adoptedRace = true
			return nil
		}
		if owner, ownerErr := readHTROwner(); ownerErr == nil && owner.Port == port && owner.Socket == socketPath && owner.Identity != "" && htrHealthyForInstanceFn(port, socketPath, owner.Identity) {
			if err := lease.updateIdentity(owner.Identity); err != nil {
				return err
			}
			identity = owner.Identity
			adoptedRace = true
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
		if owner, ownerErr := readHTROwner(); ownerErr == nil && owner.Port == port && owner.Socket == socketPath && owner.Identity != "" && htrHealthyForInstanceFn(port, socketPath, owner.Identity) {
			_ = lease.updateIdentity(owner.Identity)
			lg.Printf("htr: port %d busy, reusing existing daemon", port)
			_ = sup.RegisterShutdownCallback(lease.release)
			return HTRStatus{Running: true, Owned: false, Addr: addr, Socket: socketPath, Binary: bin, Release: lease.release}, nil
		}
		if foreignAlive() {
			lg.Printf("htr: port %d busy, adopting the daemon already running there", port)
			return adoptHTRServe(sup, addr, socketPath, lease, lg), nil
		}
		return fail(fmt.Errorf("start htrcli serve: %w%s", err, htrDaemonOutputTail(cmd)))
	}
	if !started {
		if adoptedRace {
			return adoptHTRServe(sup, addr, socketPath, lease, lg), nil
		}
		_ = sup.RegisterShutdownCallback(lease.release)
		return HTRStatus{Running: true, Owned: false, Addr: addr, Socket: socketPath, Binary: bin, Release: lease.release}, nil
	}
	died := make(chan struct{})
	go watchHTRExitNotify(cmd, sup, rec, identity, lg, died)
	// Readiness is split in two. The BLOCKING half asks one question only: did
	// the process we just spawned die during exec? That is the failure that must
	// surface while the caller is still on the boot path — desktop boot calls
	// StartBrowse synchronously, so anything longer is startup latency the user
	// waits through — and the watcher above reports it on an event, so the
	// failure costs one reap rather than the whole window.
	if !confirmSharedSpawnAlive(died, sharedSpawnConfirmBudget) {
		return fail(fmt.Errorf("htr daemon pid %d exited immediately after start on %s%s", rec.PID, addr, htrDaemonOutputTail(cmd)))
	}
	// This is the spawn site, so this process is unambiguously the spawner.
	if err := htrWriteOwner(identity, port, rec.PID, socketPath, bin, rec.StartedAt, os.Getpid()); err != nil {
		return fail(fmt.Errorf("record managed HTR owner: %w", err))
	}
	// Release the cross-process lease before the supervisor shuts down. The HTR
	// child is retained by the supervisor while another ocode process owns a
	// lease; the final release terminates only the managed daemon.
	_ = sup.RegisterShutdownCallback(lease.release)
	// The BACKGROUND half asks the question the boot path must not wait for: does
	// the daemon actually answer /api/health? It runs behind crashguard.Go,
	// touches no supervisor or host lock, and gives up at sharedVerifyBudget
	// after logging one warning — it never kills the daemon and never starts a
	// replacement. A slow-starting htrcli is not a dead one, and terminating a
	// healthy-but-slow daemon would destroy work the stop rule owns.
	launchSharedVerifyFn(func() { verifySharedDaemonAsync(port, socketPath, identity, lg) })
	lg.Printf("htr: daemon running pid %d on %s (owned, dies with ocode exit); verifying readiness in the background", rec.PID, addr)
	return HTRStatus{Running: true, Owned: true, StartedByOcode: true, Addr: addr, Socket: socketPath, Binary: bin, Release: lease.release}, nil
}

// htrDaemonOutputTail renders whatever the daemon printed, for a failure message.
// It is the only place the child's own diagnostics survive: without the capture
// os/exec would have sent them to the null device and left the operator with a
// bare "did not answer /api/health".
func htrDaemonOutputTail(cmd *exec.Cmd) string {
	out, ok := cmd.Stdout.(*htrDaemonOutput)
	if !ok {
		return ""
	}
	text := out.String()
	if text == "" {
		return " (the daemon printed nothing)"
	}
	return " (daemon output: " + text + ")"
}

// watchHTRExit is the SINGLE death detector for the daemon ocode spawned. It
// runs on its own goroutine for the daemon's whole lifetime, so every "the
// daemon is gone" transition in this file is accounted for here.
//
// Death is terminal and loud, on purpose. There is deliberately NO restart and
// NO fallback to a private per-session daemon: a silent fallback would recreate
// the two-daemon split the shared daemon exists to end, and would hide the
// disconnect the user has to notice. Recovery is the next EXPLICIT ensure — the
// next ocode start, or the Settings start button — so a daemon that died
// mid-session is restarted by a decision someone made, never as a side effect of
// one being noticed.
func watchHTRExit(cmd *exec.Cmd, sup *tool.ProcessSupervisor, rec tool.ProcessRecord, identity string, lg *log.Logger) {
	watchHTRExitNotify(cmd, sup, rec, identity, lg, nil)
}

// watchHTRExitNotify is watchHTRExit plus a death signal. died, when non-nil,
// is closed once the death has been fully accounted for — record marked,
// marker retracted, report logged — so the caller that is still on the boot path
// can return on the event instead of sleeping out a window. It is closed, not
// sent, so no caller can be left blocking on an unbuffered channel.
func watchHTRExitNotify(cmd *exec.Cmd, sup *tool.ProcessSupervisor, rec tool.ProcessRecord, identity string, lg *log.Logger, died chan<- struct{}) {
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
	// The marker is both what StopHTRServe consults and the only in-process
	// record of WHICH daemon this was, so it is also where the port comes from.
	// A death report naming a pid and no port leaves the operator guessing which
	// of possibly two daemons just went away.
	owner, ownerErr := readHTROwner()
	port := 0
	if ownerErr == nil && owner.PID == rec.PID && owner.Identity == identity {
		port = owner.Port
		if path, pathErr := htrOwnerPath(); pathErr == nil {
			_ = os.Remove(path)
		}
	}
	if lg != nil {
		if port > 0 {
			lg.Printf("htr: daemon pid %d on port %d exited with code %d; not restarting it (the next ocode start, or the Settings start button, will ensure it again)", rec.PID, port, code)
		} else {
			lg.Printf("htr: daemon pid %d exited with code %d; not restarting it (the next ocode start, or the Settings start button, will ensure it again)", rec.PID, code)
		}
	}
	// Clear whatever the boot-time ensure cached for the UI. After a death the
	// daemon is stopped, so any claim the UI is still holding is stale — and a
	// healthy-looking one would be worse than none. Nothing is written in its
	// place: the Settings row derives Running from its own liveness probe, and
	// that is what must show Stopped here.
	publishHTRStatus("")
	if died != nil {
		close(died)
	}
}
