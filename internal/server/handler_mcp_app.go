package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/mcp"
)

// mcpAppToolCallBodyMaxBytes caps a widget's tools/call body before it is
// decoded. A widget sends a tool name and a small argument object.
const mcpAppToolCallBodyMaxBytes = 1 << 20

// MCP Apps widgets (https://modelcontextprotocol.io/extensions/apps) are served
// by the web chat in a sandboxed iframe. These two endpoints give the widget its
// HTML and let it call tools on the same MCP server the agent uses. Both are
// session-scoped and read the live agent only: they never build a session.
//
// Locking: each handler takes Handler.mu only for the brief map lookup inside
// lookupAgentSession, and never as.mu. The agent pointer is written once, when
// buildAgentSession creates the agentSession, before registerAgentSession
// publishes it under Handler.mu; a turn never replaces it. A widget polls its
// frame tool about once a second during a turn, so it must not wait for the turn.

// mcpAppCSP is the widget's declared content security policy domains.
type mcpAppCSP struct {
	ConnectDomains  []string `json:"connectDomains"`
	ResourceDomains []string `json:"resourceDomains"`
}

// mcpAppResourceResponse is the body of GET .../mcp-app/resource.
type mcpAppResourceResponse struct {
	HTML          string     `json:"html"`
	CSP           *mcpAppCSP `json:"csp,omitempty"`
	PrefersBorder bool       `json:"prefers_border"`
}

// mcpAppResourceContent is one entry of a resources/read result.
type mcpAppResourceContent struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType"`
	Text     string `json:"text"`
	Blob     string `json:"blob"`
	Meta     *struct {
		UI *struct {
			CSP           *mcpAppCSP `json:"csp"`
			PrefersBorder bool       `json:"prefersBorder"`
		} `json:"ui"`
	} `json:"_meta"`
}

// mcpAppClient resolves the MCP client behind the server of a live session. It
// returns a non-zero HTTP status and a message when there is no client, or when
// the session is locked: a locked session shows no views and calls no tools for
// them.
func (h *Handler) mcpAppClient(sessionID, server string) (*mcp.MCPClient, int, string) {
	as := h.lookupAgentSession(sessionID)
	if as == nil || as.agent == nil {
		return nil, http.StatusConflict, "session is not loaded"
	}
	if as.agent.PermissionMode() == agent.PermissionModeLocked {
		return nil, http.StatusForbidden, "views are disabled while this session is locked"
	}
	client := as.agent.MCPClientForServer(server)
	if client == nil {
		return nil, http.StatusNotFound, fmt.Sprintf("MCP server %q is not connected to this session", server)
	}
	return client, 0, ""
}

// HandleMCPAppResource returns the HTML of an MCP Apps view.
//
//	GET /api/sessions/{id}/mcp-app/resource?server=<name>&uri=<ui://…>
//	-> {"html": "...", "csp": {"connectDomains": [...], "resourceDomains": [...]}, "prefers_border": bool}
//
// The HTML is returned inside JSON only, never served as a document from this
// origin. The uri must be one a listed tool of this server declares, so the
// endpoint cannot be used as a general resource reader.
func (h *Handler) HandleMCPAppResource(w http.ResponseWriter, r *http.Request, id string) {
	query := r.URL.Query()
	server, uri := query.Get("server"), query.Get("uri")
	client, status, msg := h.mcpAppClient(id, server)
	if client == nil {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	if !strings.HasPrefix(uri, "ui://") || !client.HasUIResource(uri) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "no MCP Apps view is declared at this URI"})
		return
	}
	raw, err := client.ReadResource(uri)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	resp, err := mcpAppResourceFromResult(raw)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// mcpAppResourceFromResult picks the first HTML content of a resources/read
// result and returns its HTML with the widget's declared policy.
func mcpAppResourceFromResult(raw json.RawMessage) (mcpAppResourceResponse, error) {
	var result struct {
		Contents []mcpAppResourceContent `json:"contents"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return mcpAppResourceResponse{}, fmt.Errorf("MCP resource is not a valid result: %v", err)
	}
	for _, c := range result.Contents {
		if !strings.HasPrefix(c.MIMEType, "text/html") {
			continue
		}
		html := c.Text
		if html == "" && c.Blob != "" {
			decoded, err := base64.StdEncoding.DecodeString(c.Blob)
			if err != nil {
				return mcpAppResourceResponse{}, fmt.Errorf("MCP resource blob is not base64: %v", err)
			}
			html = string(decoded)
		}
		if html == "" {
			return mcpAppResourceResponse{}, fmt.Errorf("MCP resource %s has no HTML content", c.URI)
		}
		resp := mcpAppResourceResponse{HTML: html}
		if c.Meta != nil && c.Meta.UI != nil {
			resp.PrefersBorder = c.Meta.UI.PrefersBorder
			if csp := c.Meta.UI.CSP; csp != nil {
				if csp.ConnectDomains == nil {
					csp.ConnectDomains = []string{}
				}
				if csp.ResourceDomains == nil {
					csp.ResourceDomains = []string{}
				}
				resp.CSP = csp
			}
		}
		return resp, nil
	}
	return mcpAppResourceResponse{}, errors.New("MCP resource has no text/html content")
}

// HandleMCPAppToolCall calls a tool on behalf of an MCP Apps view.
//
//	POST /api/sessions/{id}/mcp-app/tools/call
//	{"server": "...", "name": "<short tool name>", "arguments": {...}} -> {"result": <raw MCP result>}
//
// Only a tool the server lists with an explicit "app" visibility is allowed
// (see MCPClient.AppToolAllowed): a tool with no visibility, a model-only tool
// and a name the server did not list are refused with 403. A locked session is
// refused with 403 as well. An upstream JSON-RPC error is a 502 that carries
// the server's error code when it has one.
func (h *Handler) HandleMCPAppToolCall(w http.ResponseWriter, r *http.Request, id string) {
	r.Body = http.MaxBytesReader(w, r.Body, mcpAppToolCallBodyMaxBytes)
	var body struct {
		Server    string          `json:"server"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := readBodyJSON(r, &body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	args := bytes.TrimSpace(body.Arguments)
	switch {
	case len(args) == 0 || string(args) == "null":
		args = nil
	case args[0] != '{':
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "arguments must be a JSON object"})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	client, status, msg := h.mcpAppClient(id, body.Server)
	if client == nil {
		writeJSON(w, status, map[string]string{"error": msg})
		return
	}
	if !client.HasAnyUIResource() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "this MCP server declares no views"})
		return
	}
	if !client.AppToolAllowed(body.Name) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": fmt.Sprintf("tool %q is not available to views", body.Name)})
		return
	}
	raw, err := client.CallToolRaw(body.Name, args)
	if err != nil {
		resp := map[string]any{"error": err.Error()}
		var rpcErr *mcp.RPCError
		if errors.As(err, &rpcErr) {
			resp["code"] = rpcErr.Code
		}
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, map[string]json.RawMessage{"result": raw})
}

// handleMCPAppResource and handleMCPAppToolCall adapt the MCP Apps handlers to
// the mux, reading the session id from the route like the other session routes.
func (s *Server) handleMCPAppResource(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleMCPAppResource(w, r, r.PathValue("id"))
}

func (s *Server) handleMCPAppToolCall(w http.ResponseWriter, r *http.Request) {
	s.handler.HandleMCPAppToolCall(w, r, r.PathValue("id"))
}
