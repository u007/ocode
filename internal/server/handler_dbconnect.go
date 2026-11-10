package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/dbconnect"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/remote"
)

// Postgres DB connector HTTP surface (/api/dbconnect/*).
//
// Saved connections live in ocodeconfig.json under db.connections. A literal URL
// is stored as an encrypted envelope; an environment template ("${DATABASE_URL}")
// is stored in the clear because it names variables, not a credential. Unlock
// opens the envelopes with the master password and/or resolves the templates
// against one project's .env files, then keeps the plaintext URLs in memory under
// the client-supplied surface id, the same shape as the password vault's surface
// grants. Plaintext URLs are never returned, logged, or written to disk.
//
// Only unlock takes a project param (project_path), because only template
// resolution reads files. The other endpoints work on the surface's grant.

const (
	dbConnectOpenTimeout  = 15 * time.Second
	dbConnectQueryTimeout = 30 * time.Second
	// dbSurfaceIdleTTL bounds how long a surface's plaintext grant and pooled
	// handle survive without a request. The surface is keyed per panel mount, so
	// a panel closed without its unmount cleanup (browser reload, webview crash)
	// would otherwise leak a credential and a live connection for the whole
	// process lifetime.
	dbSurfaceIdleTTL = 30 * time.Minute
	// dbSurfaceMax caps remembered surfaces outright, in case a client creates
	// mounts faster than the TTL retires them.
	dbSurfaceMax = 64
	// dbSurfaceMaxLen rejects an absurd client-supplied surface key.
	dbSurfaceMaxLen = 256
)

// dbGrant maps connection name to what unlock granted for it, for one surface.
type dbGrant map[string]dbGrantEntry

// dbGrantEntry is one unlocked connection. The URL is the one the database
// really listens on; an SSH tunnel, when configured, is started when the
// connection is first opened (dbGrantDB), never during unlock.
//
// Every field is a value, so entries compare with == (sameDBGrant). The tunnel
// password is plaintext, like the URL: it lives only in this grant, is never
// logged, and is dropped with the grant on lock.
type dbGrantEntry struct {
	url            string
	tunnel         config.DBTunnel
	hasTunnel      bool
	tunnelPassword string
	// poolPort is the local tunnel port the pooled handle was opened on. A
	// restarted tunnel can bind a new port, and a pool keeps its old one.
	poolPort int
}

// sameDBGrant reports whether two entries describe the same connection, which
// is what decides whether a pool may be reused. poolPort is pool bookkeeping,
// not part of the grant.
func sameDBGrant(a, b dbGrantEntry) bool {
	a.poolPort = 0
	b.poolPort = 0
	return a == b
}

// dbGrantURL returns the unrewritten URL for an unlocked connection, if any.
// It also marks the surface as in use, which retires surfaces left behind by a
// client that never called lock (see dbTouchLocked).
func (h *Handler) dbGrantURL(surface, name string) (string, bool) {
	h.dbMu.Lock()
	stale := h.dbTouchLocked(surface, time.Now())
	e, ok := h.dbGrants[surface][name]
	h.dbMu.Unlock()
	dbClosePools(stale)
	return e.url, ok
}

// dbSetGrant replaces the grant for one surface. Pools opened under the old
// grant are closed, since they may point at a different URL.
func (h *Handler) dbSetGrant(surface string, grant dbGrant) {
	h.dbMu.Lock()
	if h.dbGrants == nil {
		h.dbGrants = make(map[string]dbGrant)
	}
	stale := h.dbTouchLocked(surface, time.Now())
	h.dbGrants[surface] = grant
	stale = append(stale, h.dbTakePoolsLocked(surface)...)
	h.dbMu.Unlock()
	dbClosePools(stale)
}

// dbAddToGrant merges entries into a surface's grant without touching the
// connections it does not name. A password-less unlock grants only the
// environment connections, so replacing the whole grant would silently revoke
// sealed connections the user had already unlocked (and close their pools).
// A pool whose URL changed is closed and reopened lazily.
func (h *Handler) dbAddToGrant(surface string, entries dbGrant) {
	h.dbMu.Lock()
	if h.dbGrants == nil {
		h.dbGrants = make(map[string]dbGrant)
	}
	grant := h.dbGrants[surface]
	if grant == nil {
		grant = make(dbGrant)
		h.dbGrants[surface] = grant
	}
	stale := h.dbTouchLocked(surface, time.Now())
	for name, e := range entries {
		if old, ok := grant[name]; ok && !sameDBGrant(old, e) {
			if db := h.dbPools[surface][name]; db != nil {
				stale = append(stale, db)
				delete(h.dbPools[surface], name)
			}
		}
		grant[name] = e
	}
	h.dbMu.Unlock()
	dbClosePools(stale)
}

// dbRemoveFromGrant forgets the named connections on one surface and closes
// their pools. A template that stopped resolving (its `.env` variable was
// removed or its file deleted) must not keep the grant from its last successful
// resolve, or the connection list would report it unlocked while the unlock
// reported it failed — and queries would keep running against the old database.
func (h *Handler) dbRemoveFromGrant(surface string, names []string) {
	if len(names) == 0 {
		return
	}
	h.dbMu.Lock()
	var stale []*sql.DB
	for _, name := range names {
		delete(h.dbGrants[surface], name)
		if db := h.dbPools[surface][name]; db != nil {
			stale = append(stale, db)
			delete(h.dbPools[surface], name)
		}
	}
	h.dbMu.Unlock()
	dbClosePools(stale)
}

