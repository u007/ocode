package cdp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// newTabID mints an opaque id for a spontaneously-opened tab's "tab:<id>"
// stateKey. crypto/rand failure is unrecoverable, mirroring browse.randToken.
func newTabID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("cdp: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// NavEvent is cdp-local nav event (Part 05 maps to browse.NavEvent).
type NavEvent struct {
	StateKey string
	URL      string
	Status   int
	Error    string
}

// TitleEvent is a cdp-local page-title update (maps to browse.TitleEvent).
// Titles change without navigation (JS document.title), so they travel on a
// dedicated channel, never inside NavEvent. URL is the page URL from the same
// targetInfo payload — the SPA applies the title only when it matches the
// surface's current URL (stale-event guard).
type TitleEvent struct {
	StateKey string
	Title    string
	URL      string
}

// NewTabEvent announces a page target Chrome attached to on its own —
// Cmd/Ctrl+click, target="_blank", window.open — rather than one this
// Manager created via Attach. StateKey is freshly minted; the SPA opens a
// background browser tab for it (mirrors a real browser's new-tab-in-
// background behavior for a modified-click).
type NewTabEvent struct {
	StateKey string
	URL      string
}

// ConsoleEvent is delivered to FrameSink.Console.
type ConsoleEvent struct {
	Level string
	Args  []string
	TS    int64
}

// NetworkEvent is delivered to FrameSink.Network.
type NetworkEvent struct {
	RequestID       string // CDP request ID for correlation
	Method          string
	URL             string
	Status          int
	DurationMs      int64
	TS              int64
	Blocked         string
	RequestHeaders  map[string]string // bounded: top headers from request
	ResponseHeaders map[string]string // bounded: top headers from response
	ContentType     string            // shortcut from response headers
	Size            int64             // encoded body length if available
	PostData        string            // bounded request body (capped at maxPostDataLen)
}

// FrameSink receives frames and telemetry for one stateKey.
type FrameSink interface {
	Frame(width, height uint32, jpeg []byte)
	Console(ConsoleEvent)
	Network(NetworkEvent)
	Performance(metrics map[string]float64)
	Error(msg string)
}

// MouseEvent describes a mouse action.
type MouseEvent struct {
	Kind           string // move|down|up|wheel
	X, Y           float64
	Button         string
	Buttons        int // CDP bitmask of buttons held: left=1, right=2, middle=4
	ClickCount     int
	DeltaX, DeltaY float64
	Modifiers      int
}

// KeyEvent describes a keyboard action.
// TouchPoint is one active contact in CSS pixels.
type TouchPoint struct {
	ID   int
	X, Y float64
}

// TouchEvent carries the changed contacts of one Input.dispatchTouchEvent.
type TouchEvent struct {
	Kind      string // start|move|end|cancel
	Points    []TouchPoint
	Modifiers int
}

type KeyEvent struct {
	Kind            string // down|up|char
	Key, Code, Text string
	Modifiers       int
	AutoRepeat      bool
}

var (
	ErrBadScheme            = errors.New("unsupported URL scheme")
	ErrChromeNotFound2      = ErrChromeNotFound
	ErrUnsupportedPlatform2 = ErrUnsupportedPlatform
)

// ManagerOptions configures the Manager.
type ManagerOptions struct {
	ChromePath  string
	IdleTimeout time.Duration
	// ScreencastQuality is the JPEG quality (1-100) for Page.startScreencast.
	// 0 means DefaultScreencastQuality (85): sharper text than the old hardcoded 70.
	ScreencastQuality int
	// HTRExtensionDir is an optional unpacked-extension directory preloaded
	// into ocode's headless Chrome via --load-extension (see chromeArgsFor).
	// Empty preserves the default --disable-extensions behavior. Resolved
	// cross-platform (absolute or ~-relative path); validated at launch.
	HTRExtensionDir   string
	HTRSocketPath     string
	HTRNativeHostName string
	// ProfileDir is Chrome's --user-data-dir. When set it is created and
	// reused across launches so cookies/logins survive an ocode restart.
	// Empty launches with an ephemeral temp profile (removed on exit).
	ProfileDir string
	Supervisor *tool.ProcessSupervisor
	Dialer     *net.Dialer
	EmitNav    func(NavEvent)
	EmitTitle  func(TitleEvent)
	EmitNewTab func(NewTabEvent)
	Log        *log.Logger
}

// DefaultScreencastQuality is the CDP screencast JPEG quality used when
// ManagerOptions.ScreencastQuality is 0.
const DefaultScreencastQuality = 85

// Manager owns the single Chrome process and per-stateKey targets.
type Manager struct {
	opts ManagerOptions

	mu sync.Mutex
	// launchMu serializes concurrent ensureChrome callers so two Attach
	// requests that both find m.conn == nil do not launch two unmanaged
	// Chrome processes. The second caller blocks until the first launch
	// completes, then reuses the newly-established connection.
	launchMu sync.Mutex

	// Chrome process state
	conn    *Conn
	exited  <-chan int
	cleanup func()
	proxy   *EgressProxy

	// launch injection for tests
	launchFn func(context.Context) (*Conn, <-chan int, func(), error)

	targets map[string]*Target
	// trusted holds hosts the user explicitly accepted a bad certificate
	// for ("Continue anyway"), per stateKey. Kept on the manager, not the
	// target, so a revoke/re-attach cycle does not forget the decision.
	trusted map[string]map[string]bool

	// pending maps a freshly created Chrome targetID to its stateKey between
	// Target.attachToTarget and the m.targets[stateKey] insert at the end of
	// Attach. Browser-level events (attachedToTarget, targetInfoChanged) can
	// arrive in that window; without this map their titles would be dropped.
	// Guarded by mu. Entries are removed on insert, Revoke, and target crash.
	pending map[string]string

	// for "log once" on chrome not found
	chromeNotFoundLogged bool

	closed bool

	idleTimer *time.Timer

	// browser-level subscription cancel
	browserCancel []func()
}

// NewManager creates a Manager. Does not launch Chrome.
func NewManager(opts ManagerOptions) *Manager {
	m := &Manager{
		opts:    opts,
		targets: make(map[string]*Target),
		pending: make(map[string]string),
	}
	m.launchFn = m.defaultLaunch
	return m
}

// SetLauncher overrides the launcher (test only).
func (m *Manager) SetLauncher(fn func(context.Context) (*Conn, <-chan int, func(), error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.launchFn = fn
}

// effectiveQuality returns the screencast JPEG quality clamped to Chrome's
// accepted 1-100 range, mapping 0 (unset) to DefaultScreencastQuality.
func (m *Manager) effectiveQuality() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return normalizeQuality(m.opts.ScreencastQuality)
}

func normalizeQuality(q int) int {
	if q == 0 {
		return DefaultScreencastQuality
	}
	if q < 1 {
		return 1
	}
	if q > 100 {
		return 100
	}
	return q
}

// SetScreencastQuality updates the screencast JPEG quality live: existing
// targets pick it up on their next restartScreencast (resize, zoom, or
// re-attach). Out-of-range values are clamped, 0 restores the default.
func (m *Manager) SetScreencastQuality(q int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if q == 0 {
		m.opts.ScreencastQuality = DefaultScreencastQuality
		return
	}
	m.opts.ScreencastQuality = normalizeQuality(q)
}

// ensureChrome launches Chrome if not already running.
func (m *Manager) ensureChrome(ctx context.Context) error {
	m.launchMu.Lock()
	defer m.launchMu.Unlock()

	m.mu.Lock()
	if m.conn != nil {
		// Check if conn is still alive (Done not closed)
		select {
		case <-m.conn.Done():
			// died, need relaunch
		default:
			m.mu.Unlock()
			return nil
		}
	}
	m.mu.Unlock()

	// Find chrome binary (for real launcher); stub launcher bypasses this.
	// For stub tests, FindChrome failure is simulated via launchFn returning ErrChromeNotFound.
	// We still need to handle ErrChromeNotFound from launchFn.
	conn, exited, cleanup, err := m.launchFn(ctx)
	if err != nil {
		// Handle not-found / unsupported specially: emit nav and log once.
		if errors.Is(err, ErrChromeNotFound) || errors.Is(err, ErrUnsupportedPlatform) {
			m.mu.Lock()
			shouldLog := !m.chromeNotFoundLogged
			m.chromeNotFoundLogged = true
			m.mu.Unlock()
			if shouldLog && m.opts.Log != nil {
				m.opts.Log.Printf("chrome not found: %v", err)
			}
		}
		return err
	}

	m.mu.Lock()
	m.conn = conn
	m.exited = exited
	m.cleanup = cleanup
	// Create egress proxy lazily if not exists.
	if m.proxy == nil {
		p, perr := NewEgressProxy(m.opts.Dialer)
		if perr != nil {
			m.mu.Unlock()
			// cleanup chrome
			_ = conn.Close()
			if cleanup != nil {
				cleanup()
			}
			return perr
		}
		m.proxy = p
	}
	// Start browser-level event handling + exit watcher.
	m.startBrowserHandlersLocked()
	m.mu.Unlock()

	// Watch for exit
	go m.watchExited(exited)

	return nil
}

func (m *Manager) defaultLaunch(ctx context.Context) (*Conn, <-chan int, func(), error) {
	path, err := FindChrome(m.opts.ChromePath)
	if err != nil {
		return nil, nil, nil, err
	}
	return launchChromeWithOptions(ctx, path, m.opts.Supervisor, m.opts.Log, resolveExtensionDir(m.opts.HTRExtensionDir), m.opts.HTRSocketPath, m.opts.HTRNativeHostName, m.opts.ProfileDir)
}

func (m *Manager) watchExited(exited <-chan int) {
	_, ok := <-exited
	if !ok {
		// channel closed without value — treat as exit
	}
	m.handleChromeExit()
}

func (m *Manager) handleChromeExit() {
	m.mu.Lock()
	// Collect targets to notify
	targets := make([]*Target, 0, len(m.targets))
	for _, t := range m.targets {
		targets = append(targets, t)
	}
	// Clear state so next Attach relaunches
	if m.cleanup != nil {
		cfn := m.cleanup
		m.cleanup = nil
		// run cleanup outside lock to avoid deadlock
		m.mu.Unlock()
		cfn()
		m.mu.Lock()
	}
	m.conn = nil
	m.exited = nil
	// Do not clear proxy — keep it for next launch? Spec says Close closes proxy.
	// Keep proxy alive across relaunches; only Close() closes it.

	// Notify sinks
	for _, t := range targets {
		if t.sink != nil {
			t.sink.Error("chrome exited")
		}
		if m.opts.EmitNav != nil {
			m.opts.EmitNav(NavEvent{StateKey: t.stateKey, Error: "chrome exited"})
		}
	}
	// Remove targets? Spec: chrome crashes → all targets get error; next Attach creates fresh.
	// We keep map entries but mark them as crashed so next Attach replaces?
	// Simpler: clear map so next Attach creates new.
	m.targets = make(map[string]*Target)
	m.pending = make(map[string]string)
	// Cancel browser subs
	for _, fn := range m.browserCancel {
		fn()
	}
	m.browserCancel = nil
	m.mu.Unlock()
}

func (m *Manager) startBrowserHandlersLocked() {
	if m.conn == nil {
		return
	}
	conn := m.conn
	ch1, cancel1 := conn.Subscribe("", "Target.targetCrashed")
	ch2, cancel2 := conn.Subscribe("", "Target.attachedToTarget")
	ch3, cancel3 := conn.Subscribe("", "Target.targetInfoChanged")
	ch4, cancel4 := conn.Subscribe("", "Target.targetCreated")
	ch5, cancel5 := conn.Subscribe("", "Target.targetDestroyed")
	m.browserCancel = append(m.browserCancel, cancel1, cancel2, cancel3, cancel4, cancel5)
	go m.handleTargetCrashed(ch1, conn)
	go m.handleAttachedToTarget(ch2, conn)
	go m.handleTargetInfoChanged(ch3)
	go m.handleTargetCreated(ch4, conn)
	go m.handleTargetDestroyed(ch5)
	// Page-level setAutoAttach (Attach step 4) only covers a page's own
	// frames and workers. A popup — window.open, target="_blank",
	// middle-click — is a sibling page with openerId set, and Chrome never
	// auto-attaches to it, so discover targets at the browser level and
	// attach to opened pages ourselves (handleTargetCreated).
	go func() {
		_ = conn.Call(context.Background(), "", "Target.setDiscoverTargets", map[string]any{"discover": true}, nil)
	}()
	go func(c *Conn) {
		<-c.Done()
		m.handleChromeExit()
	}(conn)
}

func (m *Manager) handleTargetCrashed(ch <-chan json.RawMessage, conn *Conn) {
	for raw := range ch {
		var ev struct {
			TargetID  string `json:"targetId"`
			Status    string `json:"status"`
			ErrorCode int    `json:"errorCode"`
		}
		_ = json.Unmarshal(raw, &ev)
		m.mu.Lock()
		var matched *Target
		var key string
		for k, t := range m.targets {
			if t.targetID == ev.TargetID {
				matched = t
				key = k
				break
			}
		}
		m.mu.Unlock()
		if matched != nil {
			if matched.sink != nil {
				matched.sink.Error("target crashed")
			}
			if m.opts.EmitNav != nil {
				m.opts.EmitNav(NavEvent{StateKey: key, Error: "target crashed"})
			}
			m.mu.Lock()
			delete(m.targets, key)
			delete(m.pending, ev.TargetID)
			m.mu.Unlock()
			matched.stopHandlers()
			_ = conn.Call(context.Background(), "", "Target.closeTarget", map[string]string{"targetId": ev.TargetID}, nil)
			if !matched.sharedContext {
				_ = conn.Call(context.Background(), "", "Target.disposeBrowserContext", map[string]string{"browserContextId": matched.browserContextID}, nil)
			}
		}
	}
}

// handleTargetDestroyed drops a tab whose page went away underneath us —
// window.close(), a popup closing itself, Chrome tearing the target down.
// Without this the Target kept its dead sessionID, every command to it
// ran into the default deadline, and the SPA showed a frozen tab. Targets
// we close ourselves (Revoke) are already out of m.targets by the time the
// event arrives, so they do not match here.
func (m *Manager) handleTargetDestroyed(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(raw, &ev)
		m.mu.Lock()
		var matched *Target
		var key string
		for k, t := range m.targets {
			if t.targetID == ev.TargetID {
				matched = t
				key = k
				break
			}
		}
		if matched != nil {
			delete(m.targets, key)
		}
		delete(m.pending, ev.TargetID)
		m.mu.Unlock()
		if matched == nil {
			continue
		}
		matched.stopHandlers()
		matched.Detach()
		matched.releaseTopLevelHost()
		if m.opts.EmitNav != nil {
			m.opts.EmitNav(NavEvent{StateKey: key, Error: "tab closed"})
		}
	}
}

