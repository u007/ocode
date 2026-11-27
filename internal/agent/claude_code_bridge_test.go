package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type approveCall struct {
	tool  string
	input json.RawMessage
	id    string
}

func recordingApprove(calls chan<- approveCall, allowed bool, updated json.RawMessage, msg string) ClaudeCodeApproveFunc {
	return func(_ context.Context, tool string, input json.RawMessage, id string) (bool, json.RawMessage, string) {
		calls <- approveCall{tool: tool, input: input, id: id}
		return allowed, updated, msg
	}
}

func startTestBridge(t *testing.T, approve ClaudeCodeApproveFunc) *claudeCodeBridge {
	t.Helper()
	b, err := startClaudeCodeBridge(approve)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

type bridgeReply struct {
	status int
	header http.Header
	body   []byte
}

// bridgePost sends one request to the bridge. auth is the full Authorization
// header value; an empty string sends no header.
func bridgePost(t *testing.T, ctx context.Context, b *claudeCodeBridge, method, auth, body string) bridgeReply {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.URL, reader)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return bridgeReply{status: resp.StatusCode, header: resp.Header, body: data}
}

func bearer(b *claudeCodeBridge) string {
	return "Bearer " + b.Token
}

type rpcEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeRPC(t *testing.T, data []byte) rpcEnvelope {
	t.Helper()
	var env rpcEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("response is not JSON-RPC: %v\n%s", err, data)
	}
	return env
}

// approveText returns the decision JSON string carried in a tools/call result.
func approveText(t *testing.T, result json.RawMessage) string {
	t.Helper()
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		t.Fatalf("tools/call result: %v\n%s", err, result)
	}
	if len(r.Content) != 1 || r.Content[0].Type != "text" {
		t.Fatalf("tools/call content = %s, want one text block", result)
	}
	return r.Content[0].Text
}

func callApprove(t *testing.T, b *claudeCodeBridge, args string) rpcEnvelope {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"approve","arguments":` + args + `}}`
	reply := bridgePost(t, context.Background(), b, http.MethodPost, bearer(b), body)
	if reply.status != http.StatusOK {
		t.Fatalf("status = %d, body=%s", reply.status, reply.body)
	}
	return decodeRPC(t, reply.body)
}

func TestClaudeCodeBridgeRequiresBearerToken(t *testing.T) {
	b := startTestBridge(t, recordingApprove(make(chan approveCall, 1), true, nil, ""))
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	for _, auth := range []string{"", "Bearer wrong", "Basic " + b.Token, b.Token} {
		reply := bridgePost(t, context.Background(), b, http.MethodPost, auth, init)
		if reply.status != http.StatusUnauthorized {
			t.Fatalf("auth %q: status = %d, want 401", auth, reply.status)
		}
	}
	reply := bridgePost(t, context.Background(), b, http.MethodGet, "", "")
	if reply.status != http.StatusUnauthorized {
		t.Fatalf("GET without token: status = %d, want 401", reply.status)
	}
	reply = bridgePost(t, context.Background(), b, http.MethodPost, bearer(b), init)
	if reply.status != http.StatusOK {
		t.Fatalf("valid token: status = %d, body=%s", reply.status, reply.body)
	}
}

func TestClaudeCodeBridgeInitializeEchoesProtocolVersion(t *testing.T) {
	b := startTestBridge(t, recordingApprove(make(chan approveCall, 1), true, nil, ""))
	body := `{"jsonrpc":"2.0","id":"init-1","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"claude","version":"x"}}}`
	reply := bridgePost(t, context.Background(), b, http.MethodPost, bearer(b), body)
	if reply.status != http.StatusOK {
		t.Fatalf("status = %d, body=%s", reply.status, reply.body)
	}
	if reply.header.Get("Mcp-Session-Id") == "" {
		t.Fatal("initialize must set Mcp-Session-Id")
	}
	env := decodeRPC(t, reply.body)
	var result struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result: %v\n%s", err, env.Result)
	}
	if result.ProtocolVersion != "2024-11-05" {
		t.Fatalf("protocolVersion = %q, want the client's 2024-11-05", result.ProtocolVersion)
	}
	if _, ok := result.Capabilities["tools"]; !ok {
		t.Fatalf("capabilities = %v, want tools", result.Capabilities)
	}
	if result.ServerInfo.Name != "ocode" || result.ServerInfo.Version != "1" {
		t.Fatalf("serverInfo = %+v", result.ServerInfo)
	}

	noVersion := `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`
	env = decodeRPC(t, bridgePost(t, context.Background(), b, http.MethodPost, bearer(b), noVersion).body)
	if env.Error == nil || env.Error.Code != -32602 {
		t.Fatalf("initialize without protocolVersion must be -32602, got %+v", env.Error)
	}
}

