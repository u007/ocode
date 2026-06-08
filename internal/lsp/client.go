// Package lsp speaks the Language Server Protocol over a child process's
// stdio. A single background goroutine reads framed messages and dispatches
// responses to per-request channels, so concurrent Call()s are safe, server
// notifications never get mistaken for replies, and every request is bounded
// by a timeout (the child's stdout is a pipe with no read deadline, so a
// dedicated reader is the only way to avoid a permanently blocked Call).
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout bounds a single LSP request. Initial indexing (gopls loading
// a large module) can be slow, so callers that issue the first query may want
// CallTimeout with a larger value.
const DefaultTimeout = 20 * time.Second

type rpcResult struct {
	res json.RawMessage
	err error
}

type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	langID string

	writeMu sync.Mutex // serialises writes (Call + reader answering server reqs)

	mu        sync.Mutex
	id        int
	pending   map[int]chan rpcResult
	opened    map[string]openedDoc // file URIs we've sent didOpen for
	closed    bool
	closeErr  error
	closeCh   chan struct{} // closed exactly once on the first successful Close
	closeOnce sync.Once
	exited    chan struct{} // closed when cmd.Wait returns (reaper goroutine)

	// onDiagnostics, if set, is invoked from the readLoop goroutine
	// for every textDocument/publishDiagnostics notification. The
	// callback runs synchronously on the reader goroutine, so it must
	// be cheap and non-blocking (the Manager stores into a mutex-guarded
	// map and returns immediately). Setting it is the Manager's job;
	// tests can plug in a custom hook.
	onDiagnostics func(uri string, diags []Diagnostic)
}

// SetDiagnosticsHandler installs a callback invoked for every
// textDocument/publishDiagnostics notification the server pushes. It is
// safe to call once after NewClient (the readLoop is the only caller and
// reads the field on every frame). Used by Manager to wire per-server
// diagnostics into the shared store.
func (c *Client) SetDiagnosticsHandler(fn func(uri string, diags []Diagnostic)) {
	c.mu.Lock()
	c.onDiagnostics = fn
	c.mu.Unlock()
}

// openedDoc tracks the LSP textDocument version (incremented on each didChange)
// and the text we last shipped, so file-watcher-based change detection can skip
// re-sending identical content and avoid spurious re-indexing.
type openedDoc struct {
	version int
	text    string
}

// NewClient spawns the language server and starts its reader loop. command is
// the server executable; args are passed verbatim (e.g. "--stdio").
func NewClient(command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// Discard stderr; servers log verbosely and inheriting the fd would paint
	// over the TUI (see CLAUDE.md TUI output-safety rules).
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		reader:  bufio.NewReader(stdout),
		pending: make(map[int]chan rpcResult),
		opened:  make(map[string]openedDoc),
		closeCh: make(chan struct{}),
	}
	go c.readLoop()
	// Background reaper: cmd.Wait reaps the child (no zombie) and surfaces
	// unexpected exits via failAll so any in-flight Call returns promptly.
	// Wait is called exactly once here; Close must NOT call Wait again
	// (multiple Waits on the same *exec.Cmd panic). The exited channel
	// signals Close (and other observers) that the process is gone.
	exited := make(chan struct{})
	go func() {
		err := cmd.Wait()
		close(exited)
		c.mu.Lock()
		alreadyClosed := c.closed
		c.mu.Unlock()
		if !alreadyClosed {
			c.failAll(fmt.Errorf("LSP server exited: %w", err))
		}
	}()
	c.exited = exited
	return c, nil
}

