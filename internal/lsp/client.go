package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader
	id     int
	mu     sync.Mutex
}

func NewClient(serverPath string) (*Client, error) {
	cmd := exec.Command(serverPath)
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

	return &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		reader: bufio.NewReader(stdout),
	}, nil
}

func (c *Client) Call(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.id++
	id := c.id

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.stdin.Write([]byte(header + string(data))); err != nil {
		return nil, err
	}

	line, err := c.reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	contentLength := 0
	if strings.HasPrefix(line, "Content-Length: ") {
		contentLength, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Content-Length: ")))
	}

	for {
		line, err = c.reader.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, err
	}

	var res struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  interface{}     `json:"error"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, err
	}

	if res.Error != nil {
		return nil, fmt.Errorf("LSP error: %v", res.Error)
	}

	return res.Result, nil
}

func (c *Client) Close() error {
	c.stdin.Close()
	return c.cmd.Process.Kill()
}

func (c *Client) Initialize(rootPath string) error {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return fmt.Errorf("failed to resolve root path: %w", err)
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	params := map[string]interface{}{
		"processId":    os.Getpid(),
		"rootUri":      u.String(),
		"capabilities": map[string]interface{}{},
	}
	_, err = c.Call("initialize", params)
	return err
}