func TestClaudeCodeBridgeNotificationIsAccepted(t *testing.T) {
	b := startTestBridge(t, recordingApprove(make(chan approveCall, 1), true, nil, ""))
	body := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	reply := bridgePost(t, context.Background(), b, http.MethodPost, bearer(b), body)
	if reply.status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", reply.status)
	}
	if len(reply.body) != 0 {
		t.Fatalf("notification body = %q, want empty", reply.body)
	}
}

func TestClaudeCodeBridgeGetIsNotAllowed(t *testing.T) {
	b := startTestBridge(t, recordingApprove(make(chan approveCall, 1), true, nil, ""))
	reply := bridgePost(t, context.Background(), b, http.MethodGet, bearer(b), "")
	if reply.status != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", reply.status)
	}
}

func TestClaudeCodeBridgeAnswersPing(t *testing.T) {
	b := startTestBridge(t, recordingApprove(make(chan approveCall, 1), true, nil, ""))
	body := `{"jsonrpc":"2.0","id":4,"method":"ping"}`
	reply := bridgePost(t, context.Background(), b, http.MethodPost, bearer(b), body)
	if reply.status != http.StatusOK {
		t.Fatalf("status = %d, body %s", reply.status, reply.body)
	}
	env := decodeRPC(t, reply.body)
	if env.Error != nil {
		t.Fatalf("ping returned error %+v", env.Error)
	}
	if string(env.Result) != "{}" {
		t.Fatalf("ping result = %s, want {}", env.Result)
	}
}

func TestClaudeCodeBridgeListsApproveTool(t *testing.T) {
	b := startTestBridge(t, recordingApprove(make(chan approveCall, 1), true, nil, ""))
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`
	env := decodeRPC(t, bridgePost(t, context.Background(), b, http.MethodPost, bearer(b), body).body)
	var result struct {
		Tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("tools/list result: %v\n%s", err, env.Result)
	}
	if len(result.Tools) != 1 || result.Tools[0].Name != "approve" {
		t.Fatalf("tools = %+v, want exactly approve", result.Tools)
	}
	if !strings.Contains(string(result.Tools[0].InputSchema), `"additionalProperties":true`) {
		t.Fatalf("inputSchema = %s", result.Tools[0].InputSchema)
	}
}

func TestClaudeCodeBridgeToolCallDecisions(t *testing.T) {
	cases := []struct {
		name    string
		allowed bool
		updated json.RawMessage
		msg     string
		want    string
	}{
		{"allow", true, nil, "", `{"behavior":"allow"}`},
		{"allow with updated input", true, json.RawMessage(`{"command":"ls -la"}`), "", `{"behavior":"allow","updatedInput":{"command":"ls -la"}}`},
		{"deny with reason", false, nil, "not permitted here", `{"behavior":"deny","message":"not permitted here"}`},
		{"deny without reason gets the explicit fallback", false, nil, "", `{"behavior":"deny","message":"denied by ocode"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := make(chan approveCall, 1)
			b := startTestBridge(t, recordingApprove(calls, tc.allowed, tc.updated, tc.msg))
			env := callApprove(t, b, `{"tool_name":"Bash","input":{"command":"ls"},"tool_use_id":"toolu_1"}`)
			if env.Error != nil {
				t.Fatalf("unexpected error %+v", env.Error)
			}
			if got := approveText(t, env.Result); got != tc.want {
				t.Fatalf("decision text = %s, want %s", got, tc.want)
			}
			call := <-calls
			if call.tool != "Bash" || call.id != "toolu_1" || string(call.input) != `{"command":"ls"}` {
				t.Fatalf("approve saw %+v", call)
			}
		})
	}
}

