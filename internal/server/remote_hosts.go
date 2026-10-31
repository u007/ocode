package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http/httputil"
	"runtime/debug"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/u007/ocode/internal/remote"
	"github.com/u007/ocode/internal/tool"
	"github.com/u007/ocode/internal/version"
)

// remoteHostWorkspace is the subset of remote.RemoteWorkspace that the
// registry needs — it avoids importing the struct directly and lets tests
// inject fakes without touching SSH.
type remoteHostWorkspace interface {
	APIURL() string
	Token() string
	Disconnect() error
	// ServeState / ServeTransport expose the discovered server state and the
	// host transport for the lifecycle endpoints (status/restart). They are
	// named with the Serve prefix because RemoteWorkspace already has State and
	// Transport fields, and Go forbids a method sharing a field's name.
	ServeState() remote.ServeState
	ServeTransport() remote.Transport
	// ServeSlot names the remote server instance this workspace talks to, so
	// restart kills the right process and state file.
	ServeSlot() remote.ServeSlot
	// BrowseURL is the local end of the workspace's browse tunnel ("" when the
	// remote reports no browse origin). It is the one place that encodes the
	// same-port-number tunnel contract; handlers must not re-derive it.
	BrowseURL() string
	// SyncConfig pushes the local auth-profile store and global config
	// files to the already-connected remote host. force bypasses the
	// per-host skip-if-unchanged cache.
	SyncConfig(force bool) error
}

// remoteHostStatus is the JSON body of the remote host lifecycle endpoints.
//
// Connecting/ConnectStartedAt are what lets the SPA tell "the connect has been
// running for 3s" from "it has been wedged for four minutes". Without them the
// client can only observe 503 stage=remote-connecting on every read, which is
// deliberately indistinguishable from progress (and retried forever), so a
// stalled connect has no escalation point and the UI just says "connecting…".
type remoteHostStatus struct {
	Host         string `json:"host"`
	Connected    bool   `json:"connected"`
	Version      string `json:"version"`
	LocalVersion string `json:"local_version"`
	Outdated     bool   `json:"outdated"`
	PID          int    `json:"pid"`
	// Connecting is true while a connect attempt is in flight for this
	// connection (a failed or superseded attempt is not "connecting").
	Connecting bool `json:"connecting"`
	// ConnectStartedAt is the Unix-millisecond start of the in-flight attempt,
	// 0 when nothing is connecting. The client measures elapsed time from it
	// rather than from its own mount, so a reload does not restart the clock.
	ConnectStartedAt int64 `json:"connect_started_at,omitempty"`
}

// remoteHostStageError tags a lifecycle failure with the stage that failed so
// the HTTP handler can report it in the JSON error body's `stage` field.
type remoteHostStageError struct {
	Stage string
	Err   error
}

func (e *remoteHostStageError) Error() string { return e.Err.Error() }
func (e *remoteHostStageError) Unwrap() error { return e.Err }

// remoteConnectionKey is the registry's identity for ONE SSH connection: the
// host string PLUS the port. The host string is "[user@]host" (or
// "wsl:<distro>") and does not carry the port, so two projects on one host
// reached over different ports would otherwise share a single entry — and with
// it one tunnel, one bearer token and one remote registration, silently serving
// each other's data across connections. WSL targets cannot carry a port
// (Target.Validate rejects it), and a zero port means "unspecified/default", so
// both leave the key as the bare host string. Mirrors remoteSlotKey, the exec
// pool's version of the same identity.
func remoteConnectionKey(host string, port int) string {
	if port <= 0 {
		return host
	}
	return host + ":" + strconv.Itoa(port)
}

// remoteHostEntry holds a connected workspace for a single CONNECTION
// (host + port, see remoteConnectionKey). The per-entry mutex serializes
// concurrent connect attempts for the same connection without blocking
// requests for other hosts or ports. The registry-wide mutex protects the
// byConn map; the per-entry mutex protects the workspace and connecting state.
type remoteHostEntry struct {
	mu         sync.Mutex
	workspace  remoteHostWorkspace
	connecting sync.Cond // waits for an in-flight connect to finish
	connected  bool
	failed     bool  // connect failed; entry is kept until remoteConnectCooldown elapses
	connectErr error // the error from a failed connect attempt
	failedAt   time.Time
	// startedAt is when this attempt began. status() reports it while the
	// attempt is in flight so the client can distinguish progress from a wedge
	// instead of treating every 503 as "still connecting" forever.
	startedAt time.Time
	// proxy is the cached reverse proxy built once per connect. The ErrorHandler
	// calls drop(host) on round-trip failure so the next request reconnects.
	proxy *httputil.ReverseProxy
}

