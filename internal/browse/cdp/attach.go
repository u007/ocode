package cdp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// attachResolveTimeout bounds one name lookup made for a request decision.
const attachResolveTimeout = 2 * time.Second

// resolveFunc resolves a hostname to its addresses. It is a Manager field so
// tests inject a fake and never touch the network.
type resolveFunc func(ctx context.Context, host string) ([]netip.Addr, error)

// shortcut: resolves on the ocode machine, not in the box, so DNS rebinding can still differ; upgrade when the driver runs inside the box
func lookupHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// NewAttachManager builds a Manager attached to a browser that is already
// running elsewhere. dial opens the browser-level CDP connection, for example
// a closure over DialWebSocket with the browser's URL.
//
// Attach mode never launches a local Chrome: there is no FindChrome, no
// process, no ProfileDir and no supervisor, and a dial failure is returned
// as-is, never replaced by a launch. Close closes the tabs and the connection,
// and does not ask the browser to exit. The ChromePath, ProfileDir,
// Supervisor, Dialer and HTR fields of opts are ignored.
//
// All tabs share one browser context, so cookies are shared across tabs. No
// proxy is configured, and the proxy auth handler is not installed. Instead
// every request is judged by blockedRequest, and a request that cannot be
// judged fails.
func NewAttachManager(opts ManagerOptions, dial func(context.Context) (*Conn, error)) *Manager {
	m := NewManager(opts)
	m.attachMode = true
	m.resolveHost = lookupHost
	m.launchFn = func(ctx context.Context) (*Conn, <-chan int, func(), error) {
		if dial == nil {
			return nil, nil, nil, errors.New("cdp: attach: no dial function")
		}
		conn, err := dial(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		// Closing exited when the connection ends feeds the launch-mode exit
		// path, which clears the targets so the next Attach dials again.
		exited := make(chan int)
		go func() {
			<-conn.Done()
			close(exited)
		}()
		return conn, exited, nil, nil
	}
	return m
}

// browserContextFor returns the browser context a new tab is created in.
// Launch mode gives every state key its own context behind proxyURL. Attach
// mode keeps one shared context for every tab, and sends no proxy parameter.
// The context is reused on the connection that owns it. After a redial it is
// kept if the browser still lists it, and replaced otherwise. A replacement is
// restored before any tab can be created in it. The caller holds attachMu.
func (m *Manager) browserContextFor(ctx context.Context, conn *Conn, proxyURL string) (string, error) {
	if !m.attachMode {
		var res struct {
			BrowserContextID string `json:"browserContextId"`
		}
		err := conn.Call(ctx, "", "Target.createBrowserContext", map[string]string{
			"proxyServer":     proxyURL,
			"proxyBypassList": "<-loopback>",
		}, &res)
		if err != nil {
			return "", err
		}
		if res.BrowserContextID == "" {
			return "ctx-auto", nil
		}
		return res.BrowserContextID, nil
	}

	m.mu.Lock()
	id, owner := m.sharedBC, m.sharedBCOwner
	m.mu.Unlock()
	if id != "" && owner == conn {
		return id, nil
	}
	if id != "" {
		// The socket dropped, and the browser may still hold the context. Only
		// an answer that lists it keeps it. An error is returned, never read as
		// "gone", because a gone context is disposed and replaced.
		alive, err := browserContextAlive(ctx, conn, id)
		if err != nil {
			return "", err
		}
		if alive {
			m.mu.Lock()
			m.sharedBCOwner = conn
			m.mu.Unlock()
			return id, nil
		}
	}
	return m.createSharedContext(ctx, conn, id != "")
}

// browserContextAlive reports whether the browser still lists context id.
func browserContextAlive(ctx context.Context, conn *Conn, id string) (bool, error) {
	var res struct {
		BrowserContextIDs []string `json:"browserContextIds"`
	}
	if err := conn.Call(ctx, "", "Target.getBrowserContexts", nil, &res); err != nil {
		return false, fmt.Errorf("cdp: attach: list browser contexts: %w", err)
	}
	for _, c := range res.BrowserContextIDs {
		if c == id {
			return true, nil
		}
	}
	return false, nil
}

// createSharedContext creates the shared context on conn. replacing is set when
// an earlier context was lost: the contexts this Manager still lists are then
// disposed, best effort, and the new one is restored before it is published.
// A restore that fails disposes the new context and leaves the old id
// remembered, so the next attach restores again.
func (m *Manager) createSharedContext(ctx context.Context, conn *Conn, replacing bool) (string, error) {
	if replacing {
		m.mu.Lock()
		stale := m.attachContexts
		m.attachContexts = nil
		m.mu.Unlock()
		for _, old := range stale {
			// Best effort: a context that is already gone fails here, harmlessly.
			_ = conn.Call(ctx, "", "Target.disposeBrowserContext", map[string]string{"browserContextId": old}, nil)
		}
	}
	var res struct {
		BrowserContextID string `json:"browserContextId"`
	}
	if err := conn.Call(ctx, "", "Target.createBrowserContext", nil, &res); err != nil {
		return "", err
	}
	if res.BrowserContextID == "" {
		return "", errors.New("cdp: attach: browser returned no browser context id")
	}
	bc := res.BrowserContextID
	m.mu.Lock()
	m.attachContexts = append(m.attachContexts, bc)
	m.mu.Unlock()
	if replacing {
		if err := m.restoreSharedContext(ctx, conn, bc); err != nil {
			// Nothing was restored into bc and no tab was made in it. Dispose it
			// best effort. It stays listed, so Close tries again.
			_ = conn.Call(ctx, "", "Target.disposeBrowserContext", map[string]string{"browserContextId": bc}, nil)
			return "", err
		}
	}
	m.mu.Lock()
	m.sharedBC = bc
	m.sharedBCOwner = conn
	m.attachHad = true
	m.mu.Unlock()
	return bc, nil
}

// restoreSharedContext fills a replacement context before any tab is created
// in it. The cookies and the seed come from the restore hook. With no hook the
// context stays empty, the seed is cleared, and the Manager is marked so that
// storage reads fail rather than return an empty jar.
func (m *Manager) restoreSharedContext(ctx context.Context, conn *Conn, bc string) error {
	m.mu.Lock()
	fn := m.contextRestore
	m.mu.Unlock()
	if fn == nil {
		if _, err := m.resetLoginForNewBrowser(nil); err != nil {
			return err
		}
		m.mu.Lock()
		m.contextLost = true
		m.mu.Unlock()
		return nil
	}
	cookies, seed, err := fn(ctx)
	if err != nil {
		return fmt.Errorf("cdp: attach: restore browser context: %w", err)
	}
	if len(cookies) > 0 {
		params, invalid, notes := prepareStorageCookies(cookies, nowSeconds())
		m.logNotes(notes)
		if _, err := m.sendPreparedCookies(ctx, conn, bc, params, invalid); err != nil {
			return fmt.Errorf("cdp: attach: restore browser cookies: %w", err)
		}
	}
	skipped, err := m.resetLoginForNewBrowser(seed)
	if err != nil {
		return fmt.Errorf("cdp: attach: restore login-state seed: %w", err)
	}
	if len(skipped) > 0 {
		m.logLogin("browse: attach: restored seed skipped %d origin(s) over the size limit: %s", len(skipped), strings.Join(skipped, ", "))
	}
	return nil
}

// attachMode reports whether the tab belongs to a Manager built by
// NewAttachManager.
func (t *Target) attachMode() bool {
	return t.manager != nil && t.manager.attachMode
}

// blockedRequest decides whether attach mode fails a request to rawURL. It
// never returns an error. An unparseable URL, a disallowed scheme, a host that
// is not a usable name or address, a resolver error or a timeout all mean
// blocked.
//
// Allowed schemes: http, https, ws, wss (judged by host), and data, blob,
// about (no network host). For a name, every resolved address must be public.
func blockedRequest(ctx context.Context, resolve resolveFunc, rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	switch u.Scheme {
	case "data", "blob", "about":
		return false
	case "http", "https", "ws", "wss":
	default:
		// file:, chrome:, chrome-extension:, view-source:, and anything else.
		return true
	}
	host := normalizeHost(u.Hostname())
	if host == "" {
		return true
	}
	if isLocalhostName(host) {
		return true
	}
	if a, ok := hostLiteral(host); ok {
		return blockedAddr(a)
	}
	if resolve == nil {
		return true
	}
	rctx, cancel := context.WithTimeout(ctx, attachResolveTimeout)
	defer cancel()
	addrs, err := resolve(rctx, host)
	if err != nil || len(addrs) == 0 {
		return true
	}
	for _, a := range addrs {
		if blockedAddr(a) {
			return true
		}
	}
	return false
}

// blockedFetchHost classifies a host that is already known to be a literal
// address or a localhost name: loopback, private (10/8, 172.16/12, 192.168/16,
// fc00::/7), link-local (169.254/16, fe80::/10), unspecified, or localhost.
// It never resolves a name.
func blockedFetchHost(host string) bool {
	h := normalizeHost(host)
	if isLocalhostName(h) {
		return true
	}
	a, ok := hostLiteral(h)
	return ok && blockedAddr(a)
}

func normalizeHost(host string) string {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	return strings.Trim(h, "[]")
}

func isLocalhostName(h string) bool {
	return h == "localhost" || strings.HasSuffix(h, ".localhost")
}

func hostLiteral(h string) (netip.Addr, bool) {
	a, err := netip.ParseAddr(h)
	return a, err == nil
}

func blockedAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsUnspecified()
}

