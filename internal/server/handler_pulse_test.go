package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/projects"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// pulseTestHandler builds a Handler whose projects store lives in the test's
// temp dir, so /api/pulse's disk scan cannot see the developer's real sessions.
func pulseTestHandler(t *testing.T) *Handler {
	t.Helper()
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("projects store: %v", err)
	}
	h := NewHandler()
	h.projects = store
	return h
}

// registerLiveSession registers a session with the given turn state and, when
// messages are supplied, a resident agentSession carrying them (the ask and
// last-assistant-line detection read the in-memory transcript).
func registerLiveSession(t *testing.T, h *Handler, id, root string, running bool, msgs []agent.Message) {
	t.Helper()
	h.sessions.Register(id, root)
	if running {
		h.sessions.setTurnActive(id, true)
	}
	if msgs != nil {
		h.mu.Lock()
		h.agents[id] = &agentSession{messages: msgs}
		h.mu.Unlock()
	}
}

func permissionAskMessage(requestID string, req agent.PermissionRequest) agent.Message {
	payload, _ := json.Marshal(req)
	return agent.Message{
		Role:    "tool",
		ToolID:  requestID,
		Content: tool.SentinelPermissionAsk + string(payload),
	}
}

// questionAskMessage reproduces exactly what the question tool writes into the
// transcript (internal/tool/misc.go): the prompt sentinel, the JSON body, then
// the waiting-for-user marker. The trailing marker is load-bearing — without it
// isQuestionAsk reports the ask as already answered, so a fixture omitting it
// would silently exercise the "no pending ask" path instead of the one it
// claims to test.
func questionAskMessage(requestID string, prompts []tool.QuestionPrompt) agent.Message {
	payload, _ := json.Marshal(prompts)
	return agent.Message{
		Role:    "tool",
		ToolID:  requestID,
		Content: tool.SentinelQuestionPrompt + "\n" + string(payload) + "\n\n" + tool.SentinelWaitingForUser,
	}
}

func getPulse(t *testing.T, h *Handler, query string) (*httptest.ResponseRecorder, pulsePageBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pulse"+query, nil)
	h.HandlePulse(rec, req)
	var body pulsePageBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body %q: %v", rec.Body.String(), err)
		}
	}
	return rec, body
}

