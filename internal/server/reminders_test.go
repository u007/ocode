package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/reminders"
	"github.com/u007/ocode/internal/scheduler"
)

// newRemindersTestServer builds a Server with the reminder/task routes
// registered, backed by a service with no firing loop running (so nothing
// fires on its own) and no sinks.
func newRemindersTestServer(t *testing.T) (*Server, *reminders.Service) {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "reminders.json")
	svc := reminders.NewService(storePath)
	// Load the (empty) store so lookups work, but do NOT Start the run loop:
	// a background loop would make the list change underneath a test.
	if err := svc.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(svc.Stop)

	srv := &Server{mux: http.NewServeMux(), workDir: dir}
	srv.attachReminders(svc)
	return srv, svc
}

func doRemindersJSON(t *testing.T, srv *Server, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, httptest.NewRequest(method, path, reader))
	var out map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &out)
	}
	return w, out
}

func mustPostItem(t *testing.T, srv *Server, path string, body map[string]any) map[string]any {
	t.Helper()
	w, out := doRemindersJSON(t, srv, "POST", path, body)
	if w.Code != http.StatusOK {
		t.Fatalf("POST %s: %d %s", path, w.Code, w.Body.String())
	}
	return out
}

// TestRemindersCRUD walks the whole create → read → patch → delete cycle over
// the real mux, so the route patterns and the JSON shapes are exercised
// together rather than only the service underneath.
func TestRemindersCRUD(t *testing.T) {
	srv, _ := newRemindersTestServer(t)

	created := mustPostItem(t, srv, "/api/reminders", map[string]any{
		"title":     "call the dentist",
		"message":   "it has been six months",
		"due_at_ms": float64(1_800_000_000_000),
	})
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %v", created)
	}
	if created["status"] != "pending" {
		t.Fatalf("status = %v, want pending", created["status"])
	}
	if created["kind"] != "reminder" {
		t.Fatalf("kind = %v, want reminder", created["kind"])
	}

	w, got := doRemindersJSON(t, srv, "GET", "/api/reminders/"+id, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	if got["title"] != "call the dentist" {
		t.Fatalf("title = %v, want the created title", got["title"])
	}

	w, patched := doRemindersJSON(t, srv, "PATCH", "/api/reminders/"+id, map[string]any{"title": "book the dentist"})
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", w.Code, w.Body.String())
	}
	if patched["title"] != "book the dentist" {
		t.Fatalf("patched title = %v", patched["title"])
	}
	// A one-field patch must not reset the rest.
	if patched["message"] != "it has been six months" {
		t.Fatalf("message = %v after a title-only patch; a one-field patch reset the others", patched["message"])
	}

	w, _ = doRemindersJSON(t, srv, "DELETE", "/api/reminders/"+id, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d", w.Code)
	}
	w, _ = doRemindersJSON(t, srv, "GET", "/api/reminders/"+id, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET after delete = %d, want 404", w.Code)
	}
}

// TestReminderStatusTransitionsOverHTTP is the "mark / unmark" round trip the
// feature was asked for, exercised through the endpoint the UI calls.
func TestReminderStatusTransitionsOverHTTP(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	created := mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "write the report"})
	id := created["id"].(string)

	steps := []string{"in_progress", "completed", "pending", "cancelled", "pending"}
	for _, want := range steps {
		w, got := doRemindersJSON(t, srv, "PATCH", "/api/tasks/"+id, map[string]any{"status": want})
		if w.Code != http.StatusOK {
			t.Fatalf("set %s: %d %s", want, w.Code, w.Body.String())
		}
		if got["status"] != want {
			t.Fatalf("status = %v, want %v", got["status"], want)
		}
		// A repeat must be idempotent, not a 409: a double-clicked button or a
		// retried PATCH is not a user error.
		w, _ = doRemindersJSON(t, srv, "PATCH", "/api/tasks/"+id, map[string]any{"status": want})
		if w.Code != http.StatusOK {
			t.Fatalf("idempotent set %s: %d %s", want, w.Code, w.Body.String())
		}
	}
}

