package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/tool"
)

// fakePNG is the image payload the fake server returns for image tools.
var fakePNG = []byte("\x89PNG\r\n\x1a\nfake-image-bytes")

// TestHelperMCPServer is not a test. It is the fake stdio MCP server that the
// local-transport tests launch by re-executing the test binary.
func TestHelperMCPServer(t *testing.T) {
	if os.Getenv("OCODE_MCP_TEST_SERVER") != "1" {
		t.Skip("helper process for the local MCP transport tests")
	}
	serveFakeMCP(os.Stdin, os.Stdout, os.Getenv("OCODE_MCP_TEST_MODE"), os.Getenv("OCODE_MCP_TEST_LOG"))
	os.Exit(0)
}

// serveFakeMCP speaks line-delimited JSON-RPC. Mode "legacy" answers
// initialize with -32601; mode "initfail" fails the first initialize with
// -32603. Every response is preceded by noise a client must ignore: a
// notification, a stale response with another id, a server request carrying
// the same id, a blank line and a non-JSON line.
func serveFakeMCP(in io.Reader, out io.Writer, mode, logPath string) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 32<<20)
	if mode == "chatty" {
		// An idle server that talks a lot before anyone asks: more than a pipe
		// buffer of notifications with no request waiting for them.
		for i := 0; i < 1000; i++ {
			writeLine(out, map[string]any{"jsonrpc": "2.0", "method": "notifications/message",
				"params": map[string]any{"level": "info", "data": strings.Repeat("n", 80), "seq": i}})
		}
	}
	initCalls := 0
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string          `json:"name"`
				URI       string          `json:"uri"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			os.Exit(3)
		}
		if logPath != "" {
			appendLine(logPath, req.Method)
		}
		if len(req.ID) == 0 {
			continue
		}
		writeLine(out, map[string]any{"jsonrpc": "2.0", "method": "notifications/progress", "params": map[string]any{"progress": 1}})
		writeRaw(out, `{"jsonrpc":"2.0","id":999,"result":{"content":[{"type":"text","text":"STALE"}]}}`)
		writeLine(out, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "method": "roots/list"})
		writeRaw(out, "")
		writeRaw(out, "this is not json")

		switch req.Method {
		case "initialize":
			initCalls++
			switch {
			case mode == "legacy":
				writeError(out, req.ID, mcpMethodNotFound, "Method not found")
			case mode == "initfail" && initCalls == 1:
				writeError(out, req.ID, -32603, "boom")
			default:
				writeResult(out, req.ID, map[string]any{
					"protocolVersion": mcpProtocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "fake", "version": "1"},
				})
			}
		case "tools/list":
			writeResult(out, req.ID, fakeToolListing())
		case "tools/call":
			if req.Params.Name == "reject" {
				writeError(out, req.ID, -32602, "bad")
				continue
			}
			if req.Params.Name == "hang" {
				// Never answered: a request waiting on it stays open until the
				// client gives up or the server is killed.
				continue
			}
			writeResult(out, req.ID, fakeToolCall(req.Params.Name, req.Params.Arguments))
		case "resources/read":
			writeResult(out, req.ID, map[string]any{"contents": []any{map[string]any{
				"uri":      req.Params.URI,
				"mimeType": "text/html;profile=mcp-app",
				"text":     "<html>screen</html>",
			}}})
		default:
			writeError(out, req.ID, -32601, "Method not found")
		}
	}
}

func fakeToolListing() map[string]any {
	// uiBlock is the nested _meta.ui object for a tool rendered by the
	// ui://atlas/screen.html resource with the given audiences.
	uiBlock := func(visibility ...string) map[string]any {
		return map[string]any{"resourceUri": "ui://atlas/screen.html", "visibility": visibility}
	}
	schema := map[string]any{"type": "object"}
	return map[string]any{"tools": []any{
		map[string]any{"name": "show_screen", "description": "Show a screen", "inputSchema": schema,
			"_meta": map[string]any{
				"ui":             uiBlock("model", "app"),
				"ui/resourceUri": "ui://atlas/screen.html",
			}},
		map[string]any{"name": "screen_frame", "description": "Frame", "inputSchema": schema,
			"_meta": map[string]any{"ui": uiBlock("app")}},
		map[string]any{"name": "atlas_status", "description": "Status", "inputSchema": schema},
		map[string]any{"name": "model_only", "description": "Model only", "inputSchema": schema,
			"_meta": map[string]any{"ui": map[string]any{"visibility": []string{"model"}}}},
		map[string]any{"name": "flat_ui", "description": "Flat key", "inputSchema": schema,
			"_meta": map[string]any{"ui/resourceUri": "ui://atlas/flat.html"}},
		map[string]any{"name": "big_screen", "description": "Big UI result", "inputSchema": schema,
			"_meta": map[string]any{"ui": uiBlock("model", "app")}},
		map[string]any{"name": "caption_image", "description": "Text and image", "inputSchema": schema},
		map[string]any{"name": "image_only", "description": "Image only", "inputSchema": schema},
		map[string]any{"name": "plain", "description": "Plain text", "inputSchema": schema},
		map[string]any{"name": "big", "description": "Large text", "inputSchema": schema},
		map[string]any{"name": "slow", "description": "Slow", "inputSchema": schema},
		map[string]any{"name": "fail", "description": "Reports an error", "inputSchema": schema},
		map[string]any{"name": "reject", "description": "Rejected with a JSON-RPC error", "inputSchema": schema},
		map[string]any{"name": "echo_screen", "description": "Echoes its arguments as the screen", "inputSchema": schema,
			"_meta": map[string]any{"ui": uiBlock("model", "app")}},
		map[string]any{"name": "tagged_image", "description": "Image tagged by its arguments", "inputSchema": schema},
		map[string]any{"name": "hang", "description": "Never answers", "inputSchema": schema},
		// Malformed _meta: a string, and a ui value that is not an object.
		map[string]any{"name": "bad_meta", "description": "String _meta", "inputSchema": schema, "_meta": "x"},
		map[string]any{"name": "bad_ui", "description": "Non-object ui", "inputSchema": schema,
			"_meta": map[string]any{"ui": 5}},
	}}
}

func fakeToolCall(name string, args json.RawMessage) map[string]any {
	switch name {
	case "echo_screen":
		echoed := map[string]any{}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &echoed); err != nil {
				return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": err.Error()}}}
			}
		}
		return map[string]any{
			"content":           []any{map[string]any{"type": "text", "text": "echoed"}},
			"structuredContent": echoed,
		}
	case "tagged_image":
		var tag struct {
			Tag string `json:"tag"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args, &tag); err != nil {
				return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": err.Error()}}}
			}
		}
		return map[string]any{"content": []any{
			map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString([]byte(tag.Tag)), "mimeType": "image/png"},
		}}
	case "plain":
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "plain result"}}}
	case "caption_image":
		return map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "caption"},
			map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(fakePNG), "mimeType": "image/png"},
		}}
	case "image_only":
		return map[string]any{"content": []any{
			map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(fakePNG), "mimeType": "image/png"},
		}}
	case "big":
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": strings.Repeat("x", 200*1024)}}}
	case "show_screen":
		return map[string]any{
			"content":           []any{map[string]any{"type": "text", "text": "shown"}},
			"structuredContent": map[string]any{"screen": "home"},
			"_meta":             map[string]any{"trace": "t1"},
			"isError":           false,
		}
	case "big_screen":
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "big"}},
			"structuredContent": map[string]any{"blob": strings.Repeat("y", 600*1024)}}
	case "slow":
		time.Sleep(600 * time.Millisecond)
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "slow done"}}}
	case "fail":
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "bad input"}}, "isError": true}
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": "unknown " + name}}}
}