// abandonTarget closes a target whose request guard could not be enabled and
// drops its pending entry. Attach mode fails closed: the tab is never
// registered.
func (m *Manager) abandonTarget(t *Target, targetID string) {
	t.stopHandlers()
	_ = t.conn.Call(context.Background(), "", "Target.closeTarget", map[string]string{"targetId": targetID}, nil)
	m.mu.Lock()
	delete(m.pending, targetID)
	m.mu.Unlock()
}

// popupBlocked reports whether a popup must be closed. It checks the URL seen
// at discovery and the URL now. It runs after the guard is enabled, so any
// navigation from here on is judged by the request handler.
func (m *Manager) popupBlocked(conn *Conn, targetID, discovered string) bool {
	var info struct {
		TargetInfo struct {
			URL string `json:"url"`
		} `json:"targetInfo"`
	}
	_ = conn.Call(context.Background(), "", "Target.getTargetInfo", map[string]string{"targetId": targetID}, &info)
	for _, u := range []string{discovered, info.TargetInfo.URL} {
		if u != "" && blockedRequest(context.Background(), m.resolveHost, u) {
			return true
		}
	}
	return false
}

// releaseAttached is attach mode's Close: every tab this Manager created is
// closed, then every browser context it created is disposed. The caller sends
// the connection close.
func (m *Manager) releaseAttached(ctx context.Context, conn *Conn, targets map[string]*Target, contexts []string) {
	for _, t := range targets {
		t.stopHandlers()
		_ = conn.Call(ctx, "", "Target.closeTarget", map[string]string{"targetId": t.targetID}, nil)
	}
	for _, id := range contexts {
		_ = conn.Call(ctx, "", "Target.disposeBrowserContext", map[string]string{"browserContextId": id}, nil)
	}
}
