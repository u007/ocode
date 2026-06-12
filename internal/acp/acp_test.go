package acp

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

// testServer creates a server with in-memory pipes and returns a writer for
// injecting client frames and a reader for consuming server output frames.
func testServer() (clientWriter io.WriteCloser, serverReader *bufio.Scanner, done chan error) {
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()

	done = make(chan error, 1)
	go func() {
		s := &server{
			sessions: make(map[string]*sessionBridge),
			writer:   bufio.NewWriter(stdoutW),
			pending:  make(map[int]chan json.RawMessage),
			// cfg intentionally nil — tests that only exercise JSON-RPC framing
			// do not need a real config.
		}

		scanner := bufio.NewScanner(stdinR)
		scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			s.dispatch(line)
		}
		stdoutW.Close()
		done <- scanner.Err()
	}()

	return stdinW, bufio.NewScanner(stdoutR), done
}

// readFrame reads the next JSON-RPC frame from the server output.
func readFrame(t *testing.T, sc *bufio.Scanner) inFrame {
	t.Helper()
	if !sc.Scan() {
		t.Fatal("scanner ended before a frame was received")
	}
	var f inFrame
	if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
		t.Fatalf("malformed server frame: %v — raw: %s", err, sc.Text())
	}
	return f
}

// sendLine writes a single newline-terminated line to the server.
func sendLine(t *testing.T, w io.Writer, line string) {
	t.Helper()
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		t.Fatalf("write to server: %v", err)
	}
}

// mustParseResult unmarshals f.Result into dst.
func mustParseResult(t *testing.T, f inFrame, dst interface{}) {
	t.Helper()
	if f.Error != nil {
		t.Fatalf("expected result, got error %d: %s", f.Error.Code, f.Error.Message)
	}
	if err := json.Unmarshal(f.Result, dst); err != nil {
		t.Fatalf("parse result: %v", err)
	}
}

// ---------------------------------------------------------------------------

func TestHandshake(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`)

	f := readFrame(t, sc)

	if f.Error != nil {
		t.Fatalf("initialize returned error: %v", f.Error)
	}

	var result initializeResult
	mustParseResult(t, f, &result)

	if result.ProtocolVersion != 1 {
		t.Errorf("protocolVersion = %d, want 1", result.ProtocolVersion)
	}
	if result.AgentInfo.Name != "ocode" {
		t.Errorf("agentInfo.name = %q, want \"ocode\"", result.AgentInfo.Name)
	}
	if result.AgentCapabilities.LoadSession {
		t.Error("loadSession should be false")
	}
	if !result.AgentCapabilities.PromptCapabilities.EmbeddedContext {
		t.Error("embeddedContext should be true")
	}
	if result.AgentCapabilities.PromptCapabilities.Image {
		t.Error("image should be false")
	}
	if result.AgentCapabilities.PromptCapabilities.Audio {
		t.Error("audio should be false")
	}
	if result.AuthMethods == nil {
		t.Error("authMethods must be non-nil (empty slice)")
	}

	w.Close()
}

func TestMalformedJSON(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{not valid json}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errParse {
		t.Errorf("expected parse error -32700, got %+v", f.Error)
	}
	w.Close()
}

func TestUnknownMethod(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":42,"method":"no/such/method"}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errMethodNotFound {
		t.Errorf("expected method-not-found -32601, got %+v", f.Error)
	}
	w.Close()
}

func TestUnknownNotificationSilentlyIgnored(t *testing.T) {
	w, sc, done := testServer()
	// Unknown notification (no id) should produce no output.
	sendLine(t, w, `{"jsonrpc":"2.0","method":"some/unknown/notification"}`)
	// Now send a known request to verify the server is still alive and responding.
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`)

	f := readFrame(t, sc)
	// The response must be for the initialize, not the unknown notification.
	if f.Error != nil {
		t.Fatalf("unexpected error: %+v", f.Error)
	}
	var result initializeResult
	mustParseResult(t, f, &result)
	if result.ProtocolVersion != 1 {
		t.Errorf("expected initialize response, got unexpected frame")
	}

	w.Close()
	<-done
}

