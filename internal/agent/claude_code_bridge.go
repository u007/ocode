package agent

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/u007/ocode/internal/crashguard"
)

// claudeCodeBridgeToolTimeoutMS is the per-call timeout written into the MCP
// config. The CLI aborts an HTTP tool call after 60s unless told otherwise, and
// an approval waits on a human, so this must stay above the human-ask timeout
// (10 minutes) with room to spare.
const claudeCodeBridgeToolTimeoutMS = 1200000

// claudeCodeBridgeMaxBody caps one JSON-RPC request body. A Write call carries
// the whole file content, so the cap is generous but still bounded.
const claudeCodeBridgeMaxBody = 32 << 20

// claudeCodeApprovalTool is the MCP tool name the CLI calls for a permission
// request: the server is "ocode" and the tool is "approve".
const claudeCodeApprovalTool = "mcp__ocode__approve"

// claudeCodeBridgeDeniedMessage is the deny reason sent when the approve
// callback returned a denial without one. It is logged, never silent.
const claudeCodeBridgeDeniedMessage = "denied by ocode"

// ClaudeCodeApproveFunc decides one permission request from the Claude Code
// CLI. allowed=false carries message as the deny reason. updatedInput, when
// non-nil, replaces the tool's input on allow.
type ClaudeCodeApproveFunc func(ctx context.Context, toolName string, input json.RawMessage, toolUseID string) (allowed bool, updatedInput json.RawMessage, message string)

const (
	jsonRPCParseError     = -32700
	jsonRPCInvalidRequest = -32600
	jsonRPCMethodNotFound = -32601
	jsonRPCInvalidParams  = -32602
)

// claudeCodeBridge is a loopback MCP server that answers the CLI's permission
// prompts. It serves exactly one chatClaudeCode run and is closed with it.
type claudeCodeBridge struct {
	URL   string
	Token string

	srv       *http.Server
	sessionID string
	approve   ClaudeCodeApproveFunc
}

// startClaudeCodeBridge binds a loopback listener on an ephemeral port and
// serves the approval protocol until Close.
func startClaudeCodeBridge(approve ClaudeCodeApproveFunc) (*claudeCodeBridge, error) {
	if approve == nil {
		return nil, fmt.Errorf("approval bridge requires an approve function")
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, fmt.Errorf("generate bridge token: %w", err)
	}
	sessionID, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("generate bridge session id: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for approval bridge: %w", err)
	}
	b := &claudeCodeBridge{
		URL:       "http://" + ln.Addr().String() + "/mcp",
		Token:     token,
		sessionID: sessionID,
		approve:   approve,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", b.handleMCP)
	b.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	crashguard.Go(func() {
		if err := b.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			emitDebug("CLAUDE-CODE", fmt.Sprintf("approval bridge serve: %v", err))
		}
	})
	return b, nil
}

// Close shuts the server down. It cancels in-flight request contexts, which
// ends a parked question or permission ask on the host, so a stopped run cannot
// leave a dialog behind.
func (b *claudeCodeBridge) Close() error {
	return b.srv.Close()
}

// jsonRPCMessage is one inbound JSON-RPC 2.0 message. A message without an id
// is a notification and gets no JSON-RPC response.
type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (b *claudeCodeBridge) handleMCP(w http.ResponseWriter, r *http.Request) {
	if !b.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, claudeCodeBridgeMaxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		emitDebug("CLAUDE-CODE", fmt.Sprintf("approval bridge read body: %v", err))
		http.Error(w, "read body failed", http.StatusBadRequest)
		return
	}
	var msg jsonRPCMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		writeBridgeJSON(w, jsonRPCResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &jsonRPCError{Code: jsonRPCParseError, Message: "parse error"}})
		return
	}
	if msg.JSONRPC != "2.0" {
		writeBridgeJSON(w, jsonRPCResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
			Error: &jsonRPCError{Code: jsonRPCInvalidRequest, Message: "jsonrpc must be \"2.0\""}})
		return
	}
	if len(msg.ID) == 0 {
		// Notification (e.g. notifications/initialized): acknowledge, no body.
		w.WriteHeader(http.StatusAccepted)
		return
	}

	result, rpcErr := b.dispatch(r.Context(), msg)
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: msg.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	if msg.Method == "initialize" && rpcErr == nil {
		w.Header().Set("Mcp-Session-Id", b.sessionID)
	}
	writeBridgeJSON(w, resp)
}

