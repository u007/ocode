package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// sessionsRequest sends one request, with the pane key when withKey is set. It never calls t.Fatal, so goroutines may use it.
func sessionsRequest(method, target, body string, withKey bool) (int, []byte, error) {
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if withKey {
		req.Header.Set("X-Bridge-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return resp.StatusCode, b, err
}

// sessionRefBody is one SessionRef as the wire carries it.
func sessionRefBody(id, title, created string) string {
	return fmt.Sprintf(`{"id":%q,"title":%q,"created":%q}`, id, title, created)
}

// getIndex reads the pane's session index for a workbook through the state route. A missing index is nil.
func getIndex(t *testing.T, base, wb string) []sessionRef {
	t.Helper()
	code, b, err := sessionsRequest("GET", base+"/bridge/state/"+wb+"/sessions", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if code == http.StatusNotFound {
		return nil
	}
	if code != http.StatusOK {
		t.Fatalf("GET index: status %d %s", code, b)
	}
	var refs []sessionRef
	if err := json.Unmarshal(b, &refs); err != nil {
		t.Fatalf("index is not a list: %v (%s)", err, b)
	}
	return refs
}

// newDiskBridge is a bridge whose state survives in dir, so the locking runs over the real file path.
func newDiskBridge(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	h, _, err := NewHandler(Config{OcodeURL: "http://127.0.0.1:1", PaneKey: key, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

const sampleCreated = "2026-11-01T10:00:00Z"

func TestSessionsPostPrependsAndIsIdempotent(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	for _, id := range []string{"ses_a", "ses_b"} {
		if code, b, err := sessionsRequest("POST", s.URL+"/bridge/sessions/w1", sessionRefBody(id, "t "+id, sampleCreated), true); err != nil || code != http.StatusOK {
			t.Fatalf("post %s: status %d %s %v", id, code, b, err)
		}
	}
	// Re-adding ses_a with another title changes nothing: the registered entry stays as it was.
	code, b, err := sessionsRequest("POST", s.URL+"/bridge/sessions/w1", sessionRefBody("ses_a", "renamed", sampleCreated), true)
	if err != nil || code != http.StatusOK {
		t.Fatalf("re-post: status %d %s %v", code, b, err)
	}
	want := []sessionRef{{ID: "ses_b", Title: "t ses_b", Created: sampleCreated}, {ID: "ses_a", Title: "t ses_a", Created: sampleCreated}}
	var got []sessionRef
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("POST answered %+v, want %+v", got, want)
	}
	if stored := getIndex(t, s.URL, "w1"); fmt.Sprint(stored) != fmt.Sprint(want) {
		t.Fatalf("stored index %+v, want %+v", stored, want)
	}
}

func TestSessionsValidation(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	long := strings.Repeat("é", maxSessionTitleChars)
	cases := []struct {
		name string
		wb   string
		body string
		want int
	}{
		{"valid", "w1", sessionRefBody("ses_ok", "hi", sampleCreated), http.StatusOK},
		{"created with milliseconds", "w1", sessionRefBody("ses_ms", "hi", "2026-11-01T10:00:00.000Z"), http.StatusOK},
		{"empty title", "w1", sessionRefBody("ses_empty", "", sampleCreated), http.StatusOK},
		{"title at the limit", "w1", sessionRefBody("ses_200", long, sampleCreated), http.StatusOK},
		{"title over the limit", "w1", sessionRefBody("ses_201", long+"é", sampleCreated), http.StatusBadRequest},
		{"id with a slash", "w1", sessionRefBody("ses/1", "t", sampleCreated), http.StatusBadRequest},
		{"empty id", "w1", sessionRefBody("", "t", sampleCreated), http.StatusBadRequest},
		{"created is not a time", "w1", sessionRefBody("ses_c1", "t", "yesterday"), http.StatusBadRequest},
		{"created is a date only", "w1", sessionRefBody("ses_c2", "t", "2026-11-01"), http.StatusBadRequest},
		{"unknown key", "w1", `{"id":"ses_k","title":"t","created":"2026-11-01T10:00:00Z","extra":1}`, http.StatusBadRequest},
		{"trailing data", "w1", sessionRefBody("ses_t", "t", sampleCreated) + ` {}`, http.StatusBadRequest},
		{"empty body", "w1", ``, http.StatusBadRequest},
		{"bad workbook id", "wb%201", sessionRefBody("ses_w", "t", sampleCreated), http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, b, err := sessionsRequest("POST", s.URL+"/bridge/sessions/"+c.wb, c.body, true)
			if err != nil {
				t.Fatal(err)
			}
			if code != c.want {
				t.Fatalf("status %d, want %d (%s)", code, c.want, b)
			}
		})
	}
	// Refused bodies must not have created an index.
	if stored := getIndex(t, s.URL, "w1"); len(stored) != 4 {
		t.Fatalf("index after the table has %d entries, want the 4 accepted ones", len(stored))
	}
}

func TestSessionsRequireThePaneKey(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	body := sessionRefBody("ses_auth", "t", sampleCreated)
	code, _, err := sessionsRequest("POST", s.URL+"/bridge/sessions/w1", body, false)
	if err != nil || code != http.StatusUnauthorized {
		t.Fatalf("without key: status %d %v, want 401", code, err)
	}
	req, _ := http.NewRequest("POST", s.URL+"/bridge/sessions/w1", strings.NewReader(body))
	req.Header.Set("X-Bridge-Key", "wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong key: status %d, want 401", resp.StatusCode)
	}
	if stored := getIndex(t, s.URL, "w1"); stored != nil {
		t.Fatalf("refused request wrote an index: %+v", stored)
	}
}

func TestSessionsConcurrentPostsAllSurvive(t *testing.T) {
	const n = 50
	s := newDiskBridge(t, t.TempDir())
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("ses_%03d", i)
			code, b, err := sessionsRequest("POST", s.URL+"/bridge/sessions/w1", sessionRefBody(id, "t", sampleCreated), true)
			if err != nil {
				errs <- err
				return
			}
			if code != http.StatusOK {
				errs <- fmt.Errorf("post %s: status %d %s", id, code, b)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	stored := getIndex(t, s.URL, "w1")
	seen := map[string]bool{}
	for _, r := range stored {
		seen[r.ID] = true
	}
	if len(stored) != n || len(seen) != n {
		t.Fatalf("index has %d entries (%d distinct), want %d: appends were lost", len(stored), len(seen), n)
	}
	for i := range n {
		if id := fmt.Sprintf("ses_%03d", i); !seen[id] {
			t.Errorf("entry %s was lost", id)
		}
	}
}

// TestSessionsPutAndPostNeverInterleave races the pane's whole-list PUT against many POST appends. Whatever order they
// run in, the index must be a run of POSTed ids (each at most once) on top of exactly the PUT list: an append made after
// the PUT is kept, and none is torn.
func TestSessionsPutAndPostNeverInterleave(t *testing.T) {
	const n = 30
	s := newDiskBridge(t, t.TempDir())
	pane := []sessionRef{{ID: "ses_pane", Title: "pane", Created: sampleCreated}}
	paneBody := `[` + sessionRefBody("ses_pane", "pane", sampleCreated) + `]`
	var wg sync.WaitGroup
	errs := make(chan error, n+1)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("ses_p%03d", i)
			code, b, err := sessionsRequest("POST", s.URL+"/bridge/sessions/w1", sessionRefBody(id, "t", sampleCreated), true)
			if err != nil || code != http.StatusOK {
				errs <- fmt.Errorf("post %s: status %d %s %v", id, code, b, err)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		code, b, err := sessionsRequest("PUT", s.URL+"/bridge/state/w1/sessions", paneBody, true)
		if err != nil || code != http.StatusNoContent {
			errs <- fmt.Errorf("put: status %d %s %v", code, b, err)
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	stored := getIndex(t, s.URL, "w1")
	if len(stored) < len(pane) || fmt.Sprint(stored[len(stored)-len(pane):]) != fmt.Sprint(pane) {
		t.Fatalf("index %+v does not end with the PUT list %+v", stored, pane)
	}
	seen := map[string]bool{}
	for _, r := range stored[:len(stored)-len(pane)] {
		if !strings.HasPrefix(r.ID, "ses_p") || seen[r.ID] {
			t.Fatalf("entry %q is not a fresh append (index %+v)", r.ID, stored)
		}
		seen[r.ID] = true
	}
}

// TestSessionsWritersWaitForTheIndexLock proves the PUT of the sessions key and POST share one lock per workbook: while
// the lock is held, neither completes, and both finish once it is released.
func TestSessionsWritersWaitForTheIndexLock(t *testing.T) {
	st, err := newStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sv := &srv{cfg: Config{PaneKey: key}, state: st, sessionLocks: map[string]*sync.Mutex{}}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /bridge/state/{workbook}/{key}", sv.putState)
	mux.HandleFunc("POST /bridge/sessions/{workbook}", sv.postSession)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	cases := []struct {
		name, method, path, body string
	}{
		{"PUT of the sessions key", "PUT", "/bridge/state/w1/sessions", `[]`},
		{"POST of a session", "POST", "/bridge/sessions/w1", sessionRefBody("ses_lock", "t", sampleCreated)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lock := sv.sessionsLock("w1")
			lock.Lock()
			done := make(chan int, 1)
			go func() {
				code, _, err := sessionsRequest(c.method, ts.URL+c.path, c.body, false)
				if err != nil {
					code = -1
				}
				done <- code
			}()
			select {
			case code := <-done:
				lock.Unlock()
				t.Fatalf("finished with status %d while the index lock was held", code)
			case <-time.After(100 * time.Millisecond):
			}
			lock.Unlock()
			select {
			case code := <-done:
				if code != http.StatusOK && code != http.StatusNoContent {
					t.Fatalf("status %d after the lock was released", code)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not finish after the lock was released")
			}
		})
	}
}

func TestDesktopTitleCutsTheFirstSixtyCharacters(t *testing.T) {
	cases := []struct {
		name, message, want string
	}{
		{"short message", "hi", "[desktop] hi"},
		{"exactly sixty", strings.Repeat("a", 60), "[desktop] " + strings.Repeat("a", 60)},
		{"sixty one", strings.Repeat("a", 61), "[desktop] " + strings.Repeat("a", 60)},
		{"multibyte cut by characters", strings.Repeat("é", 61), "[desktop] " + strings.Repeat("é", 60)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := desktopTitle(c.message); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSendWorkbookChatRegistersANewChatThenContinuesIt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/w1", 0o700); err != nil {
		t.Fatal(err)
	}
	msgs := transcriptFixture()
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chat":
			tailJSON(w, http.StatusOK, `{"sessionId":"ses_brand"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions/ses_brand/status":
			tailJSON(w, http.StatusOK, `{"permission_mode":"normal"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions/ses_brand/message":
			tailJSON(w, http.StatusAccepted, `{"sessionId":"ses_brand"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions/ses_brand":
			tailJSON(w, http.StatusOK, sessionBody(msgs, len(msgs)))
		default:
			tailJSON(w, http.StatusNotFound, "unexpected "+r.Method+" "+r.URL.Path)
		}
	})
	s := newLocalBridge(t, ocode.URL, root)
	openPaneStream(t, s.URL, "w1", "pane-w1")

	text, isErr := callTool(t, s.URL, "send_workbook_chat", map[string]any{"workbook": "w1", "message": strings.Repeat("x", 70)})
	if isErr || text != `{"session":"ses_brand","started":true}` {
		t.Fatalf("start: isErr=%v %q", isErr, text)
	}
	index := getIndex(t, s.URL, "w1")
	if len(index) != 1 || index[0].ID != "ses_brand" || index[0].Title != "[desktop] "+strings.Repeat("x", 60) {
		t.Fatalf("index after start %+v", index)
	}
	if _, err := time.Parse(time.RFC3339, index[0].Created); err != nil {
		t.Fatalf("created %q: %v", index[0].Created, err)
	}

	text, isErr = callTool(t, s.URL, "send_workbook_chat", map[string]any{"workbook": "w1", "message": "follow up", "session": "ses_brand"})
	if isErr || text != `{"session":"ses_brand","started":false}` {
		t.Fatalf("continue: isErr=%v %q", isErr, text)
	}
	text, isErr = callTool(t, s.URL, "read_workbook_chat", map[string]any{"workbook": "w1", "session": "ses_brand"})
	if isErr || !strings.Contains(text, `"session":"ses_brand"`) {
		t.Fatalf("read: isErr=%v %q", isErr, text)
	}
	var paths []string
	for _, r := range fo.requests() {
		paths = append(paths, r.method+" "+r.path)
	}
	want := "[POST /api/chat GET /api/sessions/ses_brand/status POST /api/sessions/ses_brand/message GET /api/sessions/ses_brand]"
	if fmt.Sprint(paths) != want {
		t.Fatalf("ocode requests %v, want %s", paths, want)
	}
}

// A chat the tool started in normal mode is still gated when it is continued: the mode is read again on every
// continuation, so a chat the person switched to yolo in the pane is refused, and its message is never posted.
func TestSendWorkbookChatContinuationRefusesAChatSwitchedToYolo(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/w1", 0o700); err != nil {
		t.Fatal(err)
	}
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/chat":
			tailJSON(w, http.StatusOK, `{"sessionId":"ses_brand"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions/ses_brand/status":
			tailJSON(w, http.StatusOK, `{"permission_mode":"yolo"}`)
		default:
			tailJSON(w, http.StatusNotFound, "unexpected "+r.Method+" "+r.URL.Path)
		}
	})
	s := newLocalBridge(t, ocode.URL, root)
	openPaneStream(t, s.URL, "w1", "pane-w1")

	if text, isErr := callTool(t, s.URL, "send_workbook_chat", map[string]any{"workbook": "w1", "message": "start"}); isErr || text != `{"session":"ses_brand","started":true}` {
		t.Fatalf("start: isErr=%v %q", isErr, text)
	}
	text, isErr := callTool(t, s.URL, "send_workbook_chat", map[string]any{"workbook": "w1", "message": "follow up", "session": "ses_brand"})
	if !isErr || !strings.Contains(text, "this chat is in yolo mode") {
		t.Fatalf("continue: want a yolo refusal, got isErr=%v %q", isErr, text)
	}
	var paths []string
	for _, r := range fo.requests() {
		paths = append(paths, r.method+" "+r.path)
	}
	want := "[POST /api/chat GET /api/sessions/ses_brand/status]"
	if fmt.Sprint(paths) != want {
		t.Fatalf("ocode requests %v, want %s", paths, want)
	}
}