type pulsePageBody struct {
	Items      []pulseBodyRow `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

type pulseBodyRow struct {
	SessionID   string `json:"session_id"`
	ProjectPath string `json:"project_path"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	ChildCount  int    `json:"child_count"`
	Todo        *struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"todo"`
	PendingAsk *struct {
		Kind    string `json:"kind"`
		Summary string `json:"summary"`
	} `json:"pending_ask"`
}

func ids(rows []pulseBodyRow) map[string]pulseBodyRow {
	out := map[string]pulseBodyRow{}
	for _, r := range rows {
		out[r.SessionID] = r
	}
	return out
}

func TestHandlePulseLiveScopeRanksAndExcludes(t *testing.T) {
	h := pulseTestHandler(t)

	// Needs you: a permission ask parked at the tail of the transcript.
	askID := session.NewSessionID()
	askRoot := t.TempDir()
	registerLiveSession(t, h, askID, askRoot, true, []agent.Message{
		permissionAskMessage("req_1", agent.PermissionRequest{ToolName: "bash", Command: "rm -rf /tmp/x"}),
	})

	// Running, with a child session and a todo plan.
	runID := session.NewSessionID()
	runRoot := t.TempDir()
	writeSessionTodoFile(t, runRoot, runID, "# Todo (revision 1)\n- [✓] t1 a\n- [•] t2 b\n")
	registerLiveSession(t, h, runID, runRoot, true, []agent.Message{
		{Role: "assistant", Content: "editing the store now"},
	})
	childID := runID + "_child_task_1"
	registerLiveSession(t, h, childID, runRoot, true, nil)

	// Idle with a failed last turn.
	errID := session.NewSessionID()
	registerLiveSession(t, h, errID, t.TempDir(), false, nil)
	h.sessions.setTurnError(errID, "llm 500")

	// Idle 30h old — outside the live window.
	oldID := session.NewSessionID()
	oldRoot := t.TempDir()
	registerLiveSession(t, h, oldID, oldRoot, false, nil)
	backdateRegistryEntry(t, h, oldID, 30*time.Hour)

	rec, body := getPulse(t, h, "?scope=live")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	byID := ids(body.Items)

	if len(body.Items) != 3 {
		t.Fatalf("items = %d (%v), want 3 (the 30h idle and the child must be absent)", len(body.Items), pulseIDsOf(body.Items))
	}
	if byID[oldID].SessionID != "" {
		t.Error("live scope included a 30h-old idle session")
	}
	if byID[childID].SessionID != "" {
		t.Error("live scope gave a child session its own row")
	}
	if got := byID[askID]; got.Status != "needs_permission" {
		t.Errorf("ask status = %q, want needs_permission", got.Status)
	}
	if got := byID[askID].PendingAsk; got == nil || got.Summary != "rm -rf /tmp/x" {
		t.Errorf("pending_ask = %+v, want the command as the summary", got)
	}
	if got := byID[runID]; got.Status != "running" {
		t.Errorf("run status = %q, want running", got.Status)
	}
	if got := byID[runID].ChildCount; got != 1 {
		t.Errorf("ChildCount = %d, want 1", got)
	}
	if got := byID[runID].Todo; got == nil || got.Done != 1 || got.Total != 2 {
		t.Errorf("todo = %+v, want 1/2 read from the project file", got)
	}
	if got := byID[errID]; got.Status != "error" {
		t.Errorf("err status = %q, want error", got.Status)
	}

	// Needs-you first, then running, then error.
	if body.Items[0].SessionID != askID {
		t.Errorf("first row = %s, want the needs-you session %s", body.Items[0].SessionID, askID)
	}
	if body.NextCursor != nil {
		t.Errorf("next_cursor = %v, want null on a single page", *body.NextCursor)
	}
}

func TestHandlePulseQuestionBecomesNeedsQuestion(t *testing.T) {
	h := pulseTestHandler(t)
	id := session.NewSessionID()
	registerLiveSession(t, h, id, t.TempDir(), true, []agent.Message{
		questionAskMessage("req_2", []tool.QuestionPrompt{{Header: "Branch", Question: "Which branch?"}}),
	})

	_, body := getPulse(t, h, "")
	byID := ids(body.Items)
	row, ok := byID[id]
	if !ok {
		t.Fatalf("session missing from %v", pulseIDsOf(body.Items))
	}
	if row.Status != "needs_question" {
		t.Errorf("status = %q, want needs_question", row.Status)
	}
	if row.PendingAsk == nil || row.PendingAsk.Kind != "question" {
		t.Errorf("pending_ask = %+v, want a question ask", row.PendingAsk)
	}
}

func TestHandlePulseAllScopeIncludesOlderIdleFromDisk(t *testing.T) {
	h := pulseTestHandler(t)
	root := t.TempDir()
	if err := h.projects.Add(root); err != nil {
		t.Fatalf("add project: %v", err)
	}

	// A real persisted session, 30h old: outside live, inside all.
	oldID := session.NewSessionID()
	if err := session.SaveForDir(root, oldID, "A finished a while ago", []agent.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}, nil); err != nil {
		t.Fatalf("save session: %v", err)
	}
	backdate(t, root, oldID, 30*time.Hour)

	// And one 8 days old: outside both windows.
	ancientID := session.NewSessionID()
	if err := session.SaveForDir(root, ancientID, "Ancient", []agent.Message{
		{Role: "user", Content: "hi"},
	}, nil); err != nil {
		t.Fatalf("save ancient session: %v", err)
	}
	backdate(t, root, ancientID, 8*24*time.Hour)

	liveRec, liveBody := getPulse(t, h, "?scope=live")
	if liveRec.Code != http.StatusOK {
		t.Fatalf("live status = %d", liveRec.Code)
	}
	if len(liveBody.Items) != 0 {
		t.Errorf("live scope returned %v, want nothing (neither session is registered)", pulseIDsOf(liveBody.Items))
	}

	allRec, allBody := getPulse(t, h, "?scope=all")
	if allRec.Code != http.StatusOK {
		t.Fatalf("all status = %d, body %s", allRec.Code, allRec.Body.String())
	}
	allIDs := pulseIDsOf(allBody.Items)
	if !containsID(allIDs, oldID) {
		t.Errorf("all scope missing the 30h session (got %v)", allIDs)
	}
	if containsID(allIDs, ancientID) {
		t.Errorf("all scope included an 8-day-old session (window is 7 days): %v", allIDs)
	}
	row := ids(allBody.Items)[oldID]
	if row.ProjectPath != root {
		t.Errorf("project_path = %q, want %q — the row must say which project it came from", row.ProjectPath, root)
	}
	if row.Title != "A finished a while ago" {
		t.Errorf("title = %q, want the persisted title", row.Title)
	}
}

func TestHandlePulseAllScopePrefersLiveData(t *testing.T) {
	// A session that is both on disk and in the registry must appear once, with
	// the registry's (fresher) state — not duplicated as a disk row.
	h := pulseTestHandler(t)
	root := t.TempDir()
	if err := h.projects.Add(root); err != nil {
		t.Fatalf("add project: %v", err)
	}
	id := session.NewSessionID()
	if err := session.SaveForDir(root, id, "On disk", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("save: %v", err)
	}
	registerLiveSession(t, h, id, root, true, []agent.Message{{Role: "assistant", Content: "still working"}})

	_, body := getPulse(t, h, "?scope=all")
	seen := 0
	for _, r := range body.Items {
		if r.SessionID == id {
			seen++
			if r.Status != "running" {
				t.Errorf("status = %q, want running (live state must win)", r.Status)
			}
		}
	}
	if seen != 1 {
		t.Errorf("session appeared %d times, want exactly 1", seen)
	}
}

func TestHandlePulseRejectsBadQuery(t *testing.T) {
	h := pulseTestHandler(t)
	for _, tc := range []struct{ name, query string }{
		{"unknown scope", "?scope=bogus"},
		{"empty scope value is not a scope", "?scope="},
		{"limit zero", "?limit=0"},
		{"limit above the cap", "?limit=101"},
		{"limit not a number", "?limit=abc"},
		{"garbage cursor", "?cursor=not-a-cursor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, _ := getPulse(t, h, tc.query)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandlePulseAcceptsScopeOmittedAndBoundaryLimits(t *testing.T) {
	h := pulseTestHandler(t)
	for _, query := range []string{"", "?limit=1", "?limit=100", "?scope=live", "?scope=all"} {
		rec, _ := getPulse(t, h, query)
		if rec.Code != http.StatusOK {
			t.Errorf("query %q: status = %d, want 200 (body %s)", query, rec.Code, rec.Body.String())
		}
	}
}

func TestHandlePulsePagesWithCursor(t *testing.T) {
	h := pulseTestHandler(t)
	for range 5 {
		id := session.NewSessionID()
		registerLiveSession(t, h, id, t.TempDir(), true, nil)
	}

	_, first := getPulse(t, h, "?limit=2")
	if len(first.Items) != 2 {
		t.Fatalf("first page has %d items, want 2", len(first.Items))
	}
	if first.NextCursor == nil {
		t.Fatal("next_cursor = null on a partial page")
	}
	_, second := getPulse(t, h, "?limit=2&cursor="+*first.NextCursor)
	if len(second.Items) != 2 {
		t.Fatalf("second page has %d items, want 2", len(second.Items))
	}
	if second.Items[0].SessionID == first.Items[0].SessionID {
		t.Error("second page repeated the first page's first row")
	}
}

// backdate rewrites a persisted session's updated_at in the per-project index
// (the source ListRefsForDir reads for current-format sessions) so the scope
// windows can be exercised without waiting days.
// backdateRegistryEntry ages a registered session's lastActivity. There is no
// public setter (the field is stamped by real registry activity), so the test
// pokes it under the manager's own lock — the only way to exercise the
// live-scope window at the HTTP layer without waiting 24 hours.
func backdateRegistryEntry(t *testing.T, h *Handler, id string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	h.sessions.mu.Lock()
	e := h.sessions.entries[id]
	if e == nil {
		h.sessions.mu.Unlock()
		t.Fatalf("no registry entry for %s", id)
	}
	e.lastActivity = when
	if e.turnEndedAt.IsZero() || e.turnEndedAt.After(when) {
		e.turnEndedAt = when
	}
	h.sessions.mu.Unlock()

	if got, _ := h.sessions.SnapshotEntry(id); got.lastActivity.After(time.Now().Add(-29 * time.Hour)) {
		t.Fatalf("fixture: backdating %s did not take (lastActivity %v)", id, got.lastActivity)
	}
}

func backdate(t *testing.T, root, id string, age time.Duration) {
	t.Helper()
	dir, err := session.GetStorageDirForPath(root)
	if err != nil {
		t.Fatalf("storage dir: %v", err)
	}
	indexPath := filepath.Join(dir, "index.sqlite")
	db, err := sql.Open("sqlite", indexPath)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()
	when := time.Now().Add(-age).UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, when, id); err != nil {
		t.Fatalf("backdate index row for %s: %v", id, err)
	}
}

func pulseIDsOf(rows []pulseBodyRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.SessionID)
	}
	return out
}

func containsID(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// TestHandlePulseDoesNotBlockOnARunningTurn pins the fix for the dashboard
// stall: runTurn holds as.mu for the WHOLE turn, so gatherLivePulseInputs must
// use TryLock (not Lock). While the turn lock is held the row still comes from
// the registry (Running=true) and the handler returns immediately.
func TestHandlePulseDoesNotBlockOnARunningTurn(t *testing.T) {
	h := pulseTestHandler(t)
	id := session.NewSessionID()
	registerLiveSession(t, h, id, t.TempDir(), true, []agent.Message{
		{Role: "assistant", Content: "editing the store now"},
	})

	h.mu.Lock()
	as := h.agents[id]
	h.mu.Unlock()
	if as == nil {
		t.Fatal("no resident agent for the session")
	}
	// Simulate runTurn holding the turn lock for its whole duration.
	as.mu.Lock()
	defer as.mu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pulse?scope=live", nil)
	done := make(chan struct{})
	go func() {
		h.HandlePulse(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HandlePulse blocked on a running turn's as.mu")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var body pulsePageBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	got, ok := ids(body.Items)[id]
	if !ok {
		t.Fatalf("session %s missing from a live-scope page while its turn holds as.mu", id)
	}
	if got.Status != "running" {
		t.Errorf("status = %q, want running", got.Status)
	}
}