func TestSessionLoadNotSupported(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"session/load"}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errMethodNotFound {
		t.Errorf("expected method-not-found, got %+v", f.Error)
	}
	w.Close()
}

func TestAuthenticateNotSupported(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"authenticate"}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errMethodNotFound {
		t.Errorf("expected method-not-found, got %+v", f.Error)
	}
	w.Close()
}

func TestSessionNewUnknownSession(t *testing.T) {
	w, sc, _ := testServer()
	// Prompt against a session ID that was never created.
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"sessionId":"nonexistent","content":[{"type":"text","text":"hi"}]}}`)

	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errInvalidParams {
		t.Errorf("expected invalid-params -32602, got %+v", f.Error)
	}
	w.Close()
}

// ---------------------------------------------------------------------------
// flattenContent tests

func TestFlattenContentText(t *testing.T) {
	blocks := []contentBlock{
		{Type: "text", Text: "hello"},
		{Type: "text", Text: "world"},
	}
	got := flattenContent(blocks)
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("flattenContent missing text: %q", got)
	}
}

func TestFlattenContentResource(t *testing.T) {
	blocks := []contentBlock{
		{Type: "text", Text: "see file"},
		{Type: "resource", Resource: &embeddedResource{URI: "file:///foo.go", Text: "package main"}},
	}
	got := flattenContent(blocks)
	if !strings.Contains(got, "foo.go") {
		t.Errorf("flattenContent should include resource URI: %q", got)
	}
	if !strings.Contains(got, "package main") {
		t.Errorf("flattenContent should include resource text: %q", got)
	}
}

func TestFlattenContentResourceLink(t *testing.T) {
	blocks := []contentBlock{
		{Type: "resource_link", URI: "file:///bar.go"},
	}
	got := flattenContent(blocks)
	if !strings.Contains(got, "bar.go") {
		t.Errorf("flattenContent should include resource_link URI: %q", got)
	}
}

// ---------------------------------------------------------------------------
// mapToolKind tests

func TestMapToolKind(t *testing.T) {
	cases := []struct{ tool, want string }{
		{"read", "read"},
		{"write", "edit"},
		{"edit", "edit"},
		{"apply_patch", "edit"},
		{"bash", "execute"},
		{"bash_output", "execute"},
		{"grep", "search"},
		{"glob", "search"},
		{"webfetch", "fetch"},
		{"question", "other"},
		{"task", "other"},
	}
	for _, tc := range cases {
		if got := mapToolKind(tc.tool); got != tc.want {
			t.Errorf("mapToolKind(%q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// makeTitle tests

func TestMakeTitleWithKnownArg(t *testing.T) {
	title := makeTitle("read", `{"path":"/foo/bar.go"}`)
	if title != "read: /foo/bar.go" {
		t.Errorf("unexpected title: %q", title)
	}
}

func TestMakeTitleNoArgs(t *testing.T) {
	title := makeTitle("task", `{}`)
	if title != "task" {
		t.Errorf("unexpected title: %q", title)
	}
}

func TestMakeTitleTruncation(t *testing.T) {
	longPath := strings.Repeat("a", 80)
	title := makeTitle("read", `{"path":"`+longPath+`"}`)
	if len(title) > len("read: ")+60+len("...") {
		t.Errorf("title not truncated: %q", title)
	}
}

// ---------------------------------------------------------------------------
// handleInitialize version negotiation tests

func TestHandshakeVersionClamp(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":99}}`)
	f := readFrame(t, sc)
	var result initializeResult
	mustParseResult(t, f, &result)
	if result.ProtocolVersion != 1 {
		t.Errorf("client v99 should be clamped to 1, got %d", result.ProtocolVersion)
	}
	w.Close()
}

