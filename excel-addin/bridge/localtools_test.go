package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// ocodeReq is one request the fake ocode received.
type ocodeReq struct {
	method, path, query, auth string
	body                      []byte
}

// fakeOcode is ocode serve for one test. Every request it gets is recorded.
type fakeOcode struct {
	mu   sync.Mutex
	seen []ocodeReq
}

func startFakeOcode(t *testing.T, route func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *fakeOcode) {
	t.Helper()
	fo := &fakeOcode{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fo.mu.Lock()
		fo.seen = append(fo.seen, ocodeReq{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, auth: r.Header.Get("Authorization"), body: body})
		fo.mu.Unlock()
		route(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, fo
}

func (f *fakeOcode) requests() []ocodeReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ocodeReq(nil), f.seen...)
}

// callTool runs one tools/call over /mcp and returns the text and whether the result is a tool error.
func callTool(t *testing.T, base, name string, args map[string]any) (string, bool) {
	t.Helper()
	_, out := rpc(t, base, "tools/call", map[string]any{"name": name, "arguments": args}, true)
	if out["error"] != nil {
		t.Fatalf("%s: protocol error %s", name, out["error"])
	}
	var res struct {
		Content []toolContent `json:"content"`
		IsError bool          `json:"isError"`
	}
	if err := json.Unmarshal(out["result"], &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: want one content block, got %+v", name, res)
	}
	return res.Content[0].Text, res.IsError
}

// putState writes one state key through the bridge's own route.
func putState(t *testing.T, base, wb, k string, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	req, _ := http.NewRequest("PUT", base+"/bridge/state/"+wb+"/"+k, bytes.NewReader(b))
	req.Header.Set("X-Bridge-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("put %s/%s: status %d", wb, k, resp.StatusCode)
	}
}

// tailJSON writes a JSON body with the given status.
func tailJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestLocalToolsAreNotInTheSharedContract(t *testing.T) {
	c, err := loadContract()
	if err != nil {
		t.Fatal(err)
	}
	local := map[string]bool{"list_workbooks": true, "read_workbook_chat": true, "send_workbook_chat": true}
	for _, tl := range c.Tools {
		if local[tl.Name] {
			t.Fatalf("local tool %q must not be in tools.json (the pane would be asked to run it)", tl.Name)
		}
	}
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	_, out := rpc(t, s.URL, "tools/list", map[string]any{}, true)
	var res struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(out["result"], &res); err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, tl := range res.Tools {
		listed[tl.Name] = true
	}
	if len(res.Tools) != len(c.Tools)+len(local) {
		t.Fatalf("tools/list has %d tools, want %d contract tools plus %d local", len(res.Tools), len(c.Tools), len(local))
	}
	for name := range local {
		if !listed[name] {
			t.Fatalf("%q missing from tools/list", name)
		}
	}
}

func TestLocalToolsRejectUnknownAndExtraArguments(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"list_workbooks", map[string]any{"limit": 0}},
		{"list_workbooks", map[string]any{"limit": 201}},
		{"list_workbooks", map[string]any{"page": 2}},
		{"read_workbook_chat", map[string]any{"limit": 101}},
		{"read_workbook_chat", map[string]any{"limit": "5"}},
		{"send_workbook_chat", map[string]any{"message": "hi", "permission_mode": "yolo"}},
	} {
		if text, isErr := callTool(t, s.URL, c.name, c.args); !isErr {
			t.Fatalf("%s %v: want a tool error, got %q", c.name, c.args, text)
		}
	}
}

func TestListWorkbooksToolMatchesTheListing(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	openPaneStream(t, s.URL, "w1", "pane-w1")
	text, isErr := callTool(t, s.URL, "list_workbooks", map[string]any{"limit": 1})
	if isErr {
		t.Fatalf("list_workbooks failed: %s", text)
	}
	var got workbookListing
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not the listing JSON: %v: %s", err, text)
	}
	if got.Total != 1 || len(got.Workbooks) != 1 || got.Workbooks[0].Workbook != "w1" {
		t.Fatalf("got %+v", got)
	}
}

