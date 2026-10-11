package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

// writeTarget returns a handler with one resident, scripted target session
// persisted on disk.
func writeTarget(t *testing.T) (*Handler, string, *scriptedAutoContinueClient) {
	t.Helper()
	h, id, cl := autoContinueTestServer(t, "")
	h.mu.Lock()
	h.cfg.Model = "fake-model"
	h.mu.Unlock()
	root := h.sessions.Lookup(id).ProjectRoot
	if err := session.SaveForDir(root, id, "old title", []agent.Message{{Role: "user", Content: "earlier"}}, nil); err != nil {
		t.Fatal(err)
	}
	h.lookupAgentSession(id).messages = []agent.Message{{Role: "user", Content: "earlier"}}
	return h, id, cl
}

func pulseWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSessionSendTool(t *testing.T) {
	h, id, cl := writeTarget(t)

	out := mustPulseTool(t, h, "session_send", `{"session_id":"`+id+`","content":"please continue"}`)
	t.Logf("session_send -> %s", out)
	if out != `{"accepted":true,"session_id":"`+id+`"}` {
		t.Fatalf("result = %s", out)
	}
	as := h.lookupAgentSession(id)
	pulseWaitFor(t, "the target's turn to finish", func() bool {
		as.mu.Lock()
		defer as.mu.Unlock()
		n := len(as.messages)
		return n >= 3 && as.messages[n-1].Role == "assistant" && !h.sessions.IsTurnActive(id)
	})
	as.mu.Lock()
	var sawUser bool
	for _, m := range as.messages {
		if m.Role == "user" && m.Content == "please continue" {
			sawUser = true
		}
	}
	as.mu.Unlock()
	if !sawUser {
		t.Fatal("target transcript lacks the sent message")
	}
	cl.mu.Lock()
	calls := cl.calls
	cl.mu.Unlock()
	if calls == 0 {
		t.Fatal("target's model was never called")
	}

	for name, args := range map[string]string{
		"own id":        `{"session_id":"` + pulseSessionPrefix + `x","content":"hi"}`,
		"child id":      `{"session_id":"ses_p_child_explore_1","content":"hi"}`,
		"unknown":       `{"session_id":"ses_nope","content":"hi"}`,
		"empty content": `{"session_id":"` + id + `","content":"  "}`,
		"no session":    `{"content":"hi"}`,
	} {
		if _, err := runPulseTool(t, h, "session_send", args); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestSessionSendRefusesBusyTargets(t *testing.T) {
	h, id, _ := writeTarget(t)
	args := `{"session_id":"` + id + `","content":"hi"}`

	h.sessions.setTurnActive(id, true)
	_, err := runPulseTool(t, h, "session_send", args)
	h.sessions.setTurnActive(id, false)
	if err == nil || !strings.Contains(err.Error(), "mid-turn") {
		t.Fatalf("mid-turn target: err = %v", err)
	}

	as := h.lookupAgentSession(id)
	as.mu.Lock()
	as.messages = append(as.messages, toolCallMessage("bash", "{}"),
		permissionAskMessage("call-1", samplePermissionRequest()))
	as.mu.Unlock()
	_, err = runPulseTool(t, h, "session_send", args)
	if err == nil || !strings.Contains(err.Error(), "pending permission ask") {
		t.Fatalf("paused target: err = %v", err)
	}
	// Nothing was queued behind the refusal.
	as.mu.Lock()
	for _, m := range as.messages {
		if m.Content == "hi" {
			t.Fatal("refused message was queued anyway")
		}
	}
	as.mu.Unlock()
}

func TestSessionCommandTool(t *testing.T) {
	h, id, _ := writeTarget(t)
	call := func(command, args string) (string, error) {
		raw, _ := json.Marshal(map[string]string{"session_id": id, "command": command, "args": args})
		return runPulseTool(t, h, "session_command", string(raw))
	}

	// Allowlist: anything else is an error naming the supported commands.
	for _, bad := range []string{"/clear", "/model", "/shell", "rm", "/exit", ""} {
		_, err := call(bad, "")
		if err == nil || !strings.Contains(err.Error(), "/compact") {
			t.Errorf("command %q: err = %v, want rejection listing supported commands", bad, err)
		}
	}
	if _, err := call("/btw", ""); err == nil {
		t.Error("/btw without a question must fail")
	}
	if _, err := call("/title", ""); err == nil {
		t.Error("/title without text must fail")
	}
	if _, err := runPulseTool(t, h, "session_command", `{"session_id":"`+pulseSessionPrefix+`x","command":"/cancel"}`); err == nil {
		t.Error("own id must be refused")
	}

	out, err := call("/title", "New title")
	if err != nil {
		t.Fatalf("/title: %v", err)
	}
	t.Logf("session_command /title -> %s", out)
	if out != `{"command":"/title","result":{"title":"New title"},"session_id":"`+id+`"}` {
		t.Fatalf("/title result = %s", out)
	}
	root := h.sessions.Lookup(id).ProjectRoot
	if got, _, _ := session.TitleForDir(root, id); got != "New title" {
		t.Fatalf("stored title = %q", got)
	}

	out, err = call("cancel", "") // leading slash optional
	if err != nil {
		t.Fatalf("/cancel: %v", err)
	}
	if !strings.Contains(out, `"cancelled":true`) {
		t.Fatalf("/cancel result = %s", out)
	}

	out, err = call("/recap", "")
	if err != nil {
		t.Fatalf("/recap: %v", err)
	}
	t.Logf("session_command /recap -> %s", out)
	if !strings.Contains(out, `"recap":"final answer"`) {
		t.Fatalf("/recap result = %s", out)
	}

	out, err = call("/btw", "what are you doing?")
	if err != nil {
		t.Fatalf("/btw: %v", err)
	}
	if !strings.Contains(out, `"status":"started"`) {
		t.Fatalf("/btw result = %s", out)
	}
	h.cancelBtwRun(id, true)
}

func TestPermissionResolveAndQuestionAnswerTools(t *testing.T) {
	h, id, _ := writeTarget(t)
	for name, args := range map[string]string{
		"bad decision":   `{"session_id":"` + id + `","request_id":"c1","decision":"always_tool"}`,
		"no request id":  `{"session_id":"` + id + `","decision":"allow"}`,
		"unknown ask":    `{"session_id":"` + id + `","request_id":"c1","decision":"allow"}`,
		"own session":    `{"session_id":"` + pulseSessionPrefix + `x","request_id":"c1","decision":"allow"}`,
		"unknown target": `{"session_id":"ses_nope","request_id":"c1","decision":"deny"}`,
	} {
		if _, err := runPulseTool(t, h, "permission_resolve", args); err == nil {
			t.Errorf("permission_resolve %s must fail", name)
		}
	}
	for name, args := range map[string]string{
		"answers not array": `{"session_id":"` + id + `","request_id":"c1","answers":{}}`,
		"no answers":        `{"session_id":"` + id + `","request_id":"c1"}`,
		"unknown ask":       `{"session_id":"` + id + `","request_id":"c1","answers":[]}`,
		"own session":       `{"session_id":"` + pulseSessionPrefix + `x","request_id":"c1","answers":[]}`,
	} {
		if _, err := runPulseTool(t, h, "question_answer", args); err == nil {
			t.Errorf("question_answer %s must fail", name)
		}
	}
}

// A pending permission on the target is resolved through the tool, via the same
// handler the dialog uses.
func TestPermissionResolveToolDeniesPendingAsk(t *testing.T) {
	h, id, _ := writeTarget(t)
	as := h.lookupAgentSession(id)
	as.mu.Lock()
	as.messages = append(as.messages, toolCallMessage("bash", `{"command":"rm -rf build"}`),
		permissionAskMessage("c1", samplePermissionRequest()))
	as.mu.Unlock()

	out, err := runPulseTool(t, h, "permission_resolve", `{"session_id":"`+id+`","request_id":"c1","decision":"deny"}`)
	if err != nil {
		t.Fatalf("permission_resolve: %v", err)
	}
	if !strings.Contains(out, `"accepted":true`) {
		t.Fatalf("result = %s", out)
	}
	pulseWaitFor(t, "the ask to be replaced by a denial", func() bool {
		as.mu.Lock()
		defer as.mu.Unlock()
		for _, m := range as.messages {
			if m.ToolID == "c1" && strings.HasPrefix(m.Content, "denied:") {
				return true
			}
		}
		return false
	})
}

// ---- the permission design: write tools Ask, read tools and memory_write do not.

type askScriptClient struct {
	mu    sync.Mutex
	calls int
	call  agent.ToolCall
}

func (c *askScriptClient) Chat(_ []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return &agent.Message{Role: "assistant", ToolCalls: []agent.ToolCall{c.call}}, nil
	}
	return &agent.Message{Role: "assistant", Content: "done"}, nil
}
func (c *askScriptClient) GetProvider() string { return "fake" }
func (c *askScriptClient) GetModel() string    { return "fake-model" }

func pulseAgentWithScript(t *testing.T, h *Handler, name, args string) (*agent.Agent, *askScriptClient) {
	t.Helper()
	var tc agent.ToolCall
	tc.ID, tc.Type = "call-1", "function"
	tc.Function.Name, tc.Function.Arguments = name, args
	cl := &askScriptClient{call: tc}
	cfg := &config.Config{}
	ag := agent.NewAgent(cl, nil, cfg, nil)
	h.configurePulseAgent(ag, cfg)
	t.Cleanup(ag.Shutdown)
	return ag, cl
}

func TestWriteToolRaisesAskThenExecutesWhenAllowed(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := pulseTestHandler(t)
	ag, _ := pulseAgentWithScript(t, h, "session_send", `{"session_id":"ses_missing","content":"hi"}`)

	msgs, err := ag.Step([]agent.Message{{Role: "user", Content: "tell it hi"}})
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	var ask *agent.Message
	for i := range msgs {
		if msgs[i].Role == "tool" && strings.HasPrefix(msgs[i].Content, tool.SentinelPermissionAsk) {
			ask = &msgs[i]
		}
	}
	if ask == nil {
		t.Fatalf("write tool did not raise a permission ask; messages: %+v", msgs)
	}
	req, ok := parsePermissionAsk(ask.Content)
	if !ok || req.ToolName != "session_send" {
		t.Fatalf("ask = %+v ok=%v", req, ok)
	}

	// Resolve with allow through the real handler: the tool then executes (and,
	// for a nonexistent target, reports its own error instead of the sentinel).
	id := pulseSessionPrefix + "ask"
	h.sessions.Register(id, t.TempDir())
	as := &agentSession{agent: ag, model: "fake-model", messages: append([]agent.Message{{Role: "user", Content: "tell it hi"}}, msgs...)}
	h.mu.Lock()
	h.agents[id] = as
	h.mu.Unlock()
	body := `{"request_id":"call-1","session_id":"` + id + `","decision":"allow"}`
	rec := httptest.NewRecorder()
	h.HandleResolvePermission(rec, httptest.NewRequest(http.MethodPost, "/api/permissions/resolve", strings.NewReader(body)))
	if rec.Code >= 400 {
		t.Fatalf("resolve = %d %s", rec.Code, rec.Body.String())
	}
	pulseWaitFor(t, "the approved tool to run", func() bool {
		as.mu.Lock()
		defer as.mu.Unlock()
		for _, m := range as.messages {
			if m.ToolID == "call-1" && strings.Contains(m.Content, "not found") {
				return true
			}
		}
		return false
	})
}

func TestReadAndMemoryToolsDoNotAsk(t *testing.T) {
	setHomeTree(t, t.TempDir())
	h := pulseTestHandler(t)
	for _, tc := range []struct{ name, args string }{
		{"pulse_board", `{}`},
		{"agent_runs", `{}`},
		{"memory_read", `{"scope":"global"}`},
	} {
		ag, _ := pulseAgentWithScript(t, h, tc.name, tc.args)
		msgs, err := ag.Step([]agent.Message{{Role: "user", Content: "go"}})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for _, m := range msgs {
			if m.Role == "tool" && strings.HasPrefix(m.Content, tool.SentinelPermissionAsk) {
				t.Errorf("%s raised a permission ask", tc.name)
			}
		}
	}
	for _, pt := range h.pulseTools() {
		want := map[string]bool{"session_send": true, "session_command": true, "permission_resolve": true, "question_answer": true}[pt.Name()]
		if pt.ask != want {
			t.Errorf("%s ask=%v, want %v", pt.Name(), pt.ask, want)
		}
	}
}

// ---- recap deadline

func TestPulseRecapWithTimeout(t *testing.T) {
	start := time.Now()
	_, err := pulseRecapWithTimeout(50*time.Millisecond, func(ctx context.Context) (string, error) {
		<-ctx.Done() // a slow model: only the deadline ends it
		return "", ctx.Err()
	})
	if err == nil || !strings.Contains(err.Error(), "timed out after 50ms") {
		t.Fatalf("err = %v, want a timeout naming the duration", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("deadline was not honoured")
	}
	text, err := pulseRecapWithTimeout(time.Second, func(ctx context.Context) (string, error) { return "ok", nil })
	if err != nil || text != "ok" {
		t.Fatalf("fast recap = %q, %v", text, err)
	}
	boom := errors.New("model down")
	if _, err := pulseRecapWithTimeout(time.Second, func(ctx context.Context) (string, error) { return "", boom }); !errors.Is(err, boom) {
		t.Fatalf("other errors must pass through, got %v", err)
	}
	if pulseRecapTimeout != 90*time.Second {
		t.Fatalf("pulseRecapTimeout = %s", pulseRecapTimeout)
	}
}

// ---- memory

func memoryHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	setHomeTree(t, t.TempDir())
	h := pulseTestHandler(t)
	proj := t.TempDir()
	h.workDir = proj
	return h, proj
}

func TestMemoryToolsGlobalAndProject(t *testing.T) {
	h, proj := memoryHandler(t)

	out := mustPulseTool(t, h, "memory_read", `{"scope":"global"}`)
	t.Logf("memory_read (empty) -> %s", out)
	var rd struct {
		Scope, Path, Content string
	}
	if err := json.Unmarshal([]byte(out), &rd); err != nil || rd.Content != "" || !strings.HasSuffix(rd.Path, filepath.Join("pulse", "memory", "global.md")) {
		t.Fatalf("empty read = %+v (err %v)", rd, err)
	}

	out = mustPulseTool(t, h, "memory_write", `{"scope":"global","content":"operator prefers terse answers"}`)
	if !strings.Contains(out, `"bytes":30`) {
		t.Fatalf("write result = %s", out)
	}
	out = mustPulseTool(t, h, "memory_read", `{"scope":"global"}`)
	t.Logf("memory_read -> %s", out)
	if err := json.Unmarshal([]byte(out), &rd); err != nil || rd.Content != "operator prefers terse answers" || rd.Scope != "global" {
		t.Fatalf("read back = %+v", rd)
	}
	// Full replace, not append.
	mustPulseTool(t, h, "memory_write", `{"scope":"global","content":"second"}`)
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "memory_read", `{"scope":"global"}`)), &rd); err != nil || rd.Content != "second" {
		t.Fatalf("replace failed: %+v", rd)
	}

	pargs, _ := json.Marshal(map[string]string{"scope": "project", "project_path": proj, "content": "project notes"})
	out = mustPulseTool(t, h, "memory_write", string(pargs))
	var wr struct{ Path string }
	_ = json.Unmarshal([]byte(out), &wr)
	if !strings.Contains(wr.Path, filepath.Join("pulse", "memory", "projects")) || !strings.HasSuffix(wr.Path, pulseProjectSlug(proj)+".md") {
		t.Fatalf("project path = %q", wr.Path)
	}
	if data, err := os.ReadFile(wr.Path); err != nil || string(data) != "project notes" {
		t.Fatalf("file = %q err %v", data, err)
	}
}