func (b *claudeCodeBridge) authorized(r *http.Request) bool {
	want := []byte("Bearer " + b.Token)
	got := []byte(r.Header.Get("Authorization"))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func (b *claudeCodeBridge) dispatch(ctx context.Context, msg jsonRPCMessage) (any, *jsonRPCError) {
	switch msg.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil || params.ProtocolVersion == "" {
			return nil, &jsonRPCError{Code: jsonRPCInvalidParams, Message: "initialize requires protocolVersion"}
		}
		return map[string]any{
			"protocolVersion": params.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "ocode", "version": "1"},
		}, nil
	case "ping":
		// MCP liveness check: an empty result is the whole reply.
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []map[string]any{{
			"name":        "approve",
			"description": "Ask ocode whether a Claude Code tool call may run.",
			"inputSchema": map[string]any{"type": "object", "additionalProperties": true},
		}}}, nil
	case "tools/call":
		return b.callTool(ctx, msg.Params)
	default:
		return nil, &jsonRPCError{Code: jsonRPCMethodNotFound, Message: "method not found: " + msg.Method}
	}
}

func (b *claudeCodeBridge) callTool(ctx context.Context, raw json.RawMessage) (any, *jsonRPCError) {
	var params struct {
		Name      string `json:"name"`
		Arguments struct {
			ToolName  string          `json:"tool_name"`
			Input     json.RawMessage `json:"input"`
			ToolUseID string          `json:"tool_use_id"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &jsonRPCError{Code: jsonRPCInvalidParams, Message: "tools/call params: " + err.Error()}
	}
	if params.Name != "approve" {
		return nil, &jsonRPCError{Code: jsonRPCInvalidParams, Message: "unknown tool: " + params.Name}
	}
	args := params.Arguments
	if strings.TrimSpace(args.ToolName) == "" {
		return nil, &jsonRPCError{Code: jsonRPCInvalidParams, Message: "approve requires tool_name"}
	}
	if len(args.Input) == 0 {
		return nil, &jsonRPCError{Code: jsonRPCInvalidParams, Message: "approve requires input"}
	}

	allowed, updatedInput, message := b.approve(ctx, args.ToolName, args.Input, args.ToolUseID)
	if ctx.Err() != nil {
		// The CLI went away while the approval was pending; nothing reads the reply.
		emitDebug("CLAUDE-CODE", fmt.Sprintf("approval for %s cancelled before a decision: %v", args.ToolName, ctx.Err()))
	}
	decision := map[string]any{"behavior": "allow"}
	if allowed {
		if updatedInput != nil {
			decision["updatedInput"] = updatedInput
		}
	} else {
		if message == "" {
			emitDebug("CLAUDE-CODE", fmt.Sprintf("approval for %s denied without a reason", args.ToolName))
			message = claudeCodeBridgeDeniedMessage
		}
		decision = map[string]any{"behavior": "deny", "message": message}
	}
	text, err := json.Marshal(decision)
	if err != nil {
		return nil, &jsonRPCError{Code: jsonRPCInvalidParams, Message: "encode decision: " + err.Error()}
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
	}, nil
}

func writeBridgeJSON(w http.ResponseWriter, resp jsonRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		emitDebug("CLAUDE-CODE", fmt.Sprintf("approval bridge encode response: %v", err))
		http.Error(w, "encode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(data); err != nil {
		emitDebug("CLAUDE-CODE", fmt.Sprintf("approval bridge write response: %v", err))
	}
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// claudeCodeMCPConfig is the --mcp-config document that points the CLI at the
// bridge. The timeout key is required: without it the CLI aborts an HTTP tool
// call after 60 seconds.
type claudeCodeMCPConfig struct {
	MCPServers map[string]claudeCodeMCPServer `json:"mcpServers"`
}

type claudeCodeMCPServer struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Timeout int               `json:"timeout"`
}

// writeClaudeCodeMCPConfig writes the bridge config to a new temp file readable
// only by the owner. The caller removes the file.
func writeClaudeCodeMCPConfig(url, token string) (string, error) {
	data, err := json.Marshal(claudeCodeMCPConfig{MCPServers: map[string]claudeCodeMCPServer{
		"ocode": {
			Type:    "http",
			URL:     url,
			Headers: map[string]string{"Authorization": "Bearer " + token},
			Timeout: claudeCodeBridgeToolTimeoutMS,
		},
	}})
	if err != nil {
		return "", fmt.Errorf("encode mcp config: %w", err)
	}
	f, err := os.CreateTemp("", "ocode-claude-mcp-*.json")
	if err != nil {
		return "", fmt.Errorf("create mcp config: %w", err)
	}
	path := f.Name()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("chmod mcp config: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("write mcp config: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close mcp config: %w", err)
	}
	return path, nil
}