// dbTouchLocked records that a surface is in use and retires surfaces that have
// been idle past dbSurfaceIdleTTL (or the oldest ones beyond dbSurfaceMax).
// The caller holds dbMu and must pass the returned pools to dbClosePools after
// releasing it, because Close waits for queries still in flight.
func (h *Handler) dbTouchLocked(surface string, now time.Time) []*sql.DB {
	if h.dbLastUsed == nil {
		h.dbLastUsed = make(map[string]time.Time)
	}
	h.dbLastUsed[surface] = now
	var stale []*sql.DB
	for name, at := range h.dbLastUsed {
		if name == surface || now.Sub(at) < dbSurfaceIdleTTL {
			continue
		}
		stale = append(stale, h.dbTakePoolsLocked(name)...)
		delete(h.dbGrants, name)
		delete(h.dbLastUsed, name)
	}
	for len(h.dbLastUsed) > dbSurfaceMax {
		oldest := ""
		var oldestAt time.Time
		for name, at := range h.dbLastUsed {
			if name == surface {
				continue
			}
			if oldest == "" || at.Before(oldestAt) {
				oldest, oldestAt = name, at
			}
		}
		if oldest == "" {
			break
		}
		stale = append(stale, h.dbTakePoolsLocked(oldest)...)
		delete(h.dbGrants, oldest)
		delete(h.dbLastUsed, oldest)
	}
	return stale
}

// dbUnlockProjectRoot resolves the project whose .env files a template may read.
//
// An explicit project_path is the authority, and it must name an allowed project
// root — the same check the file reader uses, so a remote project's path
// (which belongs to another machine) is refused instead of reading a local .env
// that happens to share its name. The "~" spelling IS accepted here: on a remote
// (SSH/WSL) project the unlock is proxied to the host's own server, which is the
// machine that owns that home directory (dbProjectRootFor).
//
// The legacy session_id fallback trusts the session registry's recorded root,
// as docs/concepts/db-connector.md specifies. That is not the same thing as
// trusting client input: Resolve only probes registered roots, so the value
// comes from the server's own session state. It is deliberately NOT re-checked
// against fileContentRootFor, because the registry is the documented boundary
// for this compatibility path and the web client no longer sends the field.
//
// There is no fallback to the server's working directory or process
// environment: that would connect a remote project to a local database.
func (h *Handler) dbUnlockProjectRoot(projectPath, sessionID string) (string, error) {
	if projectPath != "" {
		root, ok := h.dbProjectRootFor(projectPath)
		if !ok {
			return "", errors.New("project_path is not an allowed project root")
		}
		return root, nil
	}
	if sessionID != "" {
		return h.sessionProjectRoot(sessionID), nil
	}
	return "", nil
}

// dbProjectRootFor validates a client-supplied project path against this
// server's allowed roots, accepting the host's "~" spelling.
//
// A remote (SSH/WSL) project is saved by the desktop under the verbatim "~"
// form, and its unlock is proxied to the host's server (/api/remote/{host}/...),
// which is the machine that owns that home directory. The host's own registry
// holds the HOME-EXPANDED path (projects.Add expands there), so an exact-match
// check rejects every tilde-keyed remote project before a single .env is read.
//
// The fallback is the same trust decision the git, upload and session-list
// endpoints already make (resolveRegisteredProjectRoot): only a saved root can
// match, and expansion narrows rather than widens the accepted set. A "~user"
// form and every non-tilde path pass through projects.ExpandHome unchanged, so
// they can never match a root this server was not already told to serve.
func (h *Handler) dbProjectRootFor(projectPath string) (string, bool) {
	if root, ok := h.fileContentRootFor(projectPath); ok {
		return root, true
	}
	expanded, err := projects.ExpandHome(projectPath)
	if err != nil || expanded == projectPath {
		return "", false
	}
	return h.fileContentRootFor(expanded)
}

// dbDropSurface forgets every grant held by one surface and closes its pools.
func (h *Handler) dbDropSurface(surface string) {
	h.dbMu.Lock()
	delete(h.dbGrants, surface)
	stale := h.dbTakePoolsLocked(surface)
	h.dbMu.Unlock()
	dbClosePools(stale)
}

// dbDropConnection forgets one connection on every surface, after it is removed.
func (h *Handler) dbDropConnection(name string) {
	h.dbMu.Lock()
	for _, grant := range h.dbGrants {
		delete(grant, name)
	}
	var stale []*sql.DB
	for _, pools := range h.dbPools {
		if db, ok := pools[name]; ok {
			stale = append(stale, db)
			delete(pools, name)
		}
	}
	h.dbMu.Unlock()
	dbClosePools(stale)
}

