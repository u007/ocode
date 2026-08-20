package lsp

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/u007/ocode/internal/lsp/broker"
)

// docState is the daemon's canonical view of one open document, shared
// across every connected broker client. Each remote Client tracks its own
// version counter starting at 1 (see openedDoc in client.go); the daemon
// cannot forward those raw, since gopls requires one monotonic version
// sequence per URI. Instead the daemon re-versions every didOpen/didChange
// it receives against this shared counter before forwarding to the real
// server, and folds a second client's "first" didOpen into a didChange.
type docState struct {
	refs    int
	version int
	text    string
}

// daemonUpstream is the broker.Upstream that owns the one real stdio-backed
// *Client for a project root + language, and multiplexes every connected
// broker client's requests onto it. It is the daemon process's entire job:
// document-state ownership routing (see docState) and diagnostics fan-out
// (a real LSP server pushes diagnostics unprompted; broker.Upstream has no
// server-initiated-request support, so daemonUpstream re-sends each
// publishDiagnostics to every connected client via broker.PushSender).
type daemonUpstream struct {
	real *Client

	mu   sync.Mutex
	docs map[string]*docState

	connMu   sync.Mutex
	senders  map[int]*broker.PushSender
	nextConn int

	idleMu    sync.Mutex
	idleTimer *time.Timer
	idleAfter time.Duration
	onIdle    func()
}

// newDaemonUpstream wraps an already-initialized real client. onIdle is
// invoked at most once, from a timer goroutine, when the connected-client
// count has stayed at zero for idleAfter — the caller's job is to close the
// real client and remove the broker metadata (see cmd_lsp_daemon.go).
func newDaemonUpstream(real *Client, idleAfter time.Duration, onIdle func()) *daemonUpstream {
	d := &daemonUpstream{
		real:      real,
		docs:      make(map[string]*docState),
		senders:   make(map[int]*broker.PushSender),
		idleAfter: idleAfter,
		onIdle:    onIdle,
	}
	real.SetDiagnosticsHandler(d.broadcastDiagnostics)
	d.armIdleTimer() // no clients connected yet at construction time
	return d
}

// handleConn is the broker.Listen onConn callback: it runs for the lifetime
// of one accepted, authenticated connection (broker.ServeRPC blocks until
// the connection errors or closes), so wrapping connect/disconnect
// bookkeeping around it gives exact per-connection lifecycle tracking.
func (d *daemonUpstream) handleConn(conn net.Conn) {
	id := d.connOpened()
	defer d.connClosed(id)
	broker.ServeRPC(conn, d, func(p *broker.PushSender) {
		d.connMu.Lock()
		d.senders[id] = p
		d.connMu.Unlock()
	})
}

func (d *daemonUpstream) connOpened() int {
	d.connMu.Lock()
	d.nextConn++
	id := d.nextConn
	d.connMu.Unlock()
	d.cancelIdleTimer()
	return id
}

func (d *daemonUpstream) connClosed(id int) {
	d.connMu.Lock()
	delete(d.senders, id)
	remaining := len(d.senders)
	d.connMu.Unlock()
	if remaining == 0 {
		d.armIdleTimer()
	}
}

func (d *daemonUpstream) cancelIdleTimer() {
	d.idleMu.Lock()
	if d.idleTimer != nil {
		d.idleTimer.Stop()
		d.idleTimer = nil
	}
	d.idleMu.Unlock()
}

func (d *daemonUpstream) armIdleTimer() {
	d.idleMu.Lock()
	defer d.idleMu.Unlock()
	if d.idleTimer != nil {
		d.idleTimer.Stop()
	}
	d.idleTimer = time.AfterFunc(d.idleAfter, func() {
		d.connMu.Lock()
		stillIdle := len(d.senders) == 0
		d.connMu.Unlock()
		if stillIdle && d.onIdle != nil {
			d.onIdle()
		}
	})
}