func (c *Client) readLoop() {
	for {
		body, err := c.readFrame()
		if err != nil {
			c.failAll(err)
			return
		}
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			continue // skip unparseable frame
		}
		switch {
		case msg.ID != nil && msg.Method == "":
			// Response to one of our requests.
			c.mu.Lock()
			ch := c.pending[*msg.ID]
			delete(c.pending, *msg.ID)
			c.mu.Unlock()
			if ch != nil {
				var rerr error
				if msg.Error != nil {
					rerr = fmt.Errorf("LSP error: %s", strings.TrimSpace(string(msg.Error)))
				}
				ch <- rpcResult{res: msg.Result, err: rerr}
			}
		case msg.ID != nil && msg.Method != "":
			// Server -> client request. With empty client capabilities these
			// are rare, but reply null so the server never blocks waiting on us.
			// Log write errors instead of swallowing them: a stale
			// window/workDoneProgress create from a server after Close
			// arriving is a real diagnostic, not noise.
			if err := c.writeMessage(map[string]interface{}{"jsonrpc": "2.0", "id": *msg.ID, "result": nil}); err != nil {
				log.Printf("lsp: reply to server request %q failed: %v", msg.Method, err)
			}
		case msg.Method == "textDocument/publishDiagnostics":
			// Server-pushed diagnostics. Hand them to the configured
			// handler (set by the Manager when the client is created).
			// Read the handler pointer under the mutex so a concurrent
			// SetDiagnosticsHandler (rare, but allowed) is race-free.
			c.mu.Lock()
			fn := c.onDiagnostics
			c.mu.Unlock()
			if fn != nil {
				var p diagnosticParams
				if err := json.Unmarshal(msg.Params, &p); err == nil && p.URI != "" {
					fn(p.URI, buildDiagnostics(&p))
				}
			}
		default:
			// Notification (window/logMessage, $/progress, …) —
			// nothing to correlate; ignore.
		}
	}
}

func (c *Client) readFrame() ([]byte, error) {
	contentLen := 0
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(line, "Content-Length:") {
			contentLen, _ = strconv.Atoi(strings.TrimSpace(line[len("Content-Length:"):]))
		}
		if line == "\r\n" {
			break
		}
	}
	if contentLen <= 0 {
		return nil, fmt.Errorf("invalid Content-Length")
	}
	body := make([]byte, contentLen)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

// failAll resolves every pending request with err (called when the reader dies,
// e.g. the server exited) so no Call blocks until its timeout.
func (c *Client) failAll(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.closeErr = err
	for id, ch := range c.pending {
		ch <- rpcResult{err: fmt.Errorf("LSP server closed: %w", err)}
		delete(c.pending, id)
	}
}

func (c *Client) writeMessage(v map[string]interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	// Short-circuit if Close has already run: writing to a closed pipe
	// returns EPIPE synchronously on some platforms, and silently succeeds
	// (with the kernel discarding the bytes) on others. Either way, don't
	// hand the caller's payload to a dead child.
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return fmt.Errorf("LSP client closed")
	}
	_, err = io.WriteString(c.stdin, fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data))
	return err
}

// Call sends a request and waits for the matching response (or DefaultTimeout).
func (c *Client) Call(method string, params interface{}) (json.RawMessage, error) {
	return c.CallTimeout(method, params, DefaultTimeout)
}

func (c *Client) CallTimeout(method string, params interface{}, timeout time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("LSP client closed: %v", c.closeErr)
	}
	c.id++
	id := c.id
	ch := make(chan rpcResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.writeMessage(map[string]interface{}{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case r := <-ch:
		return r.res, r.err
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("LSP request %q timed out after %s", method, timeout)
	}
}

// Notify sends a notification (no response expected).
func (c *Client) Notify(method string, params interface{}) error {
	return c.writeMessage(map[string]interface{}{"jsonrpc": "2.0", "method": method, "params": params})
}

// Close shuts the child down and is safe to call multiple times. Returns the
// first non-nil error and otherwise nil. The background reaper goroutine
// started in NewClient reaps the child via cmd.Wait (called exactly once),
// so no zombie lingers. Close itself just signals shutdown; it does NOT
// call Wait.
func (c *Client) Close() error {
	var firstErr error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		// Close stdin first so the server sees EOF and exits cleanly if it
		// honours shutdown. A misbehaving server is then force-killed.
		if err := c.stdin.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		// Wait briefly for the reaper to observe the exit. If the server
		// doesn't honour stdin EOF (gopls does, others may not), force-kill
		// after a short grace window; the reaper's Wait call will then
		// return and close c.exited.
		select {
		case <-c.exited:
			// Server already exited on its own.
		case <-time.After(2 * time.Second):
			if c.cmd.Process != nil {
				if err := c.cmd.Process.Kill(); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			<-c.exited
		}
	})
	return firstErr
}

