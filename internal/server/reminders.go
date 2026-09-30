package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/u007/ocode/internal/reminders"
	"github.com/u007/ocode/internal/scheduler"
)

// remindersHandler serves the REST surface for reminders and tasks. The two
// kinds share ONE service and ONE store; the handler is parameterised by kind
// so /api/reminders and /api/tasks are the same code with a different filter,
// and an id belonging to the other kind is a 404 rather than a cross-kind edit.
//
// Route families (registered by attachReminders):
//
//	GET    /api/reminders            list, sorted + paginated
//	POST   /api/reminders            create
//	GET    /api/reminders/{id}       read one
//	PATCH  /api/reminders/{id}       edit fields and/or change status
//	DELETE /api/reminders/{id}       delete
//	POST   /api/reminders/{id}/run   fire now, bypassing the due gate
//	GET    /api/reminders/{id}/runs  run history (shared with cron)
//
// and the identical set under /api/tasks.
type remindersHandler struct {
	svc  *reminders.Service
	kind reminders.Kind
}

const remindersBodyLimit = 64 << 10

// reminderWriteRequest is the JSON body for POST and PATCH. Every field is a
// pointer so a PATCH can change one field without resetting the rest; on POST
// the pointers are required by validate.
type reminderWriteRequest struct {
	Title        *string                   `json:"title"`
	Message      *string                   `json:"message"`
	Notes        *string                   `json:"notes"`
	Owner        *string                   `json:"owner"`
	Action       *reminders.Action         `json:"action"`
	AutoComplete *bool                     `json:"auto_complete"`
	DueAtMs      *int64                    `json:"due_at_ms"`
	PermMode     *scheduler.PermissionMode `json:"perm_mode"`
	Status       *reminders.Status         `json:"status"`
}

type reminderListResponse struct {
	Items []reminders.Item `json:"items"`
	Total int              `json:"total"`
	// Limit/Offset echo the effective paging so a client can tell whether it
	// asked for a different window than it got (e.g. an over-large limit was
	// clamped to MaxPageSize).
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// list handles GET /api/{reminders,tasks}.
func (h *remindersHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := reminders.ListFilter{Kind: h.kind}

	if raw := q.Get("status"); raw != "" {
		st := reminders.Status(raw)
		if !reminders.ValidStatus(st) {
			writeError(w, http.StatusBadRequest, statusChoicesError(st))
			return
		}
		filter.Status = st
	}
	limit, err := intQuery(q.Get("limit"), 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "limit: "+err.Error())
		return
	}
	offset, err := intQuery(q.Get("offset"), 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "offset: "+err.Error())
		return
	}
	filter.Limit = limit
	filter.Offset = offset

	items, total := h.svc.List(filter)
	writeJSON(w, http.StatusOK, reminderListResponse{
		Items:  items,
		Total:  total,
		Limit:  effectiveLimit(limit),
		Offset: offset,
	})
}