func writeLine(out io.Writer, msg map[string]any) {
	data, err := json.Marshal(msg)
	if err != nil {
		os.Exit(4)
	}
	writeRaw(out, string(data))
}

func writeRaw(out io.Writer, line string) {
	_, _ = io.WriteString(out, line+"\n")
}

func writeResult(out io.Writer, id json.RawMessage, result any) {
	writeLine(out, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeError(out io.Writer, id json.RawMessage, code int, message string) {
	writeLine(out, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func appendLine(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(5)
	}
	defer f.Close()
	_, _ = io.WriteString(f, line+"\n")
}

// newFakeLocalClient starts the fake server in the given mode. It returns the
// client and the path of the file that records the methods the server saw.
func newFakeLocalClient(t *testing.T, mode string) (*MCPClient, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "methods.log")
	client, err := NewLocalClient(mcpTestServerName(t), config.MCPConfig{
		Type:    "local",
		Command: []string{os.Args[0], "-test.run=^TestHelperMCPServer$"},
		Environment: map[string]string{
			"OCODE_MCP_TEST_SERVER": "1",
			"OCODE_MCP_TEST_MODE":   mode,
			"OCODE_MCP_TEST_LOG":    logPath,
		},
		Enabled: true,
		Timeout: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.cmd.Process.Kill()
		_ = client.cmd.Wait()
	})
	return client, logPath
}

func readMethodLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func findListedTool(t *testing.T, client *MCPClient, short string) MCPTool {
	t.Helper()
	tools, err := client.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools {
		if tl.Name() == client.Name()+"_"+short {
			return tl
		}
	}
	t.Fatalf("tool %s not in listing", short)
	return MCPTool{}
}

func TestLocalClientRunsInitializeHandshakeBeforeFirstRequest(t *testing.T) {
	client, logPath := newFakeLocalClient(t, "normal")
	if _, err := client.ListTools(); err != nil {
		t.Fatal(err)
	}
	want := []string{"initialize", "notifications/initialized", "tools/list"}
	if got := readMethodLog(t, logPath); !reflect.DeepEqual(got, want) {
		t.Fatalf("server saw %v, want %v", got, want)
	}

	if _, err := client.ListTools(); err != nil {
		t.Fatal(err)
	}
	want = append(want, "tools/list")
	if got := readMethodLog(t, logPath); !reflect.DeepEqual(got, want) {
		t.Fatalf("after second request server saw %v, want %v (handshake must run once)", got, want)
	}
}

func TestLocalClientAcceptsLegacyServerWithoutInitialize(t *testing.T) {
	client, logPath := newFakeLocalClient(t, "legacy")
	for i := 0; i < 2; i++ {
		tools, err := client.ListTools()
		if err != nil {
			t.Fatalf("ListTools %d: %v", i, err)
		}
		if len(tools) == 0 {
			t.Fatalf("ListTools %d returned no tools", i)
		}
	}
	want := []string{"initialize", "tools/list", "tools/list"}
	if got := readMethodLog(t, logPath); !reflect.DeepEqual(got, want) {
		t.Fatalf("server saw %v, want %v", got, want)
	}
}

func TestLocalClientRetriesHandshakeAfterOtherError(t *testing.T) {
	client, logPath := newFakeLocalClient(t, "initfail")
	if _, err := client.ListTools(); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("first ListTools error = %v, want the initialize failure", err)
	}
	if _, err := client.ListTools(); err != nil {
		t.Fatalf("second ListTools: %v", err)
	}
	want := []string{"initialize", "initialize", "notifications/initialized", "tools/list"}
	if got := readMethodLog(t, logPath); !reflect.DeepEqual(got, want) {
		t.Fatalf("server saw %v, want %v", got, want)
	}
}

func TestLocalClientMatchesResponseIDsAndSkipsNoise(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	raw, err := client.CallToolRaw("plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "plain result") {
		t.Fatalf("result = %s, want the response to this request", raw)
	}
	if strings.Contains(string(raw), "STALE") {
		t.Fatalf("result = %s, a response with another id was returned", raw)
	}
}

func TestLocalClientReadsResponseLargerThan64KB(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	text, err := client.CallTool(client.Name()+"_big", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) != 200*1024 {
		t.Fatalf("text length = %d, want %d", len(text), 200*1024)
	}
}

func TestListToolsAppVisibilityAndUIResources(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	tools, err := client.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]MCPTool{}
	for _, tl := range tools {
		listed[tl.Name()] = tl
	}
	if _, ok := listed[client.Name()+"_screen_frame"]; ok {
		t.Fatal("app-only tool screen_frame was returned to the model")
	}
	for _, shown := range []string{"show_screen", "atlas_status", "model_only", "flat_ui"} {
		if _, ok := listed[client.Name()+"_"+shown]; !ok {
			t.Fatalf("model-visible tool %s was not returned", shown)
		}
	}

	allowed := map[string]bool{
		"screen_frame": true,  // app-only: explicit "app"
		"show_screen":  true,  // model + app: explicit "app"
		"atlas_status": false, // no _meta: no explicit "app"
		"ghost":        false, // not listed at all
		"model_only":   false, // listed with visibility ["model"]
	}
	for name, want := range allowed {
		if got := client.AppToolAllowed(name); got != want {
			t.Fatalf("AppToolAllowed(%s) = %v, want %v", name, got, want)
		}
	}

	if !client.HasUIResource("ui://atlas/screen.html") {
		t.Fatal("HasUIResource(screen.html) = false, want true")
	}
	if !client.HasUIResource("ui://atlas/flat.html") {
		t.Fatal("HasUIResource(flat.html) = false, want true (flat _meta key fallback)")
	}
	if client.HasUIResource("ui://atlas/other.html") || client.HasUIResource("") {
		t.Fatal("HasUIResource reported a resource no tool declares")
	}

	ui := listed[client.Name()+"_show_screen"]
	if got := ui.MCPAppResource(); got != "ui://atlas/screen.html" {
		t.Fatalf("MCPAppResource() = %q", got)
	}
	if got := listed[client.Name()+"_atlas_status"].MCPAppResource(); got != "" {
		t.Fatalf("MCPAppResource() of a plain tool = %q, want empty", got)
	}
	if ui.Client() != client {
		t.Fatal("Client() did not return the listing client")
	}

	def, err := json.Marshal(ui.Definition())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(def), "_meta") || strings.Contains(string(def), "ui://") {
		t.Fatalf("Definition() of a UI tool leaks UI metadata: %s", def)
	}
}