// remoteHostRegistry owns one remote.RemoteWorkspace per remote CONNECTION,
// keyed by remoteConnectionKey (host + port — NOT the bare host string, which
// would conflate two ports on one host into a single tunnel). Entries are
// created lazily on first use and torn down at shutdown or on drop. Mirrors the
// portMapRegistry pattern in handler_portmaps.go.
type remoteHostRegistry struct {
	sup *tool.ProcessSupervisor

	mu     sync.Mutex
	byConn map[string]*remoteHostEntry

	// registeredPaths tracks (connection, path) pairs already registered on the
	// remote, so Task 5 registers a project exactly once per process. Keyed by
	// connection (host + port) first, because registration happens on a SPECIFIC
	// server: the same path on a second port has NOT been registered there and
	// must be POSTed again. Connection-first ordering lets drop/closeAll clear
	// one connection's set atomically. Guarded by mu. It deliberately lives on
	// the registry, not on remoteHostEntry: a placeholder entry with no connect
	// in flight would make a later workspaceFor wait forever on a Cond nobody
	// clears.
	registeredPaths map[string]map[string]struct{}

	// expandedPaths maps a saved "~" project path to the absolute path the host
	// stored it as: host -> saved -> expanded. Only the host can expand "~" (its
	// home is not ours), and it reports the result when the project is registered
	// (ensureRemoteProject). Guarded by mu. Never cleared: the expansion depends
	// on the host's home, not on the connection.
	expandedPaths map[string]map[string]string

	// connect is the injectable constructor used by workspaceFor. nil means
	// use the real path (realConnect).
	connect func(target remote.Target, path string) (remoteHostWorkspace, error)

	// factory constructs the concrete workspace for realConnect. It is a field
	// rather than a package var so tests substitute a fake without a global.
	factory func(target remote.Target, path string, sup *tool.ProcessSupervisor) (remoteHostWorkspaceConnector, error)

	// connectTimeout overrides the default backstop on one connect attempt
	// (remoteConnectTimeout). Tests set a short value; production leaves it 0.
	connectTimeout time.Duration

	// connectCooldown overrides remoteConnectCooldown. Tests that exercise
	// retry-after-failure set a tiny value; production leaves it 0.
	connectCooldown time.Duration

	// eventsLiveness overrides the upstream-silence bound of the /api/events
	// relay (remoteEventsLiveness). Tests set a short value; production
	// leaves it 0.
	eventsLiveness time.Duration
}

// remoteHostWorkspaceConnector is the factory result used by realConnect: a
// workspace that still has to be connected.
type remoteHostWorkspaceConnector interface {
	remoteHostWorkspace
	Connect() error
}

func newRemoteHostRegistry(sup *tool.ProcessSupervisor) *remoteHostRegistry {
	reg := &remoteHostRegistry{
		sup:             sup,
		byConn:          make(map[string]*remoteHostEntry),
		registeredPaths: make(map[string]map[string]struct{}),
	}
	reg.factory = func(target remote.Target, path string, sup *tool.ProcessSupervisor) (remoteHostWorkspaceConnector, error) {
		return remote.NewRemoteWorkspace(target, path, "", sup)
	}
	reg.connect = reg.realConnect
	return reg
}

// realConnect wires through remote.ParseTarget + remote.NewRemoteWorkspace and
// then actually CONNECTS the workspace: a constructed workspace has an empty
// API URL and token until Connect runs, so returning it unconnected would make
// every proxied request target an empty origin.
func (reg *remoteHostRegistry) realConnect(target remote.Target, path string) (ws remoteHostWorkspace, err error) {
	conn, err := reg.factory(target, path, reg.sup)
	if err != nil {
		return nil, err
	}
	// If Connect panics after creating the tunnel, no one else holds a handle
	// to the half-built workspace: disconnect it here so the tunnel cannot
	// leak while the panic is converted to an error.
	defer func() {
		if r := recover(); r != nil {
			if derr := conn.Disconnect(); derr != nil {
				log.Printf("remote: disconnect half-built workspace for %s after panic: %v", target.String(), derr)
			}
			ws, err = nil, fmt.Errorf("connect %s panicked: %v", target.String(), r)
		}
	}()
	if cerr := conn.Connect(); cerr != nil {
		return nil, fmt.Errorf("connect %s: %w", target.String(), cerr)
	}
	return conn, nil
}