func TestMemoryToolsRefusals(t *testing.T) {
	h, proj := memoryHandler(t)
	big := strings.Repeat("x", pulseMemoryCap+1)
	bigArgs, _ := json.Marshal(map[string]string{"scope": "global", "content": big})
	if _, err := runPulseTool(t, h, "memory_write", string(bigArgs)); err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("over-cap write: err = %v", err)
	}
	okArgs, _ := json.Marshal(map[string]string{"scope": "global", "content": strings.Repeat("x", pulseMemoryCap)})
	mustPulseTool(t, h, "memory_write", string(okArgs)) // exactly at the cap is fine

	other := t.TempDir()
	for name, args := range map[string]string{
		"project without path":    `{"scope":"project"}`,
		"unregistered project":    `{"scope":"project","project_path":"` + other + `"}`,
		"path with global":        `{"scope":"global","project_path":"` + proj + `"}`,
		"bad scope":               `{"scope":"user"}`,
		"write without content":   `{"scope":"global"}`,
		"write unregistered":      `{"scope":"project","project_path":"` + other + `","content":"x"}`,
		"traversal-looking scope": `{"scope":"../x"}`,
	} {
		tool := "memory_read"
		if strings.HasPrefix(name, "write") {
			tool = "memory_write"
		}
		if _, err := runPulseTool(t, h, tool, args); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestMemorySectionContentAndCaps(t *testing.T) {
	h, proj := memoryHandler(t)
	if sec, err := h.pulseMemorySection(nil); err != nil || sec != "" {
		t.Fatalf("no memory -> %q, %v", sec, err)
	}
	mustPulseTool(t, h, "memory_write", `{"scope":"global","content":"GLOBAL NOTE"}`)
	pargs, _ := json.Marshal(map[string]string{"scope": "project", "project_path": proj, "content": strings.Repeat("é", pulseMemoryPromptCap)})
	mustPulseTool(t, h, "memory_write", string(pargs))

	rows := []PulseRow{{SessionID: "ses_1", ProjectPath: proj}, {SessionID: "ses_2", ProjectPath: proj}, {SessionID: "ses_3", ProjectPath: "/not/registered"}}
	sec, err := h.pulseMemorySection(rows)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sec, pulseMemoryOpen+"\n") || !strings.HasSuffix(sec, pulseMemoryClose) {
		t.Fatalf("section not wrapped: %.80s … %.40s", sec, sec[len(sec)-40:])
	}
	if !strings.Contains(sec, "## global\nGLOBAL NOTE") {
		t.Fatalf("global memory missing: %.200s", sec)
	}
	if strings.Count(sec, "## project "+proj) != 1 {
		t.Fatal("project memory must appear exactly once for duplicate rows")
	}
	if !strings.Contains(sec, "(truncated, use memory_read)") {
		t.Fatal("oversized project memory must carry the truncation marker")
	}
	if len(sec) > 8*1024+1024+8*1024 || strings.ContainsRune(sec, '�') {
		t.Fatalf("section is %d bytes or has broken runes", len(sec))
	}

	// Through the board snapshot: one string carrying both sections.
	snap := h.pulseBoardSnapshot()
	if !strings.Contains(snap, "GLOBAL NOTE") || !strings.Contains(snap, pulseMemoryOpen) {
		t.Fatalf("snapshot lacks memory: %.300s", snap)
	}
}

