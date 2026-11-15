package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Streamable HTTP is the MCP transport where every message is a POST to the
// server URL (config type "http"), and the response is either a JSON body or an
// SSE frame. It needs an initialize handshake and a session id, which the
// older POST-only remote clients here never performed.

// resetStreamable clears the negotiated session so the next request runs a
// fresh initialize handshake (used after a token refresh).
func (c *MCPClient) resetStreamable() {
	c.streamMu.Lock()
	c.sessionID = ""
	c.protocolVersion = ""
	c.streamReady = false
	c.streamMu.Unlock()
}

// ensureStreamable runs the one-time handshake if it has not completed. It
// must be called without c.mu held: the handshake issues its own HTTP requests
// and may refresh the OAuth token.
func (c *MCPClient) ensureStreamable() error {
	c.streamMu.Lock()
	ready := c.streamReady
	c.streamMu.Unlock()
	if ready {
		return nil
	}
	return c.streamableInitialize()
}

// streamableInitialize performs initialize + notifications/initialized and
// records the session id and negotiated protocol version.
func (c *MCPClient) streamableInitialize() error {
	c.initMu.Lock()
	defer c.initMu.Unlock()

	// Another goroutine may have finished the handshake while we waited.
	c.streamMu.Lock()
	if c.streamReady {
		c.streamMu.Unlock()
		return nil
	}
	c.streamID++
	id := c.streamID
	c.streamMu.Unlock()

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "ocode", "version": "1"},
		},
	})
	if err != nil {
		return fmt.Errorf("could not encode MCP initialize request")
	}

	resp, err := c.sendRemoteRequest(payload)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := strings.Join(resp.Header.Values("WWW-Authenticate"), ", ")
		_ = resp.Body.Close()
		if rerr := c.refreshAfterUnauthorized(challenge); rerr != nil {
			return rerr
		}
		resp, err = c.sendRemoteRequest(payload)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized {
			_ = resp.Body.Close()
			return &ReauthorizationRequiredError{
				ServerName: c.name,
				Reason:     "the refreshed credential was rejected",
			}
		}
	}

	result, err := decodeRemoteResponse(c.name, resp, id)
	if err != nil {
		return err
	}
	sessionID := resp.Header.Get("Mcp-Session-Id")
	version := mcpProtocolVersion
	if len(result) > 0 {
		var init struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(result, &init) == nil && init.ProtocolVersion != "" {
			version = init.ProtocolVersion
		}
	}

	c.streamMu.Lock()
	c.sessionID = sessionID
	c.protocolVersion = version
	c.streamMu.Unlock()

	// Complete the handshake. A notification has no id and the server answers
	// 202 with no body.
	notif, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if err != nil {
		return fmt.Errorf("could not encode MCP initialized notification")
	}
	nresp, err := c.sendRemoteRequest(notif)
	if err != nil {
		return err
	}
	defer nresp.Body.Close()
	if nresp.StatusCode < http.StatusOK || nresp.StatusCode >= http.StatusMultipleChoices {
		return &RemoteHTTPError{
			ServerName:             c.name,
			StatusCode:             nresp.StatusCode,
			Status:                 http.StatusText(nresp.StatusCode),
			AuthenticationRequired: nresp.StatusCode == http.StatusUnauthorized,
		}
	}

	c.streamMu.Lock()
	c.streamReady = true
	c.streamMu.Unlock()
	return nil
}

// decodeSSEJSONRPC reads an SSE response body and returns the result of the
// JSON-RPC response that matches wantID (any response when wantID <= 0).
// Notifications and unrelated frames are skipped.
func decodeSSEJSONRPC(serverName string, body io.Reader, wantID int) (json.RawMessage, error) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), maxLocalLineBytes)

	want := ""
	if wantID > 0 {
		want = strconv.Itoa(wantID)
	}
	var data []string

	handle := func() (json.RawMessage, bool, error) {
		if len(data) == 0 {
			return nil, false, nil
		}
		raw := strings.Join(data, "\n")
		data = data[:0]

		var msg struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			// Not a JSON-RPC frame (server keep-alive or vendor event): skip.
			return nil, false, nil
		}
		if msg.Method != "" {
			return nil, false, nil
		}
		id := strings.Trim(strings.TrimSpace(string(msg.ID)), `"`)
		if want != "" && id != want {
			return nil, false, nil
		}
		if msg.Error != nil {
			return nil, true, &RPCError{
				Code:       msg.Error.Code,
				Message:    msg.Error.Message,
				remote:     true,
				remoteName: serverName,
			}
		}
		if len(msg.Result) == 0 {
			return nil, false, nil
		}
		return msg.Result, true, nil
	}

	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if res, done, err := handle(); done {
				return res, err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if res, done, err := handle(); done {
		return res, err
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("remote MCP server %q stream read failed", serverName)
	}
	return nil, fmt.Errorf("remote MCP server %q returned no matching response", serverName)
}
