package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// mcpAppFakeTool is an MCP tool that declares a UI resource. A call that runs
// through ExecuteCtx records its result under the tool call ID it carries, and
// TakeMCPAppResult takes that result, as mcp.MCPTool does.
type mcpAppFakeTool struct {
	name     string
	server   string
	resource string
	result   json.RawMessage
	takes    *int32
	runs     *int32
	parallel bool

	mu   sync.Mutex
	done map[string]json.RawMessage
}

func (t *mcpAppFakeTool) Name() string        { return t.name }
func (t *mcpAppFakeTool) Description() string { return "fake MCP app tool" }
func (t *mcpAppFakeTool) Parallel() bool      { return t.parallel }
func (t *mcpAppFakeTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": t.name}
}
func (t *mcpAppFakeTool) Execute(json.RawMessage) (string, error) { return "shown", nil }
func (t *mcpAppFakeTool) ExecuteCtx(ctx context.Context, _ json.RawMessage) (string, error) {
	if t.runs != nil {
		atomic.AddInt32(t.runs, 1)
	}
	if id, ok := tool.ToolCallIDFrom(ctx); ok {
		t.mu.Lock()
		if t.done == nil {
			t.done = map[string]json.RawMessage{}
		}
		t.done[id] = t.result
		t.mu.Unlock()
	}
	return "shown", nil
}
func (t *mcpAppFakeTool) MCPServer() string      { return t.server }
func (t *mcpAppFakeTool) MCPAppResource() string { return t.resource }
func (t *mcpAppFakeTool) TakeMCPAppResult(toolCallID string) (json.RawMessage, bool) {
	if t.takes != nil {
		atomic.AddInt32(t.takes, 1)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	raw, ok := t.done[toolCallID]
	delete(t.done, toolCallID)
	return raw, ok
}

// mcpPathImageTool is an MCP tool with a "path" argument. It is not an image
// tool, so the path-image branch of handleToolCallWithImages never reads it.
type mcpPathImageTool struct{}

func (t *mcpPathImageTool) Name() string        { return "docs_path_image" }
func (t *mcpPathImageTool) Description() string { return "MCP tool with a path argument" }
func (t *mcpPathImageTool) Parallel() bool      { return false }
func (t *mcpPathImageTool) Definition() map[string]interface{} {
	return map[string]interface{}{"name": t.Name()}
}
func (t *mcpPathImageTool) Execute(json.RawMessage) (string, error) { return "text result", nil }
func (t *mcpPathImageTool) MCPServer() string                       { return "docs" }

// mcpCallImageFake is an MCP tool whose call returns an image, as mcp.MCPTool
// does: the image is kept under the tool call ID until TakeCallImage takes it.
type mcpCallImageFake struct {
	png    []byte
	mu     sync.Mutex
	images map[string][]byte
}

func (t *mcpCallImageFake) Name() string        { return "docs_snap" }
func (t *mcpCallImageFake) Description() string { return "MCP tool that returns an image" }
func (t *mcpCallImageFake) Parallel() bool      { return false }
func (t *mcpCallImageFake) Definition() map[string]interface{} {
	return map[string]interface{}{"name": t.Name()}
}
func (t *mcpCallImageFake) Execute(json.RawMessage) (string, error) { return "snapped", nil }
func (t *mcpCallImageFake) MCPServer() string                       { return "docs" }
func (t *mcpCallImageFake) ExecuteCtx(ctx context.Context, _ json.RawMessage) (string, error) {
	if id, ok := tool.ToolCallIDFrom(ctx); ok {
		t.mu.Lock()
		if t.images == nil {
			t.images = map[string][]byte{}
		}
		t.images[id] = t.png
		t.mu.Unlock()
	}
	return "snapped", nil
}
func (t *mcpCallImageFake) TakeCallImage(toolCallID string) ([]byte, string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	raw, ok := t.images[toolCallID]
	delete(t.images, toolCallID)
	return raw, "image/png", ok
}

// testPNGBytes returns a small, decodable PNG.
func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// visionModelClient reports a vision-capable model so the vision branches of
// handleToolCallWithImages run. Its model id is picked by name prefix.
type visionModelClient struct {
	*MockClient
	model string
}

func (c visionModelClient) GetProvider() string { return "mock" }
func (c visionModelClient) GetModel() string    { return c.model }

func TestMCPAppRefAttachesToSequentialToolMessage(t *testing.T) {
	var takes int32
	ui := &mcpAppFakeTool{
		name:     "docs_show_screen",
		server:   "docs",
		resource: "ui://docs/screen.html",
		result:   json.RawMessage(`{"content":[{"type":"text","text":"shown"}],"structuredContent":{"screen":"home"}}`),
		takes:    &takes,
	}
	client := &dedupStepClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{mcpCall("tc-app", "docs_show_screen", `{}`)}},
		{Role: "assistant", Content: "done"},
	}}
	a := newTestAgent(client, []tool.Tool{ui}, &config.Config{}, nil)
	a.Permissions().SetMode(PermissionModeYOLO)

	messages, err := a.Step([]Message{{Role: "user", Content: "show it"}})
	if err != nil {
		t.Fatal(err)
	}
	toolMsg := findToolMessage(t, messages, "tc-app")
	if toolMsg.MCPApp == nil {
		t.Fatal("tool message has no MCPApp for a call to an MCP Apps tool")
	}
	if toolMsg.MCPApp.Server != "docs" || toolMsg.MCPApp.Tool != "show_screen" {
		t.Fatalf("MCPApp server/tool = %q/%q, want docs/show_screen", toolMsg.MCPApp.Server, toolMsg.MCPApp.Tool)
	}
	if toolMsg.MCPApp.ResourceURI != "ui://docs/screen.html" {
		t.Fatalf("MCPApp resource = %q", toolMsg.MCPApp.ResourceURI)
	}
	if !strings.Contains(string(toolMsg.MCPApp.Result), `"screen":"home"`) {
		t.Fatalf("MCPApp result = %s, want the raw call result", toolMsg.MCPApp.Result)
	}
	if got := atomic.LoadInt32(&takes); got != 1 {
		t.Fatalf("TakeMCPAppResult called %d times, want exactly 1", got)
	}
}

