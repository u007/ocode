package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/boxsession"
	"github.com/u007/ocode/internal/browse/cdp"
	"github.com/u007/ocode/internal/upstashbox"
)

// fakeUpstashBoxAPI serves the Upstash box endpoints the boxes handlers call. boxes
// is what GET /v2/box lists; listCode, when non-zero, is the status GET answers
// with instead. calls records "METHOD path" for every request.
type fakeUpstashBoxAPI struct {
	boxes    []map[string]any
	listCode int
	// deleteCode, when non-zero, is the status a box DELETE answers with.
	deleteCode int
	// pauseOK makes POST .../pause succeed. Off, it refuses like an ephemeral box.
	pauseOK bool
	calls   []string
}

func (f *fakeUpstashBoxAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v2/box":
		if f.listCode != 0 {
			w.WriteHeader(f.listCode)
			_, _ = w.Write([]byte(`{"error":"bad key"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(f.boxes)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pause") && f.pauseOK:
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pause"):
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"Ephemeral boxes cannot be paused"}`))
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v2/box/"):
		if f.deleteCode != 0 {
			w.WriteHeader(f.deleteCode)
			_, _ = w.Write([]byte(`{"error":"delete refused"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}
}

// wireUpstashBoxClient points the handler's box client at the fake server.
func wireUpstashBoxClient(t *testing.T, h *Handler, fake *fakeUpstashBoxAPI) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	h.newUpstashBoxClient = func(key string) (upstashBoxAPI, error) {
		return upstashbox.New(key, upstashbox.WithBaseURL(srv.URL))
	}
}

// wireBox is one box in the Upstash wire form.
func wireBox(id string, created time.Time, ephemeral bool, labels []string) map[string]any {
	m := map[string]any{
		"id": id, "name": "name-" + id, "labels": labels, "status": "running",
		"size": "small", "browser": true, "ephemeral": ephemeral,
		"created_at": created.Unix(), "updated_at": created.Unix(),
	}
	if ephemeral {
		m["expires_at"] = created.Add(time.Hour).Unix()
	}
	return m
}

// listedBoxes decodes a boxes response and returns the box ids in order.
func listedBoxes(t *testing.T, raw []byte) (ids []string, body map[string]any) {
	t.Helper()
	body = upstashDecode(t, raw)
	list, ok := body["boxes"].([]any)
	if !ok {
		t.Fatalf("boxes is %T, want an array (%s)", body["boxes"], raw)
	}
	for _, b := range list {
		ids = append(ids, b.(map[string]any)["id"].(string))
	}
	return ids, body
}

// TestUpstashBoxesListJoinsPagesAndSorts pins the list contract: the account's
// boxes joined with the journal, newest first, paged, with the ours flag and a
// boxes array that is never null.
func TestUpstashBoxesListJoinsPagesAndSorts(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("GET /api/upstash/boxes", h.HandleUpstashBoxesList)
	addTestBoxAccount(t, testBoxAccount, "main")

	label, err := h.boxInstallationLabel()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	fake := &fakeUpstashBoxAPI{boxes: []map[string]any{
		wireBox("box-old", now.Add(-3*time.Hour), true, []string{label}),
		wireBox("box-mid", now.Add(-2*time.Hour), false, []string{"someone-else"}),
		wireBox("box-new", now.Add(-time.Hour), true, []string{label}),
	}}
	wireUpstashBoxClient(t, h, fake)

	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := boxsession.NewJournal(path).Add(boxsession.Entry{
		BoxID: "box-old", AccountID: testBoxAccount, SessionID: "ses_old",
		Kind: boxsession.KindEphemeral, Label: label, CreatedAt: now.Add(-3 * time.Hour), ExpiresAt: now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/boxes", nil)
	if code != http.StatusOK {
		t.Fatalf("list status = %d (%s)", code, raw)
	}
	ids, body := listedBoxes(t, raw)
	if strings.Join(ids, ",") != "box-new,box-mid,box-old" {
		t.Fatalf("order = %v, want newest first", ids)
	}
	if body["total"].(float64) != 3 || body["account_id"] != testBoxAccount || body["page_size"].(float64) != 20 {
		t.Fatalf("envelope = %v", body)
	}
	if strings.Contains(string(raw), "ubx-test-key") {
		t.Fatal("the list response carries the API key")
	}
	byID := map[string]map[string]any{}
	for _, b := range body["boxes"].([]any) {
		m := b.(map[string]any)
		byID[m["id"].(string)] = m
	}
	if byID["box-old"]["session_id"] != "ses_old" || byID["box-old"]["ours"] != true || byID["box-old"]["kind"] != "ephemeral" {
		t.Fatalf("box-old = %v, want the journal session and ours", byID["box-old"])
	}
	if byID["box-mid"]["ours"] != false || byID["box-mid"]["kind"] != "persistent" || byID["box-mid"]["session_id"] != "" {
		t.Fatalf("box-mid = %v, want a foreign persistent box with no session", byID["box-mid"])
	}
	if byID["box-new"]["session_id"] != "" || byID["box-new"]["created_at"] == "" {
		t.Fatalf("box-new = %v", byID["box-new"])
	}

	code, raw = upstashDo(t, mux, http.MethodGet, "/api/upstash/boxes?page=2&page_size=2", nil)
	if code != http.StatusOK {
		t.Fatalf("page 2 status = %d", code)
	}
	if ids, _ := listedBoxes(t, raw); strings.Join(ids, ",") != "box-old" {
		t.Fatalf("page 2 = %v, want box-old", ids)
	}

	code, raw = upstashDo(t, mux, http.MethodGet, "/api/upstash/boxes?page=9&page_size=2", nil)
	if code != http.StatusOK {
		t.Fatalf("page past the end status = %d", code)
	}
	if ids, body := listedBoxes(t, raw); len(ids) != 0 || body["total"].(float64) != 3 || !strings.Contains(string(raw), `"boxes":[]`) {
		t.Fatalf("page past the end = %s, want an empty array and the real total", raw)
	}

	for _, q := range []string{"page=0", "page=x", "page_size=0", "page_size=101"} {
		if code, _ := upstashDo(t, mux, http.MethodGet, "/api/upstash/boxes?"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, code)
		}
	}
}

// TestUpstashBoxesNoAccountIsAnEmptyList pins the no-account case: 200, an empty
// array and an empty account_id, with no upstream call.
func TestUpstashBoxesNoAccountIsAnEmptyList(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("GET /api/upstash/boxes", h.HandleUpstashBoxesList)
	code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/boxes", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d (%s)", code, raw)
	}
	if !strings.Contains(string(raw), `"boxes":[]`) || !strings.Contains(string(raw), `"account_id":""`) {
		t.Fatalf("no-account body = %s, want an empty array and an empty account_id", raw)
	}
}

// TestUpstashBoxesUnknownAccountIs404 pins that a named account must exist.
func TestUpstashBoxesUnknownAccountIs404(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("GET /api/upstash/boxes", h.HandleUpstashBoxesList)
	addTestBoxAccount(t, testBoxAccount, "main")
	code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/boxes?account=ub_nope", nil)
	if code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", code, raw)
	}
	if upstashDecode(t, raw)["error"] == nil {
		t.Fatalf("the 404 has no JSON error: %s", raw)
	}
}

// TestUpstashBoxesUpstreamFailureIs502 pins that an upstream 401 is not passed
// through, because the web client reads a 401 as an expired ocode login.
func TestUpstashBoxesUpstreamFailureIs502(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("GET /api/upstash/boxes", h.HandleUpstashBoxesList)
	addTestBoxAccount(t, testBoxAccount, "main")
	wireUpstashBoxClient(t, h, &fakeUpstashBoxAPI{listCode: http.StatusUnauthorized})
	code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/boxes", nil)
	if code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (%s)", code, raw)
	}
	if strings.Contains(string(raw), "ubx-test-key") {
		t.Fatal("the failure response carries the API key")
	}
}

// TestUpstashBoxPauseRefusalPassesThrough pins that an upstream 4xx keeps its
// status: an ephemeral box cannot be paused, and the API says so with a 400.
func TestUpstashBoxPauseRefusalPassesThrough(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("POST /api/upstash/boxes/{id}/pause", h.HandleUpstashBoxPause)
	addTestBoxAccount(t, testBoxAccount, "main")
	wireUpstashBoxClient(t, h, &fakeUpstashBoxAPI{})
	code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/boxes/box-old/pause", nil)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", code, raw)
	}
	if !strings.Contains(string(raw), "Ephemeral boxes cannot be paused") {
		t.Fatalf("the upstream message is lost: %s", raw)
	}
}

// TestUpstashBoxDeleteOwnedGoesThroughTheManager pins that a box the live manager
// owns is terminated by the manager, which also clears its journal row, and that
// the HTTP API is never asked to delete it.
func TestUpstashBoxDeleteOwnedGoesThroughTheManager(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("DELETE /api/upstash/boxes/{id}", h.HandleUpstashBoxDelete)
	addTestBoxAccount(t, testBoxAccount, "main")
	fake := &fakeUpstashBoxAPI{}
	wireUpstashBoxClient(t, h, fake)

	api := &fakeBoxAPI{}
	mgr := newTestBoxManager(t, api, "ocode-aaaaaaaa")
	h.boxes = mgr
	if _, err := mgr.Start(context.Background(), "ses_own", boxsession.StartOptions{}); err != nil {
		t.Fatal(err)
	}

	code, raw := upstashDo(t, mux, http.MethodDelete, "/api/upstash/boxes/box-1", nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete status = %d (%s)", code, raw)
	}
	if len(api.deleted) != 1 || api.deleted[0] != "box-1" {
		t.Fatalf("the manager did not delete the owned box: %v", api.deleted)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("the HTTP API was asked for an owned box: %v", fake.calls)
	}
	if got := mgr.Statuses(); len(got) != 0 {
		t.Fatalf("the owned box is still live: %+v", got)
	}
	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := boxsession.NewJournal(path).List(); err != nil || len(rows) != 0 {
		t.Fatalf("the journal still holds the owned box: %v, %v", rows, err)
	}
}

// TestUpstashBoxDeleteForeignTouchesOnlyThatBox pins that deleting a box the
// manager does not own deletes it through the API and removes only its own journal
// row, leaving every other row.
func TestUpstashBoxDeleteForeignTouchesOnlyThatBox(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("DELETE /api/upstash/boxes/{id}", h.HandleUpstashBoxDelete)
	addTestBoxAccount(t, testBoxAccount, "main")
	fake := &fakeUpstashBoxAPI{}
	wireUpstashBoxClient(t, h, fake)

	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	journal := boxsession.NewJournal(path)
	now := time.Now()
	for _, id := range []string{"box-other", "box-foreign"} {
		if err := journal.Add(boxsession.Entry{
			BoxID: id, AccountID: testBoxAccount, SessionID: "ses_" + id, Kind: boxsession.KindEphemeral,
			Label: "ocode-aaaaaaaa", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}

	code, raw := upstashDo(t, mux, http.MethodDelete, "/api/upstash/boxes/box-foreign", nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete status = %d (%s)", code, raw)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "DELETE /v2/box/box-foreign" {
		t.Fatalf("upstream calls = %v, want one delete of box-foreign", fake.calls)
	}
	rows, err := journal.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].BoxID != "box-other" {
		t.Fatalf("journal = %+v, want only box-other left", rows)
	}
}

// retainedBoxAPI is a boxsession.BoxAPI for a retained box. Create makes a running
// box, Get reads its status, Pause and Delete are recorded, so a test can tell a
// pause from a delete.
type retainedBoxAPI struct {
	created int
	status  map[string]string
	paused  []string
	deleted []string
}

var _ boxsession.BoxAPI = (*retainedBoxAPI)(nil)

func (f *retainedBoxAPI) Create(_ context.Context, opts upstashbox.CreateOptions) (*upstashbox.Box, error) {
	f.created++
	id := fmt.Sprintf("box-%d", f.created)
	if f.status == nil {
		f.status = map[string]string{}
	}
	f.status[id] = "running"
	now := time.Now().UTC()
	return &upstashbox.Box{ID: id, Labels: opts.Labels, Status: "running", CreatedAt: now, UpdatedAt: now}, nil
}

func (f *retainedBoxAPI) Get(_ context.Context, id string) (*upstashbox.Box, error) {
	st, ok := f.status[id]
	if !ok {
		return nil, fmt.Errorf("retainedBoxAPI: Get(%s): no such box", id)
	}
	return &upstashbox.Box{ID: id, Status: st}, nil
}

func (f *retainedBoxAPI) Pause(_ context.Context, id string) error {
	f.paused = append(f.paused, id)
	f.status[id] = upstashbox.StatusPaused
	return nil
}

func (f *retainedBoxAPI) Delete(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	delete(f.status, id)
	return nil
}

func (f *retainedBoxAPI) CDPURL(_ context.Context, _ string) (upstashbox.CDPURL, error) {
	return upstashbox.CDPURL{}, nil
}

// newRetainTestManager builds a manager whose default account keeps a retained box
// for a day, with a login state store and the login state "work" for every session.
// It runs no reaper: the tests drive it directly.
func newRetainTestManager(t *testing.T, api boxsession.BoxAPI, label string) *boxsession.Manager {
	t.Helper()
	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	return boxsession.New(boxsession.Deps{
		Account: func() (boxsession.Account, error) {
			return boxsession.Account{ID: testBoxAccount, APIKey: "ubx-test-key", Idle: time.Hour, HardCap: 2 * time.Hour, Retain: 24 * time.Hour}, nil
		},
		APIKeyFor: func(string) (string, error) { return "ubx-test-key", nil },
		NewAPI:    func(string) (boxsession.BoxAPI, error) { return api, nil },
		Dial: func(context.Context, string) (*cdp.Conn, error) {
			return nil, errors.New("tests do not dial a browser")
		},
		Busy:          func(string) bool { return false },
		Journal:       boxsession.NewJournal(path),
		Label:         label,
		LoginStates:   boxsession.NewLoginStateStore(t.TempDir()),
		LoginStateFor: func(string) (string, error) { return "work", nil },
	})
}

// TestUpstashBoxesListShowsARetainedRow pins the retained fields of a journaled box
// that is not live here: its kind, delete-at, login state and last use come from the
// journal row, not from the API.
func TestUpstashBoxesListShowsARetainedRow(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("GET /api/upstash/boxes", h.HandleUpstashBoxesList)
	addTestBoxAccount(t, testBoxAccount, "main")

	label, err := h.boxInstallationLabel()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	wireUpstashBoxClient(t, h, &fakeUpstashBoxAPI{boxes: []map[string]any{
		wireBox("box-ret", now.Add(-time.Hour), false, []string{label}),
	}})
	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	row := boxsession.Entry{
		BoxID: "box-ret", AccountID: testBoxAccount, SessionID: "ses_ret", Kind: boxsession.KindRetained,
		Label: label, CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(6 * 24 * time.Hour),
		LoginState: "work", Paused: true, LastUsed: now.Add(-2 * time.Hour),
	}
	if err := boxsession.NewJournal(path).Add(row); err != nil {
		t.Fatal(err)
	}

	code, raw := upstashDo(t, mux, http.MethodGet, "/api/upstash/boxes", nil)
	if code != http.StatusOK {
		t.Fatalf("list status = %d (%s)", code, raw)
	}
	_, body := listedBoxes(t, raw)
	var got map[string]any
	for _, b := range body["boxes"].([]any) {
		if m := b.(map[string]any); m["id"] == "box-ret" {
			got = m
		}
	}
	if got == nil {
		t.Fatalf("box-ret is not listed: %s", raw)
	}
	if got["kind"] != boxsession.KindRetained {
		t.Errorf("kind = %v, want %q", got["kind"], boxsession.KindRetained)
	}
	if got["expires_at"] != rfc3339OrEmpty(row.ExpiresAt) {
		t.Errorf("expires_at = %v, want the row's delete-at %s", got["expires_at"], rfc3339OrEmpty(row.ExpiresAt))
	}
	if got["login_state"] != "work" {
		t.Errorf("login_state = %v, want work", got["login_state"])
	}
	if got["last_used"] != rfc3339OrEmpty(row.LastUsed) {
		t.Errorf("last_used = %v, want the row's last use %s", got["last_used"], rfc3339OrEmpty(row.LastUsed))
	}
	if got["session_id"] != "ses_ret" {
		t.Errorf("session_id = %v, want ses_ret", got["session_id"])
	}
}

// TestUpstashBoxDeleteLiveRetainedDeletesIt pins that deleting a LIVE retained box
// from the settings UI deletes it through the API. The manager's Terminate would
// only pause it, so the box must not be paused, and the live side and journal row
// are forgotten afterwards.
func TestUpstashBoxDeleteLiveRetainedDeletesIt(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("DELETE /api/upstash/boxes/{id}", h.HandleUpstashBoxDelete)
	addTestBoxAccount(t, testBoxAccount, "main")
	fake := &fakeUpstashBoxAPI{}
	wireUpstashBoxClient(t, h, fake)

	label, err := h.boxInstallationLabel()
	if err != nil {
		t.Fatal(err)
	}
	api := &retainedBoxAPI{}
	mgr := newRetainTestManager(t, api, label)
	h.boxes = mgr
	st, err := mgr.Start(context.Background(), "ses_ret", boxsession.StartOptions{Retain: true})
	if err != nil || st.BoxID != "box-1" || st.Kind != boxsession.KindRetained {
		t.Fatalf("retained start = %+v, %v; want a retained box-1", st, err)
	}

	code, raw := upstashDo(t, mux, http.MethodDelete, "/api/upstash/boxes/box-1", nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete status = %d (%s)", code, raw)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "DELETE /v2/box/box-1" {
		t.Fatalf("upstream calls = %v, want one delete of box-1", fake.calls)
	}
	if len(api.paused) != 0 {
		t.Fatalf("the live retained box was paused instead of deleted: %v", api.paused)
	}
	if got := mgr.Statuses(); len(got) != 0 {
		t.Fatalf("the deleted box is still live: %+v", got)
	}
	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := boxsession.NewJournal(path).List(); err != nil || len(rows) != 0 {
		t.Fatalf("the journal still holds the deleted box: %v, %v", rows, err)
	}
}

// TestUpstashBoxPauseNotesARetainedBox pins that pausing a live retained box from the
// settings UI is done by its manager: the manager detaches the session, pauses the
// box through its own API client, and marks the journal row paused. The HTTP API is
// not called, so the pause cannot land before the detach.
func TestUpstashBoxPauseNotesARetainedBox(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("POST /api/upstash/boxes/{id}/pause", h.HandleUpstashBoxPause)
	addTestBoxAccount(t, testBoxAccount, "main")
	fake := &fakeUpstashBoxAPI{pauseOK: true}
	wireUpstashBoxClient(t, h, fake)

	label, err := h.boxInstallationLabel()
	if err != nil {
		t.Fatal(err)
	}
	api := &retainedBoxAPI{}
	mgr := newRetainTestManager(t, api, label)
	h.boxes = mgr
	if _, err := mgr.Start(context.Background(), "ses_ret", boxsession.StartOptions{Retain: true}); err != nil {
		t.Fatal(err)
	}

	code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/boxes/box-1/pause", nil)
	if code != http.StatusNoContent {
		t.Fatalf("pause status = %d (%s)", code, raw)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("the HTTP API was called for a retained box: %v; the manager owns its pause", fake.calls)
	}
	if len(api.paused) != 1 || api.paused[0] != "box-1" {
		t.Fatalf("manager pauses = %v, want one pause of box-1", api.paused)
	}
	if got := mgr.Statuses(); len(got) != 0 {
		t.Fatalf("the paused box is still live: %+v", got)
	}
	if _, err := mgr.Browser("ses_ret"); !errors.Is(err, boxsession.ErrPaused) {
		t.Fatalf("the session after a settings pause: Browser = %v, want ErrPaused", err)
	}
	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := boxsession.NewJournal(path).List()
	if err != nil || len(rows) != 1 || !rows[0].Paused {
		t.Fatalf("journal = %+v, %v; want the retained row marked paused", rows, err)
	}
}

// TestUpstashBoxResumeOfARetainedBoxIs409 pins that the settings Resume refuses a
// journaled retained box with a 409 and the reason, and sends nothing upstream: a
// resume through the API alone would leave the journal row saying paused.
func TestUpstashBoxResumeOfARetainedBoxIs409(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("POST /api/upstash/boxes/{id}/resume", h.HandleUpstashBoxResume)
	addTestBoxAccount(t, testBoxAccount, "main")
	fake := &fakeUpstashBoxAPI{}
	wireUpstashBoxClient(t, h, fake)

	now := time.Now().UTC()
	path, err := upstashBoxJournalPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := boxsession.NewJournal(path).Add(boxsession.Entry{
		BoxID: "box-ret", AccountID: testBoxAccount, SessionID: "ses_ret", Kind: boxsession.KindRetained,
		LoginState: "work", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), Paused: true, LastUsed: now,
	}); err != nil {
		t.Fatal(err)
	}

	code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/boxes/box-ret/resume", nil)
	if code != http.StatusConflict {
		t.Fatalf("resume status = %d (%s), want 409", code, raw)
	}
	if !strings.Contains(string(raw), "a retained box is resumed by a browser start with retain in a session that uses its login state") {
		t.Fatalf("the refusal does not say how to resume: %s", raw)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("upstream calls = %v, want none for a retained box", fake.calls)
	}
}

// TestUpstashBoxPauseOfAForeignBoxIsNotNoted pins that a pause of a box this
// installation did not make reaches only the API. The manager has no row to mark, so a
// note there would fail a pause that worked.
func TestUpstashBoxPauseOfAForeignBoxIsNotNoted(t *testing.T) {
	mux, h := upstashTestServer(t)
	mux.HandleFunc("POST /api/upstash/boxes/{id}/pause", h.HandleUpstashBoxPause)
	addTestBoxAccount(t, testBoxAccount, "main")
	fake := &fakeUpstashBoxAPI{pauseOK: true}
	wireUpstashBoxClient(t, h, fake)

	label, err := h.boxInstallationLabel()
	if err != nil {
		t.Fatal(err)
	}
	mgr := newRetainTestManager(t, &retainedBoxAPI{}, label)
	h.boxes = mgr

	code, raw := upstashDo(t, mux, http.MethodPost, "/api/upstash/boxes/box-foreign/pause", nil)
	if code != http.StatusNoContent {
		t.Fatalf("pause status = %d (%s), want the API pause to pass", code, raw)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "POST /v2/box/box-foreign/pause" {
		t.Fatalf("upstream calls = %v, want one pause of box-foreign", fake.calls)
	}
}