func (m *Manager) handleAttachedToTarget(ch <-chan json.RawMessage, conn *Conn) {
	for raw := range ch {
		var ev struct {
			SessionID          string `json:"sessionId"`
			WaitingForDebugger bool   `json:"waitingForDebugger"`
			TargetInfo         struct {
				TargetID         string `json:"targetId"`
				Type             string `json:"type"`
				Title            string `json:"title"`
				URL              string `json:"url"`
				BrowserContextID string `json:"browserContextId"`
			} `json:"targetInfo"`
		}
		_ = json.Unmarshal(raw, &ev)
		if ev.WaitingForDebugger {
			_ = conn.Call(context.Background(), ev.SessionID, "Runtime.runIfWaitingForDebugger", nil, nil)
		}
		m.emitTargetTitle(ev.TargetInfo.TargetID, ev.TargetInfo.Type, ev.TargetInfo.Title, ev.TargetInfo.URL)
		m.maybeRegisterNewTab(ev.TargetInfo.TargetID, ev.TargetInfo.Type, ev.TargetInfo.URL, ev.TargetInfo.BrowserContextID, ev.SessionID, conn)
	}
}

// handleTargetCreated attaches to page targets Chrome opened on behalf of
// one of ours and registers them as new tabs. Two shapes: window.open /
// target="_blank" carry openerId; a browser-initiated open (middle-click,
// Cmd/Ctrl+click) carries no opener but lands in the opener's browser
// context. Targets this Manager creates via Attach always get a fresh
// browser context first, so they match neither test and are ignored here,
// as are Chrome's own internal targets (browser_ui, service_worker, ...).
func (m *Manager) handleTargetCreated(ch <-chan json.RawMessage, conn *Conn) {
	for raw := range ch {
		var ev struct {
			TargetInfo struct {
				TargetID         string `json:"targetId"`
				Type             string `json:"type"`
				URL              string `json:"url"`
				OpenerID         string `json:"openerId"`
				Attached         bool   `json:"attached"`
				BrowserContextID string `json:"browserContextId"`
			} `json:"targetInfo"`
		}
		_ = json.Unmarshal(raw, &ev)
		ti := ev.TargetInfo
		if ti.Type != "page" || ti.Attached {
			continue
		}
		if ti.OpenerID == "" && !m.ownsBrowserContext(ti.BrowserContextID) {
			continue
		}
		var res struct {
			SessionID string `json:"sessionId"`
		}
		if err := conn.Call(context.Background(), "", "Target.attachToTarget", map[string]any{"targetId": ti.TargetID, "flatten": true}, &res); err != nil {
			continue
		}
		m.maybeRegisterNewTab(ti.TargetID, ti.Type, ti.URL, ti.BrowserContextID, res.SessionID, conn)
	}
}

