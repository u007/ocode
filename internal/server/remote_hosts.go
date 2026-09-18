package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http/httputil"
	"runtime/debug"
	"sort"
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
}

// remoteHostStatus is the JSON body of the remote host lifecycle endpoints.
type remoteHostStatus struct {
	Host         string `json:"host"`
	Connected    bool   `json:"connected"`
	Version      string `json:"version"`
	LocalVersion string `json:"local_version"`
	Outdated     bool   `json:"outdated"`
	PID          int    `json:"pid"`
}

// remoteHostStageError tags a lifecycle failure with the stage that failed so
// the HTTP handler can report it in the JSON error body's `stage` field.
type remoteHostStageError struct {
	Stage string
	Err   error
}

func (e *remoteHostStageError) Error() string { return e.Err.Error() }
func (e *remoteHostStageError) Unwrap() error { return e.Err }

// remoteHostEntry holds a connected workspace for a single host. The per-entry
// mutex serializes concurrent connect attempts for the same host without
// blocking requests for other hosts. The registry-wide mutex protects the
// byHost map; the per-entry mutex protects the workspace and connecting state.
type remoteHostEntry struct {
	mu         sync.Mutex
	workspace  remoteHostWorkspace
	connecting sync.Cond // waits for an in-flight connect to finish
	connected  bool
	failed     bool  // connect failed; entry should be removed
	connectErr error // the error from a failed connect attempt
	// proxy is the cached reverse proxy built once per connect. The ErrorHandler
	// calls drop(host) on round-trip failure so the next request reconnects.
	proxy *httputil.ReverseProxy
}

// remoteHostRegistry owns one remote.RemoteWorkspace per remote host, keyed by
// the raw host string. Entries are created lazily on first use and torn down
// at shutdown or on drop. Mirrors the portMapRegistry pattern in
// handler_portmaps.go.
type remoteHostRegistry struct {
	sup *tool.ProcessSupervisor

	mu     sync.Mutex
	byHost map[string]*remoteHostEntry

	// registeredPaths tracks (host, path) pairs already registered on the
	// remote, so Task 5 registers a project exactly once per process. Keyed by
	// host first so drop/closeAll can clear a host's set atomically. Guarded by
	// mu. It deliberately lives on the registry, not on remoteHostEntry: a
	// placeholder entry with no connect in flight would make a later
	// workspaceFor wait forever on a Cond nobody clears.
	registeredPaths map[string]map[string]struct{}

	// connect is the injectable constructor used by workspaceFor. nil means
	// use the real path (realConnect).
	connect func(target remote.Target, path string) (remoteHostWorkspace, error)

	// factory constructs the concrete workspace for realConnect. It is a field
	// rather than a package var so tests substitute a fake without a global.
	factory func(target remote.Target, path string, sup *tool.ProcessSupervisor) (remoteHostWorkspaceConnector, error)

	// connectTimeout overrides the default backstop on one connect attempt
	// (remoteConnectTimeout). Tests set a short value; production leaves it 0.
	connectTimeout time.Duration
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
		byHost:          make(map[string]*remoteHostEntry),
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
func (reg *remoteHostRegistry) isRegistered(host, path string) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	paths, ok := reg.registeredPaths[host]
	if !ok {
		return false
	}
	_, exists := paths[path]
	return exists
}