// get handles GET /api/{reminders,tasks}/{id}.
func (h *remindersHandler) get(w http.ResponseWriter, r *http.Request) {
	it, err := h.lookup(w, r)
	if err != nil {
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// add handles POST /api/{reminders,tasks}.
func (h *remindersHandler) add(w http.ResponseWriter, r *http.Request) {
	req, err := decodeReminderBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A body that tries to set its own kind cannot: the route decides it.
	// Silently ignoring a supplied `status` would let a POST create an item
	// that is already done, so it is an error rather than a dropped field.
	if req.Status != nil && *req.Status != reminders.StatusPending {
		writeError(w, http.StatusBadRequest, "a new item is always created pending; set status with PATCH")
		return
	}
	if req.Title == nil {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	item := reminders.Item{
		Kind:         h.kind,
		Title:        *req.Title,
		Message:      derefString(req.Message),
		Notes:        derefString(req.Notes),
		Owner:        derefString(req.Owner),
		Action:       derefAction(req.Action),
		AutoComplete: derefBool(req.AutoComplete),
		DueAtMs:      derefInt64(req.DueAtMs),
		PermMode:     derefPermMode(req.PermMode),
	}
	id, err := h.svc.Add(item)
	if err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	created, err := h.svc.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, created)
}

// update handles PATCH /api/{reminders,tasks}/{id}. It carries BOTH field
// edits and a status change, because that is how a UI "mark done" button
// naturally reads.
//
// The two are applied in two steps, and the status is VALIDATED FIRST so a
// rejected status change cannot leave the field edits half-applied: the
// service is asked nothing until the transition is known to be legal.
func (h *remindersHandler) update(w http.ResponseWriter, r *http.Request) {
	cur, err := h.lookup(w, r)
	if err != nil {
		return
	}
	req, err := decodeReminderBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Pre-validate the transition against the CURRENT status, so a rejected
	// status change cannot leave the field edits half-applied.
	//
	// The two failure kinds are deliberately separated, because the client can
	// only react usefully if it can tell them apart: an UNKNOWN value is a
	// malformed request (400, a field error the form can show), while a known
	// value that the machine forbids is a conflict with the item's current state
	// (409, meaning re-read and re-render). `Transition` wraps both in
	// ErrTransition, so the unknown-value case is checked first and separately.
	if req.Status != nil {
		if !reminders.ValidStatus(*req.Status) {
			writeError(w, http.StatusBadRequest, statusChoicesError(*req.Status))
			return
		}
		if *req.Status != cur.Status {
			if err := reminders.Transition(cur.Status, *req.Status); err != nil {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
		}
	}

	updated := cur
	if patch := req.toPatch(); patch != nil {
		updated, err = h.svc.Update(cur.ID, *patch)
		if err != nil {
			writeError(w, statusForErr(err), err.Error())
			return
		}
	}
	if req.Status != nil {
		updated, err = h.svc.SetStatus(cur.ID, *req.Status)
		if err != nil {
			writeError(w, statusForErr(err), err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, updated)
}

// remove handles DELETE /api/{reminders,tasks}/{id}.
func (h *remindersHandler) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if _, err := h.lookup(w, r); err != nil {
		return
	}
	if err := h.svc.Remove(id); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// runNow handles POST /api/{reminders,tasks}/{id}/run — the "Remind me now" /
// "Run now" button. It bypasses the due-time gate but NOT the terminal-status
// gate: firing a cancelled item would deliver a notification the user
// explicitly turned off.
func (h *remindersHandler) runNow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if _, err := h.lookup(w, r); err != nil {
		return
	}
	it, err := h.svc.FireNow(id)
	if err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// lookup resolves {id} and enforces the kind. An id that exists but belongs to
// the other kind is reported as 404: from this route's point of view it does
// not exist, and saying "that is a task, not a reminder" leaks the other
// collection for no benefit.
func (h *remindersHandler) lookup(w http.ResponseWriter, r *http.Request) (reminders.Item, error) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return reminders.Item{}, errors.New("id is required")
	}
	it, err := h.svc.Get(id)
	if err != nil {
		if errors.Is(err, reminders.ErrNotFound) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("item %s not found", id))
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return reminders.Item{}, err
	}
	if it.Kind != h.kind {
		writeError(w, http.StatusNotFound, fmt.Sprintf("item %s not found", id))
		return reminders.Item{}, fmt.Errorf("item %s not found", id)
	}
	return it, nil
}

// toPatch maps the request onto a service patch, or nil when the body carries
// no field edits at all (a status-only PATCH).
func (req reminderWriteRequest) toPatch() *reminders.ItemPatch {
	p := reminders.ItemPatch{
		Title:        req.Title,
		Message:      req.Message,
		Notes:        req.Notes,
		Owner:        req.Owner,
		Action:       req.Action,
		AutoComplete: req.AutoComplete,
		DueAtMs:      req.DueAtMs,
		PermMode:     req.PermMode,
	}
	if p.Title == nil && p.Message == nil && p.Notes == nil && p.Owner == nil &&
		p.Action == nil && p.AutoComplete == nil && p.DueAtMs == nil && p.PermMode == nil {
		return nil
	}
	return &p
}

func decodeReminderBody(r *http.Request) (reminderWriteRequest, error) {
	var req reminderWriteRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, remindersBodyLimit))
	if err != nil {
		return req, fmt.Errorf("read body: %w", err)
	}
	if len(body) == 0 {
		return req, errors.New("empty request body")
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return req, fmt.Errorf("invalid json: %w", err)
	}
	return req, nil
}

