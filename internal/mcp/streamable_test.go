package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// streamableTestServer emulates an MCP Streamable HTTP server: initialize is
// answered with an SSE frame and a session id, tools/list with an SSE frame,
// and tools/call with a plain JSON body (both response shapes are legal).
type streamableTestServer struct {
	mu       sync.Mutex
	order    []string
	sessions []string
	versions []string
}

func (s *streamableTestServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if accept := r.Header.Get("Accept"); !strings.Contains(accept, "application/json") || !strings.Contains(accept, "text/event-stream") {
			t.Errorf("Accept = %q, want JSON and SSE", accept)
		}
		raw, _ := io.ReadAll(r.Body)
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatalf("request is not JSON: %s", raw)
		}

		s.mu.Lock()
		s.order = append(s.order, msg.Method)
		s.sessions = append(s.sessions, r.Header.Get("Mcp-Session-Id"))
		s.versions = append(s.versions, r.Header.Get("MCP-Protocol-Version"))
		s.mu.Unlock()

		id := strings.TrimSpace(string(msg.ID))
		switch msg.Method {
		case "initialize":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Mcp-Session-Id", "session-123")
			_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":"+id+",\"result\":{\"protocolVersion\":\"2025-06-18\",\"serverInfo\":{\"name\":\"fake\",\"version\":\"1\"}}}\n\n")
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":"+id+",\"result\":{\"tools\":[{\"name\":\"speak\",\"description\":\"Generate speech\",\"inputSchema\":{\"type\":\"object\"}}]}}\n\n")
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+id+`,"result":{"content":[{"type":"text","text":"ok"}]}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func newStreamableClient(t *testing.T, url string) *MCPClient {
	t.Helper()
	client, err := NewRemoteClient("elevenlabs", config.MCPConfig{
		Type:    "http",
		URL:     url,
		Enabled: true,
		Timeout: 5000,
	})
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	return client
}

func TestStreamableHandshakeAndRequests(t *testing.T) {
	fake := &streamableTestServer{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	client := newStreamableClient(t, srv.URL)
	tools, err := client.ListTools()
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].name != "elevenlabs_speak" {
		t.Fatalf("tools = %#v", tools)
	}

	raw, err := client.CallToolRaw("speak", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CallToolRaw: %v", err)
	}
	if !strings.Contains(string(raw), `"ok"`) {
		t.Errorf("call result = %s", raw)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	want := []string{"initialize", "notifications/initialized", "tools/list", "tools/call"}
	if strings.Join(fake.order, ",") != strings.Join(want, ",") {
		t.Fatalf("method order = %v, want %v", fake.order, want)
	}
	// The initialize request cannot carry a session; every later request must.
	if fake.sessions[0] != "" {
		t.Errorf("initialize session = %q, want empty", fake.sessions[0])
	}
	for i := 1; i < len(fake.sessions); i++ {
		if fake.sessions[i] != "session-123" {
			t.Errorf("%s session = %q, want session-123", fake.order[i], fake.sessions[i])
		}
	}
	for i := 1; i < len(fake.versions); i++ {
		if fake.versions[i] != "2025-06-18" {
			t.Errorf("%s protocol version = %q", fake.order[i], fake.versions[i])
		}
	}
}

func TestStreamableHandshakeRunsOnce(t *testing.T) {
	fake := &streamableTestServer{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	client := newStreamableClient(t, srv.URL)
	for i := 0; i < 3; i++ {
		if _, err := client.ListTools(); err != nil {
			t.Fatalf("ListTools #%d: %v", i, err)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	initializes := 0
	for _, method := range fake.order {
		if method == "initialize" {
			initializes++
		}
	}
	if initializes != 1 {
		t.Fatalf("initialize ran %d times, want 1 (order %v)", initializes, fake.order)
	}
}

func TestDecodeSSEJSONRPC(t *testing.T) {
	// A notification and an unrelated id come first; the matching response is
	// the third frame.
	body := strings.NewReader(
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"unrelated\":true}}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"ok\":true}}\n\n",
	)
	got, err := decodeSSEJSONRPC("srv", body, 2)
	if err != nil {
		t.Fatalf("decodeSSEJSONRPC: %v", err)
	}
	if string(got) != `{"ok":true}` {
		t.Errorf("result = %s", got)
	}
}

func TestDecodeSSEJSONRPCError(t *testing.T) {
	body := strings.NewReader("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32602,\"message\":\"bad\"}}\n\n")
	_, err := decodeSSEJSONRPC("srv", body, 1)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32602 {
		t.Fatalf("error = %v, want RPCError -32602", err)
	}
}

func TestDecodeSSEJSONRPCNoMatchingFrame(t *testing.T) {
	body := strings.NewReader("data: {\"jsonrpc\":\"2.0\",\"id\":9,\"result\":{}}\n\n")
	if _, err := decodeSSEJSONRPC("srv", body, 1); err == nil ||
		!strings.Contains(err.Error(), "no matching response") {
		t.Fatalf("error = %v, want no matching response", err)
	}
}
