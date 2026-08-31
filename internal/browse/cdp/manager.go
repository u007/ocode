package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// NavEvent is cdp-local nav event (Part 05 maps to browse.NavEvent).
type NavEvent struct {
	StateKey string
	URL      string
	Status   int
	Error    string
}

// ConsoleEvent is delivered to FrameSink.Console.
type ConsoleEvent struct {
	Level string
	Args  []string
	TS    int64
}

// NetworkEvent is delivered to FrameSink.Network.
type NetworkEvent struct {
	Method     string
	URL        string
	Status     int
	DurationMs int64
	TS         int64
	Blocked    string
}

// FrameSink receives frames and telemetry for one stateKey.
type FrameSink interface {
	Frame(width, height uint32, jpeg []byte)
	Console(ConsoleEvent)
	Network(NetworkEvent)
	Error(msg string)
}

// MouseEvent describes a mouse action.
type MouseEvent struct {
	Kind       string // move|down|up|wheel
	X, Y       float64
	Button     string
	ClickCount int
	DeltaX, DeltaY float64
	Modifiers  int
}

// KeyEvent describes a keyboard action.
type KeyEvent struct {
	Kind      string // down|up|char
	Key, Code, Text string
	Modifiers int
}

var (
	ErrBadScheme           = errors.New("unsupported URL scheme")
	ErrChromeNotFound2     = ErrChromeNotFound
	ErrUnsupportedPlatform2 = ErrUnsupportedPlatform
)

// ManagerOptions configures the Manager.
type ManagerOptions struct {
	ChromePath  string
	IdleTimeout time.Duration
	Supervisor  *tool.ProcessSupervisor
	Dialer      *net.Dialer
	EmitNav     func(NavEvent)
	Log         *log.Logger
}

// Manager owns the single Chrome process and per-stateKey targets.
type Manager struct {
	opts ManagerOptions

	mu sync.Mutex

	// Chrome process state
	conn    *Conn
	exited  <-chan int
	cleanup func()
	proxy   *EgressProxy

	// launch injection for tests
	launchFn func(context.Context) (*Conn, <-chan int, func(), error)

	targets map[string]*Target

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

// ensureChrome launches Chrome if not already running.
func (m *Manager) ensureChrome(ctx context.Context) error {
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
	return launchChrome(ctx, path, m.opts.Supervisor, m.opts.Log)
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
	m.browserCancel = append(m.browserCancel, cancel1, cancel2)
	go m.handleTargetCrashed(ch1, conn)
	go m.handleAttachedToTarget(ch2, conn)
	go func(c *Conn) {
		<-c.Done()
		m.handleChromeExit()
	}(conn)
}

func (m *Manager) handleTargetCrashed(ch <-chan json.RawMessage, conn *Conn) {
	for raw := range ch {
		var ev struct {
			TargetID string `json:"targetId"`
			Status   string `json:"status"`
			ErrorCode int   `json:"errorCode"`
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
			m.mu.Unlock()
			_ = conn.Call(context.Background(), "", "Target.closeTarget", map[string]string{"targetId": ev.TargetID}, nil)
			_ = conn.Call(context.Background(), "", "Target.disposeBrowserContext", map[string]string{"browserContextId": matched.browserContextID}, nil)
		}
	}
}

func (m *Manager) handleAttachedToTarget(ch <-chan json.RawMessage, conn *Conn) {
	for raw := range ch {
		var ev struct {
			SessionID          string `json:"sessionId"`
			WaitingForDebugger bool   `json:"waitingForDebugger"`
		}
		_ = json.Unmarshal(raw, &ev)
		if ev.WaitingForDebugger {
			_ = conn.Call(context.Background(), ev.SessionID, "Runtime.runIfWaitingForDebugger", nil, nil)
		}
	}
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

	// Create new target: need proxyServer URL
	m.mu.Lock()
	proxyURL := ""
	if m.proxy != nil {
		proxyURL = m.proxy.ProxyServerURL()
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

	// 4. Enable domains and auto-attach
	_ = conn.Call(ctx, sessionID, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": false, "flatten": true}, nil)
	_ = conn.Call(ctx, sessionID, "Page.enable", nil, nil)
	_ = conn.Call(ctx, sessionID, "Runtime.enable", nil, nil)
	_ = conn.Call(ctx, sessionID, "Network.enable", nil, nil)

	t := &Target{
		manager:          m,
		stateKey:         stateKey,
		browserContextID: bcID,
		targetID:         targetID,
		sessionID:        sessionID,
		sink:             sink,
		conn:             conn,
	}
	// Setup per-target event handlers + screencast
	t.startHandlers()
	_ = t.restartScreencast(ctx)

	m.mu.Lock()
	m.targets[stateKey] = t
	// Cancel idle timer if any
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
	m.mu.Unlock()

	return t, nil
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
	needIdle := len(m.targets) == 0 && m.opts.IdleTimeout > 0
	conn := m.conn
	m.mu.Unlock()

	// Stop screencast and close target
	t.Detach()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if conn != nil {
		_ = conn.Call(ctx, "", "Target.closeTarget", map[string]string{"targetId": t.targetID}, nil)
		_ = conn.Call(ctx, "", "Target.disposeBrowserContext", map[string]string{"browserContextId": t.browserContextID}, nil)
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

// isHTTPSScheme checks http/https
func isHTTPSScheme(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}