// attachReminders wires the reminder/task REST surface into the server. Safe to
// call before or after Listen; routes are live as soon as the mux serves.
func (s *Server) attachReminders(svc *reminders.Service) {
	if s == nil || svc == nil {
		return
	}
	s.reminders = svc
	// The Kind constants are SINGULAR ("reminder"/"task") but the collections
	// are plural, so the route segment is spelled out here rather than derived
	// from the kind — deriving it produced /api/reminder, which 404s.
	for _, route := range []struct {
		segment string
		kind    reminders.Kind
	}{
		{"/reminders", reminders.KindReminder},
		{"/tasks", reminders.KindTask},
	} {
		h := &remindersHandler{svc: svc, kind: route.kind}
		base := "/api" + route.segment
		s.mux.HandleFunc("GET "+base, s.authMiddleware(h.list))
		s.mux.HandleFunc("POST "+base, s.authMiddleware(h.add))
		s.mux.HandleFunc("GET "+base+"/{id}", s.authMiddleware(h.get))
		s.mux.HandleFunc("PATCH "+base+"/{id}", s.authMiddleware(h.update))
		s.mux.HandleFunc("DELETE "+base+"/{id}", s.authMiddleware(h.remove))
		s.mux.HandleFunc("POST "+base+"/{id}/run", s.authMiddleware(h.runNow))
		// Run history is the same file and the same handler cron uses, keyed by
		// the prefixed id, so a reminder gets an identical history panel.
		s.mux.HandleFunc("GET "+base+"/{id}/runs", s.authMiddleware(s.handleCronRuns))
	}
}

// SetReminders attaches a reminders service and its REST routes. It is the
// public entry point hosts call next to SetScheduler.
func (s *Server) SetReminders(svc *reminders.Service) {
	s.attachReminders(svc)
}

// Reminders returns the attached reminders service (nil if none).
func (s *Server) Reminders() *reminders.Service {
	if s == nil {
		return nil
	}
	return s.reminders
}

// statusForErr maps a service error onto an HTTP status. A missing item is a
// 404; an illegal status transition is a 409 (the request was well-formed but
// conflicts with the item's current state); everything else is a 400, which is
// what a validation failure from Add/Update is.
func statusForErr(err error) int {
	switch {
	case errors.Is(err, reminders.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, reminders.ErrTransition):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

// statusChoicesError renders the "unknown status" message for a malformed
// request body. It is phrased for a form field rather than for a state machine:
// the value never reached the machine, so it is not a transition problem.
func statusChoicesError(got reminders.Status) string {
	return fmt.Sprintf("unknown status %q: want one of %s", got, statusChoices())
}

// statusChoices renders the accepted statuses, derived from the package's own
// registry so it cannot drift from the validator.
func statusChoices() string {
	out := ""
	for i, s := range reminders.Statuses() {
		if i > 0 {
			out += "/"
		}
		out += string(s)
	}
	return out
}

// intQuery parses a paging parameter. An empty string means "not supplied"
// (zero, i.e. the default). A malformed value is an error, never a silent 0 —
// silently paging from 0 when the client asked for offset=abc is how you get a
// list that mysteriously repeats its first page.
func intQuery(raw string, dflt int) (int, error) {
	if raw == "" {
		return dflt, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("must be an integer, got %q", raw)
	}
	return n, nil
}

// effectiveLimit mirrors ListFilter.pageSize so the echoed limit is the one
// actually applied, including the clamp.
func effectiveLimit(limit int) int {
	switch {
	case limit == 0:
		return reminders.DefaultPageSize
	case limit < 0:
		return reminders.MaxPageSize
	case limit > reminders.MaxPageSize:
		return reminders.MaxPageSize
	default:
		return limit
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefAction(p *reminders.Action) reminders.Action {
	if p == nil {
		return ""
	}
	return *p
}

func derefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefPermMode(p *scheduler.PermissionMode) scheduler.PermissionMode {
	if p == nil {
		return ""
	}
	return *p
}
