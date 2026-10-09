package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/termtabs"
)

// pulseToolByName fetches one tool from a fresh set.
func pulseToolByName(t *testing.T, h *Handler, name string) *pulseTool {
	t.Helper()
	for _, pt := range h.pulseTools() {
		if pt.Name() == name {
			return pt
		}
	}
	t.Fatalf("no pulse tool %q", name)
	return nil
}

func runPulseTool(t *testing.T, h *Handler, name, args string) (string, error) {
	t.Helper()
	return pulseToolByName(t, h, name).Execute(json.RawMessage(args))
}

func mustPulseTool(t *testing.T, h *Handler, name, args string) string {
	t.Helper()
	out, err := runPulseTool(t, h, name, args)
	if err != nil {
		t.Fatalf("%s(%s): %v", name, args, err)
	}
	if len(out) > pulseToolResultCap {
		t.Fatalf("%s result is %d bytes, over the %d cap", name, len(out), pulseToolResultCap)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("%s result is not valid JSON: %.200s", name, out)
	}
	return out
}

func TestPulseToolsShape(t *testing.T) {
	h := pulseTestHandler(t)
	tools := h.pulseTools()
	want := []string{"agent_runs", "memory_read", "memory_write", "permission_resolve", "pulse_board", "question_answer", "session_command", "session_read", "session_recap", "session_send", "terminal_read", "terminal_tabs"}
	var got []string
	for _, pt := range tools {
		got = append(got, pt.Name())
		if !pt.Parallel() {
			t.Errorf("%s must be Parallel()", pt.Name())
		}
		def := pt.Definition()
		if def["name"] != pt.Name() || def["description"] == "" {
			t.Errorf("%s: bad definition %v", pt.Name(), def)
		}
		params, ok := def["parameters"].(map[string]interface{})
		if !ok || params["type"] != "object" {
			t.Errorf("%s: parameters = %v", pt.Name(), def["parameters"])
		}
	}
	if strings.Join(sortedCopy(got), ",") != strings.Join(want, ",") {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	// Unknown argument fields are reported, never ignored.
	if _, err := runPulseTool(t, h, "pulse_board", `{"scop":"all"}`); err == nil {
		t.Fatal("misspelled argument must be an error")
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestPulseBoardTool(t *testing.T) {
	h := pulseTestHandler(t)
	registerLiveSession(t, h, "ses_b", "/work/b", true, nil)
	registerLiveSession(t, h, "ses_a", "/work/a", true, nil)
	// The assistant is registered too and must not appear.
	registerLiveSession(t, h, pulseSessionPrefix+"self", "/pulse", true, nil)

	out := mustPulseTool(t, h, "pulse_board", `{}`)
	var body struct {
		Items     []PulseRow `json:"items"`
		Truncated bool       `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 || body.Truncated {
		t.Fatalf("items = %+v truncated=%v", body.Items, body.Truncated)
	}
	// Same ordering as the board: equal rank and recency fall back to id.
	if body.Items[0].SessionID > body.Items[1].SessionID && body.Items[0].UpdatedAt.Equal(body.Items[1].UpdatedAt) {
		t.Fatalf("rows not in board order: %v then %v", body.Items[0].SessionID, body.Items[1].SessionID)
	}
	for _, r := range body.Items {
		if isPulseSession(r.SessionID) {
			t.Fatalf("assistant leaked into pulse_board: %+v", r)
		}
	}
	if _, err := runPulseTool(t, h, "pulse_board", `{"scope":"week"}`); err == nil {
		t.Fatal("invalid scope must be an error")
	}
	mustPulseTool(t, h, "pulse_board", `{"scope":"all"}`)
}

func TestPulseCappedTruncatesAndMarks(t *testing.T) {
	items := make([]string, 200)
	for i := range items {
		items[i] = strings.Repeat("é", 500) // multi-byte on purpose
	}
	out, err := pulseCapped(len(items), func(n int, truncated bool) any {
		return struct {
			Items     []string `json:"items"`
			Truncated bool     `json:"truncated,omitempty"`
		}{items[:n], truncated}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > pulseToolResultCap {
		t.Fatalf("len %d over cap", len(out))
	}
	var body struct {
		Items     []string `json:"items"`
		Truncated bool     `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Truncated || len(body.Items) == 0 || len(body.Items) >= len(items) {
		t.Fatalf("truncated=%v items=%d", body.Truncated, len(body.Items))
	}
	if !strings.HasSuffix(out, `"truncated":true}`) {
		t.Fatalf("truncated must be the trailing field: …%s", out[len(out)-40:])
	}
	// An untruncated result carries no marker.
	small, err := pulseCapped(1, func(n int, truncated bool) any {
		return struct {
			Items     []string `json:"items"`
			Truncated bool     `json:"truncated,omitempty"`
		}{items[:n], truncated}
	})
	if err != nil || strings.Contains(small, "truncated") {
		t.Fatalf("small result = %.80s err=%v", small, err)
	}
	// Nothing fits: an error, never an empty result.
	if _, err := pulseCapped(1, func(n int, truncated bool) any {
		return strings.Repeat("x", pulseToolResultCap+1)
	}); err == nil {
		t.Fatal("oversize with no shrink room must error")
	}
}

type readOut struct {
	SessionID   string `json:"session_id"`
	ProjectPath string `json:"project_path"`
	Title       string `json:"title"`
	Messages    []struct {
		Index     int    `json:"index"`
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []struct {
			Name string `json:"name"`
			Args string `json:"args"`
		} `json:"tool_calls"`
	} `json:"messages"`
	Truncated bool `json:"truncated"`
}

func toolCallMessage(name, args string) agent.Message {
	var tc agent.ToolCall
	tc.ID = "c1"
	tc.Type = "function"
	tc.Function.Name = name
	tc.Function.Arguments = args
	return agent.Message{Role: "assistant", Content: "calling", ToolCalls: []agent.ToolCall{tc}}
}

func TestSessionReadTool(t *testing.T) {
	h := pulseTestHandler(t)
	var msgs []agent.Message
	for i := 0; i < 50; i++ {
		msgs = append(msgs, agent.Message{Role: "user", Content: fmt.Sprintf("message %d needle%d", i, i%10)})
	}
	msgs = append(msgs, toolCallMessage("bash", strings.Repeat("a", 500)))
	msgs = append(msgs, agent.Message{Role: "assistant", Content: strings.Repeat("é", 5000)})
	registerLiveSession(t, h, "ses_read", "/work/read", false, msgs)

	var out readOut
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "session_read", `{"session_id":"ses_read"}`)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != pulseSessionReadDefault {
		t.Fatalf("default last = %d messages, want %d", len(out.Messages), pulseSessionReadDefault)
	}
	if out.ProjectPath != "/work/read" || out.SessionID != "ses_read" {
		t.Fatalf("header = %+v", out)
	}
	for i, m := range out.Messages {
		if i > 0 && m.Index <= out.Messages[i-1].Index {
			t.Fatalf("indexes not ascending at %d", i)
		}
	}
	lastMsg := out.Messages[len(out.Messages)-1]
	if lastMsg.Index != 51 {
		t.Fatalf("last index = %d, want 51 (absolute index)", lastMsg.Index)
	}
	if n := utf8.RuneCountInString(lastMsg.Content); n != pulseMessageRuneBudget+1 || !utf8.ValidString(lastMsg.Content) {
		t.Fatalf("content runes = %d valid=%v, want %d+ellipsis, rune-safe", n, utf8.ValidString(lastMsg.Content), pulseMessageRuneBudget)
	}
	call := out.Messages[len(out.Messages)-2]
	if len(call.ToolCalls) != 1 || call.ToolCalls[0].Name != "bash" || utf8.RuneCountInString(call.ToolCalls[0].Args) != pulseCallArgsRuneBudget+1 {
		t.Fatalf("tool call = %+v", call.ToolCalls)
	}

	// last
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "session_read", `{"session_id":"ses_read","last":3}`)), &out); err != nil || len(out.Messages) != 3 {
		t.Fatalf("last=3 -> %d messages (err %v)", len(out.Messages), err)
	}
	for _, bad := range []string{`{"session_id":"ses_read","last":0}`, `{"session_id":"ses_read","last":201}`} {
		if _, err := runPulseTool(t, h, "session_read", bad); err == nil {
			t.Fatalf("%s must be rejected", bad)
		}
	}

	// search is case-insensitive, returns absolute indexes of matches only.
	out = readOut{}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "session_read", `{"session_id":"ses_read","search":"NEEDLE7","last":200}`)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 5 {
		t.Fatalf("search matched %d messages, want 5", len(out.Messages))
	}
	for _, m := range out.Messages {
		if !strings.Contains(m.Content, "needle7") || m.Index%10 != 7 {
			t.Fatalf("unexpected match %+v", m)
		}
	}

	// unknown session, empty id
	if _, err := runPulseTool(t, h, "session_read", `{"session_id":"ses_missing"}`); err == nil {
		t.Fatal("unknown session must be an error")
	}
	if _, err := runPulseTool(t, h, "session_read", `{}`); err == nil {
		t.Fatal("missing session_id must be an error")
	}
}