func callCtx(id string) context.Context {
	return tool.WithToolCallID(context.Background(), id)
}

func TestCallToolReturnsTextAndStoresFirstImage(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	tl := findListedTool(t, client, "caption_image")

	text, err := tl.ExecuteCtx(callCtx("call-1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "caption" {
		t.Fatalf("text = %q, want %q", text, "caption")
	}
	data, mime, ok := tl.TakeCallImage("call-1")
	if !ok || !bytes.Equal(data, fakePNG) || mime != "image/png" {
		t.Fatalf("TakeCallImage = (%d bytes, %q, %v), want the call's PNG", len(data), mime, ok)
	}
	if _, _, again := tl.TakeCallImage("call-1"); again {
		t.Fatal("second TakeCallImage succeeded, want the image removed")
	}
}

func TestCallToolReturnsPlaceholderForImageOnlyResult(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	tl := findListedTool(t, client, "image_only")
	text, err := tl.ExecuteCtx(callCtx("img-1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "[image]" {
		t.Fatalf("text = %q, want [image]", text)
	}
	if _, _, ok := tl.TakeCallImage("img-1"); !ok {
		t.Fatal("TakeCallImage = false for an image-only result")
	}
}

func TestTakeMCPAppResultReturnsRawResultOnce(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	ui := findListedTool(t, client, "show_screen")
	plain := findListedTool(t, client, "plain")

	if _, err := ui.ExecuteCtx(callCtx("app-1"), nil); err != nil {
		t.Fatal(err)
	}
	raw, ran := ui.TakeMCPAppResult("app-1")
	if !ran {
		t.Fatal("TakeMCPAppResult ran = false after a successful app call")
	}
	for _, want := range []string{"structuredContent", "_meta", "shown", "home", "t1"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("TakeMCPAppResult = %s, missing %q", raw, want)
		}
	}
	if again, ranAgain := ui.TakeMCPAppResult("app-1"); ranAgain || again != nil {
		t.Fatalf("second TakeMCPAppResult = (%s, %v), want nothing", again, ranAgain)
	}

	if _, err := plain.ExecuteCtx(callCtx("plain-1"), nil); err != nil {
		t.Fatal(err)
	}
	if _, ranPlain := plain.TakeMCPAppResult("plain-1"); ranPlain {
		t.Fatal("TakeMCPAppResult of a tool with no UI resource reported ran")
	}
}

func TestTakeMCPAppResultDropsOversizedResult(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	tl := findListedTool(t, client, "big_screen")
	if _, err := tl.ExecuteCtx(callCtx("big-1"), nil); err != nil {
		t.Fatal(err)
	}
	raw, ran := tl.TakeMCPAppResult("big-1")
	if !ran {
		t.Fatal("ran = false for a successful call whose result was over the cap")
	}
	if raw != nil {
		t.Fatalf("TakeMCPAppResult kept a %d-byte result, want nil above the 512 KB cap", len(raw))
	}
}
func TestCallToolErrorResultKeepsTextAndError(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	text, err := client.CallTool(client.Name()+"_fail", nil)
	if err == nil {
		t.Fatal("CallTool error = nil for isError result")
	}
	if text != "bad input" {
		t.Fatalf("text = %q, want %q", text, "bad input")
	}
}

func TestCallToolRawAndReadResourceReturnRawResult(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	raw, err := client.CallToolRaw("plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	var callResult struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &callResult); err != nil {
		t.Fatalf("CallToolRaw result is not a result object: %v (%s)", err, raw)
	}
	if len(callResult.Content) != 1 || callResult.Content[0].Text != "plain result" {
		t.Fatalf("CallToolRaw content = %+v", callResult.Content)
	}

	res, err := client.ReadResource("ui://atlas/screen.html")
	if err != nil {
		t.Fatal(err)
	}
	var readResult struct {
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(res, &readResult); err != nil {
		t.Fatalf("ReadResource result is not a result object: %v (%s)", err, res)
	}
	if len(readResult.Contents) != 1 || readResult.Contents[0].Text != "<html>screen</html>" {
		t.Fatalf("ReadResource contents = %+v", readResult.Contents)
	}
	if got := client.Name(); got != mcpTestServerName(t) {
		t.Fatalf("Name() = %q", got)
	}
}

func TestLocalClientTimedOutRequestDoesNotCorruptNextRequest(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	// Warm up so the handshake is not what the short timeout measures.
	if _, err := client.ListTools(); err != nil {
		t.Fatal(err)
	}

	client.timeout = 200 * time.Millisecond
	if _, err := client.CallTool(client.Name()+"_slow", nil); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("slow call error = %v, want a timeout", err)
	}

	client.timeout = 5 * time.Second
	raw, err := client.CallToolRaw("plain", nil)
	if err != nil {
		t.Fatalf("request after a timeout: %v", err)
	}
	if !strings.Contains(string(raw), "plain result") || strings.Contains(string(raw), "slow done") {
		t.Fatalf("request after a timeout got %s, want its own result", raw)
	}
}

func TestListToolsToleratesMalformedMeta(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed on a malformed _meta: %v", err)
	}
	listed := map[string]MCPTool{}
	for _, tl := range tools {
		listed[tl.Name()] = tl
	}
	for _, name := range []string{"bad_meta", "bad_ui", "atlas_status"} {
		tl, ok := listed[client.Name()+"_"+name]
		if !ok {
			t.Fatalf("tool %s with malformed or absent _meta was dropped", name)
		}
		if got := tl.MCPAppResource(); got != "" {
			t.Fatalf("MCPAppResource() of %s = %q, want empty", name, got)
		}
		if client.AppToolAllowed(name) {
			t.Fatalf("AppToolAllowed(%s) = true, want false: no explicit app visibility", name)
		}
	}
	// The good tools in the same listing keep their metadata.
	if got := listed[client.Name()+"_show_screen"].MCPAppResource(); got != "ui://atlas/screen.html" {
		t.Fatalf("show_screen MCPAppResource() = %q after a malformed sibling", got)
	}
}