func TestTruncateChatCutsAndSaysHowMuch(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"short unchanged", "hello", "hello"},
		{"exactly the limit unchanged", strings.Repeat("a", maxChatContentChars), strings.Repeat("a", maxChatContentChars)},
		{"one over", strings.Repeat("a", maxChatContentChars+1), strings.Repeat("a", maxChatContentChars) + "…[truncated 1 chars]"},
		{"counts runes not bytes", strings.Repeat("é", maxChatContentChars+5), strings.Repeat("é", maxChatContentChars) + "…[truncated 5 chars]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := truncateChat(c.in); got != c.want {
				t.Fatalf("got %d bytes, want %d bytes", len(got), len(c.want))
			}
		})
	}
}

// transcriptFixture is a session as ocode returns it: a system prompt, a long user message, a tool round, an answer.
func transcriptFixture() []map[string]any {
	return []map[string]any{
		{"role": "system", "content": "SYSTEM_PROMPT_TEXT"},
		{"role": "user", "content": strings.Repeat("x", 2500)},
		{"role": "assistant", "content": "", "tool_calls": []map[string]any{{"id": "t1", "function": map[string]any{"name": "read_range", "arguments": `{"address":"A1"}`}}}},
		{"role": "tool", "content": "TOOL_BODY_SECRET", "tool_call_id": "t1"},
		{"role": "assistant", "content": "The sheet has 3 rows."},
	}
}

func sessionBody(msgs []map[string]any, total int) string {
	b, _ := json.Marshal(map[string]any{"id": "ses_new", "title": "[Excel] Budget.xlsx", "messages": msgs, "total": total})
	return string(b)
}

// chatMessages are the fields the read tool returns, decoded for assertions.
type chatResult struct {
	Session  string        `json:"session"`
	Title    string        `json:"title"`
	Total    int           `json:"total"`
	Messages []chatMessage `json:"messages"`
}

func readChat(t *testing.T, base string, args map[string]any) (chatResult, string, bool) {
	t.Helper()
	text, isErr := callTool(t, base, "read_workbook_chat", args)
	var res chatResult
	if !isErr {
		if err := json.Unmarshal([]byte(text), &res); err != nil {
			t.Fatalf("read result is not JSON: %v: %s", err, text)
		}
	}
	return res, text, isErr
}

func TestReadWorkbookChatReturnsNewestNonSystemMessages(t *testing.T) {
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		tailJSON(w, http.StatusOK, sessionBody(transcriptFixture(), 5))
	})
	s := newLocalBridge(t, ocode.URL, t.TempDir())
	openPaneStream(t, s.URL, "w1", "pane-w1")
	putState(t, s.URL, "w1", "sessions", []sessionRef{{ID: "ses_new", Title: "[Excel] Budget.xlsx"}, {ID: "ses_old"}})

	res, text, isErr := readChat(t, s.URL, map[string]any{"workbook": "w1", "limit": 10})
	if isErr {
		t.Fatalf("read failed: %s", text)
	}
	if res.Session != "ses_new" || res.Title != "[Excel] Budget.xlsx" || res.Total != 5 {
		t.Fatalf("session %q title %q total %d", res.Session, res.Title, res.Total)
	}
	if len(res.Messages) != 4 {
		t.Fatalf("want 4 non-system messages, got %d", len(res.Messages))
	}
	if !strings.HasPrefix(res.Messages[0].Content, strings.Repeat("x", 2000)) || res.Messages[0].Content != strings.Repeat("x", 2000)+"…[truncated 500 chars]" {
		t.Fatalf("long message not cut with the marker: %d bytes", len(res.Messages[0].Content))
	}
	if got := res.Messages[1]; got.Role != "assistant" || got.Content != "" || strings.Join(got.ToolCalls, ",") != "read_range" {
		t.Fatalf("tool call round: %+v", got)
	}
	if got := res.Messages[2]; got.Role != "tool" || got.Content != "" {
		t.Fatalf("tool result must carry no body: %+v", got)
	}
	for _, leak := range []string{"SYSTEM_PROMPT_TEXT", "TOOL_BODY_SECRET", "ocode-secret", "k-test", "t1"} {
		if strings.Contains(text, leak) {
			t.Fatalf("output leaks %q", leak)
		}
	}
	reqs := fo.requests()
	if len(reqs) != 1 || reqs[0].method != "GET" || reqs[0].path != "/api/sessions/ses_new" || reqs[0].query != "limit=40" {
		t.Fatalf("ocode requests %+v", reqs)
	}
	if reqs[0].auth != "Bearer ocode-secret" {
		t.Fatalf("ocode got Authorization %q, want the bridge's own token", reqs[0].auth)
	}
}