func TestSessionReadRefusesAssistantAndReadsChildAndDisk(t *testing.T) {
	h := pulseTestHandler(t)
	self := pulseSessionPrefix + "2026-10-08-000000-self"
	registerLiveSession(t, h, self, "/pulse", false, []agent.Message{{Role: "user", Content: "secret"}})
	for _, tool := range []string{"session_read", "session_recap"} {
		_, err := runPulseTool(t, h, tool, fmt.Sprintf(`{"session_id":%q}`, self))
		if err == nil || !strings.Contains(err.Error(), "own session") {
			t.Fatalf("%s on the assistant's own id: err = %v, want refusal", tool, err)
		}
	}

	// A disk-only child session under a registered root is readable.
	root := t.TempDir()
	child := "ses_parent_child_explore_1"
	if err := session.SaveForDir(root, child, "child title", []agent.Message{{Role: "user", Content: "from disk"}}, nil); err != nil {
		t.Fatalf("save child: %v", err)
	}
	h.sessions.Register(child, root)
	var out readOut
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "session_read", fmt.Sprintf(`{"session_id":%q}`, child))), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 1 || out.Messages[0].Content != "from disk" || out.ProjectPath != root {
		t.Fatalf("child read = %+v", out)
	}
	if out.Title != "child title" {
		t.Fatalf("title = %q", out.Title)
	}
}