// capturePulseClient records the messages of each Chat call.
type capturePulseClient struct {
	mu   sync.Mutex
	seen [][]agent.Message
}

func (c *capturePulseClient) Chat(m []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, append([]agent.Message(nil), m...))
	return &agent.Message{Role: "assistant", Content: "ok"}, nil
}
func (c *capturePulseClient) GetProvider() string { return "fake" }
func (c *capturePulseClient) GetModel() string    { return "fake-model" }

func TestBoardAndMemoryAreUserRoleOnTheWire(t *testing.T) {
	h, _ := memoryHandler(t)
	mustPulseTool(t, h, "memory_write", `{"scope":"global","content":"REMEMBERED FACT"}`)
	cl := &capturePulseClient{}
	cfg := &config.Config{}
	ag := agent.NewAgent(cl, nil, cfg, nil)
	t.Cleanup(ag.Shutdown)
	h.configurePulseAgent(ag, cfg)
	if _, err := ag.Step([]agent.Message{{Role: "user", Content: "what is going on?"}}); err != nil {
		t.Fatal(err)
	}
	if len(cl.seen) != 1 {
		t.Fatalf("chat calls = %d", len(cl.seen))
	}
	var tail *agent.Message
	for i, m := range cl.seen[0] {
		if strings.Contains(m.Content, "REMEMBERED FACT") || strings.HasPrefix(m.Content, "[ocode:pulse]\n") {
			if m.Role != "user" {
				t.Fatalf("message %d carrying board/memory has role %q, must be user", i, m.Role)
			}
			tail = &cl.seen[0][i]
		}
	}
	if tail == nil {
		t.Fatalf("no board message on the wire: %+v", cl.seen[0])
	}
	if !strings.HasPrefix(tail.Content, "[ocode:pulse]\n") || !strings.HasSuffix(tail.Content, "\n[/ocode:pulse]") ||
		!strings.Contains(tail.Content, "[ocode:pulse-memory]") {
		t.Fatalf("tail = %q", tail.Content)
	}
	// And the system prompt is the stable override, free of per-turn content.
	if cl.seen[0][0].Role != "system" || strings.Contains(cl.seen[0][0].Content, "REMEMBERED FACT") {
		t.Fatalf("system message = %+v", cl.seen[0][0])
	}
}

