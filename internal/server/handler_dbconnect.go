package server

import (
	"context"
	"database/sql"
	"errors"
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
// Saved connections live in ocodeconfig.json under db.connections as encrypted
// envelopes. Unlock opens every envelope with the master password and keeps the
// plaintext URLs in memory under the client-supplied surface id, the same shape
// as the password vault's surface grants. Plaintext URLs are never returned,
// logged, or written to disk. Connections are global rather than directory-bound,
// so these endpoints take no project param.

const (
	dbConnectOpenTimeout  = 15 * time.Second
	dbConnectQueryTimeout = 30 * time.Second
)

// dbGrant maps connection name to its plaintext URL for one unlocked surface.
type dbGrant map[string]string

// dbGrantURL returns the plaintext URL for an unlocked connection, if any.
func (h *Handler) dbGrantURL(surface, name string) (string, bool) {
	h.dbMu.Lock()
	defer h.dbMu.Unlock()
	u, ok := h.dbGrants[surface][name]
	return u, ok
}

// dbSetGrant replaces the grant for one surface. Pools opened under the old
// grant are closed, since they may point at a different URL.
func (h *Handler) dbSetGrant(surface string, grant dbGrant) {
	h.dbMu.Lock()
	if h.dbGrants == nil {
		h.dbGrants = make(map[string]dbGrant)
	}
	h.dbGrants[surface] = grant
	stale := h.dbTakePoolsLocked(surface)
	h.dbMu.Unlock()
	dbClosePools(stale)
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
	}
	conns := cfg.DB.Connections
	sort.Slice(conns, func(i, j int) bool { return conns[i].Name < conns[j].Name })
	out := make([]entry, 0, len(conns))
	for _, c := range conns {
		_, unlocked := h.dbGrantURL(surface, c.Name)
		out = append(out, entry{Name: c.Name, Driver: c.Driver, Unlocked: unlocked})
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

// HandleDBConnectAdd seals a plaintext URL under the master password and saves it.
// The URL and password are never logged or echoed back.
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
	if req.Name == "" || req.URL == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "name, url and password are required")
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
	for _, c := range verified {
		if _, err := dbconnect.OpenURL(c.EncryptedURL, req.Password); err != nil {
			log.Printf("dbconnect: add %q: password check against %q: %v", req.Name, c.Name, err)
			writeError(w, http.StatusUnauthorized, "master password does not match saved connection "+c.Name)
			return
		}
	}
	sealed, err := dbconnect.SealURL(req.URL, req.Password)
	if err != nil {
		log.Printf("dbconnect: add %q: seal: %v", req.Name, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.AddDBConnection(config.DBConnection{
		Name:         req.Name,
		Driver:       config.DBDriverPostgres,
		EncryptedURL: sealed,
	}, verified); err != nil {
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

// HandleDBConnectUnlock opens every saved connection with the master password and
// grants the surface access to all of them. Any envelope that fails to open
// rejects the whole unlock, so a password mismatch is never half-applied.
func (h *Handler) HandleDBConnectUnlock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Surface  string `json:"surface"`
		Password string `json:"password"`
	}
	if err := readBodyJSON(r, &req); err != nil {
		log.Printf("dbconnect: unlock: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Surface == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "surface and password are required")
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
	grant := make(dbGrant, len(cfg.DB.Connections))
	names := make([]string, 0, len(cfg.DB.Connections))
	for _, c := range cfg.DB.Connections {
		u, err := dbconnect.OpenURL(c.EncryptedURL, req.Password)
		if err != nil {
			log.Printf("dbconnect: unlock %q: %v", c.Name, err)
			writeError(w, http.StatusUnauthorized, "master password does not open connection "+c.Name)
			return
		}
		grant[c.Name] = u
		names = append(names, c.Name)
	}
	sort.Strings(names)
	h.dbSetGrant(req.Surface, grant)
	writeJSON(w, http.StatusOK, map[string]any{"unlocked": names})
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