func TestSessionReadTruncatesByBytesKeepingNewest(t *testing.T) {
	h := pulseTestHandler(t)
	var msgs []agent.Message
	for i := 0; i < 200; i++ {
		msgs = append(msgs, agent.Message{Role: "user", Content: fmt.Sprintf("#%03d ", i) + strings.Repeat("w", 1900)})
	}
	registerLiveSession(t, h, "ses_big", "/work/big", false, msgs)
	var out readOut
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "session_read", `{"session_id":"ses_big","last":200}`)), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || len(out.Messages) == 0 || len(out.Messages) >= 200 {
		t.Fatalf("truncated=%v messages=%d", out.Truncated, len(out.Messages))
	}
	if out.Messages[len(out.Messages)-1].Index != 199 {
		t.Fatalf("newest message must survive truncation, last index = %d", out.Messages[len(out.Messages)-1].Index)
	}
}

func TestSessionRecapToolErrors(t *testing.T) {
	h := pulseTestHandler(t)
	if _, err := runPulseTool(t, h, "session_recap", `{}`); err == nil {
		t.Fatal("missing session_id must error")
	}
	if _, err := runPulseTool(t, h, "session_recap", `{"session_id":"ses_missing"}`); err == nil {
		t.Fatal("unknown session must error")
	}
}

func TestTerminalTabsTool(t *testing.T) {
	h := pulseTestHandler(t)
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "term.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.termTabsStore = store
	if err := store.Set("/proj/b", termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "t2", Title: "two"}, {ID: "t1", Title: "one"}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("/proj/a", termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "t3", Title: "three"}}}); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Projects []struct {
			Project   string `json:"project"`
			Terminals []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"terminals"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_tabs", `{}`)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Projects) != 2 || out.Projects[0].Project != "/proj/a" || out.Projects[1].Project != "/proj/b" {
		t.Fatalf("projects not sorted: %+v", out.Projects)
	}
	if got := out.Projects[1].Terminals; len(got) != 2 || got[0].ID != "t1" || got[1].ID != "t2" {
		t.Fatalf("terminals not sorted by id: %+v", got)
	}

	h.termTabsStore = nil
	if _, err := runPulseTool(t, h, "terminal_tabs", `{}`); err == nil {
		t.Fatal("missing store must be an error, not an empty list")
	}
}

