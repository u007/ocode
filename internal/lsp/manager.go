package lsp

import (
	"fmt"
	"log"
	"os"
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

// ServerStartedEvent is sent on the event channel at two points in the server
// lifecycle. Phase=="starting" fires immediately before the LSP initialize
// handshake begins (server binary found, goroutine launched). Phase=="ready"
// fires once initialize completes successfully.
type ServerStartedEvent struct {
	Cmd    string // binary name, e.g. "gopls"
	LangID string // primary language ID, e.g. "go"
	Root   string // project root path
	Phase  string // "starting" | "ready"
}

// ServerStatus describes a running language server.
type ServerStatus struct {
	Cmd    string // binary name, e.g. "gopls"
	LangID string // primary language ID
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
	// diagnostics is the project-wide store of the most recently
	// published diagnostics from every server. The readLoop in each
	// Client invokes the per-server hook installed by ClientForExt,
	// which funnels entries into this store. Cleared by Close so a
	// re-launched server starts from a clean slate.
	diagnostics *DiagnosticStore
	// eventCh receives ServerStartedEvent when a server completes its
	// initialize handshake. Nil in headless mode (runcli, acp, server).
	eventCh chan ServerStartedEvent
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
		root:        root,
		clients:     make(map[string]*Client),
		openByURI:   make(map[string]string),
		diagnostics: newDiagnosticStore(),
	}
	if w, err := newFileWatcher(root, m); err == nil {
		m.watcher = w
	}
	return m
}

// Diagnostics returns the project-wide diagnostic store. The store is
// safe to read without holding the manager mutex; it has its own RWMutex.
// Returns nil only if the Manager was constructed without a store (which
// the public constructor never does — guarded for safety).
func (m *Manager) Diagnostics() *DiagnosticStore {
	if m == nil {
		return nil
	}
	return m.diagnostics
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
	if c, ok := m.clients[ext]; ok {
		m.mu.Unlock()
		return c, nil
	}
	if _, err := exec.LookPath(spec.cmd); err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("language server %q not found in PATH (install it for %s support)", spec.cmd, ext)
	}
	c, err := NewClient(spec.cmd, spec.args...)
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("start %s: %w", spec.cmd, err)
	}
	if err := c.Initialize(m.root, spec.langID); err != nil {
		m.mu.Unlock()
		c.Close()
		return nil, fmt.Errorf("initialize %s: %w", spec.cmd, err)
	}
	// Install a per-server diagnostics hook that funnels publishDiagnostics
	// frames into the manager's shared store. The handler captures the store
	// generation so stale frames from a previous server lifecycle are ignored
	// after Close/Restart bumps the generation.
	store := m.diagnostics
	if store != nil {
		generation := store.Generation()
		c.SetDiagnosticsHandler(func(uri string, diags []Diagnostic) {
			store.SetURIIfGeneration(uri, diags, generation)
		})
	}
	m.clients[ext] = c
	// Read eventCh while holding the lock, then send outside the lock.
	eventCh := m.eventCh
	m.mu.Unlock()
	if eventCh != nil {
		evt := ServerStartedEvent{Cmd: spec.cmd, LangID: spec.langID, Root: m.root, Phase: "ready"}
		select {
		case eventCh <- evt:
		default:
		}
	}
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
// Diagnostics published by the dying client are NOT cleared — gopls
// typically republishes the same set on reconnect, and removing them
// would briefly show "no errors" while the new server is initialising.
// If the restart transitions to a *different* server (e.g. a binary
// swap), the new server's first publishDiagnostics will overwrite them.
func (m *Manager) Restart(ext string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[ext]; ok {
		if m.diagnostics != nil {
			m.diagnostics.BumpGeneration()
		}
		c.Close()
		delete(m.clients, ext)
	}
}

// Close shuts down every running server, the file watcher, and clears all
// bookkeeping. Safe to call multiple times. The diagnostic store is also
// cleared so a re-launched manager (e.g. a session restart) starts from
// a clean slate — the agent must never see stale diagnostics from a
// previous server lifetime.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.diagnostics != nil {
		m.diagnostics.BumpGeneration()
	}
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
	if m.diagnostics != nil {
		m.diagnostics.clear()
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

// ActiveServers returns one ServerStatus per unique binary that has a
// running (non-closed) client. Multiple extensions mapping to the same
// binary (e.g. .ts/.tsx/.js/.jsx → typescript-language-server) produce
// one entry. Results are sorted by Cmd.
func (m *Manager) ActiveServers() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := make(map[string]ServerStatus)
	for ext, c := range m.clients {
		if c == nil {
			continue
		}
		spec := serversByExt[ext]
		if _, ok := seen[spec.cmd]; !ok {
			seen[spec.cmd] = ServerStatus{Cmd: spec.cmd, LangID: spec.langID}
		}
	}
	out := make([]ServerStatus, 0, len(seen))
	for _, s := range seen {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cmd < out[j].Cmd })
	return out
}

// SetEventChan registers a channel to receive ServerStartedEvent when a
// language server successfully initialises. Call only from the TUI layer
// after the Manager has been constructed. Headless callers (runcli, acp,
// server) never call this; Manager treats a nil channel as a no-op.
func (m *Manager) SetEventChan(ch chan ServerStartedEvent) {
	m.mu.Lock()
	m.eventCh = ch
	m.mu.Unlock()
}

// WarmUp eagerly starts language servers for every extension found under root
// without blocking the caller. Each unique server binary is started in its own
// goroutine; errors (missing binary, init failure) are logged and silently
// skipped so a missing server never delays startup.
func (m *Manager) WarmUp(root string) {
	// Collect the set of extensions present in the project (depth-limited to
	// avoid scanning huge vendor trees).
	found := make(map[string]bool)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == "node_modules" || name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if _, ok := serversByExt[ext]; ok {
			found[ext] = true
		}
		return nil
	})

	// Read eventCh once outside the loop (avoids repeated lock).
	m.mu.Lock()
	eventCh := m.eventCh
	m.mu.Unlock()

	// Start one goroutine per unique server binary (not per extension).
	launched := make(map[string]bool)
	for ext := range found {
		spec := serversByExt[ext]
		if launched[spec.cmd] {
			continue
		}
		launched[spec.cmd] = true

		// Signal "starting" immediately so the sidebar can show a spinner
		// before the blocking initialize handshake completes.
		if eventCh != nil {
			select {
			case eventCh <- ServerStartedEvent{Cmd: spec.cmd, LangID: spec.langID, Root: root, Phase: "starting"}:
			default:
			}
		}

		e, s := ext, spec
		go func() {
			if _, err := m.ClientForExt(e); err != nil {
				log.Printf("lsp warmup %s: %v", s.cmd, err)
			}
		}()
	}
}