func TestReadWorkbookChatWidensTheWindowUntilItHoldsEnough(t *testing.T) {
	// 150 messages: indexes 0-119 are user or assistant, 120-149 are system. The fake honours limit like ocode does.
	var msgs []map[string]any
	for i := 0; i < 150; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if i >= 120 {
			role = "system"
		}
		msgs = append(msgs, map[string]any{"role": role, "content": fmt.Sprintf("m%d", i)})
	}
	ocode, _ := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		from := max(0, len(msgs)-n)
		tailJSON(w, http.StatusOK, sessionBody(msgs[from:], len(msgs)))
	})
	s := newLocalBridge(t, ocode.URL, t.TempDir())
	openPaneStream(t, s.URL, "w1", "pane-w1")
	putState(t, s.URL, "w1", "sessions", []sessionRef{{ID: "ses_new"}})

	res, text, isErr := readChat(t, s.URL, map[string]any{"workbook": "w1", "limit": 5})
	if isErr {
		t.Fatalf("read failed: %s", text)
	}
	var got []string
	for _, m := range res.Messages {
		got = append(got, m.Content)
	}
	if strings.Join(got, ",") != "m115,m116,m117,m118,m119" {
		t.Fatalf("got %v, want the last 5 non-system messages", got)
	}
}

func TestReadWorkbookChatRefusesSessionsOutsideTheWorkbook(t *testing.T) {
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		tailJSON(w, http.StatusOK, sessionBody(transcriptFixture(), 5))
	})
	s := newLocalBridge(t, ocode.URL, t.TempDir())
	openPaneStream(t, s.URL, "w1", "pane-w1")
	putState(t, s.URL, "w1", "sessions", []sessionRef{{ID: "ses_mine"}})
	openPaneStream(t, s.URL, "w2", "pane-w2")
	putState(t, s.URL, "w2", "sessions", []sessionRef{{ID: "ses_theirs"}})

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"session of another workbook", map[string]any{"workbook": "w1", "session": "ses_theirs"}, "not one of workbook w1's chat sessions"},
		{"path-like session", map[string]any{"workbook": "w1", "session": "../ses_mine"}, "not one of workbook w1's chat sessions"},
		{"unknown workbook", map[string]any{"workbook": "zzz"}, "no Excel task pane is connected"},
		{"several panes and no workbook", map[string]any{}, "several workbooks are connected"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, text, isErr := readChat(t, s.URL, c.args)
			if !isErr || !strings.Contains(text, c.want) {
				t.Fatalf("want error containing %q, got isErr=%v %q", c.want, isErr, text)
			}
		})
	}
	if n := len(fo.requests()); n != 0 {
		t.Fatalf("refused reads must not reach ocode, %d requests made", n)
	}
}