func TestMCPAppRefAttachesToParallelToolMessages(t *testing.T) {
	var takes int32
	mk := func(id, short string) *mcpAppFakeTool {
		return &mcpAppFakeTool{
			name:     "docs_" + short,
			server:   "docs",
			resource: "ui://docs/" + short + ".html",
			result:   json.RawMessage(`{"tag":"` + short + `"}`),
			takes:    &takes,
			parallel: true,
		}
	}
	client := &dedupStepClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{
			mcpCall("tc-a", "docs_one", `{}`),
			mcpCall("tc-b", "docs_two", `{}`),
		}},
		{Role: "assistant", Content: "done"},
	}}
	a := newTestAgent(client, []tool.Tool{mk("tc-a", "one"), mk("tc-b", "two")}, &config.Config{}, nil)
	a.Permissions().SetMode(PermissionModeYOLO)

	messages, err := a.Step([]Message{{Role: "user", Content: "both"}})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"tc-a": "one", "tc-b": "two"} {
		msg := findToolMessage(t, messages, id)
		if msg.MCPApp == nil || msg.MCPApp.Tool != want {
			t.Fatalf("tool message %s MCPApp = %+v, want tool %q", id, msg.MCPApp, want)
		}
		if !strings.Contains(string(msg.MCPApp.Result), `"tag":"`+want+`"`) {
			t.Fatalf("tool message %s carries result %s, want its own", id, msg.MCPApp.Result)
		}
	}
}

func TestMCPAppRefNilForToolWithoutResource(t *testing.T) {
	var takes int32
	plain := &mcpAppFakeTool{name: "docs_status", server: "docs", takes: &takes}
	client := &dedupStepClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{mcpCall("tc-plain", "docs_status", `{}`)}},
		{Role: "assistant", Content: "done"},
	}}
	a := newTestAgent(client, []tool.Tool{plain}, &config.Config{}, nil)
	a.Permissions().SetMode(PermissionModeYOLO)

	messages, err := a.Step([]Message{{Role: "user", Content: "status"}})
	if err != nil {
		t.Fatal(err)
	}
	if msg := findToolMessage(t, messages, "tc-plain"); msg.MCPApp != nil {
		t.Fatalf("tool without an MCP Apps resource got MCPApp %+v", msg.MCPApp)
	}
	if got := atomic.LoadInt32(&takes); got != 0 {
		t.Fatalf("TakeMCPAppResult called %d times for a tool with no resource, want 0", got)
	}
}