// isRegistered reports whether the given (host, path) pair has been
// registered on the remote. It is a read-only check — it does NOT mark
// the pair as registered. Task 5 uses this to avoid marking before a
// POST succeeds.
func (reg *remoteHostRegistry) isRegistered(host string, port int, path string) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	paths, ok := reg.registeredPaths[remoteConnectionKey(host, port)]
	if !ok {
		return false
	}
	_, exists := paths[path]
	return exists
}

// workspaceFor returns a connected workspace for the given host at its default
// port, creating one lazily if needed. Concurrent callers for the same
// CONNECTION share one connect attempt; callers for different hosts or ports
// proceed in parallel. A failed connect is remembered for remoteConnectCooldown.
// It delegates to workspaceForPort with port 0 (meaning: no explicit port
// override).
func (reg *remoteHostRegistry) workspaceFor(host, path string) (remoteHostWorkspace, error) {
	return reg.workspaceForPort(host, path, 0)
}

// connectedWorkspace returns the workspace for a connection ONLY if it is
// already connected. It never dials, so callers that sit outside authMiddleware
// (the browse proxy) cannot be used to trigger SSH connects or provisioning.
// port follows workspaceForPort's rule: non-zero overrides the parsed target's.
func (reg *remoteHostRegistry) connectedWorkspace(host string, port int) (remoteHostWorkspace, bool) {
	target, err := remote.ParseTarget(host)
	if err != nil {
		return nil, false
	}
	if port > 0 {
		target.Port = port
	}
	if err := target.Validate(); err != nil {
		return nil, false
	}
	reg.mu.Lock()
	e, ok := reg.byConn[remoteConnectionKey(host, target.Port)]
	reg.mu.Unlock()
	if !ok {
		return nil, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.connected || e.workspace == nil {
		return nil, false
	}
	return e.workspace, true
}

// workspaceForPort is like workspaceFor but also sets the SSH port on the
// parsed Target when port is non-zero. This lets the proxy path honor a
// saved project's RemotePort without changing the host string.
func (reg *remoteHostRegistry) workspaceForPort(host, path string, port int) (remoteHostWorkspace, error) {
	target, err := remote.ParseTarget(host)
	if err != nil {
		return nil, err
	}

	// When a non-zero port is provided (e.g. from a saved project's
	// RemotePort), override the parsed target's port before validation so an
	// out-of-range saved port fails closed.
	if port > 0 {
		target.Port = port
	}
	if err := target.Validate(); err != nil {
		return nil, err
	}

	// One entry per CONNECTION. target.Port is the effective port (parsed
	// target, then the explicit override above), so the key matches the port
	// the connect actually dials.
	key := remoteConnectionKey(host, target.Port)

	// Fast path: entry already connected, or failed within the cooldown.
	reg.mu.Lock()
	if e, ok := reg.byConn[key]; ok {
		e.mu.Lock()
		if e.connected {
			ws := e.workspace
			e.mu.Unlock()
			reg.mu.Unlock()
			return ws, nil
		}
		if e.failed {
			if time.Since(e.failedAt) < reg.cooldown() {
				err := e.connectErr
				e.mu.Unlock()
				reg.mu.Unlock()
				return nil, err
			}
			// Cooldown over: evict so this call starts a fresh connect.
			delete(reg.byConn, key)
		}
		// Connect in progress — wait for it.
		e.mu.Unlock()
	}

	// Slow path: create or wait on entry.
	e, created := reg.getOrCreateEntry(key)
	reg.mu.Unlock()

	if created {
		// The connect runs in the background: a request must never stay parked for
		// a whole connect, or one slow project pins a browser connection (WebKit
		// allows ~6 per origin) and stalls every other project.
		go reg.runConnect(e, key, target, path, host)
	}

	// Every caller, owner included, waits only briefly: a proxied request parked
	// for a whole connect pins one of the browser's few per-origin connections
	// and starves every other project.
	e.mu.Lock()
	waitStart := time.Now()
	timer := time.AfterFunc(remoteConnectWaitBound, func() {
		e.mu.Lock()
		e.connecting.Broadcast()
		e.mu.Unlock()
	})
	defer timer.Stop()
	for !e.connected && !e.failed {
		if time.Since(waitStart) >= remoteConnectWaitBound {
			e.mu.Unlock()
			return nil, &remoteHostStageError{
				Stage: "remote-connecting",
				Err:   fmt.Errorf("remote connect to %s: %w", key, errConnectInProgress),
			}
		}
		e.connecting.Wait()
	}
	if e.failed {
		err := e.connectErr
		e.mu.Unlock()
		return nil, err
	}
	ws := e.workspace
	e.mu.Unlock()
	return ws, nil
}

// runConnect performs the connect for the entry the caller created and resolves it
// (connected or failed, waiters broadcast). A panic in the connect implementation
// is converted to an error by connectGuarded so the entry is always resolved.
func (reg *remoteHostRegistry) runConnect(e *remoteHostEntry, key string, target remote.Target, path, host string) {
	// The connect runs without reg.mu held (connectBounded enforces the
	// deadline); every exit below resolves the entry so no waiter is stranded.
	log.Printf("remote: connecting to %s", key)
	start := time.Now()
	ws, err := reg.connectBounded(target, path, host)
	if err != nil {
		// Publish the failure under reg.mu → e.mu, the established lock order.
		// The entry STAYS in byConn for remoteConnectCooldown so callers that
		// arrive right after the failure get this error instead of each starting
		// another connect (and another remote server). If drop()/closeAll()
		// already replaced the entry, there is nothing to keep.
		reg.mu.Lock()
		e.mu.Lock()
		e.failed = true
		e.failedAt = time.Now()
		e.connectErr = err
		e.connecting.Broadcast()
		e.mu.Unlock()
		reg.mu.Unlock()

		log.Printf("remote: connect to %s failed after %v: %v", key, time.Since(start), err)
		return
	}

	// Publish success atomically with the identity check. If drop()/closeAll()
	// removed or replaced this entry while we were connecting, the workspace is
	// orphaned: disconnect it so its tunnel cannot leak, and report
	// supersession so the caller retries against the live entry. Waiters on the
	// old entry must NOT receive the orphaned workspace, so the decision is
	// made under e.mu before the broadcast.
	reg.mu.Lock()
	e.mu.Lock()
	if reg.byConn[key] != e {
		e.failed = true
		e.connectErr = errConnectSuperseded
		e.connecting.Broadcast()
		e.mu.Unlock()
		reg.mu.Unlock()
		if derr := ws.Disconnect(); derr != nil {
			log.Printf("remote: disconnect superseded workspace for %s: %v", key, derr)
		}
		log.Printf("remote: connect to %s superseded by drop after %v", key, time.Since(start))
		return
	}
	e.workspace = ws
	e.proxy = nil
	// Build the reverse proxy once so HandleRemoteProxy never rebuilds it.
	// onError drops the entry on round-trip failure so the next request
	// reconnects. NewAPIProxy only parses the URL and builds the proxy (no
	// I/O), so building it here under the locks is safe. On failure the entry
	// is resolved exactly like a connect failure (mark failed, broadcast,
	// evict) so waiters are not stranded and the workspace's tunnel is closed.
	p, perr := remote.NewAPIProxy(ws.APIURL(), ws.Token(), func(err error) { reg.drop(host, target.Port) })
	if perr != nil {
		e.failed = true
		e.connectErr = perr
		e.connecting.Broadcast()
		if reg.byConn[key] == e {
			delete(reg.byConn, key)
		}
		e.mu.Unlock()
		reg.mu.Unlock()
		log.Printf("remote: build proxy for %s: %v", key, perr)
		if derr := ws.Disconnect(); derr != nil {
			log.Printf("remote: disconnect after proxy build failure for %s: %v", key, derr)
		}
		return
	}
	e.proxy = p
	e.connected = true
	e.connecting.Broadcast()
	e.mu.Unlock()
	reg.mu.Unlock()

	log.Printf("remote: connected to %s at %s (took %v)", key, ws.APIURL(), time.Since(start))
}

// status reports what the registry knows about host without connecting. A host
// that has never connected (or was dropped) reports connected=false and no
// version; LocalVersion is always the local build's version.
func (reg *remoteHostRegistry) status(host string, port int) remoteHostStatus {
	st := remoteHostStatus{Host: host, LocalVersion: version.Version}
	reg.mu.Lock()
	if e, ok := reg.byConn[remoteConnectionKey(host, port)]; ok {
		e.mu.Lock()
		if e.connected && e.workspace != nil {
			ws := e.workspace.ServeState()
			st.Connected = true
			st.Version = ws.Version
			st.Outdated = ws.Outdated
			st.PID = ws.PID
		} else if !e.failed && !e.startedAt.IsZero() {
			// An attempt is in flight. A failed attempt is NOT connecting: it
			// is being remembered for the cooldown, and reporting it as
			// connecting would keep a dead host looking like progress.
			st.Connecting = true
			st.ConnectStartedAt = e.startedAt.UnixMilli()
		}
		e.mu.Unlock()
	}
	reg.mu.Unlock()
	return st
}

// lastFailure returns the error the connection's most recent connect failed
// with, or nil when it has no failed entry. A caller that started a connect
// and got "connecting" back uses it to report the real cause once the
// background attempt has failed.
func (reg *remoteHostRegistry) lastFailure(host string, port int) error {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e, ok := reg.byConn[remoteConnectionKey(host, port)]
	if !ok {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failed && !e.connected {
		return e.connectErr
	}
	return nil
}

// connectHost brings host up (discover-or-start the remote server, open the
// tunnel, register the project) and returns the resulting status. Named
// connectHost, not connect, because the registry already has a connect field.
func (reg *remoteHostRegistry) connectHost(host, path string, port int) (remoteHostStatus, error) {
	if _, err := reg.workspaceForPort(host, path, port); err != nil {
		return reg.status(host, port), &remoteHostStageError{Stage: "remote-connect", Err: err}
	}
	return reg.status(host, port), nil
}

// snapshotForRestart reads the connected workspace's transport and pid and the
// set of paths registered on the host, all before drop clears them.
func (reg *remoteHostRegistry) snapshotForRestart(host string, port int) (remote.Transport, int, remote.ServeSlot, []string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	key := remoteConnectionKey(host, port)
	var transport remote.Transport
	var slot remote.ServeSlot
	pid := 0
	if e, ok := reg.byConn[key]; ok {
		e.mu.Lock()
		if e.connected && e.workspace != nil {
			transport = e.workspace.ServeTransport()
			slot = e.workspace.ServeSlot()
			pid = e.workspace.ServeState().PID
		}
		e.mu.Unlock()
	}
	paths := make([]string, 0, len(reg.registeredPaths[key]))
	for p := range reg.registeredPaths[key] {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return transport, pid, slot, paths
}

// restart kills the connected remote server (if any), drops the host entry,
// reconnects (which starts a fresh server at the local version), and
// re-registers every project that was registered on the host. Restart is
// unguarded by design: it kills running turns and terminals. On any failure the
// entry is left dropped so the next request reconnects; the returned error
// carries the stage that failed.
func (reg *remoteHostRegistry) restart(host, path string, port int) (remoteHostStatus, error) {
	transport, pid, slot, savedPaths := reg.snapshotForRestart(host, port)
	// Kill over the still-live transport, then drop unconditionally so a
	// failure at any stage leaves no half-live entry.
	var killErr error
	if transport != nil && pid > 0 {
		killErr = remote.KillServer(transport, pid, slot)
	}
	// Force: a restart requested while a connect is still running must not be a
	// no-op. killErr stays nil in that case (there is no pid yet), so without
	// the force the handler would drop nothing, reconnect into the SAME wedged
	// attempt and report success-shaped status while the host never comes up.
	reg.dropForce(host, port)
	if killErr != nil {
		return reg.status(host, port), &remoteHostStageError{Stage: "remote-kill", Err: killErr}
	}
	ws, err := reg.workspaceForPort(host, path, port)
	if err != nil {
		return reg.status(host, port), &remoteHostStageError{Stage: "remote-connect", Err: err}
	}
	for _, p := range savedPaths {
		if err := ensureRemoteProject(context.Background(), ws, reg, host, port, p); err != nil {
			return reg.status(host, port), &remoteHostStageError{Stage: "remote-register", Err: err}
		}
	}
	return reg.status(host, port), nil
}

// remoteConnectCooldown is how long a failed connect is remembered. Browser
// polls (terminal sockets, session state, the events relay) keep arriving while
// a host is down; without this each one started its own ~11s connect.
var remoteConnectCooldown = 5 * time.Second

// errConnectInProgress is returned to a caller that stopped waiting for a
// connect that is still running in the background. It is NOT a host failure:
// handlers answer 503 + Retry-After (stage "remote-connecting") so the SPA does
// not open the 2-minute unreachable window it applies to a 502.
var errConnectInProgress = errors.New("still connecting")

// cooldown returns the effective failed-connect memory: the registry's override
// when set, else remoteConnectCooldown.
func (reg *remoteHostRegistry) cooldown() time.Duration {
	if reg.connectCooldown > 0 {
		return reg.connectCooldown
	}
	return remoteConnectCooldown
}

// remoteConnectWaitBound caps how long a request waits on another caller's
// in-flight connect before failing fast.
var remoteConnectWaitBound = 2 * time.Second

// errConnectSuperseded is returned when a drop()/closeAll() removed the entry
// while its connect was in flight. The built workspace was disconnected. In
// HandleRemoteProxy this maps to a 502 response — the caller does NOT
// auto-retry — but the next request will trigger a fresh connect.
var errConnectSuperseded = errors.New("remote connect superseded by drop")

// connectGuarded runs the injected connect, converting a panic into an error so
// the owning entry is always resolved (marked failed, broadcast, evicted). The
// stack is included so a rare panic stays diagnosable after being flattened.
func connectGuarded(connect func(remote.Target, string) (remoteHostWorkspace, error), target remote.Target, path, host string) (ws remoteHostWorkspace, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("connect %s panicked: %v\n%s", host, r, debug.Stack())
		}
	}()
	return connect(target, path)
}

// remoteConnectTimeout is the default backstop for one connect attempt. It is
// deliberately generous because first use may cross-compile and upload the
// ocode binary, sync credentials, discover-or-start the remote server, and open
// the tunnel — all before the tunnel is up. The per-command ssh options
// (internal/remote sshFailFastArgs) bound each individual exec's connection
// setup, but ConnectTimeout does not bound a remote command's runtime; this
// catches the residual cases (a stalled upload, or a remote command that
// connects but never returns). Without it, a wedged connect leaves every waiter
// on this host pinned on the entry's sync.Cond forever.
//
// A spurious timeout is self-healing: the next attempt finds the binary already
// installed (BinaryExists) and reuses it, so it connects quickly.
var remoteConnectTimeout = 10 * time.Minute

// connectDeadline returns the effective connect bound: the registry's override
// when set, else the package default.
func (reg *remoteHostRegistry) connectDeadline() time.Duration {
	if reg.connectTimeout > 0 {
		return reg.connectTimeout
	}
	return remoteConnectTimeout
}

// connectBounded runs the connect with a deadline. On timeout the owning entry
// is resolved as failed by workspaceForPort's normal failure path (all waiters
// are broadcast and the entry evicted), so a hung connect can never pin
// requests for this host indefinitely. The connect goroutine cannot be killed
// (it owns an ssh process), so a late result is reaped in the background: a
// workspace it managed to build is disconnected rather than leaked after its
// entry has already been evicted.
func (reg *remoteHostRegistry) connectBounded(target remote.Target, path, host string) (remoteHostWorkspace, error) {
	type result struct {
		ws  remoteHostWorkspace
		err error
	}
	deadline := reg.connectDeadline()
	done := make(chan result, 1)
	go func() {
		ws, err := connectGuarded(reg.connect, target, path, host)
		done <- result{ws, err}
	}()

	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.ws, r.err
	case <-timer.C:
		go func() {
			if r := <-done; r.ws != nil {
				if derr := r.ws.Disconnect(); derr != nil {
					log.Printf("remote: disconnect late workspace for host %s after connect timeout: %v", host, derr)
				}
			}
		}()
		return nil, &remoteHostStageError{
			Stage: "remote-connect",
			Err:   fmt.Errorf("remote connect to %s timed out after %s", host, deadline),
		}
	}
}

