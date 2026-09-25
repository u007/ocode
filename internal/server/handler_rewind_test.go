package server

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	_ "modernc.org/sqlite"
)

type rewindFixture struct {
	server  *Server
	handler *Handler
	root    string
	id      string
	t       *testing.T
}

func newRewindFixture(t *testing.T, messages []agent.Message) *rewindFixture {
	t.Helper()

	root := t.TempDir()
	s := New("localhost:0", "user", "pass", nil)
	h := s.handler
	h.SetWorkDir(root)
	h.projects = newTestProjectStore(t, root)
	id := session.NewSessionID()
	if err := session.SaveForDir(root, id, "rewind fixture", messages, nil); err != nil {
		t.Fatalf("save rewind fixture: %v", err)
	}
	h.sessions.Register(id, root)
	return &rewindFixture{server: s, handler: h, root: root, id: id, t: t}
}

func (f *rewindFixture) request(method, path string, body any, authenticated bool) *httptest.ResponseRecorder {
	f.t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			f.t.Fatalf("marshal request: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if authenticated {
		req.SetBasicAuth("user", "pass")
	}
	rec := httptest.NewRecorder()
	f.server.mux.ServeHTTP(rec, req)
	return rec
}

func (f *rewindFixture) arm(targetIndex int, targetContent string, userSeq int) string {
	f.t.Helper()
	resource, err := session.PreparePendingRewindForDir(f.root, f.id, targetIndex, targetContent, userSeq)
	if err != nil {
		f.t.Fatalf("prepare pending rewind: %v", err)
	}
	return resource.Token
}

func (f *rewindFixture) resident(client agent.LLMClient, messages []agent.Message) *agentSession {
	f.t.Helper()
	as := newTestSession(f.handler, f.id, client)
	as.messages = append([]agent.Message(nil), messages...)
	f.handler.sessions.setAgent(f.id, as)
	f.t.Cleanup(func() {
		if as.agent != nil {
			as.agent.Shutdown()
		}
	})
	return as
}

func rewindMessages() []agent.Message {
	return []agent.Message{
		{Role: "user", Content: "keep me", UserSeq: 1},
		{Role: "assistant", Content: "answer before target"},
		{Role: "user", Content: "replace me", UserSeq: 2},
		{Role: "assistant", Content: "discard me"},
	}
}

func decodeRewindResponse(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

func rewindStatusBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	decodeRewindResponse(t, rec, &body)
	return body
}

