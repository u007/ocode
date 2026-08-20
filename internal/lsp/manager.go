package lsp

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/lsp/broker"
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
	".go":     {cmd: "gopls", langID: "go"},
	".rs":     {cmd: "rust-analyzer", langID: "rust"},
	".py":     {cmd: "pyright-langserver", args: []string{"--stdio"}, langID: "python"},
	".ts":     {cmd: "typescript-language-server", args: []string{"--stdio"}, langID: "typescript"},
	".tsx":    {cmd: "typescript-language-server", args: []string{"--stdio"}, langID: "typescriptreact"},
	".js":     {cmd: "typescript-language-server", args: []string{"--stdio"}, langID: "javascript"},
	".jsx":    {cmd: "typescript-language-server", args: []string{"--stdio"}, langID: "javascriptreact"},
	".dart":   {cmd: "dart", args: []string{"language-server", "--protocol=lsp"}, langID: "dart"},
	".php":    {cmd: "intelephense", args: []string{"--stdio"}, langID: "php"},
	".java":   {cmd: "jdtls", langID: "java"},
	".cs":     {cmd: "csharp-ls", langID: "csharp"},
	".rb":     {cmd: "solargraph", args: []string{"stdio"}, langID: "ruby"},
	".c":      {cmd: "clangd", langID: "c"},
	".h":      {cmd: "clangd", langID: "c"},
	".cpp":    {cmd: "clangd", langID: "cpp"},
	".hpp":    {cmd: "clangd", langID: "cpp"},
	".cc":     {cmd: "clangd", langID: "cpp"},
	".lua":    {cmd: "lua-language-server", langID: "lua"},
	".kt":     {cmd: "kotlin-language-server", langID: "kotlin"},
	".kts":    {cmd: "kotlin-language-server", langID: "kotlin"},
	".swift":  {cmd: "sourcekit-lsp", langID: "swift"},
	".scala":  {cmd: "metals", langID: "scala"},
	".sbt":    {cmd: "metals", langID: "scala"},
	".ex":     {cmd: "elixir-ls", langID: "elixir"},
	".exs":    {cmd: "elixir-ls", langID: "elixir"},
	".zig":    {cmd: "zls", langID: "zig"},
	".hs":     {cmd: "haskell-language-server-wrapper", args: []string{"--lsp"}, langID: "haskell"},
	".ml":     {cmd: "ocamllsp", langID: "ocaml"},
	".mli":    {cmd: "ocamllsp", langID: "ocaml"},
	".tf":     {cmd: "terraform-ls", args: []string{"serve"}, langID: "terraform"},
	".tfvars": {cmd: "terraform-ls", args: []string{"serve"}, langID: "terraform"},
	".yaml":   {cmd: "yaml-language-server", args: []string{"--stdio"}, langID: "yaml"},
	".yml":    {cmd: "yaml-language-server", args: []string{"--stdio"}, langID: "yaml"},
	".json":   {cmd: "vscode-json-language-server", args: []string{"--stdio"}, langID: "json"},
	".sh":     {cmd: "bash-language-server", args: []string{"start"}, langID: "shellscript"},
	".bash":   {cmd: "bash-language-server", args: []string{"start"}, langID: "shellscript"},
}

// ServerStartedEvent is sent on the event channel at three points in the server
// lifecycle. Phase=="starting" fires immediately before the LSP initialize
// handshake begins (server binary found, goroutine launched). Phase=="ready"
// fires once initialize completes successfully. Phase=="failed" fires when the
// server binary is missing or initialization fails, so the TUI can clear the
// indexing indicator instead of showing it forever.
type ServerStartedEvent struct {
	Cmd    string // binary name, e.g. "gopls"
	LangID string // primary language ID, e.g. "go"
	Root   string // project root path
	Phase  string // "starting" | "ready" | "failed"
	Detail string // optional error detail for "failed" phase
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
	root         string
	mu           sync.Mutex
	sharedBroker bool
	brokerMeta   map[string]broker.Metadata
	clients      map[string]*Client
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
	// Isolated by default — sharing is gated behind the lsp_shared config
	// flag via NewManagerWithShared / NewSharedManager. See
	// docs/architecture/lsp-broker-shared-server-gotchas.md and
	// docs/superpowers/specs/2026-08-20-shared-lsp-broker-design.md:108.
	return newManager(root, false)
}

// NewManagerWithShared constructs a manager with an explicit broker policy.
// This is used by config-aware callers; NewManager preserves the historical
// default of opportunistic sharing.
func NewManagerWithShared(root string, enabled bool) *Manager {
	return newManager(root, enabled)
}