// getOrCreateEntry returns the entry for a connection key (see
// remoteConnectionKey). If none exists, a new one is created with connecting
// state and created=true. If an existing entry is connected, it returns that
// entry with created=false. Callers MUST hold reg.mu.
func (reg *remoteHostRegistry) getOrCreateEntry(key string) (*remoteHostEntry, bool) {
	if e, ok := reg.byConn[key]; ok {
		return e, false
	}
	e := &remoteHostEntry{startedAt: time.Now()}
	e.connecting.L = &e.mu
	reg.byConn[key] = e
	return e, true
}

// drop removes the entry for ONE connection (host + port) and disconnects its
// workspace outside the lock. This prevents a tunnel leak when Task 5's proxy
// error handler calls it. The entry is removed from the map first so concurrent
// workspaceFor calls see it gone and create a fresh entry.
//
// It is deliberately scoped to a single port: another port on the same host is
// a separate tunnel and must not be torn down because one connection failed.
func (reg *remoteHostRegistry) drop(host string, port int) {
	reg.dropMaybe(host, port, false)
}

// dropForce evicts a connection EVEN IF a connect is still running for it.
//
// A normal drop refuses that on purpose: superseding an in-flight connect lets
// the next poller start a parallel attempt, which can relaunch the remote
// server and orphan the first. That is the right default for background
// polling, but it also means a connect that has genuinely wedged can only be
// cleared by waiting out remoteConnectTimeout. An EXPLICIT user retry after the
// stall guard fires is the one caller allowed to override it.
//
// The in-flight attempt is not cancelled — it keeps running detached. Its
// completion path compares reg.byConn[key] with its own entry and disconnects
// any late workspace it produces, so nothing leaks. registeredPaths is cleared
// so the new attempt re-registers the project on the fresh server.
func (reg *remoteHostRegistry) dropForce(host string, port int) {
	reg.dropMaybe(host, port, true)
}