func openRewindDB(t *testing.T, f *rewindFixture) *sql.DB {
	t.Helper()
	dir, err := session.GetStorageDirForPath(f.root)
	if err != nil {
		t.Fatalf("session storage dir: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, f.id+".sqlite"))
	if err != nil {
		t.Fatalf("open rewind sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func expireRewindToken(t *testing.T, f *rewindFixture, token string) {
	t.Helper()
	db := openRewindDB(t, f)
	if _, err := db.Exec(`UPDATE pending_rewinds SET expires_at = ? WHERE token = ?`, time.Now().Add(-time.Hour), token); err != nil {
		t.Fatalf("expire rewind token: %v", err)
	}
}

func failNextRewindCommit(t *testing.T, f *rewindFixture) {
	t.Helper()
	db := openRewindDB(t, f)
	if _, err := db.Exec(`
		CREATE TRIGGER rewind_test_fail_commit
		BEFORE UPDATE OF status ON pending_rewinds
		WHEN OLD.status = 'armed' AND NEW.status = 'committed'
		BEGIN
			SELECT RAISE(ABORT, 'injected pending rewind commit failure');
		END`); err != nil {
		t.Fatalf("install rewind failure trigger: %v", err)
	}
}

type rewindCountingClient struct {
	calls atomic.Int32
}

func (c *rewindCountingClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	c.calls.Add(1)
	return &agent.Message{Role: "assistant", Content: "should not run"}, nil
}

func (c *rewindCountingClient) GetProvider() string { return "fake" }
func (c *rewindCountingClient) GetModel() string    { return "fake-model" }

func TestRewindPrepareCancelAndSafeStatus(t *testing.T) {
	f := newRewindFixture(t, rewindMessages())
	before, err := session.StoredRevisionForDir(f.root, f.id)
	if err != nil {
		t.Fatalf("revision before prepare: %v", err)
	}

	unauthenticated := f.request(http.MethodPost, "/api/sessions/"+f.id+"/rewinds", map[string]any{
		"targetIndex":   2,
		"targetContent": "replace me",
		"userSeq":       2,
	}, false)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated prepare status = %d, want 401", unauthenticated.Code)
	}

	rec := f.request(http.MethodPost, "/api/sessions/"+f.id+"/rewinds", map[string]any{
		"targetIndex":   2,
		"targetContent": "replace me",
		"userSeq":       2,
	}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("prepare status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "fingerprint") || strings.Contains(rec.Body.String(), "target_content") || strings.Contains(rec.Body.String(), "replace me") {
		t.Fatalf("prepare response leaked fingerprint or target content: %s", rec.Body.String())
	}
	var dto struct {
		Token            string `json:"token"`
		Status           string `json:"status"`
		ExpiresAt        string `json:"expires_at"`
		TargetIndex      int    `json:"target_index"`
		UserSeq          int    `json:"user_seq"`
		CommittedUserSeq int    `json:"committed_user_seq"`
	}
	decodeRewindResponse(t, rec, &dto)
	if dto.Token == "" || dto.Status != "armed" || dto.ExpiresAt == "" || dto.TargetIndex != 2 || dto.UserSeq != 2 {
		t.Fatalf("safe prepare DTO = %+v", dto)
	}
	if _, err := hex.DecodeString(dto.Token); err != nil {
		t.Fatalf("prepare token is not hex: %v", err)
	}

	afterPrepare, err := session.StoredRevisionForDir(f.root, f.id)
	if err != nil {
		t.Fatalf("revision after prepare: %v", err)
	}
	if afterPrepare != before {
		t.Fatalf("prepare changed stored revision %q -> %q", before, afterPrepare)
	}

	status := f.request(http.MethodGet, "/api/sessions/"+f.id+"/rewinds/"+dto.Token, nil, true)
	if status.Code != http.StatusOK {
		t.Fatalf("status after prepare = %d, want 200: %s", status.Code, status.Body.String())
	}
	statusBody := rewindStatusBody(t, status)
	if statusBody["status"] != "armed" || statusBody["target_index"] != float64(2) {
		t.Fatalf("status body = %+v", statusBody)
	}

	cancel := f.request(http.MethodDelete, "/api/sessions/"+f.id+"/rewinds/"+dto.Token, nil, true)
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200: %s", cancel.Code, cancel.Body.String())
	}
	afterCancel, err := session.StoredRevisionForDir(f.root, f.id)
	if err != nil {
		t.Fatalf("revision after cancel: %v", err)
	}
	if afterCancel != before {
		t.Fatalf("cancel changed stored revision %q -> %q", before, afterCancel)
	}
	missing := f.request(http.MethodGet, "/api/sessions/"+f.id+"/rewinds/"+dto.Token, nil, true)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("status after cancel = %d, want 404", missing.Code)
	}
}

func TestRewindPrepareRejectsActiveTurnAndInvalidTarget(t *testing.T) {
	f := newRewindFixture(t, rewindMessages())
	f.handler.sessions.setTurnActive(f.id, true)
	active := f.request(http.MethodPost, "/api/sessions/"+f.id+"/rewinds", map[string]any{
		"targetIndex":   2,
		"targetContent": "replace me",
		"userSeq":       2,
	}, true)
	if active.Code != http.StatusConflict {
		t.Fatalf("active prepare status = %d, want 409: %s", active.Code, active.Body.String())
	}
	f.handler.sessions.setTurnActive(f.id, false)

	invalid := f.request(http.MethodPost, "/api/sessions/"+f.id+"/rewinds", map[string]any{
		"targetIndex":   2,
		"targetContent": "not the target",
		"userSeq":       2,
	}, true)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid prepare status = %d, want 400: %s", invalid.Code, invalid.Body.String())
	}
}

func TestRewindStatusCommittedAndExpired(t *testing.T) {
	t.Run("committed", func(t *testing.T) {
		f := newRewindFixture(t, rewindMessages())
		token := f.arm(2, "replace me", 2)
		commit, err := session.CommitPendingRewindForDir(f.root, f.id, token, "edited replacement")
		if err != nil {
			t.Fatalf("commit pending rewind: %v", err)
		}
		rec := f.request(http.MethodGet, "/api/sessions/"+f.id+"/rewinds/"+token, nil, true)
		if rec.Code != http.StatusOK {
			t.Fatalf("committed status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		body := rewindStatusBody(t, rec)
		if body["status"] != "committed" || body["committed_user_seq"] != float64(commit.Rewind.CommittedUserSeq) {
			t.Fatalf("committed status body = %+v, want seq %d", body, commit.Rewind.CommittedUserSeq)
		}
	})

	t.Run("expired", func(t *testing.T) {
		f := newRewindFixture(t, rewindMessages())
		token := f.arm(2, "replace me", 2)
		expireRewindToken(t, f, token)
		rec := f.request(http.MethodGet, "/api/sessions/"+f.id+"/rewinds/"+token, nil, true)
		if rec.Code != http.StatusGone {
			t.Fatalf("expired status = %d, want 410: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestRewindTokenizedAsyncSendCommitsBeforeAckAndReconciles(t *testing.T) {
	f := newRewindFixture(t, rewindMessages())
	client := newBlockingClient()
	as := f.resident(client, rewindMessages())
	token := f.arm(2, "replace me", 2)
	sub := f.handler.bus.Subscribe(nil)
	defer f.handler.bus.Unsubscribe(sub)

	rec := f.request(http.MethodPost, "/api/sessions/"+f.id+"/message", map[string]any{
		"content":     "edited replacement",
		"async":       true,
		"rewindToken": token,
	}, true)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("tokenized send status = %d, want 202: %s", rec.Code, rec.Body.String())
	}

	select {
	case <-client.started:
	case <-time.After(5 * time.Second):
		as.mu.Lock()
		stuckMessages := append([]agent.Message(nil), as.messages...)
		as.mu.Unlock()
		t.Logf("rewind job stalled: pending=%d front=%q resident=%p active=%v", f.handler.sessions.PendingCount(f.id), func() string {
			content, ok := f.handler.sessions.PendingFront(f.id)
			if !ok {
				return ""
			}
			return content
		}(), as, f.handler.sessions.IsTurnActive(f.id))
		t.Logf("stuck resident messages: %+v", stuckMessages)
		current := f.handler.lookupAgentSession(f.id)
		t.Logf("current resident=%p model=%q agent=%v", current, func() string {
			if current == nil {
				return ""
			}
			return current.model
		}(), current != nil && current.agent != nil)
		close(client.release)
		f.handler.turnJobsWG.Wait()
		t.Fatal("rewind turn never reached the blocking LLM call")
	}

	loaded, err := session.LoadForDir(f.root, f.id)
	if err != nil {
		close(client.release)
		t.Fatalf("load committed transcript: %v", err)
	}
	wantPrefix := rewindMessages()[:2]
	if len(loaded.Messages) != len(wantPrefix)+1 {
		close(client.release)
		t.Fatalf("committed transcript length = %d, want %d: %+v", len(loaded.Messages), len(wantPrefix)+1, loaded.Messages)
	}
	if loaded.Messages[len(loaded.Messages)-1].Content != "edited replacement" || loaded.Messages[len(loaded.Messages)-1].UserSeq != 2 {
		close(client.release)
		t.Fatalf("committed user row = %+v", loaded.Messages[len(loaded.Messages)-1])
	}

	as.mu.Lock()
	residentMessages := append([]agent.Message(nil), as.messages...)
	as.mu.Unlock()
	if len(residentMessages) != len(wantPrefix)+1 || residentMessages[len(residentMessages)-1].Content != "edited replacement" {
		close(client.release)
		t.Fatalf("resident messages = %+v, want kept prefix plus replacement", residentMessages)
	}
	if got := f.handler.sessions.PendingCount(f.id); got != 1 {
		close(client.release)
		t.Fatalf("pending count while turn is blocked = %d, want 1", got)
	}
	if got, ok := f.handler.sessions.PendingFront(f.id); !ok || got != "edited replacement" {
		close(client.release)
		t.Fatalf("pending front = %q, %v; want replacement", got, ok)
	}

	var broadcast []agent.Message
	deadline := time.After(5 * time.Second)
	for broadcast == nil {
		select {
		case env := <-sub:
			if env.Event != "messages" {
				continue
			}
			switch data := env.Data.(type) {
			case []agent.Message:
				broadcast = data
			default:
				t.Fatalf("messages event data type = %T", env.Data)
			}
		case <-deadline:
			close(client.release)
			f.handler.turnJobsWG.Wait()
			t.Fatal("authoritative messages broadcast never arrived before turn completion")
		}
	}
	if len(broadcast) != len(wantPrefix)+1 || broadcast[len(broadcast)-1].Content != "edited replacement" {
		close(client.release)
		f.handler.turnJobsWG.Wait()
		t.Fatalf("broadcast transcript = %+v, want kept prefix plus replacement", broadcast)
	}

	close(client.release)
	f.handler.turnJobsWG.Wait()
}

func TestRewindTokenizedAsyncSendFailuresDoNotAppend(t *testing.T) {
	t.Run("stale", func(t *testing.T) {
		f := newRewindFixture(t, rewindMessages())
		token := f.arm(2, "replace me", 2)
		if err := session.AppendUserMessageForDir(f.root, f.id, "changed after prepare"); err != nil {
			t.Fatalf("change transcript after prepare: %v", err)
		}
		rec := f.request(http.MethodPost, "/api/sessions/"+f.id+"/message", map[string]any{
			"content":     "must not append",
			"async":       true,
			"rewindToken": token,
		}, true)
		if rec.Code != http.StatusConflict {
			t.Fatalf("stale send status = %d, want 409: %s", rec.Code, rec.Body.String())
		}
		f.handler.turnJobsWG.Wait()
		loaded, err := session.LoadForDir(f.root, f.id)
		if err != nil {
			t.Fatalf("load stale transcript: %v", err)
		}
		for _, msg := range loaded.Messages {
			if msg.Content == "must not append" {
				t.Fatalf("stale rewind appended a user row: %+v", loaded.Messages)
			}
		}
	})

	t.Run("expired", func(t *testing.T) {
		f := newRewindFixture(t, rewindMessages())
		token := f.arm(2, "replace me", 2)
		expireRewindToken(t, f, token)
		rec := f.request(http.MethodPost, "/api/sessions/"+f.id+"/message", map[string]any{
			"content":     "must not append",
			"async":       true,
			"rewindToken": token,
		}, true)
		if rec.Code != http.StatusGone {
			t.Fatalf("expired send status = %d, want 410: %s", rec.Code, rec.Body.String())
		}
		f.handler.turnJobsWG.Wait()
		loaded, err := session.LoadForDir(f.root, f.id)
		if err != nil {
			t.Fatalf("load expired transcript: %v", err)
		}
		for _, msg := range loaded.Messages {
			if msg.Content == "must not append" {
				t.Fatalf("expired rewind appended a user row: %+v", loaded.Messages)
			}
		}
	})

	t.Run("not found", func(t *testing.T) {
		f := newRewindFixture(t, rewindMessages())
		rec := f.request(http.MethodPost, "/api/sessions/"+f.id+"/message", map[string]any{
			"content":     "must not append",
			"async":       true,
			"rewindToken": strings.Repeat("ab", 32),
		}, true)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("not-found send status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
		f.handler.turnJobsWG.Wait()
		loaded, err := session.LoadForDir(f.root, f.id)
		if err != nil {
			t.Fatalf("load not-found transcript: %v", err)
		}
		for _, msg := range loaded.Messages {
			if msg.Content == "must not append" {
				t.Fatalf("not-found rewind appended a user row: %+v", loaded.Messages)
			}
		}
	})
}

func TestRewindDuplicateCommittedTokenIsAcceptedWithoutSecondTurn(t *testing.T) {
	f := newRewindFixture(t, rewindMessages())
	token := f.arm(2, "replace me", 2)
	if _, err := session.CommitPendingRewindForDir(f.root, f.id, token, "already committed"); err != nil {
		t.Fatalf("initial commit: %v", err)
	}
	loaded, err := session.LoadForDir(f.root, f.id)
	if err != nil {
		t.Fatalf("load committed transcript: %v", err)
	}
	client := &rewindCountingClient{}
	as := f.resident(client, loaded.Messages)
	rec := f.request(http.MethodPost, "/api/sessions/"+f.id+"/message", map[string]any{
		"content":     "must not run twice",
		"async":       true,
		"rewindToken": token,
	}, true)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("duplicate committed send status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	f.handler.turnJobsWG.Wait()
	if got := client.calls.Load(); got != 0 {
		t.Fatalf("duplicate committed token started %d LLM calls, want 0", got)
	}
	if got := f.handler.sessions.PendingCount(f.id); got != 0 {
		t.Fatalf("duplicate committed token pending count = %d, want 0", got)
	}
	as.mu.Lock()
	gotMessages := append([]agent.Message(nil), as.messages...)
	as.mu.Unlock()
	if len(gotMessages) != len(loaded.Messages) || gotMessages[len(gotMessages)-1].Content != "already committed" {
		t.Fatalf("duplicate token changed resident transcript: %+v", gotMessages)
	}
}

func TestRewindPersistenceFailureClosesAckWithError(t *testing.T) {
	f := newRewindFixture(t, rewindMessages())
	client := &rewindCountingClient{}
	f.resident(client, rewindMessages())
	token := f.arm(2, "replace me", 2)
	failNextRewindCommit(t, f)

	rec := f.request(http.MethodPost, "/api/sessions/"+f.id+"/message", map[string]any{
		"content":     "must not append",
		"async":       true,
		"rewindToken": token,
	}, true)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("persistence-failure send status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	f.handler.turnJobsWG.Wait()
	if got := client.calls.Load(); got != 0 {
		t.Fatalf("persistence failure started %d LLM calls, want 0", got)
	}
	if got := f.handler.sessions.PendingCount(f.id); got != 0 {
		t.Fatalf("persistence failure pending count = %d, want 0", got)
	}
	loaded, err := session.LoadForDir(f.root, f.id)
	if err != nil {
		t.Fatalf("load failed rewind transcript: %v", err)
	}
	for _, msg := range loaded.Messages {
		if msg.Content == "must not append" {
			t.Fatalf("failed rewind appended a user row: %+v", loaded.Messages)
		}
	}
}

func TestRewindTokenFreeSendRemainsUnchanged(t *testing.T) {
	f := newRewindFixture(t, rewindMessages())
	client := &rewindCountingClient{}
	f.resident(client, rewindMessages())
	rec := f.request(http.MethodPost, "/api/sessions/"+f.id+"/message", map[string]any{
		"content": "ordinary follow-up",
		"async":   true,
	}, true)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("token-free send status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	f.handler.turnJobsWG.Wait()
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("token-free send LLM calls = %d, want 1", got)
	}
	loaded, err := session.LoadForDir(f.root, f.id)
	if err != nil {
		t.Fatalf("load token-free transcript: %v", err)
	}
	if len(loaded.Messages) < len(rewindMessages())+1 {
		t.Fatalf("token-free transcript unexpectedly rewound: %+v", loaded.Messages)
	}
	last := loaded.Messages[len(loaded.Messages)-1]
	if last.Role != "assistant" {
		t.Fatalf("token-free follow-up missing: %+v", loaded.Messages)
	}
	for _, msg := range loaded.Messages {
		if msg.Content == "ordinary follow-up" && msg.Role != "user" {
			t.Fatalf("token-free follow-up has wrong role: %+v", msg)
		}
	}
}

func TestRewindFixtureUsesAbsoluteProjectRoot(t *testing.T) {
	// This small guard keeps the test helper honest if its setup is changed to
	// use the process working directory accidentally.
	f := newRewindFixture(t, rewindMessages())
	entry, err := f.handler.sessions.Resolve(f.id)
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	if entry.ProjectRoot != f.root {
		t.Fatalf("resolved project root = %q, want %q", entry.ProjectRoot, f.root)
	}
	if _, err := os.Stat(f.root); err != nil {
		t.Fatalf("fixture project root: %v", err)
	}
}