func TestDeniedMCPAppCallGetsNoWidget(t *testing.T) {
	var takes, runs int32
	ui := &mcpAppFakeTool{name: "docs_show_screen", server: "docs", resource: "ui://docs/screen.html",
		result: json.RawMessage(`{"stale":true}`), takes: &takes, runs: &runs}
	client := &dedupStepClient{responses: []*Message{
		{Role: "assistant", ToolCalls: []ToolCall{mcpCall("tc-denied", "docs_show_screen", `{}`)}},
		{Role: "assistant", Content: "done"},
	}}
	a := newTestAgent(client, []tool.Tool{ui}, &config.Config{}, nil)
	a.Permissions().SetRule("docs_show_screen", PermissionDeny)

	messages, err := a.Step([]Message{{Role: "user", Content: "show it"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&runs); got != 0 {
		t.Fatalf("the denied tool executed %d times, want 0", got)
	}
	if msg := findToolMessage(t, messages, "tc-denied"); msg.MCPApp != nil {
		t.Fatalf("denied call got a widget: %+v", msg.MCPApp)
	}
	if got := atomic.LoadInt32(&takes); got != 1 {
		t.Fatalf("TakeMCPAppResult called %d times for the denied call, want 1", got)
	}
}

func TestMCPAppRefNotKeptForCallThatDidNotRun(t *testing.T) {
	ui := &mcpAppFakeTool{name: "docs_show", server: "docs", resource: "ui://docs/s.html",
		result: json.RawMessage(`{"stale":true}`)}
	a := newTestAgent(nil, []tool.Tool{ui}, &config.Config{}, nil)
	a.Permissions().SetRule("docs_show", PermissionDeny)

	if _, _, err := a.handleToolCallWithImages(context.Background(), "docs_show", json.RawMessage(`{}`), nil, "tc-no-run"); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.mcpAppRefs.Load("tc-no-run"); ok {
		t.Fatal("a call that never ran kept an MCP Apps ref")
	}
}
func TestTakeMCPAppRefIgnoresAskSentinel(t *testing.T) {
	a := &Agent{}
	ref := &MCPAppRef{Server: "docs", Tool: "show", ResourceURI: "ui://docs/s.html"}
	a.mcpAppRefs.Store("tc-ask", ref)
	if got := a.takeMCPAppRef("tc-ask", tool.SentinelPermissionAsk+" run docs_show"); got != nil {
		t.Fatalf("takeMCPAppRef on a permission-ask result = %+v, want nil", got)
	}
	if _, ok := a.mcpAppRefs.Load("tc-ask"); ok {
		t.Fatal("takeMCPAppRef on an ask result left the ref behind")
	}
}

func TestMCPAppRefNeverReachesProviderPayload(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "show it"},
		{Role: "assistant", ToolCalls: []ToolCall{mcpCall("tc-app", "docs_show_screen", `{}`)}},
		{Role: "tool", ToolID: "tc-app", Content: "shown", MCPApp: &MCPAppRef{
			Server: "docs", Tool: "show_screen", ResourceURI: "ui://docs/screen.html",
			Result: json.RawMessage(`{"structuredContent":{"secret":"widget-only"}}`),
		}},
	}
	c := &GenericClient{}
	openAI, err := c.convertToOpenAIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	anthropic, err := c.buildAnthropicMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]any{"openai": openAI, "anthropic": anthropic} {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		for _, leak := range []string{"mcp_app", "ui://docs", "widget-only", "resource_uri"} {
			if strings.Contains(string(data), leak) {
				t.Fatalf("%s payload contains MCP Apps state %q: %s", name, leak, data)
			}
		}
	}
}