func TestReadWorkbookChatWithNoSessionsOrNoPane(t *testing.T) {
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		tailJSON(w, http.StatusOK, sessionBody(transcriptFixture(), 5))
	})
	s := newLocalBridge(t, ocode.URL, t.TempDir())
	if _, text, isErr := readChat(t, s.URL, map[string]any{}); !isErr || !strings.Contains(text, "no Excel task pane is connected") {
		t.Fatalf("no pane: %q", text)
	}
	openPaneStream(t, s.URL, "w1", "pane-w1")
	if _, text, isErr := readChat(t, s.URL, map[string]any{}); !isErr || !strings.Contains(text, "no chat sessions yet") {
		t.Fatalf("no index: %q", text)
	}
	if n := len(fo.requests()); n != 0 {
		t.Fatalf("%d ocode requests for refused reads", n)
	}
}

func TestReadWorkbookChatReportsOcodeFailure(t *testing.T) {
	ocode, _ := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		tailJSON(w, http.StatusInternalServerError, "boom")
	})
	s := newLocalBridge(t, ocode.URL, t.TempDir())
	openPaneStream(t, s.URL, "w1", "pane-w1")
	putState(t, s.URL, "w1", "sessions", []sessionRef{{ID: "ses_new"}})
	_, text, isErr := readChat(t, s.URL, map[string]any{"workbook": "w1"})
	if !isErr || !strings.Contains(text, "read_workbook_chat failed") || strings.Contains(text, "boom") {
		t.Fatalf("ocode 500 must be a short tool error without the upstream body, got isErr=%v %q", isErr, text)
	}
}

// Internal failures (paths, upstream bodies) are logged, and the model gets only a short failure of the tool.
func TestLocalToolInternalErrorsStayOutOfTheModelText(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+"/w1", []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		tailJSON(w, http.StatusOK, `{"sessionId":"x"}`)
	})
	s := newLocalBridge(t, ocode.URL, root)
	openPaneStream(t, s.URL, "w1", "pane-w1")
	text, isErr := callTool(t, s.URL, "send_workbook_chat", map[string]any{"message": "hi"})
	if !isErr || !strings.Contains(text, "send_workbook_chat failed") || strings.Contains(text, root) {
		t.Fatalf("want a short tool failure without the workspace path, got isErr=%v %q", isErr, text)
	}
	if n := len(fo.requests()); n != 0 {
		t.Fatalf("%d ocode requests for a workspace that is not a folder", n)
	}
}

func TestSendWorkbookChatIntoAnExistingSession(t *testing.T) {
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			tailJSON(w, http.StatusOK, `{"permission_mode":"normal"}`)
			return
		}
		tailJSON(w, http.StatusAccepted, `{"sessionId":"ses_mine"}`)
	})
	s := newLocalBridge(t, ocode.URL, t.TempDir())
	openPaneStream(t, s.URL, "w1", "pane-w1")
	putState(t, s.URL, "w1", "sessions", []sessionRef{{ID: "ses_mine"}})

	text, isErr := callTool(t, s.URL, "send_workbook_chat", map[string]any{"workbook": "w1", "message": "check B2", "session": "ses_mine"})
	if isErr || text != `{"session":"ses_mine","started":false}` {
		t.Fatalf("got isErr=%v %q", isErr, text)
	}
	reqs := fo.requests()
	if len(reqs) != 2 || reqs[0].method != "GET" || reqs[0].path != "/api/sessions/ses_mine/status" ||
		reqs[1].method != "POST" || reqs[1].path != "/api/sessions/ses_mine/message" {
		t.Fatalf("ocode requests %+v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal(reqs[1].body, &body); err != nil {
		t.Fatal(err)
	}
	if body["content"] != "[from ocode desktop] check B2" || body["async"] != true {
		t.Fatalf("body %v", body)
	}
	if reqs[1].auth != "Bearer ocode-secret" {
		t.Fatalf("Authorization %q", reqs[1].auth)
	}
}