func (reg *remoteHostRegistry) dropMaybe(host string, port int, force bool) {
	key := remoteConnectionKey(host, port)
	reg.mu.Lock()
	e, ok := reg.byConn[key]
	if ok && !force {
		e.mu.Lock()
		inFlight := !e.connected && !e.failed
		e.mu.Unlock()
		if inFlight {
			// A connect is still running for this connection. Superseding it
			// lets the next poller start a parallel connect, which relaunches
			// the remote server and orphans the first. The in-flight connect
			// resolves the entry itself.
			reg.mu.Unlock()
			return
		}
	}
	delete(reg.byConn, key)
	// Clear unconditionally: markRegistered may have recorded paths for a
	// connection with no live entry yet, and a reconnect must re-register the
	// project.
	delete(reg.registeredPaths, key)
	reg.mu.Unlock()

	if !ok {
		return
	}

	// Disconnect outside the registry lock.
	e.mu.Lock()
	ws := e.workspace
	e.mu.Unlock()

	if ws != nil {
		if err := ws.Disconnect(); err != nil {
			log.Printf("remote: disconnect %s: %v", key, err)
		}
	}
}

// clearFailed evicts a connection whose last connect FAILED, so an explicit
// user connect starts a fresh attempt instead of replaying the cooldown error.
// It is deliberately narrower than drop: a connected entry keeps its live
// tunnel, and an in-flight connect is left to resolve (drop would tear down a
// live tunnel or supersede the in-flight connect).
func (reg *remoteHostRegistry) clearFailed(host string, port int) {
	key := remoteConnectionKey(host, port)
	reg.mu.Lock()
	defer reg.mu.Unlock()
	e, ok := reg.byConn[key]
	if !ok {
		return
	}
	e.mu.Lock()
	failed := e.failed
	e.mu.Unlock()
	if !failed {
		return
	}
	delete(reg.byConn, key)
	delete(reg.registeredPaths, key)
}