func TestHandshakeVersionZeroRejected(t *testing.T) {
	w, sc, _ := testServer()
	sendLine(t, w, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":0}}`)
	f := readFrame(t, sc)
	if f.Error == nil || f.Error.Code != errInvalidParams {
		t.Errorf("version 0 should return invalid-params, got %+v", f.Error)
	}
	w.Close()
}

// ---------------------------------------------------------------------------
// Bridge unit tests (fake LLM client, no network)

// fakeClient is a sequential LLMClient stub: each Chat call returns the next
// pre-canned response. Once exhausted it returns an empty assistant message.
type fakeClient struct {
	calls []*agent.Message
	idx   int
}

func (f *fakeClient) Chat(msgs []agent.Message, tools []map[string]interface{}) (*agent.Message, error) {
	if f.idx >= len(f.calls) {
		return &agent.Message{Role: "assistant", Content: ""}, nil
	}
	m := f.calls[f.idx]
	f.idx++
	return m, nil
}
func (f *fakeClient) GetProvider() string { return "fake" }
func (f *fakeClient) GetModel() string    { return "fake/model" }

// cancelOnFirstCallClient cancels the agent during its first Chat call.
type cancelOnFirstCallClient struct {
	bridge *sessionBridge
	fired  bool
}

func (c *cancelOnFirstCallClient) Chat(msgs []agent.Message, tools []map[string]interface{}) (*agent.Message, error) {
	if !c.fired {
		c.fired = true
		c.bridge.cancel()
	}
	return &agent.Message{Role: "assistant", Content: ""}, nil
}
func (c *cancelOnFirstCallClient) GetProvider() string { return "fake" }
func (c *cancelOnFirstCallClient) GetModel() string    { return "fake/model" }

// makeBridge builds a sessionBridge directly, bypassing newSessionBridge so
// tests don't need a real config or API key.
func makeBridge(cli agent.LLMClient) *sessionBridge {
	ag := agent.NewAgent(cli, nil, nil, nil)
	return &sessionBridge{
		id:           "test-ses",
		ag:           ag,
		messages:     nil,
		pendingTools: make(map[string]string),
	}
}

// collectUpdates runs bridge.prompt and returns all sessionUpdate notifications.
func collectUpdates(t *testing.T, b *sessionBridge, content []contentBlock, permResponse string) ([]sessionUpdate, string, error) {
	t.Helper()
	var updates []sessionUpdate
	stopReason, err := b.prompt(
		content,
		func(su sessionUpdate) { updates = append(updates, su) },
		func(toolName, rule string) string { return permResponse },
	)
	return updates, stopReason, err
}

// updateKinds extracts the Kind field from a slice of updates for easy assertion.
func updateKinds(updates []sessionUpdate) []string {
	kinds := make([]string, len(updates))
	for i, u := range updates {
		kinds[i] = u.Kind
	}
	return kinds
}

func TestBridgeTextResponse(t *testing.T) {
	cli := &fakeClient{calls: []*agent.Message{
		{Role: "assistant", Content: "hello from agent"},
	}}
	b := makeBridge(cli)
	updates, stopReason, err := collectUpdates(t, b, []contentBlock{{Type: "text", Text: "hi"}}, "reject-once")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want \"end_turn\"", stopReason)
	}
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d: %v", len(updates), updateKinds(updates))
	}
	if updates[0].Kind != "agent_message_chunk" {
		t.Errorf("update kind = %q, want \"agent_message_chunk\"", updates[0].Kind)
	}
	if len(updates[0].Content) == 0 || updates[0].Content[0].Text != "hello from agent" {
		t.Errorf("update content = %v, want text \"hello from agent\"", updates[0].Content)
	}
}

func TestBridgeNonStreamingNoDuplicateChunk(t *testing.T) {
	// Second prompt on the same bridge: deltaEmitted is reset each turn so
	// a non-streaming response always emits exactly one chunk per turn.
	cli := &fakeClient{calls: []*agent.Message{
		{Role: "assistant", Content: "turn one"},
		{Role: "assistant", Content: "turn two"},
	}}
	b := makeBridge(cli)

	updates1, _, _ := collectUpdates(t, b, []contentBlock{{Type: "text", Text: "first"}}, "reject-once")
	updates2, _, _ := collectUpdates(t, b, []contentBlock{{Type: "text", Text: "second"}}, "reject-once")

	if len(updates1) != 1 || updates1[0].Content[0].Text != "turn one" {
		t.Errorf("turn 1: unexpected updates %v", updates1)
	}
	if len(updates2) != 1 || updates2[0].Content[0].Text != "turn two" {
		t.Errorf("turn 2: unexpected updates %v", updates2)
	}
}

