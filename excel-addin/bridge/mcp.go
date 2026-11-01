package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"
)

//go:embed tools.json
var toolsJSON []byte

type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Risk        string          `json:"risk"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type contract struct {
	Version int       `json:"version"`
	Tools   []toolDef `json:"tools"`
}

func loadContract() (contract, error) {
	var c contract
	if err := json.Unmarshal(toolsJSON, &c); err != nil {
		return c, fmt.Errorf("tools.json: %w", err)
	}
	return c, nil
}

// MCP implements the subset of MCP that ocode's remote client speaks: one POST endpoint taking a
// JSON-RPC request and answering with a JSON-RPC response (internal/mcp/client.go requestRemote).
// Tool names are returned WITHOUT a server prefix; ocode prefixes them with the configured server
// name, so a server named "excel" exposes "excel_read_range".
type MCP struct {
	hub         *Hub
	tools       map[string]toolDef
	local       map[string]localTool
	listed      []toolDef
	CallTimeout time.Duration
}

// NewMCP lists the contract tools (run in a pane) and the local tools (answered by the bridge). They are sorted
// together by name, so tools/list stays sorted. A name defined twice is an error.
func NewMCP(h *Hub, c contract, local []localTool) (*MCP, error) {
	m := &MCP{hub: h, tools: map[string]toolDef{}, local: map[string]localTool{}, CallTimeout: 90 * time.Second}
	add := func(t toolDef) error {
		if _, dup := m.tools[t.Name]; dup {
			return fmt.Errorf("tool %q is defined twice", t.Name)
		}
		m.tools[t.Name] = t
		m.listed = append(m.listed, t)
		return nil
	}
	for _, t := range c.Tools {
		if err := add(t); err != nil {
			return nil, err
		}
	}
	for _, l := range local {
		if err := add(l.def); err != nil {
			return nil, err
		}
		m.local[l.def.Name] = l
	}
	sort.Slice(m.listed, func(i, j int) bool { return m.listed[i].Name < m.listed[j].Name })
	return m, nil
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

func textResult(s string, isErr bool) toolResult {
	return toolResult{Content: []toolContent{{Type: "text", Text: s}}, IsError: isErr}
}

func (m *MCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST a JSON-RPC request", http.StatusMethodNotAllowed)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}})
		return
	}
	// A notification has no id and gets no body.
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "ocode-excel-bridge", "version": "1.0.0"},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		out := make([]map[string]any, 0, len(m.listed))
		for _, t := range m.listed {
			out = append(out, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema})
		}
		resp.Result = map[string]any{"tools": out}
	case "tools/call":
		resp.Result, resp.Error = m.call(r.Context(), req.Params)
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	writeRPC(w, resp)
}

func (m *MCP) call(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		return nil, &rpcError{Code: -32602, Message: "tools/call needs a name"}
	}
	if _, ok := m.tools[p.Name]; !ok {
		return nil, &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
	}
	args := p.Arguments
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage("{}")
	}
	if l, ok := m.local[p.Name]; ok {
		text, err := l.run(ctx, args)
		if err != nil {
			log.Printf("excel bridge: local tool %s failed: %v", p.Name, err)
			var me *modelError
			if errors.As(err, &me) {
				return textResult(me.msg, true), nil
			}
			return textResult(p.Name+" failed; the bridge logged the cause.", true), nil
		}
		return textResult(text, false), nil
	}
	var probe struct {
		Workbook string `json:"workbook"`
	}
	if err := json.Unmarshal(args, &probe); err != nil {
		return textResult("arguments must be a JSON object: "+err.Error(), true), nil
	}
	res, err := m.hub.Call(ctx, probe.Workbook, p.Name, args, m.CallTimeout)
	if err != nil {
		// A transport problem is reported to the model as a tool error, not a protocol error, so it can react.
		return textResult("Excel bridge: "+err.Error(), true), nil
	}
	if !res.OK {
		return textResult(res.Error, true), nil
	}
	return textResult(res.Content, false), nil
}

func writeRPC(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
