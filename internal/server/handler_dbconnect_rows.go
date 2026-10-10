package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/u007/ocode/internal/dbconnect"
)

// Table browse and row edits for the Postgres connector. Both run against an
// unlocked connection (see dbGrantURL). Keys and values are bound as parameters
// and cast to catalog types; identifiers are checked against the table first.

// dbOpenGranted returns the pooled handle for the connection this surface
// unlocked. On failure it writes the response and returns ok=false, so callers
// only write their own success path. The grant store owns the handle; callers
// must not close it.
func (h *Handler) dbOpenGranted(ctx context.Context, w http.ResponseWriter, surface, name string) (*sql.DB, bool) {
	db, granted, err := h.dbGrantDB(ctx, surface, name)
	if !granted {
		writeError(w, http.StatusForbidden, "connection is locked; unlock it first")
		return nil, false
	}
	if err != nil {
		log.Printf("dbconnect: %q: open: %v", name, err)
		writeError(w, http.StatusBadGateway, "could not connect")
		return nil, false
	}
	return db, true
}

// HandleDBConnectRows returns one page of a table: its columns, the rows, and
// has_more. Optional sort (a column of the table), dir (asc or desc), and filter
// (a read-only SQL predicate) shape the page.
func (h *Handler) HandleDBConnectRows(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	surface, name, table := q.Get("surface"), q.Get("connection"), q.Get("table")
	if surface == "" || name == "" || table == "" {
		writeError(w, http.StatusBadRequest, "surface, connection and table are required")
		return
	}
	limit, offset, ok := parseDBTablePage(q.Get("limit"), q.Get("offset"))
	if !ok {
		writeError(w, http.StatusBadRequest, "limit must be 1-500 and offset must be 0 or more")
		return
	}
	desc, ok := parseDBDirection(q.Get("dir"))
	if !ok {
		writeError(w, http.StatusBadRequest, "dir must be asc or desc")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbConnectQueryTimeout)
	defer cancel()
	db, ok := h.dbOpenGranted(ctx, w, surface, name)
	if !ok {
		return
	}

	page, err := dbconnect.BrowsePage(ctx, db, table, q.Get("sort"), desc, q.Get("filter"), limit, offset)
	switch {
	case errors.Is(err, dbconnect.ErrNoSuchTable):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, dbconnect.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case dbconnect.IsReadOnlyViolation(err):
		// A filter is a predicate; a write in it is not a request to confirm.
		writeError(w, http.StatusBadRequest, "the filter must be a read-only expression")
	case err != nil:
		log.Printf("dbconnect: rows %q %q: %v", name, table, err)
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, page)
	}
}

// HandleDBConnectRow inserts, updates or deletes one row. Update and delete must
// match exactly one row by primary key; a key that matches none is 409 (the row
// changed or was deleted), and one that matches several is refused as 409.
func (h *Handler) HandleDBConnectRow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Surface    string         `json:"surface"`
		Connection string         `json:"connection"`
		Op         string         `json:"op"`
		Table      string         `json:"table"`
		Key        map[string]any `json:"key"`
		Values     map[string]any `json:"values"`
	}
	dec := json.NewDecoder(r.Body)
	dec.UseNumber() // a key's digits must reach the database exactly as sent
	if err := dec.Decode(&req); err != nil {
		log.Printf("dbconnect: row: decode request: %v", err)
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if req.Surface == "" || req.Connection == "" || req.Table == "" {
		writeError(w, http.StatusBadRequest, "surface, connection and table are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbConnectQueryTimeout)
	defer cancel()
	db, ok := h.dbOpenGranted(ctx, w, req.Surface, req.Connection)
	if !ok {
		return
	}

	start := time.Now()
	n, err := dbconnect.ApplyRowChange(ctx, db, req.Op, req.Table, req.Key, req.Values)
	switch {
	case errors.Is(err, dbconnect.ErrNoRowMatched), errors.Is(err, dbconnect.ErrManyRowsMatched):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, dbconnect.ErrNoSuchTable):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		log.Printf("dbconnect: row %s %q: %v", req.Op, req.Table, err)
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"rows_affected": n,
			"elapsed_ms":    time.Since(start).Milliseconds(),
		})
	}
}

// parseDBDirection reads the optional dir value. Absent means ascending.
func parseDBDirection(raw string) (desc, ok bool) {
	switch raw {
	case "", "asc":
		return false, true
	case "desc":
		return true, true
	default:
		return false, false
	}
}
