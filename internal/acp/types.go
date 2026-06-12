package acp

import "encoding/json"

// JSON-RPC 2.0 framing types.

// inFrame is the common parse target for all incoming messages.
// A request has Method + optional ID. A notification has Method, no ID.
// A response (to our client-bound requests) has Result/Error + ID, no Method.
type inFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // string, number, or null
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// outResponse is a JSON-RPC response (or error) to a client request.
type outResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// outNotify is a JSON-RPC notification (no id, no response expected).
type outNotify struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// outRequest is a JSON-RPC request issued by the agent to the client.
// We use integer IDs for our own outbound requests.
type outRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

// ACP protocol types (hand-rolled from the ACP schema).

type initializeParams struct {
	ProtocolVersion int `json:"protocolVersion"`
}

type initializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentInfo         agentInfo         `json:"agentInfo"`
	AgentCapabilities agentCapabilities `json:"agentCapabilities"`
	AuthMethods       []interface{}     `json:"authMethods"`
}

type agentInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type agentCapabilities struct {
	LoadSession        bool               `json:"loadSession"`
	PromptCapabilities promptCapabilities `json:"promptCapabilities"`
}

type promptCapabilities struct {
	EmbeddedContext bool `json:"embeddedContext"`
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
}

type sessionNewParams struct {
	CWD string `json:"cwd,omitempty"`
	// mcpServers accepted but ignored in v1
}

type sessionNewResult struct {
	SessionID string `json:"sessionId"`
}

// contentBlock is a single element inside a prompt's content array.
type contentBlock struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	URI      string            `json:"uri,omitempty"`
	MimeType string            `json:"mimeType,omitempty"`
	Resource *embeddedResource `json:"resource,omitempty"`
}

// embeddedResource carries inline file content (Zed @-mention).
type embeddedResource struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

type sessionPromptParams struct {
	SessionID string         `json:"sessionId"`
	Content   []contentBlock `json:"content"`
}

type sessionPromptResult struct {
	StopReason string `json:"stopReason"`
}

type sessionCancelParams struct {
	SessionID string `json:"sessionId"`
}

// sessionUpdateParams wraps a session/update notification.
type sessionUpdateParams struct {
	SessionID     string        `json:"sessionId"`
	SessionUpdate sessionUpdate `json:"sessionUpdate"`
}

// sessionUpdate is the polymorphic payload for session/update notifications.
// The Kind field identifies the variant; only the relevant fields are populated.
type sessionUpdate struct {
	Kind       string         `json:"kind"`
	Content    []contentBlock `json:"content,omitempty"`
	ToolCallID string         `json:"toolCallId,omitempty"`
	Title      string         `json:"title,omitempty"`
	ToolKind   string         `json:"toolKind,omitempty"`
	Status     string         `json:"status,omitempty"`
}

// permRequestParams is sent to the client for session/request_permission.
type permRequestParams struct {
	SessionID string   `json:"sessionId"`
	ToolName  string   `json:"toolName"`
	Rule      string   `json:"rule,omitempty"`
	Options   []string `json:"options"`
}

// permResponseResult is what the client returns for session/request_permission.
type permResponseResult struct {
	Selected string `json:"selected"` // "allow-once" | "allow-always" | "reject-once" | "cancelled"
}