func TestClaudeCodeBridgeRejectsBadRequests(t *testing.T) {
	calls := make(chan approveCall, 1)
	b := startTestBridge(t, recordingApprove(calls, true, nil, ""))
	cases := []struct {
		name string
		body string
		code int
	}{
		{"unknown method", `{"jsonrpc":"2.0","id":1,"method":"server/discover"}`, -32601},
		{"unknown tool", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"other","arguments":{}}}`, -32602},
		{"missing tool_name", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"approve","arguments":{"input":{}}}}`, -32602},
		{"missing input", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"approve","arguments":{"tool_name":"Bash"}}}`, -32602},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := decodeRPC(t, bridgePost(t, context.Background(), b, http.MethodPost, bearer(b), tc.body).body)
			if env.Error == nil || env.Error.Code != tc.code {
				t.Fatalf("error = %+v, want code %d", env.Error, tc.code)
			}
		})
	}
	if len(calls) != 0 {
		t.Fatal("a rejected request must never reach the approve callback")
	}
}

func TestClaudeCodeBridgeRejectsOversizedBody(t *testing.T) {
	b := startTestBridge(t, recordingApprove(make(chan approveCall, 1), true, nil, ""))
	huge := bytes.Repeat([]byte("a"), claudeCodeBridgeMaxBody+1)
	reply := bridgePost(t, context.Background(), b, http.MethodPost, bearer(b), string(huge))
	if reply.status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", reply.status)
	}
}

// TestClaudeCodeBridgeCancelsApprovalOnDisconnect checks that a CLI that goes
// away while a human decision is pending cancels the request context the
// approve callback is waiting on.
func TestClaudeCodeBridgeCancelsApprovalOnDisconnect(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan error, 1)
	approve := func(ctx context.Context, _ string, _ json.RawMessage, _ string) (bool, json.RawMessage, string) {
		close(started)
		<-ctx.Done()
		cancelled <- ctx.Err()
		return false, nil, "cancelled"
	}
	b := startTestBridge(t, approve)

	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	body := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"approve","arguments":{"tool_name":"Bash","input":{"command":"sleep 999"}}}}`
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, b.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", bearer(b))
	done := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("approve was never called")
	}
	cancelReq()

	select {
	case err := <-cancelled:
		if err == nil {
			t.Fatal("approve context was not cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client disconnect did not cancel the pending approval")
	}
	<-done
}

// TestClaudeCodeBridgeCloseCancelsPendingApproval checks the Close contract: an
// approval parked on a human when the bridge closes sees its context cancelled.
func TestClaudeCodeBridgeCloseCancelsPendingApproval(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	approve := func(ctx context.Context, _ string, _ json.RawMessage, _ string) (bool, json.RawMessage, string) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return false, nil, "cancelled"
	}
	b, err := startClaudeCodeBridge(approve)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"approve","arguments":{"tool_name":"Bash","input":{"command":"sleep 999"}}}}`
	req, err := http.NewRequest(http.MethodPost, b.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", bearer(b))
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("approve was never called")
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel the parked approval")
	}
}

func TestClaudeCodeBridgeCloseStopsServing(t *testing.T) {
	b, err := startClaudeCodeBridge(recordingApprove(make(chan approveCall, 1), true, nil, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, b.URL, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", bearer(b))
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("bridge still answers after Close")
	}
}

func TestStartClaudeCodeBridgeRequiresApprove(t *testing.T) {
	if _, err := startClaudeCodeBridge(nil); err == nil {
		t.Fatal("a bridge without an approve callback must not start")
	}
}

func TestWriteClaudeCodeMCPConfig(t *testing.T) {
	path, err := writeClaudeCodeMCPConfig("http://127.0.0.1:9/mcp", "tok")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o, want 0600", mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg claudeCodeMCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	server := cfg.MCPServers["ocode"]
	if server.URL != "http://127.0.0.1:9/mcp" || server.Headers["Authorization"] != "Bearer tok" || server.Timeout != claudeCodeBridgeToolTimeoutMS {
		t.Fatalf("config = %s", data)
	}
}