// NewSharedManager opts into the gopls broker path.
func NewSharedManager(root string) *Manager {
	return newManager(root, true)
}

func newManager(root string, sharedBroker bool) *Manager {
	if root == "" {
		root = "."
	}
	m := &Manager{
		root:         root,
		sharedBroker: sharedBroker,
		brokerMeta:   make(map[string]broker.Metadata),
		clients:      make(map[string]*Client),
		openByURI:    make(map[string]string),
		diagnostics:  newDiagnosticStore(),
	}
	if w, err := newFileWatcher(root, m); err == nil {
		m.watcher = w
	}
	return m
}

// AttachBroker records an authenticated broker endpoint for ext. The endpoint
// is consumed by ClientForExt on first use.
func (m *Manager) AttachBroker(ext string, meta broker.Metadata) error {
	if m == nil {
		return fmt.Errorf("nil LSP manager")
	}
	spec, ok := serversByExt[ext]
	if !ok {
		return fmt.Errorf("no language server configured for %q files", ext)
	}
	if meta.Port <= 0 || meta.Token == "" {
		return fmt.Errorf("shared broker attachment for %q requires port and token", ext)
	}
	if meta.Protocol != broker.ProtocolVersion {
		return fmt.Errorf("shared broker attachment for %q has unsupported protocol %d", ext, meta.Protocol)
	}
	canonicalRoot, err := broker.CanonicalRoot(m.root)
	if err != nil {
		return fmt.Errorf("shared broker attachment for %q: resolve manager root: %w", ext, err)
	}
	if meta.Identity.Root != canonicalRoot {
		return fmt.Errorf("shared broker attachment for %q has root %q, want %q", ext, meta.Identity.Root, canonicalRoot)
	}
	if meta.Identity.LanguageID != spec.langID || meta.Identity.Executable == "" {
		return fmt.Errorf("shared broker attachment for %q has incompatible identity", ext)
	}
	m.mu.Lock()
	m.brokerMeta[ext] = meta
	m.mu.Unlock()
	return nil
}

// BrokerAttached reports whether an explicit broker endpoint was recorded for ext.
func (m *Manager) BrokerAttached(ext string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	_, ok := m.brokerMeta[ext]
	m.mu.Unlock()
	return ok
}

// SharedBrokerEnabled reports the construction-time broker policy.
func (m *Manager) SharedBrokerEnabled() bool {
	return m != nil && m.sharedBroker
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

func (m *Manager) installDiagnosticsHandler(ext string, c *Client) {
	store := m.diagnostics
	if store == nil {
		return
	}
	generation := store.Generation()
	cmd := serversByExt[ext].cmd
	c.SetDiagnosticsHandler(func(uri string, diags []Diagnostic) {
		for i := range diags {
			diags[i].ServerCmd = cmd
		}
		store.SetURIIfGeneration(uri, diags, generation)
	})
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
		m.emitReady(spec)
		return c, nil
	}
	meta, attached := m.brokerMeta[ext]
	if !attached && m.sharedBroker {
		m.mu.Unlock()
		if discovered, ok := discoverOrSpawnDaemon(m.root, ext, spec); ok {
			meta, attached = discovered, true
		}
		m.mu.Lock()
	}
	if attached {
		m.mu.Unlock()
		c, err := NewBrokerClient(context.Background(), meta, fmt.Sprintf("manager-%s", strings.TrimPrefix(ext, ".")))
		if err != nil {
			// A stale/dead broker must never make LSP unavailable. Remove only
			// this attachment and continue with the isolated stdio path.
			m.mu.Lock()
			delete(m.brokerMeta, ext)
		} else {
			m.mu.Lock()
			if existing := m.clients[ext]; existing != nil {
				m.mu.Unlock()
				_ = c.Close()
				m.emitReady(spec)
				return existing, nil
			}
			m.installDiagnosticsHandler(ext, c)
			m.clients[ext] = c
			m.mu.Unlock()
			m.emitReady(spec)
			return c, nil
		}
	}
	// Do not hold the manager mutex while starting or initializing a process.
	// Initialization can block on the server and would otherwise serialize all
	// callers (and deadlock callbacks that need manager state).
	m.mu.Unlock()
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
	m.mu.Lock()
	if existing := m.clients[ext]; existing != nil {
		m.mu.Unlock()
		_ = c.Close()
		m.emitReady(spec)
		return existing, nil
	}
	m.installDiagnosticsHandler(ext, c)
	m.clients[ext] = c
	m.mu.Unlock()
	m.emitReady(spec)
	return c, nil
}

