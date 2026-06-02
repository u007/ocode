package lsp

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// serverSpec describes how to launch a language server for a file extension.
type serverSpec struct {
	cmd    string
	args   []string
	langID string
}

// serversByExt maps file extensions to their language server. gopls is the
// only entry validated end-to-end; the rest are best-effort (correct stdio
// invocations, but untested here) — see TODO.md.
var serversByExt = map[string]serverSpec{
	".go":  {cmd: "gopls", langID: "go"},
	".rs":  {cmd: "rust-analyzer", langID: "rust"},
	".py":  {cmd: "pyright-langserver", args: []string{"--stdio"}, langID: "python"},
	".ts":  {cmd: "typescript-language-server", args: []string{"--stdio"}, langID: "typescript"},
	".tsx": {cmd: "typescript-language-server", args: []string{"--stdio"}, langID: "typescriptreact"},
	".js":  {cmd: "typescript-language-server", args: []string{"--stdio"}, langID: "javascript"},
	".jsx": {cmd: "typescript-language-server", args: []string{"--stdio"}, langID: "javascriptreact"},
}

// Manager owns one initialised Client per language extension, lazily started
// and reused. It also owns a single file-watcher that pushes post-edit text
// (via textDocument/didChange) into whichever server has the file open, so
// the LSP semantic tool stays current across the agent's own Write/Edit calls.
//
// It is safe for concurrent use.
type Manager struct {
	root    string
	mu      sync.Mutex
	clients map[string]*Client
	// openByURI maps file:// URI -> server extension (e.g. ".go"). The watcher
	// calls back into handleFileChange, which uses this map to dispatch to
	// the right client. The map's keys are file URIs, not paths, so they
	// match what the LSP client itself tracks.
	openByURI map[string]string
	watcher   *fileWatcher
}

// NewManager returns a Manager rooted at root (the project directory used for
// LSP initialize). Pass "." for the current working directory. The manager
// starts a background file watcher; if watcher creation fails (e.g. inotify
// limits on a hostile host), the manager still works but files edited
// out-of-band will not trigger didChange.
func NewManager(root string) *Manager {
	if root == "" {
		root = "."
	}
	m := &Manager{
		root:      root,
		clients:   make(map[string]*Client),
		openByURI: make(map[string]string),
	}
	if w, err := newFileWatcher(root, m); err == nil {
		m.watcher = w
	}
	return m
}

// ClientForExt returns an initialised client for the given file extension,
// starting the server on first use. It returns a descriptive error (never a
// silent fallback) when no server is configured or the binary is missing.
func (m *Manager) ClientForExt(ext string) (*Client, error) {
	spec, ok := serversByExt[ext]
	if !ok {
		return nil, fmt.Errorf("no language server configured for %q files (supported: %s)", ext, SupportedExtensions())
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[ext]; ok {
		return c, nil
	}
	if _, err := exec.LookPath(spec.cmd); err != nil {
		return nil, fmt.Errorf("language server %q not found in PATH (install it for %s support)", spec.cmd, ext)
	}
	c, err := NewClient(spec.cmd, spec.args...)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.cmd, err)
	}
	if err := c.Initialize(m.root, spec.langID); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize %s: %w", spec.cmd, err)
	}
	m.clients[ext] = c
	return c, nil
}

// ClientForFile is ClientForExt keyed by a file path's extension.
func (m *Manager) ClientForFile(path string) (*Client, error) {
	return m.ClientForExt(filepath.Ext(path))
}

// EnsureOpen opens path with the right client and registers a file watch so
// subsequent edits push didChange into the server. This is the entry point
// the LSP tool layer should use; calling Client.EnsureOpen directly skips
// the watcher and leaves the document stale across out-of-band edits.
func (m *Manager) EnsureOpen(path string) error {
	client, err := m.ClientForFile(path)
	if err != nil {
		return err
	}
	if err := client.EnsureOpen(path); err != nil {
		return err
	}
	abs, absErr := filepath.Abs(path)
	if absErr != nil {
		return nil // opened, but watcher registration failed — not fatal
	}
	uri := fileURI(abs)
	m.mu.Lock()
	ext := filepath.Ext(path)
	m.openByURI[uri] = ext
	m.mu.Unlock()
	if m.watcher != nil {
		m.watcher.Add(abs)
	}
	return nil
}

// NotifyEdited pushes newText into whichever server has path open (skipping
// the disk read). Use this from the in-process file editor so position-based
// queries stay in sync without a save round-trip.
func (m *Manager) NotifyEdited(path string, newText string) error {
	client, err := m.ClientForFile(path)
	if err != nil {
		return err
	}
	return client.UpdateText(path, newText)
}

// handleFileChange is the fsnotify callback. It looks up the right client and
// ships the on-disk content via didChange. Errors are logged (the watcher has
// no useful recovery for a broken didChange); the next save will retry.
func (m *Manager) handleFileChange(absPath string) {
	uri := fileURI(absPath)
	m.mu.Lock()
	ext, ok := m.openByURI[uri]
	m.mu.Unlock()
	if !ok {
		// Not open in any server (e.g. user-edited file the agent hasn't
		// touched); drop the event. Opening lazily on first query is a
		// larger design change and out of scope.
		return
	}
	m.mu.Lock()
	client := m.clients[ext]
	m.mu.Unlock()
	if client == nil {
		return
	}
	if err := client.UpdateText(absPath, ""); err != nil {
		log.Printf("lsp: didChange for %s failed: %v", absPath, err)
	}
}

// Restart closes and forgets the client for ext (next use restarts it).
func (m *Manager) Restart(ext string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[ext]; ok {
		c.Close()
		delete(m.clients, ext)
	}
}

// Close shuts down every running server, the file watcher, and clears all
// bookkeeping. Safe to call multiple times.
func (m *Manager) Close() {
	m.mu.Lock()
	for ext, c := range m.clients {
		c.Close()
		delete(m.clients, ext)
	}
	m.openByURI = make(map[string]string)
	m.mu.Unlock()
	if m.watcher != nil {
		_ = m.watcher.Close()
		m.watcher = nil
	}
}

// SupportedExtensions lists configured extensions, sorted.
func SupportedExtensions() string {
	exts := make([]string, 0, len(serversByExt))
	for ext := range serversByExt {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return strings.Join(exts, ", ")
}

// ServerForExt reports the server command for ext and whether it is installed.
func ServerForExt(ext string) (cmd string, installed bool, ok bool) {
	spec, ok := serversByExt[ext]
	if !ok {
		return "", false, false
	}
	_, err := exec.LookPath(spec.cmd)
	return spec.cmd, err == nil, true
}

// KnownServers returns the distinct configured server commands, sorted.
func KnownServers() []string {
	seen := map[string]bool{}
	for _, spec := range serversByExt {
		seen[spec.cmd] = true
	}
	out := make([]string, 0, len(seen))
	for cmd := range seen {
		out = append(out, cmd)
	}
	sort.Strings(out)
	return out
}