// ownsBrowserContext reports whether a live target already lives in bcID.
func (m *Manager) ownsBrowserContext(bcID string) bool {
	if bcID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.targets {
		if t.browserContextID == bcID {
			return true
		}
	}
	return false
}

// maybeRegisterNewTab turns a spontaneous "page" target — one Chrome
// attached to on its own — into a fresh browsable tab. Attach() records its
// own targetID in m.pending before Chrome can echo the matching
// attachedToTarget event back at us, so a pending (or already-registered)
// targetID means this event is just the tail of a normal Attach() and is
// left alone; only a targetID neither tracks is a genuine unsolicited tab
// (Cmd/Ctrl+click, target="_blank", window.open).
func (m *Manager) maybeRegisterNewTab(targetID, targetType, url, browserContextID, sessionID string, conn *Conn) {
	if targetType != "page" || targetID == "" || sessionID == "" {
		return
	}
	m.mu.Lock()
	if _, pending := m.pending[targetID]; pending {
		m.mu.Unlock()
		return
	}
	for _, t := range m.targets {
		if t.targetID == targetID {
			m.mu.Unlock()
			return
		}
	}
	stateKey := "tab:" + newTabID()
	m.pending[targetID] = stateKey
	m.mu.Unlock()

	// Same domain set Attach() enables for a target it creates itself. No
	// screencast yet — that starts lazily (Attach's "existing target" branch)
	// only once a viewer actually opens this tab.
	_ = conn.Call(context.Background(), sessionID, "Page.enable", nil, nil)
	_ = conn.Call(context.Background(), sessionID, "Runtime.enable", nil, nil)
	_ = conn.Call(context.Background(), sessionID, "Network.enable", nil, nil)
	_ = conn.Call(context.Background(), sessionID, "Performance.enable", nil, nil)
	m.injectInPageSelectPicker(context.Background(), conn, sessionID)

	// A popup is discovered at creation with an empty URL and usually has
	// already issued its document request by the time Network is enabled
	// above, so no responseReceived (and no NavEvent) will follow for it.
	// Ask Chrome where the target is now; if the navigation is still in
	// flight this stays empty and the Network handlers report it normally.
	if url == "" {
		var info struct {
			TargetInfo struct {
				URL string `json:"url"`
			} `json:"targetInfo"`
		}
		_ = conn.Call(context.Background(), "", "Target.getTargetInfo", map[string]string{"targetId": targetID}, &info)
		url = info.TargetInfo.URL
	}

	t := &Target{
		manager:          m,
		stateKey:         stateKey,
		browserContextID: browserContextID,
		targetID:         targetID,
		sessionID:        sessionID,
		conn:             conn,
		sharedContext:    true,
	}
	t.perfRecording = true
	t.startHandlers()
	t.startFileChooser()

	m.mu.Lock()
	m.targets[stateKey] = t
	delete(m.pending, targetID)
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
	m.mu.Unlock()

	if m.opts.EmitNewTab != nil {
		m.opts.EmitNewTab(NewTabEvent{StateKey: stateKey, URL: url})
	}
	if url != "" && url != "about:blank" {
		m.emitNav(NavEvent{StateKey: stateKey, URL: url, Status: 200})
	}
}