// TestSendWorkbookChatOnlyPostsIntoANormalChat: a message reaches an existing chat only when ocode's status route
// reports exactly "normal". Any other answer, including a failed call, a missing field or an unknown mode, is refused
// before the message POST, so no message is ever recorded for it.
func TestSendWorkbookChatOnlyPostsIntoANormalChat(t *testing.T) {
	const statusPath = "/api/sessions/ses_mine/status"
	const postPath = "/api/sessions/ses_mine/message"
	cases := []struct {
		name       string
		status     int
		statusBody string
		wantPost   bool
		wantErr    string
	}{
		{"normal posts", http.StatusOK, `{"permission_mode":"normal"}`, true, ""},
		{"yolo refused", http.StatusOK, `{"permission_mode":"yolo"}`, false, "this chat is in yolo mode; send_workbook_chat only posts into chats in normal mode. Switch the chat to Ask in the pane first."},
		{"locked refused", http.StatusOK, `{"permission_mode":"locked"}`, false, "this chat is in locked mode; send_workbook_chat only posts into chats in normal mode. Switch the chat to Ask in the pane first."},
		{"sandbox refused", http.StatusOK, `{"permission_mode":"sandbox"}`, false, "this chat is in sandbox mode; send_workbook_chat only posts into chats in normal mode. Switch the chat to Ask in the pane first."},
		{"unknown mode refused", http.StatusOK, `{"permission_mode":"auto"}`, false, `ocode reported permission mode "auto" for chat ses_mine`},
		{"empty mode refused", http.StatusOK, `{"permission_mode":""}`, false, `ocode reported permission mode "" for chat ses_mine`},
		{"field missing refused", http.StatusOK, `{"title":"Budget"}`, false, "cannot read chat ses_mine's permission mode, so the message was not sent"},
		{"not json refused", http.StatusOK, `<html>`, false, "cannot read chat ses_mine's permission mode, so the message was not sent"},
		{"status 500 refused", http.StatusInternalServerError, "boom", false, "cannot read chat ses_mine's permission mode, so the message was not sent"},
		{"status 404 refused", http.StatusNotFound, `{"error":"session not found"}`, false, "cannot read chat ses_mine's permission mode, so the message was not sent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == statusPath {
					tailJSON(w, c.status, c.statusBody)
					return
				}
				tailJSON(w, http.StatusAccepted, `{"sessionId":"ses_mine"}`)
			})
			s := newLocalBridge(t, ocode.URL, t.TempDir())
			openPaneStream(t, s.URL, "w1", "pane-w1")
			putState(t, s.URL, "w1", "sessions", []sessionRef{{ID: "ses_mine"}})

			text, isErr := callTool(t, s.URL, "send_workbook_chat", map[string]any{"workbook": "w1", "message": "check B2", "session": "ses_mine"})
			var paths []string
			for _, r := range fo.requests() {
				paths = append(paths, r.method+" "+r.path)
			}
			if c.wantPost {
				if isErr || text != `{"session":"ses_mine","started":false}` {
					t.Fatalf("got isErr=%v %q", isErr, text)
				}
				if want := fmt.Sprint([]string{"GET " + statusPath, "POST " + postPath}); fmt.Sprint(paths) != want {
					t.Fatalf("ocode requests %v, want %s", paths, want)
				}
				return
			}
			if !isErr || !strings.Contains(text, c.wantErr) {
				t.Fatalf("want error containing %q, got isErr=%v %q", c.wantErr, isErr, text)
			}
			if fmt.Sprint(paths) != fmt.Sprint([]string{"GET " + statusPath}) {
				t.Fatalf("refused message must make only the status call, ocode saw %v", paths)
			}
		})
	}
}