// TestIllegalStatusTransitionIs409: completed -> cancelled is not a single legal
// step, and the client must be able to tell that apart from a 400 validation
// failure so it can re-fetch and re-render instead of showing a field error.
func TestIllegalStatusTransitionIs409(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	id := mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "one step only"})["id"].(string)

	w, _ := doRemindersJSON(t, srv, "PATCH", "/api/tasks/"+id, map[string]any{"status": "completed"})
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d", w.Code)
	}
	w, body := doRemindersJSON(t, srv, "PATCH", "/api/tasks/"+id, map[string]any{"status": "cancelled"})
	if w.Code != http.StatusConflict {
		t.Fatalf("completed -> cancelled: %d, want 409 (%s)", w.Code, w.Body.String())
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Fatal("409 carried no error message")
	}
}

// TestUnknownStatusIsRejected: a value outside the registry is a 400, never a
// silently coerced default.
func TestUnknownStatusIsRejected(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	id := mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "strict"})["id"].(string)
	w, body := doRemindersJSON(t, srv, "PATCH", "/api/tasks/"+id, map[string]any{"status": "archived"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=archived: %d, want 400", w.Code)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Fatal("400 carried no error message")
	}
}

// TestRejectedStatusChangeLeavesFieldsUnapplied: the status is validated BEFORE
// the field edits, so a bad status change cannot half-apply a patch.
func TestRejectedStatusChangeLeavesFieldsUnapplied(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	id := mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "original"})["id"].(string)
	doRemindersJSON(t, srv, "PATCH", "/api/tasks/"+id, map[string]any{"status": "completed"})

	w, _ := doRemindersJSON(t, srv, "PATCH", "/api/tasks/"+id, map[string]any{
		"title":  "renamed anyway",
		"status": "cancelled",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", w.Code)
	}
	_, got := doRemindersJSON(t, srv, "GET", "/api/tasks/"+id, nil)
	if got["title"] != "original" {
		t.Fatalf("title = %v; the field edit was applied despite the rejected status change", got["title"])
	}
	if got["status"] != "completed" {
		t.Fatalf("status = %v, want completed", got["status"])
	}
}

// TestKindsDoNotBleedAcrossRoutes: a task id is not a reminder id. Reporting it
// as anything but 404 would let a stale client edit the wrong collection.
func TestKindsDoNotBleedAcrossRoutes(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	taskID := mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "a task"})["id"].(string)

	w, _ := doRemindersJSON(t, srv, "GET", "/api/reminders/"+taskID, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /api/reminders/<task id> = %d, want 404", w.Code)
	}
	w, _ = doRemindersJSON(t, srv, "PATCH", "/api/reminders/"+taskID, map[string]any{"title": "hijack"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("PATCH /api/reminders/<task id> = %d, want 404", w.Code)
	}
	w, _ = doRemindersJSON(t, srv, "DELETE", "/api/reminders/"+taskID, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE /api/reminders/<task id> = %d, want 404", w.Code)
	}
	// The list views must not see each other either.
	_, list := doRemindersJSON(t, srv, "GET", "/api/reminders", nil)
	if items, _ := list["items"].([]any); len(items) != 0 {
		t.Fatalf("/api/reminders returned %d items, want 0", len(items))
	}
	_, list = doRemindersJSON(t, srv, "GET", "/api/tasks", nil)
	if items, _ := list["items"].([]any); len(items) != 1 {
		t.Fatalf("/api/tasks returned %d items, want 1", len(items))
	}
}

