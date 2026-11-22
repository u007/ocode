package server

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/upstashbox"
)

// Upstash Box listing and actions for the web Boxes panel. The list is every box
// the account holds, joined with this installation's journal and the live box
// manager. Upstream calls use the account's key, which never leaves the server.

const (
	upstashBoxesDefaultPageSize = 20
	upstashBoxesMaxPageSize     = 100
	// upstashBoxRequestTimeout bounds one call to the Upstash Box API.
	upstashBoxRequestTimeout = 15 * time.Second
)

// errRetainedBoxResume refuses a resume of a retained box through the API. It is
// shown to the user as is, with status 409.
var errRetainedBoxResume = errors.New("a retained box is resumed by a browser start with retain in a session that uses its login state")

// upstashBoxAPI is the subset of *upstashbox.Client the boxes endpoints use.
type upstashBoxAPI interface {
	List(ctx context.Context, label string) ([]upstashbox.Box, error)
	Pause(ctx context.Context, id string) error
	Resume(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

// *upstashbox.Client is the production upstashBoxAPI. This assertion fails the build if its method set drifts.
var _ upstashBoxAPI = (*upstashbox.Client)(nil)

// upstashBoxView is one box as GET /api/upstash/boxes returns it. Times are RFC
// 3339 in UTC, or empty when unknown. session_id and last_used come only from
// this process's journal and live manager, so they are empty for other boxes.
type upstashBoxView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Size      string `json:"size"`
	Kind      string `json:"kind"`
	Ours      bool   `json:"ours"`
	SessionID string `json:"session_id"`
	LastUsed  string `json:"last_used"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
	// LoginState is the login state a running box of this installation was started
	// with. Empty for none, for a box that is not running here, and for other boxes.
	LoginState string `json:"login_state"`
}

// upstashBoxesResponse is the body of GET /api/upstash/boxes. Boxes is never null.
type upstashBoxesResponse struct {
	Boxes     []upstashBoxView `json:"boxes"`
	Total     int              `json:"total"`
	Page      int              `json:"page"`
	PageSize  int              `json:"page_size"`
	AccountID string           `json:"account_id"`
}

// upstashBoxClient builds the API client for one key. A test replaces it through
// Handler.newUpstashBoxClient.
func (h *Handler) upstashBoxClient(apiKey string) (upstashBoxAPI, error) {
	if h.newUpstashBoxClient != nil {
		return h.newUpstashBoxClient(apiKey)
	}
	c, err := upstashbox.New(apiKey)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// upstashBoxAccountFromQuery resolves ?account=. A named account must exist: an
// unknown id is config.ErrUpstashBoxNotFound. An empty value means the default
// account, which is found=false when none is configured.
func upstashBoxAccountFromQuery(raw string) (acc config.UpstashBoxAccount, found bool, err error) {
	cfg, err := config.LoadUpstashBoxConfig()
	if err != nil {
		return config.UpstashBoxAccount{}, false, err
	}
	if raw != "" {
		acc, ok := cfg.FindAccount(raw)
		if !ok {
			return config.UpstashBoxAccount{}, false, config.ErrUpstashBoxNotFound
		}
		return acc, true, nil
	}
	acc, found = cfg.DefaultAccount()
	return acc, found, nil
}

// upstashBoxesAccount resolves the request's account and writes the error
// response when it cannot. ok is false only when a response was written.
func upstashBoxesAccount(w http.ResponseWriter, r *http.Request) (acc config.UpstashBoxAccount, found, ok bool) {
	acc, found, err := upstashBoxAccountFromQuery(r.URL.Query().Get("account"))
	if err != nil {
		writeError(w, upstashErrStatus(err), err.Error())
		return config.UpstashBoxAccount{}, false, false
	}
	return acc, found, true
}

// upstashBoxClientFor returns the client for an account's key. ok is false when a
// response was written: 409 when the account has no key, 500 when the client cannot be built.
func (h *Handler) upstashBoxClientFor(w http.ResponseWriter, acc config.UpstashBoxAccount) (upstashBoxAPI, bool) {
	key, ok := auth.UpstashBoxAPIKey(acc.ID)
	if !ok {
		writeError(w, http.StatusConflict, "the Upstash Box account has no API key")
		return nil, false
	}
	client, err := h.upstashBoxClient(key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "build the Upstash Box client: "+err.Error())
		return nil, false
	}
	return client, true
}

// upstashBoxesPaging reads page and page_size. Bad values are a 400.
func upstashBoxesPaging(w http.ResponseWriter, r *http.Request) (page, pageSize int, ok bool) {
	page, pageSize = 1, upstashBoxesDefaultPageSize
	q := r.URL.Query()
	if raw := q.Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "page must be a positive integer")
			return 0, 0, false
		}
		page = n
	}
	if raw := q.Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > upstashBoxesMaxPageSize {
			writeError(w, http.StatusBadRequest, "page_size must be between 1 and 100")
			return 0, 0, false
		}
		pageSize = n
	}
	return page, pageSize, true
}

// upstashBoxesPage returns one page of items. A page past the end is an empty
// slice, never nil.
func upstashBoxesPage[T any](items []T, page, pageSize int) []T {
	pages := (len(items) + pageSize - 1) / pageSize
	if page > pages {
		return []T{}
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, len(items))
	return append([]T{}, items[start:end]...)
}

// upstashBoxJournal returns the journal at the global data directory.
func upstashBoxJournal() (*boxsession.Journal, error) {
	path, err := upstashBoxJournalPath()
	if err != nil {
		return nil, err
	}
	return boxsession.NewJournal(path), nil
}

// rfc3339OrEmpty formats t in UTC, or returns "" for the zero time.
func rfc3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// upstashBoxViewOf joins one box with the journal and the live manager.
func upstashBoxViewOf(b upstashbox.Box, label string, live map[string]boxsession.Status, journaled map[string]boxsession.Entry) upstashBoxView {
	v := upstashBoxView{
		ID:        b.ID,
		Name:      b.Name,
		Status:    b.Status,
		Size:      b.Size,
		Kind:      "persistent",
		Ours:      slices.Contains(b.Labels, label),
		ExpiresAt: rfc3339OrEmpty(b.ExpiresAt),
		CreatedAt: rfc3339OrEmpty(b.CreatedAt),
	}
	if b.Ephemeral {
		v.Kind = boxsession.KindEphemeral
	}
	e, isJournaled := journaled[b.ID]
	if isJournaled {
		// The journal knows what ocode made the box as; the API only knows its lifetime.
		if e.Kind != "" {
			v.Kind = e.Kind
		}
		v.SessionID = e.SessionID
	}
	st, isLive := live[b.ID]
	if isLive {
		v.SessionID = st.SessionID
		v.LastUsed = rfc3339OrEmpty(st.LastUsed)
		v.LoginState = st.LoginState
	}
	if isJournaled && e.Kind == boxsession.KindRetained {
		// A retained box is deleted at its journal row's delete-at, not at the API's expiry.
		v.ExpiresAt = rfc3339OrEmpty(e.ExpiresAt)
		if !isLive {
			v.LoginState = e.LoginState
			v.LastUsed = rfc3339OrEmpty(e.LastUsed)
		}
	}
	return v
}

// HandleUpstashBoxesList serves GET /api/upstash/boxes: every box of the account,
// newest first, one page at a time.
func (h *Handler) HandleUpstashBoxesList(w http.ResponseWriter, r *http.Request) {
	page, pageSize, ok := upstashBoxesPaging(w, r)
	if !ok {
		return
	}
	acc, found, ok := upstashBoxesAccount(w, r)
	if !ok {
		return
	}
	resp := upstashBoxesResponse{Boxes: []upstashBoxView{}, Page: page, PageSize: pageSize}
	if !found {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.AccountID = acc.ID

	client, ok := h.upstashBoxClientFor(w, acc)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstashBoxRequestTimeout)
	defer cancel()
	boxes, err := client.List(ctx, "")
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	label, err := h.boxInstallationLabel()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	journal, err := upstashBoxJournal()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries, err := journal.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	journaled := make(map[string]boxsession.Entry, len(entries))
	for _, e := range entries {
		journaled[e.BoxID] = e
	}
	live := map[string]boxsession.Status{}
	if mgr := h.boxManagerIfBuilt(); mgr != nil {
		for _, st := range mgr.Statuses() {
			live[st.BoxID] = st
		}
	}

	slices.SortStableFunc(boxes, func(a, b upstashbox.Box) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(a.ID, b.ID))
	})
	views := make([]upstashBoxView, 0, len(boxes))
	for _, b := range boxes {
		views = append(views, upstashBoxViewOf(b, label, live, journaled))
	}
	resp.Total = len(views)
	resp.Boxes = upstashBoxesPage(views, page, pageSize)
	writeJSON(w, http.StatusOK, resp)
}

// upstashBoxIDOf reads the box id from the path. Ids that would change the path are a 400.
func upstashBoxIDOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if id == "" || strings.ContainsAny(id, "/?# \t\r\n") {
		writeError(w, http.StatusBadRequest, "invalid box id")
		return "", false
	}
	return id, true
}

// upstashBoxOp runs one upstream action on a box of the request's account.
func (h *Handler) upstashBoxOp(w http.ResponseWriter, r *http.Request, op func(ctx context.Context, c upstashBoxAPI, id string) error) {
	id, ok := upstashBoxIDOf(w, r)
	if !ok {
		return
	}
	acc, found, ok := upstashBoxesAccount(w, r)
	if !ok {
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no Upstash Box account is configured; add one in Settings")
		return
	}
	client, ok := h.upstashBoxClientFor(w, acc)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstashBoxRequestTimeout)
	defer cancel()
	if err := op(ctx, client, id); err != nil {
		writeError(w, upstashBoxHTTPStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleUpstashBoxPause serves POST /api/upstash/boxes/{id}/pause. A retained box of
// this installation is paused by its manager, which detaches its live session first.
// Any other box is paused through the API.
func (h *Handler) HandleUpstashBoxPause(w http.ResponseWriter, r *http.Request) {
	h.upstashBoxOp(w, r, func(ctx context.Context, c upstashBoxAPI, id string) error {
		handled, err := h.pauseRetainedBox(ctx, id)
		if err != nil || handled {
			return err
		}
		return c.Pause(ctx, id)
	})
}

// pauseRetainedBox pauses a retained box of this installation through its manager.
// handled is false for any other box. The manager is built here when the journal
// holds the box and no manager exists yet: the box's live session, if any, must be
// detached through it, never left to a second manager.
func (h *Handler) pauseRetainedBox(ctx context.Context, id string) (bool, error) {
	retained, err := journaledRetained(id)
	if err != nil || !retained {
		return false, err
	}
	mgr, err := h.boxManager()
	if err != nil {
		return true, err
	}
	return mgr.PauseBox(ctx, id)
}

// journaledRetained reports whether this installation's journal holds a retained box
// with id.
func journaledRetained(id string) (bool, error) {
	journal, err := upstashBoxJournal()
	if err != nil {
		return false, err
	}
	rows, err := journal.List()
	if err != nil {
		return false, err
	}
	for _, e := range rows {
		if e.BoxID == id && e.Kind == boxsession.KindRetained {
			return true, nil
		}
	}
	return false, nil
}

// HandleUpstashBoxResume serves POST /api/upstash/boxes/{id}/resume. A retained box is
// refused: its resume is a browser start, which owns the journal row and the live
// side. A resume through the API alone would leave the row saying paused.
func (h *Handler) HandleUpstashBoxResume(w http.ResponseWriter, r *http.Request) {
	h.upstashBoxOp(w, r, func(ctx context.Context, c upstashBoxAPI, id string) error {
		retained, err := journaledRetained(id)
		if err != nil {
			return err
		}
		if retained {
			return errRetainedBoxResume
		}
		return c.Resume(ctx, id)
	})
}

// HandleUpstashBoxDelete serves DELETE /api/upstash/boxes/{id}. A live ephemeral box
// is terminated through the manager, which closes its browser and journal row. Any
// other box, a live retained one included, is deleted through the API, and then the
// manager (or the journal, with no manager) forgets it.
func (h *Handler) HandleUpstashBoxDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := upstashBoxIDOf(w, r)
	if !ok {
		return
	}
	acc, found, ok := upstashBoxesAccount(w, r)
	if !ok {
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no Upstash Box account is configured; add one in Settings")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), upstashBoxRequestTimeout)
	defer cancel()
	if h.deleteLiveBox(ctx, w, id) {
		return
	}

	client, ok := h.upstashBoxClientFor(w, acc)
	if !ok {
		return
	}
	if err := client.Delete(ctx, id); err != nil {
		writeError(w, upstashBoxHTTPStatus(err), err.Error())
		return
	}
	if err := h.forgetDeletedBox(id); err != nil {
		writeError(w, http.StatusInternalServerError, "the box was deleted but its local record could not be cleared: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// forgetDeletedBox clears what this installation knows of a box the API just
// deleted: its journal row, and any live side the manager holds. The manager's
// ForgetBox makes no API call. Without a manager the row is removed directly.
func (h *Handler) forgetDeletedBox(id string) error {
	if mgr := h.boxManagerIfBuilt(); mgr != nil {
		return mgr.ForgetBox(id)
	}
	journal, err := upstashBoxJournal()
	if err != nil {
		return err
	}
	return journal.Remove(id)
}

// deleteLiveBox ends an ephemeral box this process's manager owns, through the
// manager, which deletes it and closes its browser. It reports whether it answered
// the request. A box the manager no longer holds (ErrNotStarted, ErrExpired) is not
// an answer: the caller deletes it through the API. A retained box is not handled
// here at all: the manager's Terminate would only pause it, so the caller deletes it
// through the API and then forgets it.
func (h *Handler) deleteLiveBox(ctx context.Context, w http.ResponseWriter, id string) bool {
	mgr := h.boxManagerIfBuilt()
	if mgr == nil {
		return false
	}
	sessionID := ""
	for _, st := range mgr.Statuses() {
		if st.BoxID == id {
			if st.Kind != boxsession.KindEphemeral {
				return false
			}
			sessionID = st.SessionID
			break
		}
	}
	if sessionID == "" {
		return false
	}
	err := mgr.Terminate(ctx, sessionID)
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	if errors.Is(err, boxsession.ErrNotStarted) || errors.Is(err, boxsession.ErrExpired) {
		return false
	}
	writeError(w, http.StatusBadGateway, err.Error())
	return true
}