func TestSendWorkbookChatStartsANewChatInTheWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/w1", 0o700); err != nil {
		t.Fatal(err)
	}
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		tailJSON(w, http.StatusOK, `{"sessionId":"ses_brand","model":"m"}`)
	})
	s := newLocalBridge(t, ocode.URL, root)
	openPaneStream(t, s.URL, "w1", "pane-w1")
	index := []sessionRef{{ID: "ses_old"}}
	putState(t, s.URL, "w1", "sessions", index)

	text, isErr := callTool(t, s.URL, "send_workbook_chat", map[string]any{"workbook": "w1", "message": strings.Repeat("é", 8000)})
	if isErr || text != `{"session":"ses_brand","started":true}` {
		t.Fatalf("got isErr=%v %q", isErr, text)
	}
	reqs := fo.requests()
	if len(reqs) != 1 || reqs[0].path != "/api/chat" {
		t.Fatalf("ocode requests %+v", reqs)
	}
	var body map[string]any
	if err := json.Unmarshal(reqs[0].body, &body); err != nil {
		t.Fatal(err)
	}
	if body["project_path"] != root+"/w1" || body["permission_mode"] != "normal" || body["async"] != true {
		t.Fatalf("chat body %v", body)
	}
	if pinned, _ := body["pinned_mcp_servers"].([]any); len(pinned) != 1 || pinned[0] != "excel" {
		t.Fatalf("a bridge-started chat must pin exactly the excel MCP server, got %v", body["pinned_mcp_servers"])
	}
	if body["content"] != "[from ocode desktop] "+strings.Repeat("é", 8000) {
		t.Fatalf("content not prefixed or not sent whole: %v", body["content"])
	}
	// A bridge-started chat is registered at the front of the index; the pane's own entries stay behind it.
	req, _ := http.NewRequest("GET", s.URL+"/bridge/state/w1/sessions", nil)
	req.Header.Set("X-Bridge-Key", key)
	got, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	var stored []sessionRef
	if err := json.NewDecoder(got.Body).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0].ID != "ses_brand" || stored[1].ID != "ses_old" {
		t.Fatalf("session index is %+v, want ses_brand then ses_old", stored)
	}
}

func TestSendWorkbookChatRefusals(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/w1", 0o700); err != nil {
		t.Fatal(err)
	}
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		tailJSON(w, http.StatusInternalServerError, "chat failed")
	})
	s := newLocalBridge(t, ocode.URL, root)
	openPaneStream(t, s.URL, "w1", "pane-w1")
	putState(t, s.URL, "w1", "sessions", []sessionRef{{ID: "ses_mine"}})

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"empty message", map[string]any{"workbook": "w1", "message": ""}, "message must be 1 to 8000"},
		{"whitespace message", map[string]any{"workbook": "w1", "message": "  \n "}, "message must be 1 to 8000"},
		{"oversize message", map[string]any{"workbook": "w1", "message": strings.Repeat("a", 8001)}, "message must be 1 to 8000"},
		{"session of another workbook", map[string]any{"workbook": "w1", "message": "hi", "session": "ses_theirs"}, "not one of workbook w1's chat sessions"},
		{"unknown workbook", map[string]any{"workbook": "zzz", "message": "hi"}, "no Excel task pane is connected"},
		{"new chat fails in ocode", map[string]any{"workbook": "w1", "message": "hi"}, "send_workbook_chat failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, isErr := callTool(t, s.URL, "send_workbook_chat", c.args)
			if !isErr || !strings.Contains(text, c.want) {
				t.Fatalf("want error containing %q, got isErr=%v %q", c.want, isErr, text)
			}
		})
	}
	// Only the new-chat failure may reach ocode, and it does so once.
	if n := len(fo.requests()); n != 1 {
		t.Fatalf("%d ocode requests, want 1 (the new-chat failure)", n)
	}
}

func TestSendWorkbookChatNeedsAWorkspaceFolderForANewChat(t *testing.T) {
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		tailJSON(w, http.StatusOK, `{"sessionId":"x"}`)
	})
	s := newLocalBridge(t, ocode.URL, t.TempDir()) // the workbook's folder is not created
	openPaneStream(t, s.URL, "w1", "pane-w1")
	text, isErr := callTool(t, s.URL, "send_workbook_chat", map[string]any{"message": "hi"})
	if !isErr || !strings.Contains(text, "has no workspace folder yet") {
		t.Fatalf("got isErr=%v %q", isErr, text)
	}
	if n := len(fo.requests()); n != 0 {
		t.Fatalf("%d ocode requests without a workspace", n)
	}
}