// TestReminderRequiresDueTime: the 400 must come from the service's rule, and it
// must arrive on POST rather than at fire time.
func TestReminderRequiresDueTime(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	w, body := doRemindersJSON(t, srv, "POST", "/api/reminders", map[string]any{"title": "no time"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("reminder without a due time: %d, want 400", w.Code)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Fatal("400 carried no error message")
	}
	// A task without one is fine — that is the whole point of the task list.
	w, _ = doRemindersJSON(t, srv, "POST", "/api/tasks", map[string]any{"title": "no time needed"})
	if w.Code != http.StatusOK {
		t.Fatalf("task without a due time: %d, want 200", w.Code)
	}
}

// TestCreateRejectsNonPendingStatus: silently dropping a supplied status would
// let a POST create an item that is already done.
func TestCreateRejectsNonPendingStatus(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	w, _ := doRemindersJSON(t, srv, "POST", "/api/tasks", map[string]any{
		"title":  "done at birth",
		"status": "completed",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST with status=completed: %d, want 400", w.Code)
	}
	// The explicit pending form is accepted, not rejected on a technicality.
	w, _ = doRemindersJSON(t, srv, "POST", "/api/tasks", map[string]any{"title": "normal", "status": "pending"})
	if w.Code != http.StatusOK {
		t.Fatalf("POST with status=pending: %d, want 200", w.Code)
	}
}

// TestListIsSortedAndPaginated pins the paging contract the UI relies on: a
// total independent of the page, an echoed effective limit, and a malformed
// parameter rejected instead of silently becoming 0.
func TestListIsSortedAndPaginated(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	due := func(hours int) float64 {
		return float64(time.Now().Add(time.Duration(hours) * time.Hour).UnixMilli())
	}
	mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "third", "due_at_ms": due(3)})
	mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "first", "due_at_ms": due(1)})
	mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "no deadline"})
	mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "second", "due_at_ms": due(2)})

	w, page := doRemindersJSON(t, srv, "GET", "/api/tasks?limit=2&offset=0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	if page["total"].(float64) != 4 {
		t.Fatalf("total = %v, want 4 (the filter total, not the page size)", page["total"])
	}
	items := page["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("page size = %d, want 2", len(items))
	}
	if items[0].(map[string]any)["title"] != "first" {
		t.Fatalf("first item = %v, want the soonest due", items[0].(map[string]any)["title"])
	}
	if page["limit"].(float64) != 2 {
		t.Fatalf("echoed limit = %v, want 2", page["limit"])
	}

	_, page2 := doRemindersJSON(t, srv, "GET", "/api/tasks?limit=2&offset=2", nil)
	items2 := page2["items"].([]any)
	if len(items2) != 2 {
		t.Fatalf("second page size = %d, want 2", len(items2))
	}
	if items2[0].(map[string]any)["title"] != "third" {
		t.Fatalf("second page starts at %v, want \"third\"", items2[0].(map[string]any)["title"])
	}
	// The undated item must sort last, i.e. into the second page's tail.
	if items2[1].(map[string]any)["title"] != "no deadline" {
		t.Fatalf("last item = %v, want the undated one", items2[1].(map[string]any)["title"])
	}

	// An over-large limit is clamped, and the echo says so.
	_, big := doRemindersJSON(t, srv, "GET", "/api/tasks?limit=100000", nil)
	if big["limit"].(float64) != float64(reminders.MaxPageSize) {
		t.Fatalf("clamped limit = %v, want %d", big["limit"], reminders.MaxPageSize)
	}

	// Malformed paging is a 400, not a silent first page.
	for _, bad := range []string{"limit=abc", "offset=xyz"} {
		w, _ := doRemindersJSON(t, srv, "GET", "/api/tasks?"+bad, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("GET ?%s = %d, want 400", bad, w.Code)
		}
	}
	// An unknown status filter is rejected rather than returning everything.
	w, _ = doRemindersJSON(t, srv, "GET", "/api/tasks?status=archived", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GET ?status=archived = %d, want 400", w.Code)
	}
}

// TestListStatusFilter pins the status query the UI uses for its filter chips.
func TestListStatusFilter(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	done := mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "done"})["id"].(string)
	mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "open"})

	doRemindersJSON(t, srv, "PATCH", "/api/tasks/"+done, map[string]any{"status": "completed"})

	_, pending := doRemindersJSON(t, srv, "GET", "/api/tasks?status=pending", nil)
	items := pending["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["title"] != "open" {
		t.Fatalf("pending filter = %+v, want just the open task", items)
	}
	_, completed := doRemindersJSON(t, srv, "GET", "/api/tasks?status=completed", nil)
	citems := completed["items"].([]any)
	if len(citems) != 1 || citems[0].(map[string]any)["title"] != "done" {
		t.Fatalf("completed filter = %+v, want just the done task", citems)
	}
}

// TestRunNowRefusesTerminalItem: a cancelled task must not be deliverable on
// demand either.
func TestRunNowRefusesTerminalItem(t *testing.T) {
	srv, svc := newRemindersTestServer(t)
	id := mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "cancelled"})["id"].(string)
	doRemindersJSON(t, srv, "PATCH", "/api/tasks/"+id, map[string]any{"status": "cancelled"})

	w, _ := doRemindersJSON(t, srv, "POST", "/api/tasks/"+id+"/run", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("run-now on a cancelled item = %d, want 400", w.Code)
	}
	// And a pending one is accepted (the loop is not started in this harness,
	// so the item is not delivered — but the request must not be refused for
	// the wrong reason).
	open := mustPostItem(t, srv, "/api/tasks", map[string]any{"title": "open"})["id"].(string)
	w, _ = doRemindersJSON(t, srv, "POST", "/api/tasks/"+open+"/run", nil)
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusOK {
		t.Fatalf("run-now on a pending item = %d, want 200 or 500", w.Code)
	}
	if _, err := svc.Get(open); err != nil {
		t.Fatalf("item vanished: %v", err)
	}
}

