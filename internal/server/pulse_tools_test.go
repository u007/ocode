package server

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/termtabs"
	"github.com/u007/ocode/internal/tool"
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
	want := []string{"agent_runs", "memory_read", "memory_write", "permission_resolve", "pulse_board", "question_answer", "question_dismiss", "session_ask", "session_close", "session_command", "session_new", "session_read", "session_recap", "session_send", "terminal_kill", "terminal_new", "terminal_read", "terminal_screen", "terminal_tabs"}
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

// TestVisibleCarriageSegmentHonoursErase: an erase sequence after a \r is a
// redraw that clears the line, so it must not fall back to the stale text
// before the \r. A line ending in \r with nothing redrawn still keeps its last
// redraw.
func TestVisibleCarriageSegmentHonoursErase(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"cr with nothing redrawn keeps last redraw", "old\r", "old"},
		{"erase after cr clears the line", "old\r\x1b[K", ""},
		{"erase then redraw shows the redraw", "old\r\x1b[2Knew", "new"},
		{"sgr after cr keeps last redraw", "old\r\x1b[0m", "old"},
		{"erase display after cr clears the line", "old\r\x1b[J", ""},
		{"erase without cr clears nothing visible", "\x1b[2K", ""},
		{"plain text", "plain", "plain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := visibleCarriageSegment(c.in); got != c.want {
				t.Fatalf("visibleCarriageSegment(%q) = %q, want %q", c.in, got, c.want)
			}
		})
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

func TestSessionAskReadsPendingAsks(t *testing.T) {
	h, id, _ := writeTarget(t)
	as := h.lookupAgentSession(id)
	as.mu.Lock()
	as.messages = append(as.messages,
		permissionAskMessage("p-1", samplePermissionRequest()),
		questionAskMessage("q-1", []tool.QuestionPrompt{
			{Header: "H", Question: "Which one?", Options: []tool.QuestionOption{{Label: "A", Description: "desc A"}}, Multiple: true},
		}),
	)
	as.mu.Unlock()

	out := mustPulseTool(t, h, "session_ask", `{"session_id":"`+id+`"}`)
	if !strings.Contains(out, `"request_id":"p-1"`) || !strings.Contains(out, `"tool":"bash"`) {
		t.Fatalf("permission ask missing: %s", out)
	}
	if !strings.Contains(out, `"request_id":"q-1"`) || !strings.Contains(out, `"question":"Which one?"`) {
		t.Fatalf("question ask missing: %s", out)
	}
	if !strings.Contains(out, `"label":"A"`) || !strings.Contains(out, `"multiple":true`) {
		t.Fatalf("question options/multiple missing: %s", out)
	}
}

func TestSessionAskEmptyWhenNothingPending(t *testing.T) {
	h, id, _ := writeTarget(t)
	out := mustPulseTool(t, h, "session_ask", `{"session_id":"`+id+`"}`)
	if !strings.Contains(out, `"permissions":[]`) || !strings.Contains(out, `"questions":[]`) {
		t.Fatalf("expected empty asks, got %s", out)
	}
}

func TestSessionAskRefusesOwnSession(t *testing.T) {
	h := pulseTestHandler(t)
	_, err := runPulseTool(t, h, "session_ask", `{"session_id":"`+pulseSessionPrefix+`x"}`)
	if err == nil {
		t.Fatal("session_ask on own pulse session must be refused")
	}
}

func TestSessionAskReadsChildPermissionAsk(t *testing.T) {
	h, id, _ := writeTarget(t)
	as := h.lookupAgentSession(id)
	// Park a sub-agent permission ask the way runTurn/child dispatch does.
	req := samplePermissionRequest()
	ev := newPermissionEvent("c-9", req)
	if as.childAsks == nil {
		as.childAsks = newChildPermAsks()
	}
	if !as.childAsks.add("c-9", &childPermAsk{req: req, event: ev, respCh: make(chan agent.PermissionResponse, 1)}) {
		t.Fatal("could not park child ask")
	}

	out := mustPulseTool(t, h, "session_ask", `{"session_id":"`+id+`"}`)
	if !strings.Contains(out, `"request_id":"c-9"`) || !strings.Contains(out, `"tool":"bash"`) {
		t.Fatalf("child ask not surfaced: %s", out)
	}
}

// TestSessionAskRedactsCommandsWithoutTerminalAccess pins the gate: a command
// line reaches the model only when terminal access is allowed (the same gate
// pulseTerminalRows applies). The request_id and tool stay so the operator can
// still resolve the ask, and the redaction must not mutate the live ask, which
// the terminal-access run after it proves.
func TestSessionAskRedactsCommandsWithoutTerminalAccess(t *testing.T) {
	h, id, _ := writeTarget(t)
	as := h.lookupAgentSession(id)
	// DenyReason and Summary quote the command, and Rule carries the prefix:
	// all three must stay out of the redacted result, not just Command.
	req := samplePermissionRequest()
	req.DenyReason = "rm -rf build is destructive"
	req.Summary = "clean the build dir with rm -rf build"
	as.mu.Lock()
	as.messages = append(as.messages, permissionAskMessage("p-1", req))
	as.mu.Unlock()

	out := mustPulseTool(t, h, "session_ask", `{"session_id":"`+id+`"}`)
	if strings.Contains(out, "rm -rf build") {
		t.Fatalf("command leaked without terminal access: %s", out)
	}
	if strings.Contains(out, "bash.prefix.rm") {
		t.Fatalf("rule (carries the command prefix) leaked without terminal access: %s", out)
	}
	if !strings.Contains(out, `"request_id":"p-1"`) || !strings.Contains(out, `"tool":"bash"`) {
		t.Fatalf("redacted ask must keep request_id and tool: %s", out)
	}

	h.terminalLoopback = true
	out = mustPulseTool(t, h, "session_ask", `{"session_id":"`+id+`"}`)
	if !strings.Contains(out, "rm -rf build") {
		t.Fatalf("command missing with terminal access allowed: %s", out)
	}
}

func TestSessionAskErrorsOnUnknownSession(t *testing.T) {
	h := pulseTestHandler(t)
	_, err := runPulseTool(t, h, "session_ask", `{"session_id":"ses_no_such_session"}`)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("session_ask on a session that does not exist must fail with not found, got %v", err)
	}
}

func TestQuestionDismissRefusesNonPending(t *testing.T) {
	h, id, _ := writeTarget(t)
	_, err := runPulseTool(t, h, "question_dismiss",
		`{"session_id":"`+id+`","request_id":"does-not-exist"}`)
	if err == nil {
		t.Fatal("dismissing a non-pending question must error")
	}
}

func TestPermissionResolveOnNonPendingAskErrors(t *testing.T) {
	h, id, _ := writeTarget(t)
	_, err := runPulseTool(t, h, "permission_resolve",
		`{"session_id":"`+id+`","request_id":"does-not-exist","decision":"allow"}`)
	if err == nil {
		t.Fatal("resolving a non-pending permission must error")
	}
}