// closeAll disconnects every live entry. It is called from Handler.Shutdown.
func (reg *remoteHostRegistry) closeAll(ctx context.Context) {
	reg.mu.Lock()
	entries := make(map[string]*remoteHostEntry, len(reg.byConn))
	for k, v := range reg.byConn {
		entries[k] = v
	}
	// Clear the map so no new connects reuse stale entries.
	reg.byConn = make(map[string]*remoteHostEntry)
	reg.registeredPaths = make(map[string]map[string]struct{})
	reg.mu.Unlock()

	for key, e := range entries {
		e.mu.Lock()
		ws := e.workspace
		e.mu.Unlock()

		if ws != nil {
			if err := ws.Disconnect(); err != nil {
				log.Printf("remote: shutdown disconnect %s: %v", key, err)
			}
		}
	}
}

// recordExpandedPath remembers the path a host stored a saved "~" project under.
func (reg *remoteHostRegistry) recordExpandedPath(host, saved, expanded string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.expandedPaths == nil {
		reg.expandedPaths = make(map[string]map[string]string)
	}
	paths := reg.expandedPaths[host]
	if paths == nil {
		paths = make(map[string]string)
		reg.expandedPaths[host] = paths
	}
	paths[saved] = expanded
}

// expandedPath returns the path a host stored a saved "~" project under, and
// false until this process has registered that project on the host.
func (reg *remoteHostRegistry) expandedPath(host, saved string) (string, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	expanded, ok := reg.expandedPaths[host][saved]
	return expanded, ok
}