// TestRunNowOnMissingItemIs404.
func TestRunNowOnMissingItemIs404(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	w, _ := doRemindersJSON(t, srv, "POST", "/api/reminders/does-not-exist/run", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("run-now on a missing item = %d, want 404", w.Code)
	}
}

// TestRunHistoryRouteIsRegistered: the prefixed id is what the shared runs
// handler will look for, so the route must exist under both kinds.
func TestRunHistoryRouteIsRegistered(t *testing.T) {
	srv, _ := newRemindersTestServer(t)
	dir := t.TempDir()
	srv.schedulerRuns = scheduler.NewRunHistory(filepath.Join(dir, "jobs.json"))
	// A missing history file is not an error: the list is simply empty.
	for _, base := range []string{"/api/reminders", "/api/tasks"} {
		w, body := doRemindersJSON(t, srv, "GET", base+"/abc12345/runs", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s/{id}/runs = %d %s", base, w.Code, w.Body.String())
		}
		if _, ok := body["runs"]; !ok {
			t.Fatalf("GET %s/{id}/runs response has no runs key: %v", base, body)
		}
	}
}

// TestReminderItemAsCronJob pins the projection onto the shared cron runner. The
// id namespacing is the load-bearing part: the runner derives its transcript
// path from "cron:" + Job.ID, and a cron job's id is a bare 8-hex string, so an
// unprefixed reminder id would share a transcript with an unrelated job.
func TestReminderItemAsCronJob(t *testing.T) {
	if reminderItemAsCronJob(nil) != nil {
		t.Fatal("nil item projected to a non-nil job")
	}
	due := int64(1_800_000_000_000)
	it := reminders.Item{
		ID:          "abc12345",
		Kind:        reminders.KindTask,
		Title:       "ship it",
		Message:     "the prompt",
		Notes:       "context",
		Owner:       "/work/dir",
		PermMode:    scheduler.PermSandbox,
		DueAtMs:     due,
		CreatedAtMs: 7,
	}
	got := reminderItemAsCronJob(&it)
	if got == nil {
		t.Fatal("nil job for a non-nil item")
	}
	if got.ID != "rt-abc12345" {
		t.Fatalf("job id = %q, want the namespaced form", got.ID)
	}
	if got.ID == it.ID {
		t.Fatal("job id collides with the raw item id; a cron job could share this transcript")
	}
	if got.Schedule.Kind != scheduler.KindAt || got.Schedule.AtMs != due {
		t.Fatalf("schedule = %+v, want KindAt at %d", got.Schedule, due)
	}
	if got.Payload.Message != "the prompt" {
		t.Fatalf("payload message = %q", got.Payload.Message)
	}
	if got.Payload.PermMode != scheduler.PermSandbox {
		t.Fatalf("perm mode = %q, want it carried through", got.Payload.PermMode)
	}
	if got.Payload.Owner != "/work/dir" {
		t.Fatalf("owner = %q, want the workdir hint carried through", got.Payload.Owner)
	}
	if got.Name != "ship it" {
		t.Fatalf("name = %q", got.Name)
	}

	// An item with no message falls back to the title, so an agent firing always
	// has a prompt.
	noMsg := reminders.Item{ID: "x", Title: "title only"}
	if body := reminderItemAsCronJob(&noMsg).Payload.Message; body != "title only" {
		t.Fatalf("fallback message = %q, want the title", body)
	}
}

// TestIsReminderDeliveryID pins the prefix the drainer routes on. A bare
// un-prefixed id must NOT be treated as a reminder, or an orphaned cron
// delivery would be forwarded to a chat on a guess.
func TestIsReminderDeliveryID(t *testing.T) {
	for _, id := range []string{"reminder:abc", "task:abc"} {
		if !isReminderDeliveryID(id) {
			t.Errorf("isReminderDeliveryID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"abc12345", "", "reminders:abc", "Reminders:abc"} {
		if isReminderDeliveryID(id) {
			t.Errorf("isReminderDeliveryID(%q) = true, want false", id)
		}
	}
}