func TestCallToolRawReturnsTypedRPCError(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	_, err := client.CallToolRaw("reject", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("CallToolRaw error = nil for a JSON-RPC error response")
	}
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error %T %v is not a *RPCError", err, err)
	}
	if rpcErr.Code != -32602 || rpcErr.Message != "bad" {
		t.Fatalf("RPCError = %+v, want code -32602 message bad", rpcErr)
	}
	if got, want := err.Error(), "MCP error (-32602): bad"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestDecodeRemoteResponseReturnsTypedRPCError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad"}}`)),
	}
	_, err := decodeRemoteResponse("docs", resp)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error %T %v is not a *RPCError", err, err)
	}
	if rpcErr.Code != -32602 || rpcErr.Message != "bad" {
		t.Fatalf("RPCError = %+v, want code -32602 message bad", rpcErr)
	}
	if got, want := err.Error(), `remote MCP server "docs" returned protocol error -32602`; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestInterleavedCallsOfSameToolKeepTheirOwnOutcomes(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	echo := findListedTool(t, client, "echo_screen")
	tagged := findListedTool(t, client, "tagged_image")

	// Two calls of each tool, interleaved, with different call IDs.
	for _, step := range []struct {
		tl   MCPTool
		id   string
		args string
	}{
		{echo, "a", `{"screen":"one"}`},
		{tagged, "a", `{"tag":"one"}`},
		{echo, "b", `{"screen":"two"}`},
		{tagged, "b", `{"tag":"two"}`},
	} {
		if _, err := step.tl.ExecuteCtx(callCtx(step.id), json.RawMessage(step.args)); err != nil {
			t.Fatal(err)
		}
	}

	// Take B before A: each call still gets its own outcome.
	rawB, ranB := echo.TakeMCPAppResult("b")
	if !ranB || !strings.Contains(string(rawB), `"two"`) || strings.Contains(string(rawB), `"one"`) {
		t.Fatalf("app result of call b = (%s, %v), want its own", rawB, ranB)
	}
	rawA, ranA := echo.TakeMCPAppResult("a")
	if !ranA || !strings.Contains(string(rawA), `"one"`) || strings.Contains(string(rawA), `"two"`) {
		t.Fatalf("app result of call a = (%s, %v), want its own", rawA, ranA)
	}
	imgB, _, okB := tagged.TakeCallImage("b")
	if !okB || string(imgB) != "two" {
		t.Fatalf("image of call b = (%q, %v), want two", imgB, okB)
	}
	imgA, _, okA := tagged.TakeCallImage("a")
	if !okA || string(imgA) != "one" {
		t.Fatalf("image of call a = (%q, %v), want one", imgA, okA)
	}
}