// ---- prompt endpoint

func TestPulseSystemPromptEndpointRoundTripAndLiveReapply(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)

	get := func() (prompt, def string) {
		rec := httptest.NewRecorder()
		h.HandleGetPulseSystemPrompt(rec, httptest.NewRequest(http.MethodGet, "/api/config/pulse-system-prompt", nil))
		var b map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || rec.Code != 200 {
			t.Fatalf("GET = %d %s", rec.Code, rec.Body.String())
		}
		return b["prompt"], b["default"]
	}
	put := func(body string) int {
		rec := httptest.NewRecorder()
		h.HandleSetPulseSystemPrompt(rec, httptest.NewRequest(http.MethodPut, "/api/config/pulse-system-prompt", strings.NewReader(body)))
		return rec.Code
	}

	prompt, def := get()
	if prompt != "" || !strings.Contains(def, "- session_send:") || strings.Contains(def, "{{TOOLS}}") {
		t.Fatalf("initial: prompt=%q default=%.120s", prompt, def)
	}

	// A resident assistant picks the change up without a rebuild.
	_, body, _ := getPulseAssistant(t, h)
	as, err := h.getOrCreateAgentSession(body["session_id"])
	if err != nil {
		t.Fatal(err)
	}
	if got := as.agent.BasePromptMessages()[0].Content; !strings.Contains(got, "Pulse assistant") {
		t.Fatalf("built-in prompt not in effect: %.100s", got)
	}

	if code := put(`{"prompt":"CUSTOM PROMPT"}`); code != 200 {
		t.Fatalf("PUT = %d", code)
	}
	if prompt, _ := get(); prompt != "CUSTOM PROMPT" {
		t.Fatalf("after set prompt=%q", prompt)
	}
	if got := as.agent.BasePromptMessages()[0].Content; !strings.HasSuffix(got, "CUSTOM PROMPT") || strings.Contains(got, "READ") {
		t.Fatalf("live agent did not re-apply: %q", got)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Ocode.PulseSystemPrompt != "CUSTOM PROMPT" {
		t.Fatalf("not persisted: %v %q", err, cfg.Ocode.PulseSystemPrompt)
	}
	// Fresh builds use it too.
	if again, _, err := h.buildAgentSession(body["session_id"], pulseTestModel, nil, h.sessions.Lookup(body["session_id"]).ProjectRoot); err != nil {
		t.Fatal(err)
	} else {
		defer again.agent.Shutdown()
		if got := again.agent.BasePromptMessages()[0].Content; !strings.HasSuffix(got, "CUSTOM PROMPT") {
			t.Fatalf("rebuilt agent prompt = %q", got)
		}
	}

	if code := put(`{"prompt":""}`); code != 200 {
		t.Fatalf("clear = %d", code)
	}
	if got := as.agent.BasePromptMessages()[0].Content; !strings.Contains(got, "- session_send:") {
		t.Fatalf("clear did not restore the default: %.100s", got)
	}
	if code := put(`{}`); code != http.StatusBadRequest {
		t.Fatalf("missing key = %d, want 400", code)
	}
}