// emitReady sends a "ready" ServerStartedEvent for spec, if an event channel
// is registered. Called on every ClientForExt return path (fresh client,
// cached client, or broker-attached client) so the sidebar's indexing
// indicator always clears — previously only the isolated-stdio path emitted
// this, leaving the broker and cached-client paths stuck showing "indexing…"
// forever.
func (m *Manager) emitReady(spec serverSpec) {
	m.mu.Lock()
	eventCh := m.eventCh
	m.mu.Unlock()
	if eventCh == nil {
		return
	}
	evt := ServerStartedEvent{Cmd: spec.cmd, LangID: spec.langID, Root: m.root, Phase: "ready"}
	select {
	case eventCh <- evt:
	default:
	}
}

// ClientForFile is ClientForExt keyed by a file path's extension.
func (m *Manager) ClientForFile(path string) (*Client, error) {
	return m.ClientForExt(filepath.Ext(path))
}

// daemonConnectAttempts/daemonConnectDelay bound how long ClientForExt waits
// for a just-spawned daemon to publish its metadata and accept a connection
// before giving up and falling back to an isolated stdio client. gopls's own
// startup is much slower than this window, but the daemon only needs to bind
// its listener and write metadata before this succeeds — the LSP
// initialize handshake itself happens inside the daemon process, off this
// call's critical path only for the *next* caller, not this one, since this
// call still waits on NewBrokerClient -> Connect, which succeeds as soon as
// the daemon's listener is up (the daemon's own real client Initialize call
// runs beforehand as part of its start() function — see cmd_daemon.go — so a
// slow gopls index does delay the very first ClientForExt call for a fresh
// project, same as today's isolated-client path).
const (
	daemonConnectAttempts = 20
	daemonConnectDelay    = 500 * time.Millisecond
)

// discoverOrSpawnDaemon finds a reachable daemon for (root, ext), spawning
// one (detached, not waited on) if none is reachable, and returns its
// metadata. Returns ok=false on any failure — callers must fall back to an
// isolated stdio client rather than treat this as a hard error, since a
// shared daemon is an optimization, not a requirement.
func discoverOrSpawnDaemon(root, ext string, spec serverSpec) (broker.Metadata, bool) {
	if underTempDir(root) {
		// A daemon's shared identity is keyed on root, so a root inside the
		// OS temp dir (e.g. t.TempDir() in tests) is guaranteed unique per
		// run and can never be reconnected to — spawning one here would
		// only ever produce an orphan detached process. Refuse and let the
		// caller fall back to the isolated stdio client instead.
		log.Printf("lsp: refusing to spawn shared daemon for %s under temp root %q (never shareable)", ext, root)
		return broker.Metadata{}, false
	}
	identity, err := broker.NewIdentity(root, spec.cmd, spec.args, spec.langID, nil, nil)
	if err != nil {
		return broker.Metadata{}, false
	}
	path, err := broker.MetadataPath(identity)
	if err != nil {
		return broker.Metadata{}, false
	}

	if meta, ok := tryConnect(path); ok {
		return meta, true
	}

	if err := spawnDaemonProcess(root, ext); err != nil {
		log.Printf("lsp: spawn shared daemon for %s: %v", ext, err)
		return broker.Metadata{}, false
	}

	for i := 0; i < daemonConnectAttempts; i++ {
		time.Sleep(daemonConnectDelay)
		if meta, ok := tryConnect(path); ok {
			return meta, true
		}
	}
	return broker.Metadata{}, false
}

// underTempDir reports whether root resolves inside the OS temp directory
// (where t.TempDir() and similar throwaway workspaces live). Best-effort:
// on any resolution failure it returns false so callers fall through to the
// normal (safe, just non-deduplicated) spawn path rather than blocking it.
func underTempDir(root string) bool {
	canonicalRoot, err := broker.CanonicalRoot(root)
	if err != nil {
		return false
	}
	tmp, err := broker.CanonicalRoot(os.TempDir())
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(tmp, canonicalRoot)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && rel != "")
}

