package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/dbbrowse"
	"github.com/u007/ocode/internal/paths"
)

// dbQueryTimeout bounds a single SQLite browser request. modernc interrupts the
// engine on context cancellation, so a runaway recursive CTE cannot pin a
// goroutine indefinitely.
const dbQueryTimeout = 30 * time.Second

// logDBError records a database-browser failure. These endpoints are reached
// from the web UI, never the running TUI, so the stdlib logger (which the TUI
// redirects into its debug panel) is the right sink.
func logDBError(op, path string, err error) {
	log.Printf("db browser: %s %q: %v", op, path, err)
}

// pathWithinAllowedRoots reports whether path is contained in one of the
// server's allowed project roots (workDir + saved local projects +
// ExtraAllowedPaths). The DB endpoints require this unconditionally, even when
// no project_root is supplied: unlike a file *read*, the browser gains write
// capability in a later phase, so it must never address an arbitrary absolute
// path.
//
// This is containment for ?path ONLY. It is NOT the whole read/write boundary:
// paths named inside the caller's SQL were never checked here, which is why
// internal/dbbrowse opens every connection with query_only(1) AND rejects
// ATTACH/DETACH/multi-statement input. Keep the two in step — raising the roots
// widens this, but so does any SQL-side escape, independently.
//
// It also shares ExtraAllowedPaths with the permission system: "always allow" on
// an extra dir appends to that list (see handler_permissions_resolve.go), so a
// permission grant silently widens this surface. The sidebar's "Extra Dirs"
// section shows the live list, which is the mitigation.
func (h *Handler) pathWithinAllowedRoots(path string) bool {
	for _, root := range h.dbRoots() {
		if containedIn(path, root) {
			return true
		}
	}
	return false
}

// dbRoots lists the non-empty roots the DB endpoints may address.
func (h *Handler) dbRoots() []string {
	roots := h.allowedProjectRoots()
	h.mu.Lock()
	if h.cfg != nil {
		roots = append(roots, h.cfg.Ocode.ExtraAllowedPaths...)
	}
	h.mu.Unlock()
	out := roots[:0:0]
	for _, root := range roots {
		if root != "" {
			out = append(out, root)
		}
	}
	return out
}

// resolveDBPath applies BOTH the file-content containment boundary and the
// unconditional allowed-roots check. Every DB endpoint MUST route through it:
// calling resolveProjectFilePath alone skips containment for an absolute path
// with no project_root, which is the sibling-asymmetry bug where one endpoint
// accepts a path another rejects.
func (h *Handler) resolveDBPath(projectRoot, path string) (string, string) {
	resolved, errMsg := h.resolveProjectFilePath(projectRoot, path)
	if errMsg != "" {
		return "", errMsg
	}
	// Resolve symlinks and judge/open the REAL path: a symlink inside a root
	// pointing at a database outside it must not pass containment lexically
	// and then be opened (and, on the write endpoints, mutated) at its target.
	real, err := filepath.EvalSymlinks(resolved)
	if errors.Is(err, fs.ErrNotExist) {
		// Nothing exists to follow, so there is no target to escape to; the
		// caller's open/probe reports the missing file (404), and a dangling
		// symlink fails that same probe before any write. Containment is still
		// enforced, lexically, because there is no resolved form to check.
		for _, root := range h.dbRoots() {
			if containedLexical(resolved, root) {
				return resolved, ""
			}
		}
		return "", "path is outside the allowed project roots"
	}
	if err != nil {
		return "", "file not found"
	}
	if !h.pathWithinAllowedRoots(real) {
		return "", "path is outside the allowed project roots"
	}
	return real, ""
}

