package server

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/mcp"
	"github.com/u007/ocode/internal/tool"
)

const mcpAppTestServerName = "docs"

// TestHelperMCPAppServer is not a test. It is the fake stdio MCP server the
// MCP Apps handler tests launch by re-executing the test binary.
func TestHelperMCPAppServer(t *testing.T) {
	if os.Getenv("OCODE_MCP_APP_TEST_SERVER") != "1" {
		t.Skip("helper process for the MCP Apps handler tests")
	}
	mcpAppFakeServe(os.Stdin, os.Stdout)
	os.Exit(0)
}

// mcpAppFakeServe speaks line-delimited JSON-RPC. It exits when stdin closes,
// which happens when the test process exits.
func mcpAppFakeServe(in io.Reader, out io.Writer) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
				URI  string `json:"uri"`
			} `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			os.Exit(3)
		}
		if len(req.ID) == 0 {
			continue // notifications such as notifications/initialized
		}
		switch req.Method {
		case "initialize":
			mcpAppFakeWrite(out, req.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "docs-fake", "version": "1"},
			})
		case "tools/list":
			mcpAppFakeWrite(out, req.ID, mcpAppFakeListing())
		case "tools/call":
			if req.Params.Name == "bad_args" {
				mcpAppFakeWriteError(out, req.ID, -32602, "bad arguments")
				continue
			}
			mcpAppFakeWrite(out, req.ID, mcpAppFakeCall(req.Params.Name))
		case "resources/read":
			result, ok := mcpAppFakeResource(req.Params.URI)
			if !ok {
				mcpAppFakeWriteError(out, req.ID, -32002, "resource not found")
				continue
			}
			mcpAppFakeWrite(out, req.ID, result)
		default:
			mcpAppFakeWriteError(out, req.ID, -32601, "Method not found")
		}
	}
}

// mcpAppFakeListing declares:
//   - show_screen: model and app visible, UI resource ui://docs/screen.html
//   - screen_frame: app only, same resource
//   - model_only: model visible, not callable by a view
//   - blob_view: model visible, UI resource ui://docs/blob.html (blob content)
//   - bad_args: app callable, fails with -32602
//   - plain_tool: no UI metadata
func mcpAppFakeListing() map[string]any {
	schema := map[string]any{"type": "object"}
	ui := func(uri string, visibility ...string) map[string]any {
		return map[string]any{"ui": map[string]any{"resourceUri": uri, "visibility": visibility}}
	}
	return map[string]any{"tools": []any{
		map[string]any{"name": "show_screen", "description": "Show", "inputSchema": schema,
			"_meta": ui("ui://docs/screen.html", "model", "app")},
		map[string]any{"name": "screen_frame", "description": "Frame", "inputSchema": schema,
			"_meta": ui("ui://docs/screen.html", "app")},
		map[string]any{"name": "model_only", "description": "Model only", "inputSchema": schema,
			"_meta": map[string]any{"ui": map[string]any{"resourceUri": "ui://docs/screen.html", "visibility": []string{"model"}}}},
		map[string]any{"name": "blob_view", "description": "Blob view", "inputSchema": schema,
			"_meta": ui("ui://docs/blob.html", "model", "app")},
		map[string]any{"name": "bad_args", "description": "Fails", "inputSchema": schema,
			"_meta": ui("ui://docs/screen.html", "model", "app")},
		map[string]any{"name": "plain_tool", "description": "Plain", "inputSchema": schema},
	}}
}

func mcpAppFakeCall(name string) map[string]any {
	switch name {
	case "show_screen":
		return map[string]any{
			"content":           []any{map[string]any{"type": "text", "text": "shown"}},
			"structuredContent": map[string]any{"screen": "home"},
		}
	case "screen_frame":
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "frame"}}}
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": "ran " + name}}}
}

// mcpAppFakeResource serves the two views. The screen view is text with a CSP
// and prefersBorder; the blob view is base64 and has no CSP.
func mcpAppFakeResource(uri string) (map[string]any, bool) {
	switch uri {
	case "ui://docs/screen.html":
		return map[string]any{"contents": []any{map[string]any{
			"uri":      uri,
			"mimeType": "text/html;profile=mcp-app",
			"text":     "<html>screen</html>",
			"_meta": map[string]any{"ui": map[string]any{
				"csp":           map[string]any{"connectDomains": []string{"https://api.example.test"}},
				"prefersBorder": true,
			}},
		}}}, true
	case "ui://docs/blob.html":
		return map[string]any{"contents": []any{map[string]any{
			"uri":      uri,
			"mimeType": "text/html",
			"blob":     base64.StdEncoding.EncodeToString([]byte("<p>blob</p>")),
		}}}, true
	}
	return nil, false
}

func mcpAppFakeWrite(out io.Writer, id json.RawMessage, result any) {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if err != nil {
		os.Exit(4)
	}
	_, _ = io.WriteString(out, string(data)+"\n")
}

func mcpAppFakeWriteError(out io.Writer, id json.RawMessage, code int, message string) {
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message}})
	if err != nil {
		os.Exit(4)
	}
	_, _ = io.WriteString(out, string(data)+"\n")
}

// newMCPAppHandler returns a handler with one live session, id, whose agent
// holds the tools of the fake MCP server. The fake process exits with the test.
func newMCPAppHandler(t *testing.T, id string) *Handler {
	t.Helper()
	h := testConfigHandler(t)
	client, err := mcp.NewLocalClient(mcpAppTestServerName, config.MCPConfig{
		Type:    "local",
		Command: []string{os.Args[0], "-test.run=^TestHelperMCPAppServer$"},
		Environment: map[string]string{
			"OCODE_MCP_APP_TEST_SERVER": "1",
		},
		Enabled: true,
		Timeout: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := client.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	tools := make([]tool.Tool, 0, len(listed))
	for _, mt := range listed {
		tools = append(tools, mt)
	}
	ag := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(ag.Shutdown)
	ag.AddMCPTools(tools)

	h.sessions.Register(id, t.TempDir())
	h.mu.Lock()
	h.agents[agentKey(id)] = &agentSession{agent: ag, model: "fake-model"}
	h.mu.Unlock()
	return h
}

func getMCPAppResource(h *Handler, sessionID, server, uri string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	target := "/api/sessions/" + sessionID + "/mcp-app/resource?server=" + server + "&uri=" + uri
	r := httptest.NewRequest(http.MethodGet, target, nil)
	h.HandleMCPAppResource(w, r, sessionID)
	return w
}

func postMCPAppToolCall(h *Handler, sessionID, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/mcp-app/tools/call", strings.NewReader(body))
	h.HandleMCPAppToolCall(w, r, sessionID)
	return w
}

func TestMCPAppResourceReturnsHTMLAndPolicy(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_res")
	w := getMCPAppResource(h, "ses_mcp_app_res", mcpAppTestServerName, "ui://docs/screen.html")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var got struct {
		HTML string `json:"html"`
		CSP  struct {
			ConnectDomains  []string `json:"connectDomains"`
			ResourceDomains []string `json:"resourceDomains"`
		} `json:"csp"`
		PrefersBorder bool `json:"prefers_border"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.HTML != "<html>screen</html>" {
		t.Fatalf("html = %q", got.HTML)
	}
	if len(got.CSP.ConnectDomains) != 1 || got.CSP.ConnectDomains[0] != "https://api.example.test" {
		t.Fatalf("csp.connectDomains = %v", got.CSP.ConnectDomains)
	}
	if got.CSP.ResourceDomains == nil {
		t.Fatal("csp.resourceDomains is null, want an empty array")
	}
	if !got.PrefersBorder {
		t.Fatal("prefers_border = false, want true")
	}
}