func TestPulseTailLines(t *testing.T) {
	text := "old\n\x1b[31mred\x1b[0m line\nprogress 10%\rprogress 100%\n\x1b]0;title\x07last\n"
	got := pulseTailLines(text, 3)
	want := []string{"red line", "progress 100%", "last"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	if got := pulseTailLines("", 5); len(got) != 0 {
		t.Fatalf("empty -> %q", got)
	}
}

func TestTerminalReadTool(t *testing.T) {
	h := pulseTestHandler(t)
	proj := t.TempDir()
	h.workDir = proj
	h.terminalLoopback = true
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "term.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.termTabsStore = store
	if err := store.Set(proj, termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "term-1", Title: "dev"}}}); err != nil {
		t.Fatal(err)
	}
	logPath, err := terminalHistoryPath(proj, "term-1")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "\x1b[32mline %d\x1b[0m\r\n", i)
	}
	if err := os.WriteFile(logPath, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	var out struct {
		TerminalID string   `json:"terminal_id"`
		Project    string   `json:"project"`
		Lines      []string `json:"lines"`
		Truncated  bool     `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_read", `{"terminal_id":"term-1"}`)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != pulseTerminalDefaultLines || out.Lines[0] != "line 200" || out.Lines[99] != "line 299" || out.Project != proj {
		t.Fatalf("default read: %d lines, first %q last %q project %q", len(out.Lines), out.Lines[0], out.Lines[len(out.Lines)-1], out.Project)
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_read", `{"terminal_id":"term-1","lines":2}`)), &out); err != nil || len(out.Lines) != 2 || out.Lines[0] != "line 298" {
		t.Fatalf("lines=2 -> %q (err %v)", out.Lines, err)
	}
	for _, bad := range []string{`{"terminal_id":"term-1","lines":0}`, `{"terminal_id":"term-1","lines":1001}`, `{}`, `{"terminal_id":"nope"}`, `{"terminal_id":"anon-1"}`} {
		if _, err := runPulseTool(t, h, "terminal_read", bad); err == nil {
			t.Fatalf("%s must be rejected", bad)
		}
	}

	// Access gate mirrors the history endpoint.
	h.terminalLoopback = false
	if _, err := runPulseTool(t, h, "terminal_read", `{"terminal_id":"term-1"}`); err == nil {
		t.Fatal("terminal access gate must apply")
	}
	h.terminalLoopback = true

	// A terminal of an unregistered project is refused.
	other := t.TempDir()
	if err := store.Set(other, termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "term-x"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runPulseTool(t, h, "terminal_read", `{"terminal_id":"term-x"}`); err == nil {
		t.Fatal("terminal outside the served projects must be refused")
	}

	// A huge log is read through its tail window and still fits the cap.
	bigPath, err := terminalHistoryPath(proj, "term-1")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(bigPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(f, "big %05d %s\n", i, strings.Repeat("z", 60))
	}
	f.Close()
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_read", `{"terminal_id":"term-1","lines":1000}`)), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || out.Lines[len(out.Lines)-1] != "big 19999 "+strings.Repeat("z", 60) {
		t.Fatalf("truncated=%v last=%q", out.Truncated, out.Lines[len(out.Lines)-1])
	}
}

func TestAgentRunsTool(t *testing.T) {
	h := pulseTestHandler(t)
	a := agent.NewAgent(nil, nil, nil, nil)
	worker := a.Runs().New("worker")
	sub := agent.NewAgent(&agent.GenericClient{Model: "test-model"}, nil, nil, nil)
	sub.Runs().New("helper")
	worker.Sub = sub
	b := agent.NewAgent(nil, nil, nil, nil)
	b.Runs().New("other")
	idle := agent.NewAgent(nil, nil, nil, nil)
	self := agent.NewAgent(nil, nil, nil, nil)
	self.Runs().New("must-not-appear")

	h.mu.Lock()
	h.agents["ses_b"] = &agentSession{agent: b}
	h.agents["ses_a"] = &agentSession{agent: a}
	h.agents["ses_idle"] = &agentSession{agent: idle}
	h.agents[pulseSessionPrefix+"self"] = &agentSession{agent: self}
	h.mu.Unlock()

	type runs struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Runs      []struct {
				Name     string `json:"name"`
				Status   string `json:"status"`
				Children []struct {
					Name string `json:"name"`
				} `json:"children"`
			} `json:"runs"`
		} `json:"sessions"`
	}
	var out runs
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "agent_runs", `{}`)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Sessions) != 2 || out.Sessions[0].SessionID != "ses_a" || out.Sessions[1].SessionID != "ses_b" {
		t.Fatalf("sessions = %+v, want ses_a, ses_b (sorted, no idle, no assistant)", out.Sessions)
	}
	r := out.Sessions[0].Runs
	if len(r) != 1 || r[0].Name != "worker" || r[0].Status != "running" || len(r[0].Children) != 1 || r[0].Children[0].Name != "helper" {
		t.Fatalf("nested tree = %+v", r)
	}

	out = runs{}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "agent_runs", `{"session_id":"ses_b"}`)), &out); err != nil || len(out.Sessions) != 1 || out.Sessions[0].SessionID != "ses_b" {
		t.Fatalf("filtered = %+v (err %v)", out, err)
	}
	if _, err := runPulseTool(t, h, "agent_runs", `{"session_id":"ses_gone"}`); err == nil {
		t.Fatal("non-resident session must be an error")
	}
	if _, err := runPulseTool(t, h, "agent_runs", fmt.Sprintf(`{"session_id":%q}`, pulseSessionPrefix+"self")); err == nil {
		t.Fatal("assistant's own id must be refused")
	}
}

func TestRenderPulseBoard(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	if got := renderPulseBoard(nil, now); got != "No live sessions." {
		t.Fatalf("empty board = %q", got)
	}
	rows := []PulseRow{
		{SessionID: "ses_1", ProjectPath: "/Users/x/proj", Title: "line one\nline two", Status: PulseStatusNeedsPermission,
			PendingAsk: &PulseAsk{Kind: PulseAskKindPermission, Summary: "rm -rf build"}},
		{SessionID: "ses_2", ProjectPath: "/Users/x/other", Title: strings.Repeat("t", 200), Status: PulseStatusRunning,
			CurrentTask: &PulseTask{Kind: PulseTaskKindTool, Text: "bash go test"}},
	}
	out := renderPulseBoard(rows, now)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	if lines[1] != "needs_permission | proj | ses_1 | line one line two | ask(permission): rm -rf build" {
		t.Fatalf("row 1 = %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "running | other | ses_2 | "+strings.Repeat("t", 80)+"… | bash go test") {
		t.Fatalf("row 2 = %q", lines[2])
	}

	many := make([]PulseRow, 75)
	for i := range many {
		many[i] = PulseRow{SessionID: fmt.Sprintf("ses_%02d", i), ProjectPath: "/p", Status: PulseStatusRunning}
	}
	out = renderPulseBoard(many, now)
	if n := len(strings.Split(out, "\n")); n != pulseBoardMaxRows+1 {
		t.Fatalf("board has %d lines, want header + %d rows", n, pulseBoardMaxRows)
	}
	if !strings.Contains(out, "(60 of 75 shown)") || strings.Contains(out, "ses_60") {
		t.Fatalf("cap not applied: %.200s", out)
	}
}

func TestPulseSystemPrompt(t *testing.T) {
	h := pulseTestHandler(t)
	tools := h.pulseTools()
	def := pulseSystemPrompt("", tools)
	for _, pt := range tools {
		if !strings.Contains(def, "- "+pt.Name()+": "+pt.Description()) {
			t.Errorf("default prompt missing tool %q", pt.Name())
		}
	}
	if strings.Contains(def, "{{TOOLS}}") {
		t.Fatal("placeholder left in prompt")
	}
	if !strings.Contains(def, "CURRENT message explicitly asks") {
		t.Fatal("default prompt must gate write tools on the operator's current message")
	}
	if got := pulseSystemPrompt("  custom prompt  ", tools); got != "  custom prompt  " {
		t.Fatalf("custom prompt must be used verbatim, got %q", got)
	}
}