// markRegistered records that the given (host, path) has been registered on
// the remote. Returns true if this is the first registration for that pair,
// false if it was already registered. Task 5 uses this to register a project
// exactly once per process. It never creates a connection entry — the path set
// is registry-level, so calling it before workspaceFor cannot leave a
// placeholder that blocks a later connect.
func (reg *remoteHostRegistry) markRegistered(host string, port int, path string) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	key := remoteConnectionKey(host, port)
	paths := reg.registeredPaths[key]
	if paths == nil {
		paths = make(map[string]struct{})
		reg.registeredPaths[key] = paths
	}
	if _, exists := paths[path]; exists {
		return false
	}
	paths[path] = struct{}{}
	return true
}

// proxyFor returns the cached reverse proxy for a connection (host + port). It
// must be called on a connected entry — callers obtain the proxy under e.mu
// (see HandleRemoteProxy). Keyed like every other registry lookup so the proxy
// handed back belongs to the connection the caller asked for.
func (reg *remoteHostRegistry) proxyFor(host string, port int) (*httputil.ReverseProxy, bool) {
	reg.mu.Lock()
	e, ok := reg.byConn[remoteConnectionKey(host, port)]
	reg.mu.Unlock()
	if !ok {
		return nil, false
	}
	e.mu.Lock()
	p := e.proxy
	e.mu.Unlock()
	return p, p != nil
}

