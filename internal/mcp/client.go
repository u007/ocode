package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"

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
	// Local
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Scanner
	// Remote
	url     string
	headers map[string]string
	sse     *sse.Client

	mu sync.Mutex
	id int
}

func NewRemoteClient(name string, cfg config.MCPConfig) (*MCPClient, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("no URL specified for remote MCP server %s", name)
	}

	client := sse.NewClient(cfg.URL)
	for k, v := range cfg.Headers {
		client.Headers[k] = v
	}

	return &MCPClient{
		name:    name,
		isLocal: false,
		url:     cfg.URL,
		headers: cfg.Headers,
		sse:     client,
	}, nil
}

func NewLocalClient(name string, cfg config.MCPConfig) (*MCPClient, error) {
	if len(cfg.Command) == 0 {
		return nil, fmt.Errorf("no command specified for MCP server %s", name)
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
	}, nil
}

func (c *MCPClient) request(method string, params interface{}) (json.RawMessage, error) {
	if c.isLocal {
		return c.requestLocal(method, params)
	}
	return c.requestRemote(method, params)
}

func (c *MCPClient) requestRemote(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id++

	// For remote MCP, we typically use POST for requests and SSE for notifications/responses if streaming.
	// However, standard tool calling is usually a single POST.
	// If it's a true SSE MCP, we'd subscribe here.
	// For simplicity, let's stick to the POST-based request/response which most remote MCPs support.

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

	resp, err := http.DefaultClient.Do(req)
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

	if !c.reader.Scan() {
		return nil, fmt.Errorf("failed to read response from MCP server %s", c.name)
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