// dbTakePoolsLocked removes and returns every pool one surface holds. The caller
// holds dbMu and passes the result to dbClosePools after releasing it.
func (h *Handler) dbTakePoolsLocked(surface string) []*sql.DB {
	var out []*sql.DB
	for _, db := range h.dbPools[surface] {
		out = append(out, db)
	}
	delete(h.dbPools, surface)
	return out
}

// dbClosePools closes pools already removed from dbPools. It runs after dbMu is
// released because Close waits for queries still in flight.
func dbClosePools(pools []*sql.DB) {
	for _, db := range pools {
		if err := db.Close(); err != nil {
			log.Printf("dbconnect: close pooled connection: %v", err)
		}
	}
}

// dbGrantDB returns the pooled handle for an unlocked connection, opening it on
// first use. granted is false when the surface holds no grant for name. The open
// pings the server, so it runs outside dbMu. A grant locked or replaced in the
// meantime discards the new handle.
func (h *Handler) dbGrantDB(ctx context.Context, surface, name string) (db *sql.DB, granted bool, err error) {
	h.dbMu.Lock()
	e, ok := h.dbGrants[surface][name]
	if !ok {
		h.dbMu.Unlock()
		return nil, false, nil
	}
	// A tunnelled connection always goes through the tunnel manager, even with
	// a pool cached: that is what restarts a dead tunnel and yields the port the
	// pool must match. A direct connection is served from the pool as before.
	if !e.hasTunnel {
		if cached := h.dbPools[surface][name]; cached != nil {
			h.dbMu.Unlock()
			return cached, true, nil
		}
	}
	h.dbMu.Unlock()

	dsn := e.url
	port := 0
	if e.hasTunnel {
		// ssh can take seconds, so the tunnel is started outside dbMu.
		port, err = dbTunnelPort(ctx, name, e)
		if err != nil {
			return nil, true, err
		}
		dsn, err = rewriteURLForTunnel(e.url, port)
		if err != nil {
			return nil, true, err
		}
		h.dbMu.Lock()
		cur, ok := h.dbGrants[surface][name]
		if !ok || !sameDBGrant(cur, e) {
			h.dbMu.Unlock()
			return nil, false, nil
		}
		if cached := h.dbPools[surface][name]; cached != nil && cur.poolPort == port {
			h.dbMu.Unlock()
			return cached, true, nil
		}
		h.dbMu.Unlock()
	}

	opened, err := dbconnect.Open(ctx, dsn)
	if err != nil {
		return nil, true, err
	}

	h.dbMu.Lock()
	cur, ok := h.dbGrants[surface][name]
	if !ok || !sameDBGrant(cur, e) {
		// Locked or replaced while we connected: discard the new handle.
		h.dbMu.Unlock()
		dbClosePools([]*sql.DB{opened})
		return nil, false, nil
	}
	var stale []*sql.DB
	if cached := h.dbPools[surface][name]; cached != nil {
		if !e.hasTunnel || cur.poolPort == port {
			// A concurrent request opened this connection first; share its handle.
			h.dbMu.Unlock()
			dbClosePools([]*sql.DB{opened})
			return cached, true, nil
		}
		// The tunnel restarted on another port, so the cached handle dials a
		// dead one. Replace it.
		stale = append(stale, cached)
		delete(h.dbPools[surface], name)
	}
	if h.dbPools == nil {
		h.dbPools = make(map[string]map[string]*sql.DB)
	}
	if h.dbPools[surface] == nil {
		h.dbPools[surface] = make(map[string]*sql.DB)
	}
	h.dbPools[surface][name] = opened
	cur.poolPort = port
	h.dbGrants[surface][name] = cur
	h.dbMu.Unlock()
	dbClosePools(stale)
	return opened, true, nil
}

// dbEnsureTunnel starts or reuses the SSH tunnel for one saved connection. It is
// a variable so tests can stand in for ssh.
var dbEnsureTunnel = func(ctx context.Context, name string, t *config.DBTunnel, password string) (int, error) {
	return remote.DefaultDBTunnelManager().Ensure(ctx, name, t, password)
}

// dbTunnelOpenTries and dbTunnelOpenGap bound how long one open waits on a
// tunnel that is still connecting. ssh normally answers in well under a second,
// and a request must not stay parked on a slow host.
const (
	dbTunnelOpenTries = 12
	dbTunnelOpenGap   = 250 * time.Millisecond
)

// dbTunnelPort returns the local port of the connection's tunnel, starting it
// if needed. The tunnel is shared by every surface, so it runs on a context
// that one caller hanging up does not cancel. It would otherwise kill the ssh
// process that others are using.
func dbTunnelPort(ctx context.Context, name string, e dbGrantEntry) (int, error) {
	tctx := context.WithoutCancel(ctx)
	t := e.tunnel
	for try := 1; ; try++ {
		port, err := dbEnsureTunnel(tctx, name, &t, e.tunnelPassword)
		if err == nil {
			return port, nil
		}
		if !errors.Is(err, remote.ErrTunnelInProgress) || try >= dbTunnelOpenTries {
			return 0, err
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(dbTunnelOpenGap):
		}
	}
}