func TestMCPAppResourceDecodesBlobContent(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_blob")
	w := getMCPAppResource(h, "ses_mcp_app_blob", mcpAppTestServerName, "ui://docs/blob.html")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var blob struct {
		HTML string `json:"html"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &blob); err != nil {
		t.Fatal(err)
	}
	if blob.HTML != "<p>blob</p>" {
		t.Fatalf("blob html = %q, want the base64 content decoded", blob.HTML)
	}
	if strings.Contains(w.Body.String(), `"csp"`) {
		t.Fatalf("csp present for a resource that declared none: %s", w.Body.String())
	}
}

func TestMCPAppResourceRejectsUndeclaredURI(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_403")
	for _, uri := range []string{"ui://docs/other.html", "https://docs.example.test/screen.html", ""} {
		if w := getMCPAppResource(h, "ses_mcp_app_403", mcpAppTestServerName, uri); w.Code != http.StatusForbidden {
			t.Fatalf("uri %q: status = %d, want 403 (body=%s)", uri, w.Code, w.Body.String())
		}
	}
}

func TestMCPAppResourceUnknownServerIs404(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_404")
	if w := getMCPAppResource(h, "ses_mcp_app_404", "nope", "ui://docs/screen.html"); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
}

func TestMCPAppUnknownSessionIs409(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_live")
	w := getMCPAppResource(h, "ses_not_loaded", mcpAppTestServerName, "ui://docs/screen.html")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "session is not loaded") {
		t.Fatalf("resource status = %d body=%s, want 409 session is not loaded", w.Code, w.Body.String())
	}
	w = postMCPAppToolCall(h, "ses_not_loaded", `{"server":"docs","name":"show_screen"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("tools/call status = %d, want 409", w.Code)
	}
}

func TestMCPAppToolCallReturnsRawResult(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_call")
	w := postMCPAppToolCall(h, "ses_mcp_app_call", `{"server":"docs","name":"show_screen","arguments":{"x":1}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Result struct {
			StructuredContent struct {
				Screen string `json:"screen"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Result.StructuredContent.Screen != "home" {
		t.Fatalf("result = %s, want the raw MCP result", w.Body.String())
	}
}

func TestMCPAppToolCallAllowsAppOnlyTool(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_frame")
	w := postMCPAppToolCall(h, "ses_mcp_app_frame", `{"server":"docs","name":"screen_frame"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("app-only tool status = %d, body=%s", w.Code, w.Body.String())
	}
}

func TestMCPAppToolCallRefusesModelOnlyTool(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_model")
	w := postMCPAppToolCall(h, "ses_mcp_app_model", `{"server":"docs","name":"model_only"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", w.Code, w.Body.String())
	}
	var denied struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &denied); err != nil {
		t.Fatal(err)
	}
	if want := `tool "model_only" is not available to views`; denied.Error != want {
		t.Fatalf("error = %q, want %q", denied.Error, want)
	}
}

func TestMCPAppToolCallUpstreamErrorIs502WithCode(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_rpc")
	w := postMCPAppToolCall(h, "ses_mcp_app_rpc", `{"server":"docs","name":"bad_args","arguments":{}}`)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body=%s)", w.Code, w.Body.String())
	}
	var got struct {
		Error string `json:"error"`
		Code  *int   `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Code == nil || *got.Code != -32602 {
		t.Fatalf("code = %v, want -32602 (body=%s)", got.Code, w.Body.String())
	}
	if got.Error == "" {
		t.Fatal("error message is empty")
	}
}

func TestMCPAppToolCallRejectsNonObjectArguments(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_args")
	for _, args := range []string{`[1,2]`, `"text"`, `42`} {
		body := `{"server":"docs","name":"show_screen","arguments":` + args + `}`
		if w := postMCPAppToolCall(h, "ses_mcp_app_args", body); w.Code != http.StatusBadRequest {
			t.Fatalf("arguments %s: status = %d, want 400 (body=%s)", args, w.Code, w.Body.String())
		}
	}
}

func TestMCPAppToolCallRejectsOversizedBody(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_big")
	huge := `{"server":"docs","name":"show_screen","arguments":{"pad":"` + strings.Repeat("x", mcpAppToolCallBodyMaxBytes) + `"}}`
	if w := postMCPAppToolCall(h, "ses_mcp_app_big", huge); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", w.Code)
	}
}

// TestToolResultEventCarriesMCPApp pins the field the web chat reads to render a
// widget. Live replay buffers the marshalled event, so the JSON key is the
// contract.
func TestToolResultEventCarriesMCPApp(t *testing.T) {
	withApp, err := json.Marshal(ToolResultEvent{Tool: "tool", CallID: "tc-1", Output: "shown",
		MCPApp: &agent.MCPAppRef{Server: "docs", Tool: "show_screen", ResourceURI: "ui://docs/screen.html"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(withApp), `"mcp_app":{"server":"docs","tool":"show_screen","resource_uri":"ui://docs/screen.html"}`) {
		t.Fatalf("tool_result JSON = %s, want the mcp_app object", withApp)
	}
	plain, err := json.Marshal(ToolResultEvent{Tool: "tool", CallID: "tc-2", Output: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "mcp_app") {
		t.Fatalf("tool_result without an app serialized mcp_app: %s", plain)
	}
}

func TestMCPAppToolCallRefusesUnlistedAndUnvisibleTools(t *testing.T) {
	h := newMCPAppHandler(t, "ses_mcp_app_strict")
	for _, name := range []string{"no_such_tool", "plain_tool"} {
		w := postMCPAppToolCall(h, "ses_mcp_app_strict", `{"server":"docs","name":"`+name+`"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("tool %s: status = %d, want 403 (body=%s)", name, w.Code, w.Body.String())
		}
	}
}

func TestMCPAppLockedSessionRefusesViews(t *testing.T) {
	const id = "ses_mcp_app_locked"
	h := newMCPAppHandler(t, id)
	as := h.lookupAgentSession(id)
	if as == nil || as.agent == nil {
		t.Fatal("test session not registered")
	}
	as.agent.Permissions().SetMode(agent.PermissionModeLocked)

	const want = "views are disabled while this session is locked"
	w := getMCPAppResource(h, id, mcpAppTestServerName, "ui://docs/screen.html")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("resource on a locked session: status=%d body=%s", w.Code, w.Body.String())
	}
	w = postMCPAppToolCall(h, id, `{"server":"docs","name":"show_screen"}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("tools/call on a locked session: status=%d body=%s", w.Code, w.Body.String())
	}

	as.agent.Permissions().SetMode(agent.PermissionModeNormal)
	if w := postMCPAppToolCall(h, id, `{"server":"docs","name":"show_screen"}`); w.Code != http.StatusOK {
		t.Fatalf("tools/call after unlocking: status=%d body=%s", w.Code, w.Body.String())
	}
}