// broadcastDiagnostics is the real client's onDiagnostics hook: it re-sends
// every publishDiagnostics notification to every currently connected broker
// client. The wire shape must match the client-side parser installed by
// NewBrokerClient (client.go), which expects {uri, diagnostics}.
func (d *daemonUpstream) broadcastDiagnostics(uri string, diags []Diagnostic) {
	payload := struct {
		URI         string       `json:"uri"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}{URI: uri, Diagnostics: diags}

	d.connMu.Lock()
	senders := make([]*broker.PushSender, 0, len(d.senders))
	for _, s := range d.senders {
		senders = append(senders, s)
	}
	d.connMu.Unlock()

	for _, s := range senders {
		if err := s.Push("textDocument/publishDiagnostics", payload); err != nil {
			log.Printf("lsp daemon: push diagnostics for %s failed: %v", uri, err)
		}
	}
}

// Call implements broker.Upstream: every request/response LSP method
// (hover, definition, references, …) forwards to the real client verbatim —
// these are read-only queries with no shared state to reconcile.
func (d *daemonUpstream) Call(method string, params json.RawMessage) (json.RawMessage, error) {
	return d.real.Call(method, params)
}

// Notify implements broker.Upstream. didOpen/didChange are intercepted for
// document-state ownership routing (see docState); everything else forwards
// verbatim.
func (d *daemonUpstream) Notify(method string, params json.RawMessage) error {
	switch method {
	case "textDocument/didOpen":
		return d.handleDidOpen(params)
	case "textDocument/didChange":
		return d.handleDidChange(params)
	default:
		return d.real.Notify(method, params)
	}
}

type textDocumentOpenParams struct {
	TextDocument struct {
		URI        string `json:"uri"`
		LanguageID string `json:"languageId"`
		Text       string `json:"text"`
	} `json:"textDocument"`
}

type textDocumentChangeParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

func (d *daemonUpstream) handleDidOpen(raw json.RawMessage) error {
	var p textDocumentOpenParams
	if err := json.Unmarshal(raw, &p); err != nil || p.TextDocument.URI == "" {
		return fmt.Errorf("lsp daemon: malformed didOpen: %w", err)
	}

	d.mu.Lock()
	doc, exists := d.docs[p.TextDocument.URI]
	if !exists {
		doc = &docState{refs: 0, version: 1, text: p.TextDocument.Text}
		d.docs[p.TextDocument.URI] = doc
	}
	doc.refs++
	d.mu.Unlock()

	if exists {
		// The real server already has this document open (a different
		// connected client opened it first) — fold this client's own
		// "first open" into a change against our canonical version, in
		// case its snapshot differs from what the server currently has.
		return d.applyChange(p.TextDocument.URI, p.TextDocument.Text)
	}

	return d.real.Notify("textDocument/didOpen", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": p.TextDocument.URI, "languageId": p.TextDocument.LanguageID,
			"version": 1, "text": p.TextDocument.Text,
		},
	})
}

func (d *daemonUpstream) handleDidChange(raw json.RawMessage) error {
	var p textDocumentChangeParams
	if err := json.Unmarshal(raw, &p); err != nil || p.TextDocument.URI == "" || len(p.ContentChanges) == 0 {
		return fmt.Errorf("lsp daemon: malformed didChange: %w", err)
	}
	// UpdateText always sends a single full-document replacement (see
	// client.go's UpdateText), never incremental ranges, so the last entry
	// is the complete new text.
	return d.applyChange(p.TextDocument.URI, p.ContentChanges[len(p.ContentChanges)-1].Text)
}

func (d *daemonUpstream) applyChange(uri, text string) error {
	d.mu.Lock()
	doc, ok := d.docs[uri]
	if !ok {
		// Defensive: a didChange with no preceding didOpen from this
		// daemon's perspective. Synthesize the open rather than dropping
		// the edit — a missing languageId is tolerated by every server
		// this package targets once a document is already indexed by
		// extension.
		doc = &docState{refs: 1, version: 1, text: text}
		d.docs[uri] = doc
		d.mu.Unlock()
		return d.real.Notify("textDocument/didOpen", map[string]interface{}{
			"textDocument": map[string]interface{}{"uri": uri, "languageId": "", "version": 1, "text": text},
		})
	}
	if doc.text == text {
		d.mu.Unlock()
		return nil
	}
	doc.version++
	doc.text = text
	version := doc.version
	d.mu.Unlock()

	return d.real.Notify("textDocument/didChange", map[string]interface{}{
		"textDocument":   map[string]interface{}{"uri": uri, "version": version},
		"contentChanges": []map[string]interface{}{{"text": text}},
	})
}