// dbWriteGuard rejects a write to a file the browser must never mutate: a
// non-SQLite file, or anything under ocode's own data directory (the session
// store, snapshot journal and usage ledger all live there). Read-only browsing
// of any allowed-root file stays allowed; only mutation is refused. It returns
// the HTTP status and client-facing message to reject with, or (0, "") when the
// write is permitted.
func (h *Handler) dbWriteGuard(path string) (int, string) {
	isSQLite, err := dbbrowse.Probe(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return http.StatusBadRequest, "file not found"
		}
		logDBError("probe for write", path, err)
		return http.StatusInternalServerError, "failed to read database file: " + err.Error()
	}
	if !isSQLite {
		return http.StatusBadRequest, "not a SQLite database"
	}
	// Fail closed when the data dir cannot be resolved: the write is refused,
	// as sqliteWriteGuard refuses it, instead of being allowed unchecked.
	dataDir, err := paths.GlobalDataDir()
	if err != nil {
		logDBError("resolve data dir for write", path, err)
		return http.StatusInternalServerError, "cannot resolve ocode's data directory; refusing to write"
	}
	if dataDir == "" {
		logDBError("resolve data dir for write", path, errors.New("data directory is empty"))
		return http.StatusInternalServerError, "ocode's data directory is empty; refusing to write"
	}
	if containedIn(path, dataDir) || containedLexical(path, dataDir) {
		return http.StatusBadRequest, "refusing to write to ocode's own data directory"
	}
	return 0, ""
}

// resolveDBRequest resolves the ?path / ?project_root pair to an absolute file
// path, writing the client-facing error itself. ok is false when it has already
// responded.
func (h *Handler) resolveDBRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return "", false
	}
	resolved, errMsg := h.resolveDBPath(r.URL.Query().Get("project_root"), path)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return "", false
	}
	return resolved, true
}

// dbIntParam parses a non-negative integer query parameter, returning def when
// it is empty or malformed.
func dbIntParam(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// isReadonlyError reports whether err is SQLite's "attempt to write a readonly
// database". It is a BACKSTOP for a write that reached the engine, not the
// read/write boundary — that boundary is query_only(1) plus dbbrowse's
// statement allowlist, and a write the allowlist catches never produces this
// error at all. Anything the confirmation escalation (P3) needs to reason about
// must therefore be checked against ErrStatementNotReadOnly too, or it will
// miss every allowlisted write.
func isReadonlyError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "readonly")
}