// tryConnect reads metadata at path and probes it with a single short-lived
// connection, closing it immediately — this call only establishes
// reachability, the real Client connection is made separately by
// NewBrokerClient in ClientForExt.
func tryConnect(path string) (broker.Metadata, bool) {
	meta, err := broker.ReadMetadata(path)
	if err != nil {
		return broker.Metadata{}, false
	}
	conn, err := broker.Connect(context.Background(), meta, "manager-probe", 1, 0)
	if err != nil {
		return broker.Metadata{}, false
	}
	_ = conn.Close()
	return meta, true
}

// spawnDaemonProcess launches `<this executable> lsp-daemon --root=... --ext=...`
// detached from the current process (see detachProcAttr) so it outlives
// this ocode process's exit — the whole point of the daemon is to keep
// serving other ocode processes after the one that spawned it is gone.
// Not waited on: the daemon's own StartOnce/metadata-lock handles startup
// races between multiple ocode processes spawning it concurrently.
func spawnDaemonProcess(root, ext string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve ocode executable: %w", err)
	}
	cmd := exec.Command(exe, "lsp-daemon", "--root="+root, "--ext="+ext)
	// Do not inherit the parent terminal (TUI Output Safety: raw daemon
	// log writes would corrupt the alt-screen). Stdin from empty reader,
	// stdout/stderr discarded; see internal/lsp/daemon.go:144.
	cmd.Stdin = strings.NewReader("")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	detachProcAttr(cmd)
	return cmd.Start()
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
	clients := make([]*Client, 0, len(m.clients))
	for ext, c := range m.clients {
		clients = append(clients, c)
		delete(m.clients, ext)
	}
	m.openByURI = make(map[string]string)
	m.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
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

// AnyServerInstalled reports whether at least one known language-server
// binary is present on PATH. Used to decide whether to register the
// semantic "ast" tool by default (no plugin toggle when LSP is available).
func AnyServerInstalled() bool {
	for _, cmd := range KnownServers() {
		if _, err := exec.LookPath(cmd); err == nil {
			return true
		}
	}
	return false
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
				if eventCh != nil {
					select {
					case eventCh <- ServerStartedEvent{Cmd: s.cmd, LangID: s.langID, Root: root, Phase: "failed", Detail: err.Error()}:
					default:
					}
				}
			}
		}()
	}
}

// installHints maps LSP server binary names to user-friendly install
// instructions. Used by the tool layer to surface actionable notices when
// a server is missing from PATH.
var installHints = map[string]string{
	"gopls":                           "go install golang.org/x/tools/gopls@latest",
	"rust-analyzer":                   "rustup component add rust-analyzer",
	"pyright-langserver":              "npm install -g pyright",
	"typescript-language-server":      "npm install -g typescript typescript-language-server",
	"dart":                            "install the Dart SDK (https://dart.dev/get-dart) or Flutter, which bundles it",
	"intelephense":                    "npm install -g intelephense",
	"jdtls":                           "install Eclipse JDT LS (e.g. brew install jdtls) and ensure a JDK 17+ is on PATH",
	"csharp-ls":                       "dotnet tool install --global csharp-ls",
	"solargraph":                      "gem install solargraph",
	"clangd":                          "install clangd via your package manager (e.g. brew install llvm, apt install clangd)",
	"lua-language-server":             "brew install lua-language-server (or download from https://github.com/LuaLS/lua-language-server)",
	"kotlin-language-server":          "brew install kotlin-language-server (or build from https://github.com/fwcd/kotlin-language-server)",
	"sourcekit-lsp":                   "install the Swift toolchain or Xcode; sourcekit-lsp ships with both",
	"metals":                          "install Coursier, then: coursier install metals",
	"elixir-ls":                       "install ElixirLS (e.g. brew install elixir-ls) and ensure Elixir/Erlang are on PATH",
	"zls":                             "brew install zls (or download from https://github.com/zigtools/zls)",
	"haskell-language-server-wrapper": "install via ghcup: ghcup install hls (https://haskell-language-server.readthedocs.io)",
	"ocamllsp":                        "opam install ocaml-lsp-server",
	"terraform-ls":                    "brew install hashicorp/tap/terraform-ls (or download from https://github.com/hashicorp/terraform-ls)",
	"yaml-language-server":            "npm install -g yaml-language-server",
	"vscode-json-language-server":     "npm install -g vscode-langservers-extracted",
	"bash-language-server":            "npm install -g bash-language-server",
}

// InstallHint returns a human-friendly install command for the given LSP
// server binary, or a generic fallback if no hint is available.
func InstallHint(cmd string) string {
	if hint, ok := installHints[cmd]; ok {
		return hint
	}
	return "check your package manager for a language server that supports this language"
}
