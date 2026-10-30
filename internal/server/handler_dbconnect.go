package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/dbconnect"
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

// dbGrant maps connection name to its plaintext URL for one unlocked surface.
type dbGrant map[string]string

// dbGrantURL returns the plaintext URL for an unlocked connection, if any.
// It also marks the surface as in use, which retires surfaces left behind by a
// client that never called lock (see dbTouchLocked).
func (h *Handler) dbGrantURL(surface, name string) (string, bool) {
	h.dbMu.Lock()
	stale := h.dbTouchLocked(surface, time.Now())
	u, ok := h.dbGrants[surface][name]
	h.dbMu.Unlock()
	dbClosePools(stale)
	return u, ok
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
	for name, u := range entries {
		if old, ok := grant[name]; ok && old != u {
			if db := h.dbPools[surface][name]; db != nil {
				stale = append(stale, db)
				delete(h.dbPools[surface], name)
			}
		}
		grant[name] = u
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
// that happens to share its name.
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
		root, ok := h.fileContentRootFor(projectPath)
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
	u, ok := h.dbGrants[surface][name]
	if !ok {
		h.dbMu.Unlock()
		return nil, false, nil
	}
	if cached := h.dbPools[surface][name]; cached != nil {
		h.dbMu.Unlock()
		return cached, true, nil
	}
	h.dbMu.Unlock()

	opened, err := dbconnect.Open(ctx, u)
	if err != nil {
		return nil, true, err
	}

	h.dbMu.Lock()
	if cur, ok := h.dbGrants[surface][name]; !ok || cur != u {
		h.dbMu.Unlock()
		dbClosePools([]*sql.DB{opened})
		return nil, false, nil
	}
	if cached := h.dbPools[surface][name]; cached != nil {
		// A concurrent request opened this connection first; share its handle.
		h.dbMu.Unlock()
		dbClosePools([]*sql.DB{opened})
		return cached, true, nil
	}
	if h.dbPools == nil {
		h.dbPools = make(map[string]map[string]*sql.DB)
	}
	if h.dbPools[surface] == nil {
		h.dbPools[surface] = make(map[string]*sql.DB)
	}
	h.dbPools[surface][name] = opened
	h.dbMu.Unlock()
	return opened, true, nil
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
		Name     string `json:"name"`
		URL      string `json:"url"`
		Password string `json:"password"`
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
	verified := cfg.DB.Connections
	if !isTemplate {
		if err := dbCheckMasterPassword(verified, req.Password); err != nil {
			log.Printf("dbconnect: add %q: %v", req.Name, err)
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
	}
	conn := config.DBConnection{
		Name:   req.Name,
		Driver: config.DBDriverPostgres,
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
		if c.EncryptedURL == "" {
			continue
		}
		if _, err := dbconnect.OpenURL(c.EncryptedURL, password); err != nil {
			return fmt.Errorf("master password does not match saved connection %s", c.Name)
		}
	}
	return nil
}

// HandleDBConnectRemove deletes a saved connection and drops any grant for it.
func (h *Handler) HandleDBConnectRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := config.RemoveDBConnection(name); err != nil {
		log.Printf("dbconnect: remove %q: %v", name, err)
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h.dbDropConnection(name)
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
		Surface  string `json:"surface"`
		Password string `json:"password"`
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
			grant[c.Name] = resolved
			names = append(names, c.Name)
			continue
		}
		if req.Password == "" {
			// Sealed connection and no master password: leave it out of
			// the grant. The caller is loading an environment connection,
			// not unlocking the sealed set.
			continue
		}
		opened, err := dbconnect.OpenURL(c.EncryptedURL, req.Password)
		if err != nil {
			log.Printf("dbconnect: unlock %q: %v", c.Name, err)
			writeError(w, http.StatusUnauthorized, "master password does not open connection "+c.Name)
			return
		}
		grant[c.Name] = opened
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
	// they are asked for before anything runs.
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