func TestBridgeToolCallFlow(t *testing.T) {
	// Fake client: first call returns a tool call, second returns final text.
	// "nonexistent_tool" is not in the agent's tool map — HandleToolCall wraps
	// the "tool not found" error as the result string, which becomes a tool
	// result message (role=tool).
	cli := &fakeClient{calls: []*agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{{
				ID:   "tc1",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "nonexistent_tool", Arguments: `{}`},
			}},
		},
		{Role: "assistant", Content: "all done"},
	}}
	b := makeBridge(cli)

	var updates []sessionUpdate
	var permAsked string
	stopReason, err := b.prompt(
		[]contentBlock{{Type: "text", Text: "run it"}},
		func(su sessionUpdate) { updates = append(updates, su) },
		func(toolName, _ string) string { permAsked = toolName; return "allow-once" },
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want \"end_turn\"", stopReason)
	}
	if permAsked != "nonexistent_tool" {
		t.Errorf("expected permission ask for nonexistent_tool, got %q", permAsked)
	}

	kinds := updateKinds(updates)
	// Must see: tool_call, tool_call_update, agent_message_chunk (in that order).
	if len(kinds) < 3 {
		t.Fatalf("expected ≥3 updates, got %d: %v", len(kinds), kinds)
	}
	if kinds[0] != "tool_call" {
		t.Errorf("updates[0].Kind = %q, want \"tool_call\"", kinds[0])
	}
	if kinds[1] != "tool_call_update" {
		t.Errorf("updates[1].Kind = %q, want \"tool_call_update\"", kinds[1])
	}
	if kinds[len(kinds)-1] != "agent_message_chunk" {
		t.Errorf("last update kind = %q, want \"agent_message_chunk\"", kinds[len(kinds)-1])
	}
	// ToolCallID must be echoed on both updates.
	if updates[0].ToolCallID != "tc1" || updates[1].ToolCallID != "tc1" {
		t.Errorf("ToolCallID mismatch: tool_call=%q tool_call_update=%q",
			updates[0].ToolCallID, updates[1].ToolCallID)
	}
	// Status is always "completed" (fragile error-prefix check was removed).
	if updates[1].Status != "completed" {
		t.Errorf("tool_call_update.Status = %q, want \"completed\"", updates[1].Status)
	}
	// Unknown tool maps to kind "other".
	if updates[0].ToolKind != "other" {
		t.Errorf("tool_call.ToolKind = %q, want \"other\"", updates[0].ToolKind)
	}
}

func TestBridgeCancel(t *testing.T) {
	b := &sessionBridge{
		id:           "cancel-test",
		pendingTools: make(map[string]string),
	}
	cli := &cancelOnFirstCallClient{bridge: b}
	b.ag = agent.NewAgent(cli, nil, nil, nil)

	var updates []sessionUpdate
	stopReason, err := b.prompt(
		[]contentBlock{{Type: "text", Text: "start work"}},
		func(su sessionUpdate) { updates = append(updates, su) },
		func(toolName, _ string) string { return "reject-once" },
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopReason != "cancelled" {
		t.Errorf("stopReason = %q, want \"cancelled\"", stopReason)
	}
}

func TestBridgePermissionDeny(t *testing.T) {
	cli := &fakeClient{calls: []*agent.Message{
		{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{{
				ID:   "tc2",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "nonexistent_tool", Arguments: `{}`},
			}},
		},
		{Role: "assistant", Content: "ok"},
	}}
	b := makeBridge(cli)

	var updates []sessionUpdate
	var permAsked string
	stopReason, err := b.prompt(
		[]contentBlock{{Type: "text", Text: "do it"}},
		func(su sessionUpdate) { updates = append(updates, su) },
		func(toolName, _ string) string { permAsked = toolName; return "reject-once" },
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want \"end_turn\"", stopReason)
	}
	if permAsked != "nonexistent_tool" {
		t.Errorf("expected permission ask, got %q", permAsked)
	}
	kinds := updateKinds(updates)
	// Deny still produces tool_call + tool_call_update (with denied message as content).
	found := false
	for _, k := range kinds {
		if k == "tool_call_update" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected tool_call_update after deny, got kinds: %v", kinds)
	}
}