// TestDispatchRefusesWhenTurnRegistered: a turn registered in turnInFlight after a
// pulse pre-check passed must refuse the next pulse dispatch, not queue behind it.
func TestDispatchRefusesWhenTurnRegistered(t *testing.T) {
	h, id, _ := writeTarget(t)
	h.cancelMu.Lock()
	h.turnInFlight[id] = 1
	h.cancelMu.Unlock()
	defer func() {
		h.cancelMu.Lock()
		delete(h.turnInFlight, id)
		h.cancelMu.Unlock()
	}()

	_, err := h.dispatchTurn(id, "fake-model", "hi", turnOptions{refuseIfBusy: true})
	if !errors.Is(err, errTurnInFlight) {
		t.Fatalf("pulse dispatch behind a registered turn: err = %v, want errTurnInFlight", err)
	}
	h.cancelMu.Lock()
	if n := h.turnInFlight[id]; n != 1 {
		t.Fatalf("refused dispatch changed turnInFlight to %d, want 1", n)
	}
	h.cancelMu.Unlock()
}

// TestPulseSendRefusedWhenSessionTurnActive: a pulse send to a session with an
// active turn gets 409, and nothing is appended or injected into that turn.
func TestPulseSendRefusedWhenSessionTurnActive(t *testing.T) {
	h, id, _ := writeTarget(t)
	h.sessions.setTurnActive(id, true)
	defer h.sessions.setTurnActive(id, false)

	ctx := context.WithValue(context.Background(), pulseOriginKey{}, true)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/message",
		strings.NewReader(`{"content":"hi","async":true}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.HandleSendMessage(rec, req, id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("pulse send to an active turn: status = %d, want 409 (body=%s)", rec.Code, rec.Body.String())
	}

	as := h.lookupAgentSession(id)
	as.mu.Lock()
	for _, m := range as.messages {
		if m.Content == "hi" {
			t.Fatal("refused pulse message was appended to the transcript")
		}
	}
	as.mu.Unlock()
	if pending := as.agent.DrainPendingInjections(); len(pending) != 0 {
		t.Fatalf("refused pulse message was injected into the running turn: %d pending", len(pending))
	}
}

// TestPulseSessionBusyNamesBriefLockAsNotMidTurn: a held turn lock with no turn
// signals is a brief reader, and the refusal must not claim a turn is running.
func TestPulseSessionBusyNamesBriefLockAsNotMidTurn(t *testing.T) {
	h, id, _ := writeTarget(t)
	as := h.lookupAgentSession(id)
	as.mu.Lock()
	err := h.pulseSessionBusy(id)
	as.mu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "briefly locked") {
		t.Fatalf("held lock with no turn: err = %v, want a briefly-locked refusal", err)
	}
	if strings.Contains(err.Error(), "mid-turn") {
		t.Fatalf("brief lock reported as mid-turn: %v", err)
	}
}
