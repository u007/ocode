package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jamesmercstudio/ocode/internal/auth"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/r3labs/sse/v2"
)

type MCPTool struct {
	server *MCPClient
	name   string
	desc   string
	schema map[string]interface{}
}

func (t MCPTool) Name() string        { return t.name }
func (t MCPTool) Description() string { return t.desc }
func (t MCPTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        t.name,
		"description": t.desc,
		"parameters":  t.schema,
	}
}

func (t MCPTool) Execute(args json.RawMessage) (string, error) {
	return t.server.CallTool(t.name, args)
}

type MCPClient struct {
	name    string
	isLocal bool
	timeout time.Duration
	// Local
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Scanner
	// Remote
	url       string
	headers   map[string]string
	sse       *sse.Client
	httpCli   *http.Client
	oauthCfg  *config.MCPOAuthConfig
	token     *auth.MCPAuthToken
	tokenMu   sync.RWMutex

	mu sync.Mutex
	id int
}

func NewRemoteClient(name string, cfg config.MCPConfig) (*MCPClient, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("no URL specified for remote MCP server %s", name)
	}

	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	client := sse.NewClient(cfg.URL)
	for k, v := range cfg.Headers {
		client.Headers[k] = v
	}

	httpCli := &http.Client{Timeout: timeout}

	mc := &MCPClient{
		name:     name,
		isLocal:  false,
		url:      cfg.URL,
		headers:  cfg.Headers,
		sse:      client,
		timeout:  timeout,
		httpCli:  httpCli,
		oauthCfg: cfg.OAuth,
	}

	if mc.needsOAuth() {
		token, err := mc.loadOrRefreshToken()
		if err != nil {
			return nil, err
		}
		if token != nil {
			mc.token = token
		}
	}

	return mc, nil
}

func NewLocalClient(name string, cfg config.MCPConfig) (*MCPClient, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("no command specified for MCP server %s", name)
	}

	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &MCPClient{
		name:    name,
		isLocal: true,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		reader:  bufio.NewScanner(stdout),
		timeout: timeout,
	}, nil
}

func (c *MCPClient) request(method string, params interface{}) (json.RawMessage, error) {
	if c.isLocal {
		return c.requestLocal(method, params)
	}
	return c.requestRemote(method, params)
}

func (c *MCPClient) needsOAuth() bool {
	if c.oauthCfg == nil {
		return false
	}
	if c.oauthCfg.Enabled != nil {
		return *c.oauthCfg.Enabled
	}
	return c.oauthCfg.AuthorizationURL != "" && c.oauthCfg.TokenURL != "" && c.oauthCfg.ClientID != ""
}

func (c *MCPClient) loadOrRefreshToken() (*auth.MCPAuthToken, error) {
	token, ok := auth.GetMCPAuth(c.name)
	if !ok {
		return nil, nil
	}
	if token.IsExpired() {
		if c.oauthCfg.TokenURL == "" || c.oauthCfg.ClientID == "" {
			return nil, fmt.Errorf("mcp server %s token expired and no refresh config available", c.name)
		}
		refreshed, err := auth.RefreshMCPAuthToken(c.name, c.oauthCfg.TokenURL, c.oauthCfg.ClientID, token)
		if err != nil {
			return nil, fmt.Errorf("refresh mcp token for %s: %w", c.name, err)
		}
		return &refreshed, nil
	}
	return &token, nil
}

func (c *MCPClient) ensureValidToken() error {
	if !c.needsOAuth() {
		return nil
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token == nil {
		token, err := c.loadOrRefreshToken()
		if err != nil {
			return err
		}
		c.token = token
	}
	if c.token != nil && c.token.IsExpired() {
		if c.oauthCfg.TokenURL == "" || c.oauthCfg.ClientID == "" {
			return fmt.Errorf("mcp server %s token expired and no refresh config available", c.name)
		}
		refreshed, err := auth.RefreshMCPAuthToken(c.name, c.oauthCfg.TokenURL, c.oauthCfg.ClientID, *c.token)
		if err != nil {
			return fmt.Errorf("refresh mcp token for %s: %w", c.name, err)
		}
		c.token = &refreshed
	}
	if c.token == nil {
		return fmt.Errorf("mcp server %s requires authentication: run /mcp-auth %s", c.name, c.name)
	}
	return nil
}

func (c *MCPClient) AuthHeader() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	if c.token == nil {
		return ""
	}
	return c.token.AuthorizationHeader()
}

func (c *MCPClient) requestRemote(method string, params interface{}) (json.RawMessage, error) {
	if err := c.ensureValidToken(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      c.id,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP request: %w", err)
	}

	req, err := http.NewRequest("POST", c.url, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if authHdr := c.AuthHeader(); authHdr != "" {
		req.Header.Set("Authorization", authHdr)
	}

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}

	if r.Error != nil {
		return nil, fmt.Errorf("remote MCP error (%d): %s", r.Error.Code, r.Error.Message)
	}

	return r.Result, nil
}

func (c *MCPClient) requestLocal(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      c.id,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	fmt.Fprintln(c.stdin, string(data))

	type scanResult struct {
		ok  bool
		err error
	}
	done := make(chan scanResult, 1)
	go func() {
		done <- scanResult{ok: c.reader.Scan(), err: c.reader.Err()}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("MCP server %s timed out after %s", c.name, c.timeout)
	case res := <-done:
		if !res.ok {
			if res.err != nil {
				return nil, fmt.Errorf("failed to read response from MCP server %s: %w", c.name, res.err)
			}
			return nil, fmt.Errorf("failed to read response from MCP server %s", c.name)
		}
	}

	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(c.reader.Bytes(), &resp); err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("MCP error (%d): %s", resp.Error.Code, resp.Error.Message)
	}

	return resp.Result, nil
}

func (c *MCPClient) ListTools() ([]MCPTool, error) {
	result, err := c.request("tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var toolsResp struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(result, &toolsResp); err != nil {
		return nil, err
	}

	var tools []MCPTool
	for _, t := range toolsResp.Tools {
		tools = append(tools, MCPTool{
			server: c,
			name:   c.name + "_" + t.Name,
			desc:   t.Description,
			schema: t.InputSchema,
		})
	}
	return tools, nil
}

func (c *MCPClient) CallTool(name string, args json.RawMessage) (string, error) {
	// Strip prefix
	shortName := strings.TrimPrefix(name, c.name+"_")

	params := map[string]interface{}{
		"name":      shortName,
		"arguments": args,
	}

	result, err := c.request("tools/call", params)
	if err != nil {
		return "", err
	}

	var callResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}

	if err := json.Unmarshal(result, &callResp); err != nil {
		return "", err
	}

	var b strings.Builder
	for _, c := range callResp.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}

	if callResp.IsError {
		return b.String(), fmt.Errorf("MCP tool call returned error")
	}

	return b.String(), nil
}