// workspaceFor returns a connected workspace for the given host, creating one
// lazily if needed. Concurrent callers for the same host share one connect
// attempt; callers for different hosts proceed in parallel. A failed connect
// is not cached — the next call retries. It delegates to workspaceForPort
// with port 0 (meaning: no explicit port override).
func (reg *remoteHostRegistry) workspaceFor(host, path string) (remoteHostWorkspace, error) {
	return reg.workspaceForPort(host, path, 0)
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

	// Fast path: entry already connected.
	reg.mu.Lock()
	if e, ok := reg.byHost[host]; ok {
		e.mu.Lock()
		if e.connected {
			ws := e.workspace
			e.mu.Unlock()
			reg.mu.Unlock()
			return ws, nil
		}
		// Connect in progress or failed — wait for it.
		e.mu.Unlock()
	}

	// Slow path: create or wait on entry.
	e, created := reg.getOrCreateEntry(host)
	reg.mu.Unlock()

	if !created {
		// Another goroutine is connecting. Wait for it.
		e.mu.Lock()
		for !e.connected && !e.failed {
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

	// We own the connect. Run it outside the registry-wide mutex. A panic in
	// the connect implementation is converted to an error so the entry is
	// always resolved; otherwise it would stay in byHost with nobody to
	// broadcast and every later caller for this host would block forever.
	log.Printf("remote: connecting to host %s", host)
	start := time.Now()
	ws, err := reg.connectBounded(target, path, host)
	if err != nil {
		// Publish the failure and evict the entry under reg.mu → e.mu, the
		// established lock order. The delete is conditional on identity: a
		// concurrent drop()/closeAll() may have replaced this entry with a
		// fresh one that must not be evicted by this older failure.
		reg.mu.Lock()
		e.mu.Lock()
		e.failed = true
		e.connectErr = err
		e.connecting.Broadcast()
		if reg.byHost[host] == e {
			delete(reg.byHost, host)
		}
		e.mu.Unlock()
		reg.mu.Unlock()

		log.Printf("remote: connect to host %s failed after %v: %v", host, time.Since(start), err)
		return nil, err
	}

	// Publish success atomically with the identity check. If drop()/closeAll()
	// removed or replaced this entry while we were connecting, the workspace is
	// orphaned: disconnect it so its tunnel cannot leak, and report
	// supersession so the caller retries against the live entry. Waiters on the
	// old entry must NOT receive the orphaned workspace, so the decision is
	// made under e.mu before the broadcast.
	reg.mu.Lock()
	e.mu.Lock()
	if reg.byHost[host] != e {
		e.failed = true
		e.connectErr = errConnectSuperseded
		e.connecting.Broadcast()
		e.mu.Unlock()
		reg.mu.Unlock()
		if derr := ws.Disconnect(); derr != nil {
			log.Printf("remote: disconnect superseded workspace for host %s: %v", host, derr)
		}
		log.Printf("remote: connect to host %s superseded by drop after %v", host, time.Since(start))
		return nil, errConnectSuperseded
	}
	e.workspace = ws
	e.proxy = nil
	// Build the reverse proxy once so HandleRemoteProxy never rebuilds it.
	// onError drops the entry on round-trip failure so the next request
	// reconnects. NewAPIProxy only parses the URL and builds the proxy (no
	// I/O), so building it here under the locks is safe. On failure the entry
	// is resolved exactly like a connect failure (mark failed, broadcast,
	// evict) so waiters are not stranded and the workspace's tunnel is closed.
	p, perr := remote.NewAPIProxy(ws.APIURL(), ws.Token(), func(err error) { reg.drop(host) })
	if perr != nil {
		e.failed = true
		e.connectErr = perr
		e.connecting.Broadcast()
		if reg.byHost[host] == e {
			delete(reg.byHost, host)
		}
		e.mu.Unlock()
		reg.mu.Unlock()
		log.Printf("remote: build proxy for host %s: %v", host, perr)
		if derr := ws.Disconnect(); derr != nil {
			log.Printf("remote: disconnect after proxy build failure for host %s: %v", host, derr)
		}
		return nil, perr
	}
	e.proxy = p
	e.connected = true
	e.connecting.Broadcast()
	e.mu.Unlock()
	reg.mu.Unlock()

	log.Printf("remote: connected to host %s at %s (took %v)", host, ws.APIURL(), time.Since(start))
	return ws, nil
}

// status reports what the registry knows about host without connecting. A host
// that has never connected (or was dropped) reports connected=false and no
// version; LocalVersion is always the local build's version.
func (reg *remoteHostRegistry) status(host string) remoteHostStatus {
	st := remoteHostStatus{Host: host, LocalVersion: version.Version}
	reg.mu.Lock()
	if e, ok := reg.byHost[host]; ok {
		e.mu.Lock()
		if e.connected && e.workspace != nil {
			ws := e.workspace.ServeState()
			st.Connected = true
			st.Version = ws.Version
			st.Outdated = ws.Outdated
			st.PID = ws.PID
		}
		e.mu.Unlock()
	}
	reg.mu.Unlock()
	return st
}

// connectHost brings host up (discover-or-start the remote server, open the
// tunnel, register the project) and returns the resulting status. Named
// connectHost, not connect, because the registry already has a connect field.
func (reg *remoteHostRegistry) connectHost(host, path string, port int) (remoteHostStatus, error) {
	if _, err := reg.workspaceForPort(host, path, port); err != nil {
		return reg.status(host), &remoteHostStageError{Stage: "remote-connect", Err: err}
	}
	return reg.status(host), nil
}

// snapshotForRestart reads the connected workspace's transport and pid and the
// set of paths registered on the host, all before drop clears them.
func (reg *remoteHostRegistry) snapshotForRestart(host string) (remote.Transport, int, []string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	var transport remote.Transport
	pid := 0
	if e, ok := reg.byHost[host]; ok {
		e.mu.Lock()
		if e.connected && e.workspace != nil {
			transport = e.workspace.ServeTransport()
			pid = e.workspace.ServeState().PID
		}
		e.mu.Unlock()
	}
	paths := make([]string, 0, len(reg.registeredPaths[host]))
	for p := range reg.registeredPaths[host] {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return transport, pid, paths
}

// restart kills the connected remote server (if any), drops the host entry,
// reconnects (which starts a fresh server at the local version), and
// re-registers every project that was registered on the host. Restart is
// unguarded by design: it kills running turns and terminals. On any failure the
// entry is left dropped so the next request reconnects; the returned error
// carries the stage that failed.
func (reg *remoteHostRegistry) restart(host, path string, port int) (remoteHostStatus, error) {
	transport, pid, savedPaths := reg.snapshotForRestart(host)
	// Kill over the still-live transport, then drop unconditionally so a
	// failure at any stage leaves no half-live entry.
	var killErr error
	if transport != nil && pid > 0 {
		killErr = remote.KillServer(transport, pid)
	}
	reg.drop(host)
	if killErr != nil {
		return reg.status(host), &remoteHostStageError{Stage: "remote-kill", Err: killErr}
	}
	ws, err := reg.workspaceForPort(host, path, port)
	if err != nil {
		return reg.status(host), &remoteHostStageError{Stage: "remote-connect", Err: err}
	}
	for _, p := range savedPaths {
		if err := ensureRemoteProject(context.Background(), ws, reg, host, p); err != nil {
			return reg.status(host), &remoteHostStageError{Stage: "remote-register", Err: err}
		}
	}
	return reg.status(host), nil
}

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

// getOrCreateEntry returns the entry for host. If none exists, a new one is
// created with connecting state and created=true. If an existing entry is
// connected, it returns that entry with created=false. Callers MUST hold
// reg.mu.
func (reg *remoteHostRegistry) getOrCreateEntry(host string) (*remoteHostEntry, bool) {
	if e, ok := reg.byHost[host]; ok {
		return e, false
	}
	e := &remoteHostEntry{}
	e.connecting.L = &e.mu
	reg.byHost[host] = e
	return e, true
}

// drop removes the entry for host and disconnects its workspace outside the
// lock. This prevents a tunnel leak when Task 5's proxy error handler calls
// it. The entry is removed from the map first so concurrent workspaceFor
// calls see it gone and create a fresh entry.
func (reg *remoteHostRegistry) drop(host string) {
	reg.mu.Lock()
	e, ok := reg.byHost[host]
	delete(reg.byHost, host)
	// Clear unconditionally: markRegistered may have recorded paths for a host
	// with no live entry yet, and a reconnect must re-register the project.
	delete(reg.registeredPaths, host)
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
			log.Printf("remote: disconnect host %s: %v", host, err)
		}
	}
}

// closeAll disconnects every live entry. It is called from Handler.Shutdown.
func (reg *remoteHostRegistry) closeAll(ctx context.Context) {
	reg.mu.Lock()
	entries := make(map[string]*remoteHostEntry, len(reg.byHost))
	for k, v := range reg.byHost {
		entries[k] = v
	}
	// Clear the map so no new connects reuse stale entries.
	reg.byHost = make(map[string]*remoteHostEntry)
	reg.registeredPaths = make(map[string]map[string]struct{})
	reg.mu.Unlock()

	for host, e := range entries {
		e.mu.Lock()
		ws := e.workspace
		e.mu.Unlock()

		if ws != nil {
			if err := ws.Disconnect(); err != nil {
				log.Printf("remote: shutdown disconnect host %s: %v", host, err)
			}
		}
	}
}

// markRegistered records that the given (host, path) has been registered on
// the remote. Returns true if this is the first registration for that pair,
// false if it was already registered. Task 5 uses this to register a project
// exactly once per process. It never creates a connection entry — the path set
// is registry-level, so calling it before workspaceFor cannot leave a
// placeholder that blocks a later connect.
func (reg *remoteHostRegistry) markRegistered(host, path string) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	paths := reg.registeredPaths[host]
	if paths == nil {
		paths = make(map[string]struct{})
		reg.registeredPaths[host] = paths
	}
	if _, exists := paths[path]; exists {
		return false
	}
	paths[path] = struct{}{}
	return true
}

// proxyFor returns the cached reverse proxy for host. It must be called on a
// connected entry — callers obtain the proxy under e.mu (see
// HandleRemoteProxy).
func (reg *remoteHostRegistry) proxyFor(host string) (*httputil.ReverseProxy, bool) {
	reg.mu.Lock()
	e, ok := reg.byHost[host]
	reg.mu.Unlock()
	if !ok {
		return nil, false
	}
	e.mu.Lock()
	p := e.proxy
	e.mu.Unlock()
	return p, p != nil
}