// HandleDBConnectList lists saved connections, sorted by name. unlocked reflects
// the caller's surface. Not paginated on purpose: a user saves a handful of
// connections, and the whole list is needed to pick one.
func (h *Handler) HandleDBConnectList(w http.ResponseWriter, r *http.Request) {
	surface := r.URL.Query().Get("surface")
	if len(surface) > dbSurfaceMaxLen {
		writeError(w, http.StatusBadRequest, "surface is too long")
		return
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		log.Printf("dbconnect: list: load config: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load saved connections")
		return
	}
	type entry struct {
		Name     string `json:"name"`
		Driver   string `json:"driver"`
		Unlocked bool   `json:"unlocked"`
		// FromEnv reports that the connection's URL comes from an
		// environment template rather than a stored secret. The template
		// itself is NOT returned: it names variables, but the resolved URL
		// is a credential and stays on the server.
		FromEnv bool `json:"from_env"`
	}
	conns := cfg.DB.Connections
	if pp := r.URL.Query().Get("project_path"); pp != "" {
		if root, ok := h.dbProjectRootFor(pp); ok {
			if pc, err := config.LoadProjectDBConnections(root); err == nil {
				conns = config.MergeDBConnections(conns, pc)
			}
		}
	}
	sort.Slice(conns, func(i, j int) bool { return conns[i].Name < conns[j].Name })
	out := make([]entry, 0, len(conns))
	for _, c := range conns {
		_, unlocked := h.dbGrantURL(surface, c.Name)
		out = append(out, entry{
			Name:     c.Name,
			Driver:   c.Driver,
			Unlocked: unlocked,
			FromEnv:  c.EnvTemplate != "",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

// HandleDBConnectAdd saves a connection. A literal URL is sealed under the
// master password; the URL and password are never logged or echoed back.
//
// A URL that contains an environment reference ("${DATABASE_URL}") is NOT a
// secret: it names a variable, not a credential. It is saved as a template,
// in the clear, and resolved per project when the connection is unlocked. That
// is the "read the value from .env" flow, so it needs NO master password —
// there is nothing to encrypt.
func (h *Handler) HandleDBConnectAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string           `json:"name"`
		URL         string           `json:"url"`
		Password    string           `json:"password"`
		ProjectPath string           `json:"project_path"`
		Driver      string           `json:"driver"`
		Tunnel      *config.DBTunnel `json:"tunnel"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		log.Printf("dbconnect: add: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Name == "" || req.URL == "" {
		writeError(w, http.StatusBadRequest, "name and url are required")
		return
	}
	if req.Driver != "" && req.Driver != config.DBDriverPostgres && req.Driver != config.DBDriverMySQL {
		writeError(w, http.StatusBadRequest, "unsupported db driver "+req.Driver)
		return
	}
	// A template names variables instead of carrying a credential, so there is
	// nothing to encrypt and no master password is involved. A literal URL is a
	// secret and still requires the password that seals it.
	isTemplate := dbconnect.IsTemplate(req.URL)
	if isTemplate && dbconnect.TemplateHasLiteralSecret(req.URL) {
		// A template is stored in the clear. One that also carries a literal
		// password would write that password to ocodeconfig.json unencrypted.
		writeError(w, http.StatusBadRequest,
			"an environment-backed connection must not contain a literal password; use ${VAR} for the password instead")
		return
	}
	if !isTemplate && req.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required to encrypt a connection URL")
		return
	}
	// Every saved envelope must open with the same master password, or unlock
	// would reject all of them. The check runs outside the config lock; the save
	// then fails if the saved set changed in between.
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		log.Printf("dbconnect: add %q: load config: %v", req.Name, err)
		writeError(w, http.StatusInternalServerError, "could not load saved connections")
		return
	}
	// A tunnel password stored in the global config is sealed under the master
	// password. It is verified against the saved set like a sealed URL, so a new
	// seal can never drift from the rest. A project connection never stores one.
	storesTunnelPassword := req.Tunnel != nil && req.Tunnel.AuthMethod == "password" &&
		req.Tunnel.Password != "" && req.ProjectPath == ""
	if storesTunnelPassword && req.Password == "" {
		writeError(w, http.StatusBadRequest,
			"the master password is required to store a tunnel password; leave the tunnel password empty to enter it at unlock")
		return
	}
	verified := cfg.DB.Connections
	if !isTemplate || storesTunnelPassword {
		if err := dbCheckMasterPassword(verified, req.Password); err != nil {
			log.Printf("dbconnect: add %q: %v", req.Name, err)
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
	}
	// The explicit driver (validated above) wins. The URL scheme is only a guess
	// for a caller that did not name one: an environment template such as
	// "${DATABASE_URL}" does not parse, and must not silently become postgres.
	driver := req.Driver
	if driver == "" {
		driver = config.DBDriverPostgres
		if u, perr := dbconnect.ParseURL(req.URL); perr == nil && u.Scheme == "mysql" {
			driver = config.DBDriverMySQL
		}
	}
	// Validate + normalize the optional tunnel descriptor.
	if req.Tunnel != nil {
		if err := remote.ValidateTunnelDescriptor(req.Tunnel); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Tunnel.AuthMethod == "" {
			req.Tunnel.AuthMethod = "key"
		}
	}
	conn := config.DBConnection{
		Name:   req.Name,
		Driver: driver,
		Tunnel: req.Tunnel,
	}
	// Seal the tunnel password (if password auth) under the master password.
	if storesTunnelPassword {
		sealed, err := dbconnect.SealPassword(req.Tunnel.Password, req.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		conn.Tunnel.Password = sealed
	}
	if isTemplate {
		// A template is not a secret, so it is stored in the clear. It is
		// resolved — and validated — when the connection is unlocked.
		conn.EnvTemplate = req.URL
	} else {
		sealed, err := dbconnect.SealURL(req.URL, req.Password)
		if err != nil {
			log.Printf("dbconnect: add %q: seal: %v", req.Name, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		conn.EncryptedURL = sealed
	}
	// Per-project config path: store the connection in .ocode/db.json instead of
	// the global ocodeconfig.json. Only templates (no secrets) are allowed there.
	if req.ProjectPath != "" {
		root, ok := h.dbProjectRootFor(req.ProjectPath)
		if !ok {
			writeError(w, http.StatusBadRequest, "project_path is not an allowed project root")
			return
		}
		if !isTemplate {
			writeError(w, http.StatusBadRequest, "only environment-backed connections can be saved per project")
			return
		}
		existing, err := config.LoadProjectDBConnections(root)
		if err != nil {
			log.Printf("dbconnect: add %q: load project config: %v", req.Name, err)
			writeError(w, http.StatusInternalServerError, "could not load project connections")
			return
		}
		for _, c := range existing {
			if c.Name == req.Name {
				writeError(w, http.StatusConflict, fmt.Sprintf("db connection %q already exists", req.Name))
				return
			}
		}
		if conn.Tunnel != nil {
			conn.Tunnel.Password = ""
		}
		if err := config.SaveProjectDBConnections(root, append(existing, conn)); err != nil {
			log.Printf("dbconnect: add %q: save project config: %v", req.Name, err)
			writeError(w, http.StatusInternalServerError, "could not save project connection")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name})
		return
	}
	if err := config.AddDBConnection(conn, verified); err != nil {
		log.Printf("dbconnect: add %q: save: %v", req.Name, err)
		if errors.Is(err, config.ErrDBConnectionsChanged) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name})
}

// dbCheckMasterPassword verifies that the master password opens every saved
// envelope, so a later unlock cannot be half-applied.
//
// A template connection has no envelope, so it contributes nothing to this
// check. Templates are resolved from the project's .env files and never require
// this password; it gates the sealed connections only.
func dbCheckMasterPassword(conns []config.DBConnection, password string) error {
	for _, c := range conns {
		if c.EncryptedURL != "" {
			if _, err := dbconnect.OpenURL(c.EncryptedURL, password); err != nil {
				return fmt.Errorf("master password does not match saved connection %s", c.Name)
			}
		}
		// A sealed tunnel password is opened at unlock, so it must match too.
		if c.Tunnel != nil && c.Tunnel.Password != "" {
			if _, err := dbconnect.OpenPassword(c.Tunnel.Password, password); err != nil {
				return fmt.Errorf("master password does not match the saved tunnel password of %s", c.Name)
			}
		}
	}
	return nil
}

// HandleDBConnectRemove deletes a saved connection and drops any grant for it.
func (h *Handler) HandleDBConnectRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if pp := r.URL.Query().Get("project_path"); pp != "" {
		root, ok := h.dbProjectRootFor(pp)
		if !ok {
			writeError(w, http.StatusBadRequest, "project_path is not an allowed project root")
			return
		}
		existing, err := config.LoadProjectDBConnections(root)
		if err != nil {
			log.Printf("dbconnect: remove %q: load project config: %v", name, err)
			writeError(w, http.StatusInternalServerError, "could not load project connections")
			return
		}
		out := make([]config.DBConnection, 0, len(existing))
		found := false
		for _, c := range existing {
			if c.Name == name {
				found = true
				continue
			}
			out = append(out, c)
		}
		if !found {
			writeError(w, http.StatusNotFound, fmt.Sprintf("db connection %q not found", name))
			return
		}
		if err := config.SaveProjectDBConnections(root, out); err != nil {
			log.Printf("dbconnect: remove %q: save project config: %v", name, err)
			writeError(w, http.StatusInternalServerError, "could not save project connections")
			return
		}
		h.dbDropConnection(name)
		remote.DefaultDBTunnelManager().StopConnection(name)
		writeJSON(w, http.StatusOK, map[string]string{"removed": name})
		return
	}
	if err := config.RemoveDBConnection(name); err != nil {
		log.Printf("dbconnect: remove %q: %v", name, err)
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h.dbDropConnection(name)
	// The ssh -L process outlives the grant, so it must be stopped here too.
	remote.DefaultDBTunnelManager().StopConnection(name)
	writeJSON(w, http.StatusOK, map[string]string{"removed": name})
}

// dbUnlockFailure names one connection that could not be unlocked. The message
// names a variable or a parameter, never a value, so it is safe to show.
type dbUnlockFailure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// HandleDBConnectUnlock grants the surface access to saved connections.
//
// A connection saved as an environment template ("${DATABASE_URL}") carries no
// secret of its own: it names variables that are read from the project's .env /
// .env.local at unlock time. Such a connection needs NO master password — there
// is nothing to decrypt. This is the "read the URL from .env" flow, not a secret
// store.
//
// A connection saved as a sealed URL still needs the master password that sealed
// it. With a password, every envelope must open (a mismatch rejects the whole
// unlock, so it is never half-applied) and templates are resolved too. Without a
// password, only the template connections are granted; any sealed connection is
// left locked, and the existing grant is MERGED rather than replaced.
//
// A template needs a project to read: project_path (validated against the
// server's allowed project roots) or, for a legacy caller, session_id. With
// neither, a template is reported as failed and is never resolved from the
// server's own process environment — that would silently connect a remote
// project to the wrong database.
//
// Templates that fail are reported per connection in "failed"; the ones that
// resolved are still granted. Only a wrong master password aborts the whole
// unlock. The resolved URL is never returned, logged, or written to disk.
func (h *Handler) HandleDBConnectUnlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Surface        string `json:"surface"`
		Password       string `json:"password"`
		TunnelPassword string `json:"tunnel_password"`
		// ProjectPath is the project whose .env files provide template
		// values. It is resolved only when it names a project root this
		// server may read.
		ProjectPath string `json:"project_path"`
		// SessionID is the pre-project-scoped fallback: the project of this
		// session provides the .env files. Ignored when ProjectPath is set.
		SessionID string `json:"session_id"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		log.Printf("dbconnect: unlock: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Surface == "" {
		writeError(w, http.StatusBadRequest, "surface is required")
		return
	}
	if len(req.Surface) > dbSurfaceMaxLen {
		writeError(w, http.StatusBadRequest, "surface is too long")
		return
	}
	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		log.Printf("dbconnect: unlock: load config: %v", err)
		writeError(w, http.StatusInternalServerError, "could not load saved connections")
		return
	}
	// Merge per-project connections (.ocode/db.json) when a project is given.
	if req.ProjectPath != "" {
		if root, ok := h.dbProjectRootFor(req.ProjectPath); ok {
			if pc, err := config.LoadProjectDBConnections(root); err == nil {
				cfg.DB.Connections = config.MergeDBConnections(cfg.DB.Connections, pc)
			}
		}
	}
	if len(cfg.DB.Connections) == 0 {
		writeError(w, http.StatusBadRequest, "no saved connections")
		return
	}
	// A project is only needed to resolve an environment template, so resolve
	// (and validate) it only when the saved set has one. A sealed-only unlock
	// must not fail because a project directory moved or unmounted since the
	// user last opened it — it reads no files.
	hasTemplate := false
	for _, c := range cfg.DB.Connections {
		if c.EnvTemplate != "" {
			hasTemplate = true
			break
		}
	}
	projectRoot := ""
	if hasTemplate {
		projectRoot, err = h.dbUnlockProjectRoot(req.ProjectPath, req.SessionID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	grant := make(dbGrant, len(cfg.DB.Connections))
	names := make([]string, 0, len(cfg.DB.Connections))
	// failures lists the connections that could not be unlocked, without
	// aborting the rest. One obsolete template used to block unlocking a
	// sealed connection with the correct password in every project.
	failures := make([]dbUnlockFailure, 0)
	// staleNames are the templates that FAILED to resolve. Their previous grant
	// (and pool) must be dropped, or a connection whose `.env` value disappeared
	// stays reported as unlocked and keeps querying the old database.
	var staleNames []string
	// Every envelope is decrypted EXACTLY ONCE. Argon2id is deliberately
	// slow, so a second pass over the same envelopes would double the cost
	// of an unlock for no benefit. The first envelope that fails to open
	// rejects the whole unlock, so a password mismatch is never
	// half-applied.
	//
	// A template connection has no envelope, so it never contributes to
	// that check. With no password it is the ONLY kind granted: there is
	// nothing to decrypt, and a sealed connection must never open without
	// the password that sealed it.
	for _, c := range cfg.DB.Connections {
		if c.EnvTemplate == "" && req.Password == "" {
			// Sealed connection and no master password: leave it out of
			// the grant. The caller is loading an environment connection,
			// not unlocking the sealed set.
			continue
		}
		// The URL is resolved (and a sealed one opened) BEFORE the tunnel
		// password, so a wrong master password still aborts the whole unlock.
		var connURL string
		if c.EnvTemplate != "" {
			if projectRoot == "" {
				// Never fall back to the process environment: this panel
				// exists for a project, and a remote project's .env lives on
				// the host. Silently using the server's own DATABASE_URL
				// would connect (and allow writes) to the wrong database.
				msg := "an environment-backed connection needs a project; its .env / .env.local cannot be read here (a remote project keeps them on the host)"
				log.Printf("dbconnect: unlock %q: %s", c.Name, msg)
				failures = append(failures, dbUnlockFailure{Name: c.Name, Error: msg})
				continue
			}
			resolved, err := dbResolveTemplate(c.EnvTemplate, projectRoot)
			if err != nil {
				// The error names the missing variable or the invalid URL
				// shape, never a value, so it is safe to show the user.
				log.Printf("dbconnect: unlock %q: resolve template: %v", c.Name, err)
				failures = append(failures, dbUnlockFailure{Name: c.Name, Error: err.Error()})
				staleNames = append(staleNames, c.Name)
				continue
			}
			connURL = resolved
		} else {
			opened, err := dbconnect.OpenURL(c.EncryptedURL, req.Password)
			if err != nil {
				log.Printf("dbconnect: unlock %q: %v", c.Name, err)
				writeError(w, http.StatusUnauthorized, "master password does not open connection "+c.Name)
				return
			}
			connURL = opened
		}
		entry := dbGrantEntry{url: connURL}
		if c.Tunnel != nil {
			// The SSH tunnel is NOT started here. A tunnel is an ssh to the host
			// the saved config names, and a project's db.json comes from the
			// repo: starting it during unlock would make unlocking a cloned repo
			// connect to a repo-chosen host, and one slow tunnel would stall
			// every connection. It starts when this connection is first opened
			// (dbGrantDB), after its credentials are known to be right.
			tunnelPassword, perr := dbUnlockTunnelPassword(c, req.TunnelPassword, req.Password)
			if perr != nil {
				// Per connection, never a whole-unlock abort: one bad tunnel
				// secret must not lock the other connections out.
				log.Printf("dbconnect: unlock %q: tunnel password: %v", c.Name, perr)
				failures = append(failures, dbUnlockFailure{Name: c.Name, Error: perr.Error()})
				staleNames = append(staleNames, c.Name)
				continue
			}
			// Reject a URL the tunnel cannot route now, not at the first query.
			if _, rerr := rewriteURLForTunnel(connURL, 1); rerr != nil {
				log.Printf("dbconnect: unlock %q: tunnel: %v", c.Name, rerr)
				failures = append(failures, dbUnlockFailure{Name: c.Name, Error: rerr.Error()})
				staleNames = append(staleNames, c.Name)
				continue
			}
			entry.hasTunnel = true
			entry.tunnel = *c.Tunnel
			entry.tunnelPassword = tunnelPassword
		}
		grant[c.Name] = entry
		names = append(names, c.Name)
	}
	// Drop the grant of every template that no longer resolves, whatever the
	// outcome below. This runs BEFORE the "nothing unlocked" early return: a
	// surface whose only connection just broke would otherwise keep the stale
	// grant and keep querying the old database. A password unlock replaces the
	// whole grant anyway, so only the password-less path needs it.
	if req.Password == "" {
		h.dbRemoveFromGrant(req.Surface, staleNames)
	}
	sort.Strings(names)
	if len(grant) == 0 {
		msg := "no connection can be unlocked: a master password is required for encrypted connections"
		if len(failures) > 0 {
			msg = "connection " + failures[0].Name + ": " + failures[0].Error
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	// A password unlock is the full set, so it replaces the grant. The
	// password-less flow grants only the environment connections: merge, or
	// it would revoke the sealed ones the user already unlocked.
	if req.Password == "" {
		h.dbAddToGrant(req.Surface, grant)
	} else {
		h.dbSetGrant(req.Surface, grant)
	}
	writeJSON(w, http.StatusOK, map[string]any{"unlocked": names, "failed": failures})
}

// HandleDBConnectLock drops the surface's grant.
func (h *Handler) HandleDBConnectLock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Surface string `json:"surface"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		log.Printf("dbconnect: lock: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Surface == "" {
		writeError(w, http.StatusBadRequest, "surface is required")
		return
	}
	if len(req.Surface) > dbSurfaceMaxLen {
		writeError(w, http.StatusBadRequest, "surface is too long")
		return
	}
	h.dbDropSurface(req.Surface)
	writeJSON(w, http.StatusOK, map[string]string{"locked": req.Surface})
}

// HandleDBConnectTables lists the public tables of an unlocked connection.
func (h *Handler) HandleDBConnectTables(w http.ResponseWriter, r *http.Request) {
	surface := r.URL.Query().Get("surface")
	name := r.URL.Query().Get("connection")
	if surface == "" || name == "" {
		writeError(w, http.StatusBadRequest, "surface and connection are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbConnectOpenTimeout)
	defer cancel()
	db, ok := h.dbOpenGranted(ctx, w, surface, name)
	if !ok {
		return
	}
	limit, offset, ok := parseDBTablePage(r.URL.Query().Get("limit"), r.URL.Query().Get("offset"))
	if !ok {
		writeError(w, http.StatusBadRequest, "limit must be 1-500 and offset must be 0 or more")
		return
	}
	page, err := dbconnect.ListTables(ctx, db, limit, offset)
	if err != nil {
		log.Printf("dbconnect: tables %q: %v", name, err)
		writeError(w, http.StatusBadGateway, "could not list tables")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

const (
	dbTablesDefaultLimit = 100
	dbTablesMaxLimit     = 500
)

// parseDBTablePage reads the optional limit and offset query values. An absent
// value takes its default; a malformed or out-of-range one is refused.
func parseDBTablePage(rawLimit, rawOffset string) (limit, offset int, ok bool) {
	limit = dbTablesDefaultLimit
	if rawLimit != "" {
		n, err := strconv.Atoi(rawLimit)
		if err != nil || n < 1 || n > dbTablesMaxLimit {
			return 0, 0, false
		}
		limit = n
	}
	if rawOffset != "" {
		n, err := strconv.Atoi(rawOffset)
		if err != nil || n < 0 {
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

// dbConnectWriteNeedsConfirm is the 409 message for a statement the server
// refused as a write. The client matches the status, not the text.
const dbConnectWriteNeedsConfirm = "this statement writes to the database; confirm to run it. The change is permanent and cannot be undone."

// HandleDBConnectQuery runs one statement on an unlocked connection. Every
// statement is tried in a READ ONLY transaction first; the server's refusal
// (SQLSTATE 25006) is what marks it as a write. Without confirm that is a 409,
// and the client asks the user. With confirm it runs in a committed read-write
// transaction. Other errors come back as 400 with the server's message, which
// never contains the URL.
func (h *Handler) HandleDBConnectQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Surface    string `json:"surface"`
		Connection string `json:"connection"`
		SQL        string `json:"sql"`
		Confirm    bool   `json:"confirm"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		log.Printf("dbconnect: query: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Surface == "" || req.Connection == "" || req.SQL == "" {
		writeError(w, http.StatusBadRequest, "surface, connection and sql are required")
		return
	}
	// A read-only transaction does not stop calls that act on the server, so
	// they are asked for before anything runs. The generic check runs before
	// connecting (no tunnel/connection is needed to refuse).
	if !req.Confirm && dbconnect.HasServerSideEffect(req.SQL) {
		log.Printf("dbconnect: query %q: server-side statement awaiting confirmation", req.Connection)
		writeError(w, http.StatusConflict, dbConnectWriteNeedsConfirm)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbConnectQueryTimeout)
	defer cancel()
	db, ok := h.dbOpenGranted(ctx, w, req.Surface, req.Connection)
	if !ok {
		return
	}
	// The driver-aware check needs the open handle (MySQL has INTO OUTFILE,
	// GET_LOCK, SLEEP...), so it adds to the generic one above.
	if !req.Confirm && dbconnect.HasServerSideEffectDB(db, req.SQL) {
		log.Printf("dbconnect: query %q: server-side statement awaiting confirmation", req.Connection)
		writeError(w, http.StatusConflict, dbConnectWriteNeedsConfirm)
		return
	}
	res, err := dbconnect.QueryReadOnly(ctx, db, req.SQL)
	if err != nil && dbconnect.IsReadOnlyViolation(err) {
		if !req.Confirm {
			log.Printf("dbconnect: query %q: write awaiting confirmation", req.Connection)
			writeError(w, http.StatusConflict, dbConnectWriteNeedsConfirm)
			return
		}
		res, err = dbconnect.ExecWrite(ctx, db, req.SQL)
	}
	if err != nil {
		log.Printf("dbconnect: query %q: %v", req.Connection, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// dbUnlockTunnelPassword returns the SSH password for one connection at unlock.
//
// A stored (sealed) tunnel password is opened with the master password, and
// the client's value is never used in its place: that would authenticate with
// the wrong secret. Only a connection with NO stored password takes the
// password the client supplied for this unlock.
func dbUnlockTunnelPassword(c config.DBConnection, supplied, master string) (string, error) {
	if c.Tunnel == nil || c.Tunnel.Password == "" {
		return supplied, nil
	}
	if master == "" {
		return "", errors.New("the master password is needed to open the saved tunnel password")
	}
	dec, err := dbconnect.OpenPassword(c.Tunnel.Password, master)
	if err != nil {
		return "", errors.New("the master password does not open the saved tunnel password")
	}
	return dec, nil
}

// errTunnelURL is returned when a URL cannot be routed through a tunnel. It
// never includes the URL: the URL carries the password.
var errTunnelURL = errors.New("connection url cannot be routed through its ssh tunnel")

// rewriteURLForTunnel replaces the host:port of a connection URL with the local
// tunnel port. Only that span changes. Everything else (scheme, userinfo with
// its exact escaping, path, query) is copied byte for byte, because a re-encode
// through url.URL.String can change the credentials.
//
// It returns an error rather than the original URL when it cannot do this. A
// caller that connected to the unrewritten URL would bypass the tunnel.
func rewriteURLForTunnel(raw string, port int) (string, error) {
	if _, err := url.Parse(raw); err != nil {
		return "", errTunnelURL
	}
	scheme := strings.Index(raw, "://")
	if scheme < 0 {
		return "", errTunnelURL
	}
	start := scheme + len("://")
	// The authority ends at the first path, query or fragment delimiter, the same
	// split net/url makes.
	end := len(raw)
	if i := strings.IndexAny(raw[start:], "/?#"); i >= 0 {
		end = start + i
	}
	authority := raw[start:end]
	// The userinfo ends at the LAST '@', as net/url does, so an '@' inside the
	// password is handled the same way by both.
	at := strings.LastIndex(authority, "@")
	userinfo := authority[:at+1] // "" when there is no '@'
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host != authority[at+1:] {
		// The surgery and the parser disagree on where the host is: refuse
		// rather than guess.
		return "", errTunnelURL
	}
	return raw[:start] + userinfo + fmt.Sprintf("127.0.0.1:%d", port) + raw[end:], nil
}