// SyncHost pushes local config/auth to one connected remote host. force
// bypasses the per-host skip-if-unchanged cache. Returns an error if the
// host is not currently connected.
func (reg *remoteHostRegistry) SyncHost(host string, port int, force bool) error {
	reg.mu.Lock()
	entry, ok := reg.byConn[remoteConnectionKey(host, port)]
	reg.mu.Unlock()
	if !ok || entry == nil || !entry.connected || entry.workspace == nil {
		return fmt.Errorf("remote host not connected: %s", host)
	}
	entry.mu.Lock()
	ws := entry.workspace
	entry.mu.Unlock()
	return ws.SyncConfig(force)
}

// SyncConnectedHosts fans out a cache-aware sync to every currently
// connected remote host. Best-effort: per-host errors are logged and do
// not abort the fan-out. Used to propagate local config/auth changes to
// remote servers without a reconnect.
func (reg *remoteHostRegistry) SyncConnectedHosts() {
	reg.mu.Lock()
	type item struct {
		key string
		ws  remoteHostWorkspace
	}
	items := make([]item, 0, len(reg.byConn))
	for key, e := range reg.byConn {
		if e == nil || !e.connected || e.workspace == nil {
			continue
		}
		e.mu.Lock()
		ws := e.workspace
		e.mu.Unlock()
		items = append(items, item{key: key, ws: ws})
	}
	reg.mu.Unlock()

	for _, it := range items {
		if err := it.ws.SyncConfig(false); err != nil {
			log.Printf("remote sync: %s failed: %v", it.key, err)
		}
	}
}