// handleTargetInfoChanged forwards page-title updates (including JS
// document.title changes, which fire no navigation event) to the owning
// stateKey. Child targets (iframes — type "iframe") are ignored so embedded
// frames can never overwrite the tab title.
func (m *Manager) handleTargetInfoChanged(ch <-chan json.RawMessage) {
	for raw := range ch {
		var ev struct {
			TargetInfo struct {
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
				Title    string `json:"title"`
				URL      string `json:"url"`
			} `json:"targetInfo"`
		}
		_ = json.Unmarshal(raw, &ev)
		m.emitTargetTitle(ev.TargetInfo.TargetID, ev.TargetInfo.Type, ev.TargetInfo.Title, ev.TargetInfo.URL)
	}
}

// emitTargetTitle resolves a Chrome targetID to its stateKey (registered
// targets first, then the Attach pending window) and invokes EmitTitle
// outside the manager lock. Unknown targetIDs (stale events after Revoke)
// and non-page types are dropped silently.
func (m *Manager) emitTargetTitle(targetID, targetType, title, url string) {
	if targetID == "" || targetType != "page" {
		return
	}
	m.mu.Lock()
	var key string
	for k, t := range m.targets {
		if t.targetID == targetID {
			key = k
			break
		}
	}
	if key == "" {
		key = m.pending[targetID]
	}
	m.mu.Unlock()
	if key == "" {
		return
	}
	m.emitTitle(TitleEvent{StateKey: key, Title: title, URL: url})
}