// Initialize performs the LSP handshake: initialize request + initialized
// notification. Without the notification gopls never fully activates.
func (c *Client) Initialize(rootPath string, langID string) error {
	c.langID = langID
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return fmt.Errorf("resolve root path: %w", err)
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	// Initial query can trigger a full module load; give it room.
	if _, err := c.CallTimeout("initialize", map[string]interface{}{
		"processId":    os.Getpid(),
		"rootUri":      u.String(),
		"capabilities": map[string]interface{}{},
	}, 60*time.Second); err != nil {
		return err
	}
	return c.Notify("initialized", map[string]interface{}{})
}

// EnsureOpen sends textDocument/didOpen for path once, or didChange on
// subsequent calls (with an incremented version). Position queries
// (references/definition/…) are unreliable until the document is opened.
//
// Files larger than maxOpenBytes are refused: re-shipping a multi-megabyte
// buffer to the server on every edit is wasteful when the server already
// has its own index, and a 50MB didOpen stalls the tool call (and any
// other request queued behind it) for the full read.
func (c *Client) EnsureOpen(path string) error {
	return c.UpdateText(path, "")
}

// maxOpenBytes caps a single didOpen/didChange payload. 8 MiB covers almost
// every realistic source file; anything bigger should be edited through a
// dedicated tool rather than the LSP semantic tool.
const maxOpenBytes = 8 << 20

// UpdateText is EnsureOpen + a forced text override. It sends didOpen on the
// first call for path, then didChange (with version+1) on subsequent calls.
// Used by the manager's file watcher to push post-edit content into the
// server without making it re-read the file from disk.
func (c *Client) UpdateText(path string, newText string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	uri := fileURI(abs)

	// If newText is empty, read from disk; otherwise use the caller's text.
	text := newText
	if text == "" {
		data, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		text = string(data)
	}
	if len(text) > maxOpenBytes {
		return fmt.Errorf("refusing to open %s: %d bytes exceeds %d-byte cap (use a smaller file)", path, len(text), maxOpenBytes)
	}

	c.mu.Lock()
	prev, already := c.opened[uri]
	c.mu.Unlock()

	if !already {
		if err := c.Notify("textDocument/didOpen", map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": uri, "languageId": c.langID, "version": 1, "text": text,
			},
		}); err != nil {
			return err
		}
		c.mu.Lock()
		c.opened[uri] = openedDoc{version: 1, text: text}
		c.mu.Unlock()
		return nil
	}

	// Already open: emit didChange with the new version. Skip the roundtrip
	// when the content is byte-identical (file-watcher churn, fsync storms).
	if prev.text == text {
		return nil
	}
	version := prev.version + 1
	if err := c.Notify("textDocument/didChange", map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri, "version": version},
		"contentChanges": []map[string]interface{}{{
			"text": text,
		}},
	}); err != nil {
		return err
	}
	c.mu.Lock()
	c.opened[uri] = openedDoc{version: version, text: text}
	c.mu.Unlock()
	return nil
}

func fileURI(absPath string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(absPath)}
	return u.String()
}

func absURI(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return fileURI(abs), nil
}

// uriToPath converts a file:// URI back to a local filesystem path. It is
// the inverse of fileURI; used by the diagnostics parser to translate the
// server's URI back into a path the tool layer can display relative to
// the working directory. Unrecognised URIs are returned with the
// "file://" prefix stripped (best-effort).
func uriToPath(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" {
		// On Windows u.Path may be "/C:/foo"; TrimPrefix the leading slash
		// only when the next char looks like a drive letter so we don't
		// break POSIX paths that legitimately start with two slashes
		// (//host/share). Linux/macOS paths never have a drive letter.
		p := u.Path
		if len(p) >= 3 && p[0] == '/' && ((p[1] >= 'A' && p[1] <= 'Z') || (p[1] >= 'a' && p[1] <= 'z')) && p[2] == ':' {
			p = p[1:]
		}
		return filepath.FromSlash(p)
	}
	return strings.TrimPrefix(uri, "file://")
}
