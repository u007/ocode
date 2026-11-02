package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/u007/ocode/internal/filelock"
)

// maxPersistedTabPorts caps tab-ports.json. Past the cap the entry used least
// recently is evicted, so the file cannot grow without bound.
const maxPersistedTabPorts = 512

// tabPortEntry is one (stateKey, scheme, host) binding and the loopback port its
// root-mounted tab origin listens on.
type tabPortEntry struct {
	StateKey    string `json:"state_key"`
	Scheme      string `json:"scheme"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	UsedAtMs    int64  `json:"used_at_ms"`
	ProjectHost string `json:"project_host,omitempty"`
}

// tabPortStore persists tab origin ports in <GlobalDataDir>/browse/tab-ports.json.
// Every mutation holds the cross-process file lock, re-reads the file, changes
// only its own key and writes the whole map back atomically, so two servers
// sharing the data dir never drop each other's bindings. The file is the
// truth; the store keeps no cache.
type tabPortStore struct {
	path     string
	lockPath string
}

func newTabPortStoreAt(path string) *tabPortStore {
	return &tabPortStore{path: path, lockPath: path + ".lock"}
}

// tabPortKey is the map key for one binding. strconv.Quote makes the key
// unambiguous whatever the strings contain. A local binding (projectHost "") keeps
// the three-field key every saved entry already has; a remote binding appends its
// project host, so a local and a remote tab on the same upstream never share a port.
func tabPortKey(projectHost, stateKey, scheme, host string) string {
	k := strconv.Quote(stateKey) + " " + strconv.Quote(scheme) + " " + strconv.Quote(host)
	if projectHost != "" {
		k += " " + strconv.Quote(projectHost)
	}
	return k
}

func (e tabPortEntry) key() string {
	return tabPortKey(e.ProjectHost, e.StateKey, e.Scheme, e.Host)
}

// load reads the whole file. A missing file is an empty map; a file that does
// not parse is an error, never a silent reset.
func (st *tabPortStore) load() (map[string]tabPortEntry, error) {
	data, err := os.ReadFile(st.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]tabPortEntry{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", st.path, err)
	}
	// unsorted per spec: internal lookup map
	m := map[string]tabPortEntry{}
	if len(data) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", st.path, err)
	}
	if m == nil {
		m = map[string]tabPortEntry{}
	}
	return m, nil
}

// Get returns the saved port for a binding, 0 when none is saved.
func (st *tabPortStore) Get(projectHost, stateKey, scheme, host string) (int, error) {
	m, err := st.load()
	if err != nil {
		return 0, err
	}
	return m[tabPortKey(projectHost, stateKey, scheme, host)].Port, nil
}

// Put records one binding, replacing only its own key, then evicts the least
// recently used bindings past maxPersistedTabPorts.
func (st *tabPortStore) Put(e tabPortEntry) error {
	return st.withLock(func() error {
		m, err := st.load()
		if err != nil {
			return err
		}
		m[e.key()] = e
		evictLeastRecentTabPorts(m, maxPersistedTabPorts)
		return st.save(m)
	})
}

// DeleteState drops every binding saved for stateKey. Other keys are untouched.
func (st *tabPortStore) DeleteState(stateKey string) error {
	return st.withLock(func() error {
		m, err := st.load()
		if err != nil {
			return err
		}
		removed := false
		for k, e := range m {
			if e.StateKey == stateKey {
				delete(m, k)
				removed = true
			}
		}
		if !removed {
			return nil
		}
		return st.save(m)
	})
}

// evictLeastRecentTabPorts removes the entries with the oldest UsedAtMs until
// at most keep remain. Ties break on the key so eviction is deterministic.
func evictLeastRecentTabPorts(m map[string]tabPortEntry, keep int) {
	if len(m) <= keep {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := m[keys[i]], m[keys[j]]
		if a.UsedAtMs != b.UsedAtMs {
			return a.UsedAtMs < b.UsedAtMs
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys[:len(m)-keep] {
		delete(m, k)
	}
}

// save writes the map atomically (temp file + rename), like termtabs.
func (st *tabPortStore) save(m map[string]tabPortEntry) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tab ports: %w", err)
	}
	dir := filepath.Dir(st.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tab-ports-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp tab ports file: %w", err)
	}
	tmpName := tmp.Name()
	// Remove the temp file on every failure path; after a successful rename
	// this is a harmless no-op.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp tab ports file: %w", err)
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp tab ports file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp tab ports file: %w", err)
	}
	if err := os.Rename(tmpName, st.path); err != nil {
		return fmt.Errorf("rename temp tab ports file: %w", err)
	}
	return nil
}

// withLock runs fn under the cross-process lock. The directory is created first
// because a fresh install has no browse dir yet.
func (st *tabPortStore) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(st.lockPath), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(st.lockPath), err)
	}
	return filelock.WithFileLock(st.lockPath, fn)
}

// tabOrigins hands out the root-mounted per-tab origins of local clients, and of
// saved remote projects through forwarding listeners. It owns the in-memory map
// of live bindings; the browse server owns the sockets.
type tabOrigins struct {
	srv      *Server
	bindHost string
	store    *tabPortStore

	// mu guards live only. It is never held across anything slower than the
	// ListenTab call itself.
	mu sync.Mutex
	// live maps each open binding to its entry. A second request for a live
	// binding reuses it: ListenTab refuses a duplicate.
	// unsorted per spec: internal lookup map
	live map[string]tabPortEntry
}

// browseProxyBindHost is the host the browse listeners bind: the hostname of the
// SPA origin, so a page on that host is same-site with them. 127.0.0.1 when the
// origin does not parse.
func browseProxyBindHost(spaOrigin string) string {
	if u, err := url.Parse(spaOrigin); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "127.0.0.1"
}

// enableTabOrigins wires the tab-origin manager. It needs EnableBrowse first,
// because the grant and the listeners come from s.browse.
func (s *Server) enableTabOrigins(bindHost string, store *tabPortStore) {
	s.tabOrigins = &tabOrigins{
		srv:      s,
		bindHost: bindHost,
		store:    store,
		live:     map[string]tabPortEntry{},
	}
}

// bind returns the live binding for (projectHost, stateKey, scheme, host), opening
// its listener first if needed. projectHost is "" for a local upstream. The saved
// port is tried first. Only EADDRINUSE on the saved port falls back to a new port,
// and that is logged, because the tab's storage is keyed by origin and will reset.
func (m *tabOrigins) bind(projectHost, stateKey, scheme, host string) (tabPortEntry, error) {
	saved, err := m.store.Get(projectHost, stateKey, scheme, host)
	if err != nil {
		return tabPortEntry{}, fmt.Errorf("read saved tab port: %w", err)
	}
	key := tabPortKey(projectHost, stateKey, scheme, host)
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.live[key]; ok {
		return e, nil
	}
	ln, err := m.listen(projectHost, stateKey, scheme, host, saved)
	if saved != 0 && errors.Is(err, syscall.EADDRINUSE) {
		log.Printf("browse tab origin: saved port %d for %s://%s in state %q is in use, binding a new port (this tab's storage resets): %v", saved, scheme, host, stateKey, err)
		ln, err = m.listen(projectHost, stateKey, scheme, host, 0)
	}
	if err != nil {
		return tabPortEntry{}, err
	}
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return tabPortEntry{}, fmt.Errorf("parse tab listener address %s: %w", ln.Addr(), err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return tabPortEntry{}, fmt.Errorf("parse tab listener port %q: %w", portStr, err)
	}
	e := tabPortEntry{ProjectHost: projectHost, StateKey: stateKey, Scheme: scheme, Host: host, Port: port}
	m.live[key] = e
	return e, nil
}

// listen opens one tab listener on bindHost:port. With localTLS set the
// listener is wrapped in the sniff listener, which also serves plain HTTP. A
// remote binding (projectHost set) is a forwarding listener to that host.
func (m *tabOrigins) listen(projectHost, stateKey, scheme, host string, port int) (net.Listener, error) {
	addr := net.JoinHostPort(m.bindHost, strconv.Itoa(port))
	var wrap func(net.Listener) net.Listener
	if cert := m.srv.localTLS; cert != nil {
		wrap = func(l net.Listener) net.Listener { return NewSniffTLSListener(l, cert) }
	}
	var ln net.Listener
	var err error
	if projectHost == "" {
		ln, _, err = m.srv.browse.ListenTab(stateKey, scheme, host, addr, wrap)
	} else {
		ln, _, err = m.srv.browse.ListenForwardTab(projectHost, stateKey, scheme, host, addr, wrap, m.srv.remoteTabTunnel(projectHost))
	}
	if err != nil {
		return nil, err
	}
	return ln, nil
}

// persist records a bound entry, so the next server process reuses its port.
func (m *tabOrigins) persist(e tabPortEntry) error {
	e.UsedAtMs = time.Now().UnixMilli()
	return m.store.Put(e)
}

// dropLiveTabOrigins forgets the in-memory bindings at shutdown. browse.Close has
// already closed their listeners, and the persisted ports are kept, so the next
// start reuses them.
func (s *Server) dropLiveTabOrigins() {
	if s.tabOrigins == nil {
		return
	}
	s.tabOrigins.mu.Lock()
	defer s.tabOrigins.mu.Unlock()
	s.tabOrigins.live = map[string]tabPortEntry{}
}

// revokeTabOrigins runs after browse.Revoke closed a panel's listeners. It forgets
// the panel's bindings, then tells each remote host the panel reached to revoke its
// own browse session. The host call is best effort and logged, because the panel is
// already closed here.
func (s *Server) revokeTabOrigins(r *http.Request, stateKey string) {
	if s.tabOrigins == nil {
		return
	}
	for _, host := range s.tabOrigins.remoteTabHosts(stateKey) {
		s.revokeRemoteBrowse(r, host, stateKey)
	}
	s.forgetTabOrigins(stateKey)
}

// forgetTabOrigins drops a panel's live bindings and its persisted ports, because a
// closed panel's storage is discarded with it. The browse revoke has already closed
// the sockets, so this only clears bookkeeping.
func (s *Server) forgetTabOrigins(stateKey string) {
	if s.tabOrigins == nil {
		return
	}
	m := s.tabOrigins
	m.mu.Lock()
	for k, e := range m.live {
		if e.StateKey == stateKey {
			delete(m.live, k)
		}
	}
	m.mu.Unlock()
	if err := m.store.DeleteState(stateKey); err != nil {
		log.Printf("browse revoke: drop persisted tab ports for %s: %v", stateKey, err)
	}
}

// remoteTabHosts lists, sorted, the remote project hosts a panel's tab origins were
// opened for, from the live bindings and the persisted ports both.
func (m *tabOrigins) remoteTabHosts(stateKey string) []string {
	seen := map[string]bool{}
	m.mu.Lock()
	for _, e := range m.live {
		if e.StateKey == stateKey && e.ProjectHost != "" {
			seen[e.ProjectHost] = true
		}
	}
	m.mu.Unlock()
	saved, err := m.store.load()
	if err != nil {
		log.Printf("browse revoke: list persisted tab ports for %s: %v", stateKey, err)
	}
	for _, e := range saved {
		if e.StateKey == stateKey && e.ProjectHost != "" {
			seen[e.ProjectHost] = true
		}
	}
	hosts := make([]string, 0, len(seen))
	for h := range seen {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

// mintBrowseGrant issues the one-time grant for stateKey, bound to the SPA page
// that asked for it. The grant endpoint and the tab-origin endpoint share it.
func (s *Server) mintBrowseGrant(r *http.Request, stateKey string) string {
	return s.browse.MintGrant(stateKey, requestOrigin(r))
}

// tabBindingHost is the host of an upstream URL with its scheme's default port
// removed, so https://h:443/ and https://h/ share one binding.
func tabBindingHost(u *url.URL) string {
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		return strings.TrimSuffix(u.Host, ":"+port)
	}
	return u.Host
}

// handleBrowseTabOrigin serves POST /api/browse/tab-origin. The body names a
// panel (state_key), the upstream URL the tab navigates to, and an optional
// project host (body "host", or ?host=). The reply is the tab's root-mounted
// origin, a grant for the panel, and the URL to load in the iframe.
//
// A local project's tab is served by this machine's browse origin. A saved remote
// project's tab is a forwarding listener on this machine: its requests are answered
// by the host's own browse origin, which holds the grant and the session (tab_remote.go).
//
// Only a client on this machine can use a tab origin, and a remote-mode server never
// opens one. Those callers get 409: the SPA chooses prefix mode.
func (s *Server) handleBrowseTabOrigin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StateKey string `json:"state_key"`
		URL      string `json:"url"`
		Host     string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "tab origin: bad request body: "+err.Error())
		return
	}
	if req.StateKey == "" {
		writeError(w, http.StatusBadRequest, "tab origin: state_key is required")
		return
	}
	if s.tabOrigins == nil || s.browse == nil {
		writeError(w, http.StatusServiceUnavailable, "tab origin: browse origin not available")
		return
	}
	projectHost := req.Host
	if q := hostParam(r); q != "" {
		if projectHost != "" && projectHost != q {
			writeError(w, http.StatusBadRequest, "tab origin: host in the body and ?host= differ")
			return
		}
		projectHost = q
	}
	if s.remoteMode {
		writeError(w, http.StatusConflict, "tab origin unavailable: this server runs in remote mode")
		return
	}
	if !browseClientIsLocal(r) {
		writeError(w, http.StatusConflict, "tab origin unavailable: the client is not on this machine")
		return
	}
	upstream, err := url.Parse(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "tab origin: url is not valid: "+err.Error())
		return
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" {
		writeError(w, http.StatusBadRequest, "tab origin: url scheme must be http or https")
		return
	}
	if upstream.Hostname() == "" || upstream.User != nil || strings.HasSuffix(upstream.Host, ":") {
		writeError(w, http.StatusBadRequest, "tab origin: url must name a host without credentials")
		return
	}
	if projectHost != "" {
		if _, ok := s.handler.firstSavedProjectForHost(projectHost); !ok {
			writeError(w, http.StatusForbidden, "tab origin: host is not a saved project")
			return
		}
	}
	host := tabBindingHost(upstream)
	entry, err := s.tabOrigins.bind(projectHost, req.StateKey, upstream.Scheme, host)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "tab origin unavailable: "+err.Error())
		return
	}
	if err := s.tabOrigins.persist(entry); err != nil {
		writeError(w, http.StatusInternalServerError, "tab origin: persist port: "+err.Error())
		return
	}
	// The origin is built per request, not taken from the listener address: the
	// listener is an IP literal, and a page on localhost must frame a same-site
	// origin or the per-tab cookie is third-party. requestHostName is loopback
	// here, because browseClientIsLocal held.
	origin := s.browseProxyScheme(r) + "://" + net.JoinHostPort(requestHostName(r), strconv.Itoa(entry.Port))
	grant, ok := s.tabGrant(w, r, projectHost, req.StateKey)
	if !ok {
		return
	}
	path := upstream.EscapedPath()
	if path == "" {
		path = "/"
	}
	rawQuery := "__grant=" + url.QueryEscape(grant)
	if upstream.RawQuery != "" {
		rawQuery = upstream.RawQuery + "&" + rawQuery
	}
	tabURL := origin + path + "?" + rawQuery
	if upstream.Fragment != "" {
		tabURL += "#" + upstream.EscapedFragment()
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"origin": origin,
		"grant":  grant,
		"url":    tabURL,
	})
}

// tabGrant mints the one-time grant for stateKey on the browse origin that serves the
// tab: this machine's for a local project, the project host's for a remote one. It
// writes the error and returns false when the host refuses.
func (s *Server) tabGrant(w http.ResponseWriter, r *http.Request, projectHost, stateKey string) (string, bool) {
	if projectHost == "" {
		return s.mintBrowseGrant(r, stateKey), true
	}
	return s.mintRemoteGrant(w, r, projectHost, stateKey)
}