// Attach lazily launches Chrome, creates context+target for the key if absent,
// replaces sink, restarts screencast.
func (m *Manager) Attach(ctx context.Context, stateKey string, sink FrameSink) (*Target, error) {
	if err := m.ensureChrome(ctx); err != nil {
		if errors.Is(err, ErrChromeNotFound) || errors.Is(err, ErrUnsupportedPlatform) {
			if m.opts.EmitNav != nil {
				m.opts.EmitNav(NavEvent{StateKey: stateKey, Error: err.Error()})
			}
		}
		return nil, err
	}

	m.mu.Lock()
	if existing, ok := m.targets[stateKey]; ok {
		// Replace sink
		existing.mu.Lock()
		existing.sink = sink
		existing.mu.Unlock()
		// Restart screencast
		m.mu.Unlock()
		_ = existing.restartScreencast(ctx)
		return existing, nil
	}
	m.mu.Unlock()

	// Create new target: need proxyServer URL. Chrome does not honor userinfo
	// in proxyServer, so the Fetch.authRequired handler (startHandlers) answers
	// 407 challenges with these credentials.
	m.mu.Lock()
	proxyURL := ""
	var proxyUser, proxyPass string
	if m.proxy != nil {
		proxyURL = m.proxy.ProxyServerURL()
		proxyUser, proxyPass = m.proxy.UserPass()
	}
	conn := m.conn
	m.mu.Unlock()
	if conn == nil {
		return nil, errors.New("chrome not running")
	}

	// 1. createBrowserContext
	var bcRes struct {
		BrowserContextID string `json:"browserContextId"`
	}
	err := conn.Call(ctx, "", "Target.createBrowserContext", map[string]string{
		"proxyServer":     proxyURL,
		"proxyBypassList": "<-loopback>",
	}, &bcRes)
	if err != nil {
		return nil, err
	}
	bcID := bcRes.BrowserContextID
	if bcID == "" {
		bcID = "ctx-auto"
	}
	// 2. createTarget
	var tgtRes struct {
		TargetID string `json:"targetId"`
	}
	err = conn.Call(ctx, "", "Target.createTarget", map[string]string{
		"url":              "about:blank",
		"browserContextId": bcID,
	}, &tgtRes)
	if err != nil {
		_ = conn.Call(context.Background(), "", "Target.disposeBrowserContext", map[string]string{"browserContextId": bcID}, nil)
		return nil, err
	}
	targetID := tgtRes.TargetID

	// 3. attachToTarget
	var attRes struct {
		SessionID string `json:"sessionId"`
	}
	err = conn.Call(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	}, &attRes)
	if err != nil {
		_ = conn.Call(context.Background(), "", "Target.closeTarget", map[string]string{"targetId": targetID}, nil)
		_ = conn.Call(context.Background(), "", "Target.disposeBrowserContext", map[string]string{"browserContextId": bcID}, nil)
		return nil, err
	}
	sessionID := attRes.SessionID

	// Register the targetID early: attachedToTarget/targetInfoChanged can
	// arrive before the m.targets insert below. Removed on insert/Revoke.
	m.mu.Lock()
	m.pending[targetID] = stateKey
	m.mu.Unlock()

	// 4. Enable domains and auto-attach
	_ = conn.Call(ctx, sessionID, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": false, "flatten": true}, nil)
	_ = conn.Call(ctx, sessionID, "Page.enable", nil, nil)
	_ = conn.Call(ctx, sessionID, "Runtime.enable", nil, nil)
	_ = conn.Call(ctx, sessionID, "Network.enable", nil, nil)
	_ = conn.Call(ctx, sessionID, "Performance.enable", nil, nil)
	m.injectInPageSelectPicker(ctx, conn, sessionID)

	t := &Target{
		manager:          m,
		stateKey:         stateKey,
		proxyUser:        proxyUser,
		proxyPass:        proxyPass,
		browserContextID: bcID,
		targetID:         targetID,
		sessionID:        sessionID,
		sink:             sink,
		conn:             conn,
	}
	// The Performance domain was enabled above, so recording starts on.
	// PerfStop/PerfStart toggle it per socket command; the flag is the
	// authoritative state re-sent to each newly attached socket.
	t.perfRecording = true
	// Setup per-target event handlers + screencast
	t.startHandlers()
	t.startFileChooser()
	_ = t.restartScreencast(ctx)

	m.mu.Lock()
	m.targets[stateKey] = t
	delete(m.pending, targetID)
	// Cancel idle timer if any
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
	m.mu.Unlock()

	// Best-effort initial title: about:blank has none, but a restored tab may
	// navigate immediately after Attach; the query covers titles set before
	// the first targetInfoChanged arrives.
	m.emitInitialTitle(conn, targetID, stateKey)

	return t, nil
}