// dbbrowseQueryError maps a browse failure to a status. A rejected statement
// is 409 Conflict with the read-only message, not 400: the caller's SQL is
// syntactically fine and the refusal is about what this preview permits.
// Branching on dbbrowse.ErrStatementNotReadOnly rather than on the error TEXT
// is load-bearing — the allowlist's message says "read-only", which the
// engine's "readonly database" substring test does not match, so a text-only
// check silently downgraded every allowlisted write to a 400.
func dbbrowseQueryError(w http.ResponseWriter, err error) {
	if errors.Is(err, dbbrowse.ErrStatementNotReadOnly) || isReadonlyError(err) {
		writeError(w, http.StatusConflict, "This statement needs confirmation to run.")
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

// HandleDBInfo probes a file and, when it is a SQLite database, lists its
// tables and views (sorted by name). A non-SQLite file returns
// {"is_sqlite": false} so the viewer can fall back rather than hand garbage to
// the engine.
func (h *Handler) HandleDBInfo(w http.ResponseWriter, r *http.Request) {
	path, ok := h.resolveDBRequest(w, r)
	if !ok {
		return
	}
	isSQLite, err := dbbrowse.Probe(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if !isSQLite {
		writeJSON(w, http.StatusOK, map[string]any{"is_sqlite": false, "path": path})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	defer cancel()
	tables, err := dbbrowse.ListTables(ctx, path)
	if err != nil {
		logDBError("list tables", path, err)
		writeError(w, http.StatusInternalServerError, "failed to read database: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"is_sqlite": true,
		"path":      path,
		"tables":    tables,
	})
}

// HandleDBTable returns one page of rows from a table or view, together with
// its schema.
func (h *Handler) HandleDBTable(w http.ResponseWriter, r *http.Request) {
	path, ok := h.resolveDBRequest(w, r)
	if !ok {
		return
	}
	table := r.URL.Query().Get("table")
	if table == "" {
		writeError(w, http.StatusBadRequest, "table is required")
		return
	}
	limit := dbIntParam(r.URL.Query().Get("limit"), 0)
	offset := dbIntParam(r.URL.Query().Get("offset"), 0)

	// IDE paging: a user filter expression, a sort column and the total row
	// count. All three are optional, and with none of them the response is
	// exactly what this handler produced before they existed.
	opts := dbbrowse.PageOptions{
		Limit:  limit,
		Offset: offset,
		Filter: r.URL.Query().Get("filter"),
		SortBy: r.URL.Query().Get("sort"),
	}
	dir := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("dir")))
	switch dir {
	case "", "asc":
	case "desc":
		opts.SortDesc = true
	default:
		writeError(w, http.StatusBadRequest, "dir must be asc or desc")
		return
	}
	withCount := dbBoolParam(r.URL.Query().Get("count"))

	ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	defer cancel()

	schema, err := dbbrowse.DescribeTable(ctx, path, table)
	if err != nil {
		if errors.Is(err, dbbrowse.ErrNoSuchTable) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		logDBError("describe table", path, err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// When the table has no declared primary key it is addressed by the true
	// rowid, which is not part of `SELECT *`; expose it as the leading _rowid_
	// column so the grid can build a key for row edits.
	keyCols, _ := schema.KeyColumns()
	opts.IncludeRowID = len(keyCols) == 1 && keyCols[0] == dbbrowse.RowIDColumn

	// Both paths must honour IncludeRowID: the unfiltered fast path is a
	// different function from the filtered one, and a rowid table reached
	// without filter/sort/count would lose the column its row edits are keyed
	// on. TablePage is the rowid-free projection, so it is only safe when no
	// rowid column was requested.
	var page dbbrowse.ResultSet
	var total int64
	if withCount || opts.Filter != "" || opts.SortBy != "" || opts.IncludeRowID {
		opts.WithCount = withCount || opts.Filter != "" || opts.SortBy != ""
		page, total, err = dbbrowse.TablePageFiltered(ctx, path, table, opts)
	} else {
		page, err = dbbrowse.TablePage(ctx, path, table, limit, offset)
	}
	if err != nil {
		if errors.Is(err, dbbrowse.ErrNoSuchTable) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, dbbrowse.ErrStatementNotReadOnly) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// A bad filter expression (unknown column, type mismatch) is the
		// caller's input too — surface it as 400 so the grid can show it.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := map[string]any{"schema": schema, "result": page}
	if withCount || opts.Filter != "" || opts.SortBy != "" {
		resp["total"] = total
	}
	// Exact row keys, index-aligned with result.rows. They are strings because a
	// browser parses JSON integers into float64 and two adjacent ids past 2^53
	// collapse into one, so a key rebuilt from the grid's cells can address the
	// NEIGHBOURING row. Omitted entirely when the table has no addressable key
	// (a view), in which case the client keeps its previous behaviour.
	if keys := dbbrowse.RowKeys(schema, page); len(keys) > 0 {
		resp["row_keys"] = keys
	}
	writeJSON(w, http.StatusOK, resp)
}

// dbBoolParam reads a truthy query parameter. Only an explicit true accepts it,
// so a stray `count=0` or `count=false` means "no count" rather than flipping the
// meaning of the request.
func dbBoolParam(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// HandleDBMaintenance runs one database-level maintenance operation: ANALYZE
// (populates sqlite_stat1 so the table list shows real counts), VACUUM (rewrites
// the file to reclaim free pages) or integrity_check (reports damage).
//
// The op is mapped to a fixed SQL string in dbbrowse, never to user text, so an
// unrecognised op is refused outright rather than falling through to something
// executable. ANALYZE and VACUUM mutate the file and so pass the write guard;
// integrity_check only reads.
func (h *Handler) HandleDBMaintenance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path        string `json:"path"`
		ProjectRoot string `json:"project_root"`
		Op          string `json:"op"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" || strings.TrimSpace(req.Op) == "" {
		writeError(w, http.StatusBadRequest, "path and op are required")
		return
	}
	resolved, errMsg := h.resolveDBPath(req.ProjectRoot, req.Path)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if status, msg := h.dbWriteGuard(resolved); msg != "" {
		writeError(w, status, msg)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	defer cancel()

	switch req.Op {
	case "analyze":
		res, err := dbbrowse.Analyze(ctx, resolved)
		if err != nil {
			logDBError("analyze", resolved, err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
	case "vacuum":
		res, err := dbbrowse.Vacuum(ctx, resolved)
		if err != nil {
			logDBError("vacuum", resolved, err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
	case "integrity_check":
		rows, err := dbbrowse.IntegrityCheck(ctx, resolved)
		if err != nil {
			logDBError("integrity check", resolved, err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
	default:
		writeError(w, http.StatusBadRequest, "op must be analyze, vacuum or integrity_check")
	}
}

// HandleDBQuery runs a SQL statement from the query editor. In this phase the
// statement runs on a read-only connection: a SELECT (or a read PRAGMA)
// succeeds, and anything that mutates fails with SQLite's readonly error, which
// is surfaced as 409. The write-escalation path (a confirm flag that retries on
// a read-write connection) is a later phase.
func (h *Handler) HandleDBQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path        string `json:"path"`
		ProjectRoot string `json:"project_root"`
		SQL         string `json:"sql"`
		Limit       int    `json:"limit"`
		Confirm     bool   `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" || strings.TrimSpace(req.SQL) == "" {
		writeError(w, http.StatusBadRequest, "path and sql are required")
		return
	}
	resolved, errMsg := h.resolveDBPath(req.ProjectRoot, req.Path)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	defer cancel()

	// Confirmed write: the client has shown the user what will change and they
	// accepted it. ATTACH/DETACH/VACUUM INTO are refused even here (there is no
	// authorizer hook, so ATTACH could reach a file outside the containment
	// boundary), and ocode's own data directory is off limits.
	if req.Confirm {
		if kw, blocked := dbbrowse.BlockedWriteStatement(req.SQL); blocked {
			writeError(w, http.StatusBadRequest, kw+" is not allowed from the SQL editor.")
			return
		}
		if status, msg := h.dbWriteGuard(resolved); msg != "" {
			writeError(w, status, msg)
			return
		}
		res, err := dbbrowse.Exec(ctx, resolved, req.SQL)
		if err != nil {
			logDBError("confirmed query", resolved, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}

	rs, err := dbbrowse.Query(ctx, resolved, req.SQL, req.Limit)
	if err != nil {
		dbbrowseQueryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

// dbRowMaxBodyBytes bounds a row save. A BLOB may travel base64-encoded in
// `values` (the row dialog's file picker), so the limit is the 64 MB blob cap
// grossed up for base64's 4/3 inflation, plus headroom for the other columns.
const dbRowMaxBodyBytes = 96 << 20

// HandleDBRow performs one parameterized row insert, update or delete. The row
// is identified by its primary key, or by the synthetic _rowid_ column for a
// table with no declared primary key. UPDATE/DELETE require exactly one row to
// match; a key that matches none or many is a 409 so the caller never silently
// changes rows it did not intend to.
func (h *Handler) HandleDBRow(w http.ResponseWriter, r *http.Request) {
	// A row save can now carry a BLOB as base64 in `values`, so the body needs a
	// bound: 64 MB of blob inflates to ~85 MB, plus room for the rest of the row.
	// Without this the endpoint buffers whatever it is sent, which is
	// indistinguishable from a memory leak triggered by a stranger's curl.
	r.Body = http.MaxBytesReader(w, r.Body, dbRowMaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.UseNumber() // preserve int64 ids that a float64 would round
	var req struct {
		Path        string         `json:"path"`
		ProjectRoot string         `json:"project_root"`
		Op          string         `json:"op"`
		Table       string         `json:"table"`
		Key         map[string]any `json:"key"`
		Values      map[string]any `json:"values"`
	}
	if err := dec.Decode(&req); err != nil {
		// An over-cap body must say so: without this it is indistinguishable from
		// malformed JSON, and the user is told their request was invalid when the
		// real problem is the size of the blob they attached.
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("Row save is larger than the %d MB limit.", dbRowMaxBodyBytes>>20))
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" || strings.TrimSpace(req.Table) == "" {
		writeError(w, http.StatusBadRequest, "path and table are required")
		return
	}
	resolved, errMsg := h.resolveDBPath(req.ProjectRoot, req.Path)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if status, msg := h.dbWriteGuard(resolved); msg != "" {
		writeError(w, status, msg)
		return
	}
	key, err := dbbrowse.NormalizeRow(req.Key)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	values, err := dbbrowse.NormalizeRow(req.Values)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	defer cancel()

	// Take a snapshot BEFORE the mutation. Every statement runs inside a
	// transaction that commits only on success (dbbrowse.execExact), so a failed
	// attempt leaves the file unchanged and a snapshot taken here is always the
	// state the user could return to — including after a 409, where the snapshot
	// is simply a copy of what is already there.
	//
	// It must happen before Exec, not after: a snapshot taken afterwards would
	// record the change it was supposed to protect against. It is taken before the
	// mutation rather than inside dbbrowse so the copy is the browser's
	// recoverability contract, not something each row operation must remember.
	if _, err := dbbrowse.Backup(ctx, resolved); err != nil {
		logDBError("backup before "+req.Op, resolved, err)
		writeError(w, http.StatusInternalServerError, "could not back up the database before writing: "+err.Error())
		return
	}

	var res dbbrowse.ExecResult
	switch req.Op {
	case "insert":
		res, err = dbbrowse.RowInsert(ctx, resolved, req.Table, values)
	case "update":
		res, err = dbbrowse.RowUpdate(ctx, resolved, req.Table, key, values)
	case "delete":
		res, err = dbbrowse.RowDelete(ctx, resolved, req.Table, key)
	default:
		writeError(w, http.StatusBadRequest, "op must be insert, update or delete")
		return
	}
	if err != nil {
		switch {
		case errors.Is(err, dbbrowse.ErrNoRowsAffected):
			writeError(w, http.StatusConflict, "No row matched — it may have changed or been deleted.")
			return
		case errors.Is(err, dbbrowse.ErrMultipleRowsAffected):
			writeError(w, http.StatusConflict, "The key matched more than one row; refusing to change it.")
			return
		}
		logDBError(req.Op+" row", resolved, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// HandleDBSchema performs one guided DDL operation. The SQL is built by
// dbbrowse from validated parts (quoted identifiers, an allowlisted type shape,
// a literal default), never from raw user text.
func (h *Handler) HandleDBSchema(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path         string               `json:"path"`
		ProjectRoot  string               `json:"project_root"`
		Op           string               `json:"op"`
		Table        string               `json:"table"`
		Index        string               `json:"index"`
		Column       *dbbrowse.ColumnDef  `json:"column"`
		Columns      []dbbrowse.ColumnDef `json:"columns"`
		IndexColumns []string             `json:"index_columns"`
		Unique       bool                 `json:"unique"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	resolved, errMsg := h.resolveDBPath(req.ProjectRoot, req.Path)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if status, msg := h.dbWriteGuard(resolved); msg != "" {
		writeError(w, status, msg)
		return
	}

	var (
		stmt string
		err  error
	)
	switch req.Op {
	case "add_column":
		if req.Column == nil {
			writeError(w, http.StatusBadRequest, "column is required")
			return
		}
		stmt, err = dbbrowse.BuildAddColumn(req.Table, *req.Column)
	case "create_table":
		stmt, err = dbbrowse.BuildCreateTable(req.Table, req.Columns)
	case "drop_table":
		stmt, err = dbbrowse.BuildDropTable(req.Table)
	case "create_index":
		stmt, err = dbbrowse.BuildCreateIndex(req.Table, req.Index, req.IndexColumns, req.Unique)
	case "drop_index":
		stmt, err = dbbrowse.BuildDropIndex(req.Index)
	default:
		writeError(w, http.StatusBadRequest, "unknown schema op")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	defer cancel()
	res, err := dbbrowse.Exec(ctx, resolved, stmt)
	if err != nil {
		logDBError("schema "+req.Op, resolved, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