func TestMessageMCPAppJSONRoundTrip(t *testing.T) {
	in := Message{Role: "tool", ToolID: "tc-1", Content: "shown", MCPApp: &MCPAppRef{
		Server: "docs", Tool: "show_screen", ResourceURI: "ui://docs/screen.html",
		Result: json.RawMessage(`{"ok":true}`),
	}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Message
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.MCPApp == nil || out.MCPApp.Server != "docs" || out.MCPApp.Tool != "show_screen" ||
		out.MCPApp.ResourceURI != "ui://docs/screen.html" || string(out.MCPApp.Result) != `{"ok":true}` {
		t.Fatalf("round trip lost mcp_app: %+v", out.MCPApp)
	}
	if !strings.Contains(string(data), `"mcp_app":{"server":"docs"`) {
		t.Fatalf("serialized form %s does not use the mcp_app key", data)
	}

	plain, err := json.Marshal(Message{Role: "tool", ToolID: "tc-2", Content: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "mcp_app") {
		t.Fatalf("a message without MCPApp serialized mcp_app: %s", plain)
	}
}

func TestMCPToolWithPathArgumentSkipsPathImageBranch(t *testing.T) {
	// A real, decodable PNG. An MCP tool is not an image-result tool, so the
	// path branch never reads it, even with a vision-capable model.
	pngPath := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(pngPath, testPNGBytes(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestAgent(visionModelClient{MockClient: &MockClient{}, model: "claude-opus-test"},
		[]tool.Tool{&mcpPathImageTool{}}, &config.Config{}, nil)
	a.Permissions().SetRule("docs_path_image", PermissionAllow)

	args, _ := json.Marshal(map[string]string{"path": pngPath})
	text, images, err := a.handleToolCallWithImages(context.Background(), "docs_path_image", args, nil, "tc-path")
	if err != nil {
		t.Fatal(err)
	}
	if text != "text result" {
		t.Fatalf("text = %q, want the tool's own result", text)
	}
	if len(images) != 0 {
		t.Fatalf("MCP tool with a path argument produced %d vision images, want none", len(images))
	}
}
func TestMCPCallImageDrainedWhenModelCannotSeeIt(t *testing.T) {
	snap := &mcpCallImageFake{png: testPNGBytes(t)}
	// mock-model is not a vision model, so the call's image has no consumer.
	a := newTestAgent(&MockClient{}, []tool.Tool{snap}, &config.Config{}, nil)
	a.Permissions().SetRule("docs_snap", PermissionAllow)

	text, images, err := a.handleToolCallWithImages(context.Background(), "docs_snap", json.RawMessage(`{}`), nil, "tc-snap")
	if err != nil {
		t.Fatal(err)
	}
	if text != "snapped" || len(images) != 0 {
		t.Fatalf("result = (%q, %d images), want (snapped, 0)", text, len(images))
	}
	if _, _, ok := snap.TakeCallImage("tc-snap"); ok {
		t.Fatal("the unused image was retained after the call")
	}
}

func TestMCPCallImageAttachedWhenModelCanSeeIt(t *testing.T) {
	snap := &mcpCallImageFake{png: testPNGBytes(t)}
	a := newTestAgent(visionModelClient{MockClient: &MockClient{}, model: "claude-opus-test"},
		[]tool.Tool{snap}, &config.Config{}, nil)
	a.Permissions().SetRule("docs_snap", PermissionAllow)

	text, images, err := a.handleToolCallWithImages(context.Background(), "docs_snap", json.RawMessage(`{}`), nil, "tc-snap-v")
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("images = %d, want the call's image", len(images))
	}
	if text != "snapped\n[image shown below]" {
		t.Fatalf("text = %q", text)
	}
	if _, _, ok := snap.TakeCallImage("tc-snap-v"); ok {
		t.Fatal("the image was retained after the call")
	}
}

// mcpCall builds a function tool call with the given ID, name and arguments.
func mcpCall(id, name, args string) ToolCall {
	var tc ToolCall
	tc.ID = id
	tc.Type = "function"
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

// findToolMessage returns the tool message answering toolCallID.
func findToolMessage(t *testing.T, messages []Message, toolCallID string) Message {
	t.Helper()
	for _, m := range messages {
		if m.Role == "tool" && m.ToolID == toolCallID {
			return m
		}
	}
	t.Fatalf("no tool message for %s in %d messages", toolCallID, len(messages))
	return Message{}
}