// emitInitialTitle queries Target.getTargetInfo once and forwards a page
// title when present. Failures are silent — the event stream is the live
// source of truth.
func (m *Manager) emitInitialTitle(conn *Conn, targetID, stateKey string) {
	if conn == nil || targetID == "" {
		return
	}
	var res struct {
		TargetInfo struct {
			Type  string `json:"type"`
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"targetInfo"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Call(ctx, "", "Target.getTargetInfo", map[string]string{"targetId": targetID}, &res); err != nil {
		return
	}
	if res.TargetInfo.Type != "page" || res.TargetInfo.Title == "" {
		return
	}
	m.emitTitle(TitleEvent{StateKey: stateKey, Title: res.TargetInfo.Title, URL: res.TargetInfo.URL})
}

// ErrNoTarget is returned by SetFiles when stateKey has no attached target.
var ErrNoTarget = errors.New("no chrome target for state key")

// SetFiles forwards uploaded file paths to stateKey's pending file chooser.
func (m *Manager) SetFiles(ctx context.Context, stateKey string, paths []string) error {
	m.mu.Lock()
	t, ok := m.targets[stateKey]
	m.mu.Unlock()
	if !ok {
		return ErrNoTarget
	}
	return t.SetFiles(ctx, paths)
}

// TrustHost records that the user accepted host's untrusted certificate for
// stateKey (host is "name" or "name:port", as the SPA reports it) and, when
// that tab is already on the host, tells Chrome to stop enforcing it.
func (m *Manager) TrustHost(ctx context.Context, stateKey, host string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	m.mu.Lock()
	if m.trusted == nil {
		m.trusted = make(map[string]map[string]bool)
	}
	if m.trusted[stateKey] == nil {
		m.trusted[stateKey] = make(map[string]bool)
	}
	m.trusted[stateKey][host] = true
	t, ok := m.targets[stateKey]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return t.applyCertPolicy(ctx)
}

func (m *Manager) isTrusted(stateKey, hostname, hostport string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	hosts := m.trusted[stateKey]
	return hosts[hostname] || hosts[hostport]
}

// Revoke closes target + disposes context; no-op if absent.
func (m *Manager) Revoke(stateKey string) {
	m.mu.Lock()
	t, ok := m.targets[stateKey]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.targets, stateKey)
	delete(m.trusted, stateKey)
	delete(m.pending, t.targetID)
	needIdle := len(m.targets) == 0 && m.opts.IdleTimeout > 0
	conn := m.conn
	m.mu.Unlock()

	// Stop screencast and close target
	t.Detach()
	t.stopHandlers()
	t.releaseTopLevelHost()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn != nil {
		_ = conn.Call(ctx, "", "Target.closeTarget", map[string]string{"targetId": t.targetID}, nil)
		if !t.sharedContext {
			_ = conn.Call(ctx, "", "Target.disposeBrowserContext", map[string]string{"browserContextId": t.browserContextID}, nil)
		}
	}

	m.mu.Lock()
	if needIdle {
		if m.idleTimer != nil {
			m.idleTimer.Stop()
		}
		timeout := m.opts.IdleTimeout
		m.idleTimer = time.AfterFunc(timeout, func() {
			m.mu.Lock()
			conn := m.conn
			cfn := m.cleanup
			should := len(m.targets) == 0 && m.conn != nil && m.cleanup != nil
			if should {
				m.cleanup = nil
				m.conn = nil
				m.exited = nil
			}
			m.mu.Unlock()
			if should {
				bctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				_ = conn.Call(bctx, "", "Browser.close", nil, nil)
				cancel()
				cfn()
			}
		})
	}
	m.mu.Unlock()
}

// Close shuts down Chrome via Browser.close, waits for exit bounded by ctx, closes proxy, removes temp dir (via cleanup).
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
	conn := m.conn
	exited := m.exited
	cleanup := m.cleanup
	proxy := m.proxy
	m.cleanup = nil
	m.conn = nil
	m.exited = nil
	m.proxy = nil
	m.targets = make(map[string]*Target)
	m.mu.Unlock()

	if conn != nil {
		_ = conn.Call(ctx, "", "Browser.close", nil, nil)
		_ = conn.Close()
	}
	if exited != nil {
		waitCtx := ctx
		var cancel context.CancelFunc
		if _, ok := ctx.Deadline(); !ok {
			waitCtx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancel()
		}
		select {
		case <-exited:
		case <-waitCtx.Done():
		}
	}
	if cleanup != nil {
		cleanup()
	}
	if proxy != nil {
		_ = proxy.Close()
	}
	return nil
}