func TestExecuteWithoutCallIDRecordsNothing(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	echo := findListedTool(t, client, "echo_screen")
	tagged := findListedTool(t, client, "tagged_image")

	if _, err := echo.Execute(json.RawMessage(`{"screen":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tagged.Execute(json.RawMessage(`{"tag":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if _, ran := echo.TakeMCPAppResult(""); ran {
		t.Fatal("Execute without a call ID recorded an app result")
	}
	if _, _, ok := tagged.TakeCallImage(""); ok {
		t.Fatal("Execute without a call ID recorded an image")
	}
	if _, ran := echo.TakeMCPAppResult("x"); ran {
		t.Fatal("Execute without a call ID was kept under some ID")
	}
	if _, _, ok := tagged.TakeCallImage("x"); ok {
		t.Fatal("Execute without a call ID was kept under some ID")
	}
}

func TestLocalClientRoutesResponsesThroughIdleChattyServer(t *testing.T) {
	client, _ := newFakeLocalClient(t, "chatty")
	if _, err := client.ListTools(); err != nil {
		t.Fatalf("ListTools against a server that floods notifications: %v", err)
	}
	raw, err := client.CallToolRaw("plain", nil)
	if err != nil {
		t.Fatalf("request after a notification flood: %v", err)
	}
	if !strings.Contains(string(raw), "plain result") {
		t.Fatalf("result = %s, want the response to this request", raw)
	}
}

func pendingCount(c *MCPClient) int {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	return len(c.pending)
}

func TestLateResponseAfterTimeoutIsDroppedAndNextRequestGetsItsOwn(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	if _, err := client.ListTools(); err != nil {
		t.Fatal(err)
	}
	client.timeout = 200 * time.Millisecond
	if _, err := client.CallTool(client.Name()+"_slow", nil); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("slow call error = %v, want a timeout", err)
	}
	if n := pendingCount(client); n != 0 {
		t.Fatalf("pending requests after a timeout = %d, want 0 (the waiter must be removed)", n)
	}
	client.timeout = 5 * time.Second
	raw, err := client.CallToolRaw("plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "plain result") || strings.Contains(string(raw), "slow done") {
		t.Fatalf("next request got %s, want its own result", raw)
	}
}

func TestCloseFailsPendingRequestAndEndsReader(t *testing.T) {
	client, _ := newFakeLocalClient(t, "normal")
	if _, err := client.ListTools(); err != nil {
		t.Fatal(err)
	}
	client.timeout = time.Minute
	errc := make(chan error, 1)
	go func() {
		_, err := client.CallToolRaw("hang", nil)
		errc <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for pendingCount(client) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the hanging request never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("pending request succeeded after Close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending request did not fail promptly after Close")
	}
	select {
	case <-client.readerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("stdout reader did not exit after Close")
	}
	if _, err := client.CallToolRaw("plain", nil); err == nil {
		t.Fatal("request after Close succeeded")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