func (m *Manager) emitNav(ev NavEvent) {
	if m.opts.EmitNav != nil {
		m.opts.EmitNav(ev)
	}
}

func (m *Manager) emitTitle(ev TitleEvent) {
	if m.opts.EmitTitle != nil {
		m.opts.EmitTitle(ev)
	}
}

// EmitTitleForTest fires the configured EmitTitle closure exactly as
// emitTargetTitle would (payload passthrough, no lock held by the caller).
func (m *Manager) EmitTitleForTest(ev TitleEvent) { m.emitTitle(ev) }

// TargetKeys lists the stateKeys with a live Chrome target.
func (m *Manager) TargetKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.targets))
	for k := range m.targets {
		keys = append(keys, k)
	}
	return keys
}

// TargetPerf snapshots every target's Performance metrics, keyed by stateKey.
// JSHeapUsedSize backs the Processes tab memory column; TaskDuration is
// available for CPU attribution. Targets without samples yet are absent.
func (m *Manager) TargetPerf() map[string]map[string]float64 {
	m.mu.Lock()
	targets := make(map[string]*Target, len(m.targets))
	for k, t := range m.targets {
		targets[k] = t
	}
	m.mu.Unlock()
	out := make(map[string]map[string]float64, len(targets))
	for k, t := range targets {
		out[k] = t.PerfSnapshot()
	}
	return out
}

// RendererProcess is one Chrome renderer from SystemInfo.getProcessInfo.
type RendererProcess struct {
	PID     int32
	CPUTime float64 // cumulative seconds of CPU time
	Title   string  // page title/URL as reported by Chrome
}

// RendererProcesses queries the browser-level SystemInfo.getProcessInfo and
// returns the renderer-type processes. Best-effort: older Chrome builds may
// lack the method (nil error, empty result — callers fall back to JS heap).
func (m *Manager) RendererProcesses(ctx context.Context) ([]RendererProcess, error) {
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()
	if conn == nil {
		return nil, nil
	}
	var res struct {
		ProcessInfo []struct {
			ID      string  `json:"id"`
			Type    string  `json:"type"`
			Title   string  `json:"title"`
			CPUTime float64 `json:"cpuTime"`
		} `json:"processInfo"`
	}
	if err := conn.Call(ctx, "", "SystemInfo.getProcessInfo", nil, &res); err != nil {
		return nil, nil
	}
	var out []RendererProcess
	for _, pi := range res.ProcessInfo {
		if pi.Type != "renderer" {
			continue
		}
		var pid int32
		if n, err := strconv.Atoi(pi.ID); err == nil {
			pid = int32(n)
		}
		out = append(out, RendererProcess{PID: pid, CPUTime: pi.CPUTime, Title: pi.Title})
	}
	return out, nil
}

// EmitNavForTest fires the configured EmitNav closure exactly as emitNav
// would. Package consumers (internal/browse) use it to pin the adapter
// contract — payload passthrough plus the chrome-mode stamp — without
// launching Chrome.
func (m *Manager) EmitNavForTest(ev NavEvent) { m.emitNav(ev) }

// isHTTPSScheme checks http/https
func isHTTPSScheme(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// inPageSelectPickerScript switches every <select> to Chrome's customizable
// select (appearance: base-select) so its picker renders in the page's top
// layer. Headless Chrome draws the native <select> popup as a separate OS
// widget: the screencast never shows it and Input.dispatchKeyEvent on the
// page session never reaches it, so clicking a dropdown or pressing
// ArrowDown appeared to do nothing. With base-select the picker is part of
// the frame and mouse/arrow/Enter work as in-page DOM events. Uses
// adoptedStyleSheets because the script runs before <head> exists.
//
// The second rule hides the date/time picker icon: its calendar/clock popup
// is the same kind of invisible off-frame widget, and opening then dismissing
// it can freeze the page session for ~10s. Without the icon the value is
// still editable per segment (click a segment, ArrowUp/Down or type digits).
const inPageSelectPickerScript = `(() => {
	try {
		const s = new CSSStyleSheet();
		s.replaceSync(
			'select, ::picker(select) { appearance: base-select; }' +
			'input::-webkit-calendar-picker-indicator { display: none; }');
		document.adoptedStyleSheets = [...document.adoptedStyleSheets, s];
	} catch (e) {
		console.warn('ocode: in-page select picker unavailable', e);
	}
})();`

// injectInPageSelectPicker registers inPageSelectPickerScript for every
// document (all frames) of the session, including one already loaded
// (runImmediately). Failure is non-fatal: the tab still works, only native
// <select> dropdowns stay invisible.
func (m *Manager) injectInPageSelectPicker(ctx context.Context, conn *Conn, sessionID string) {
	if err := conn.Call(ctx, sessionID, "Page.addScriptToEvaluateOnNewDocument", map[string]any{
		"source": inPageSelectPickerScript, "runImmediately": true,
	}, nil); err != nil && m.opts.Log != nil {
		m.opts.Log.Printf("browse: addScriptToEvaluateOnNewDocument (select picker) failed: %v", err)
	}
}
