package cdp

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrBlocked is wrapped by a policy dialer's Control hook (or a test dialer)
// to signal that the connect target fell in a blocked/private range. The
// proxy maps it to HTTP 403 so the page sees a "blocked" network error rather
// than a generic connection failure. Part 05's browse.NewSafeDialer wraps its
// private-address sentinel with this so the real Chrome egress guard also
// yields 403.
var ErrBlocked = errors.New("cdp: proxy policy blocked connection")

// proxyRealm authenticates proxy clients (Chrome). Both plain and CONNECT
// requests require it.
const proxyRealm = "ocode-browse"

// EgressProxy is an in-process HTTP forward proxy bound to loopback whose
// outbound connections all go through a caller-supplied SSRF-safe dialer. It
// handles plain absolute-URI requests (GET/POST…), HTTP Upgrade (WebSocket)
// tunnels, and CONNECT (https/wss) tunnels. Chrome is configured to route a
// browser context through it via Target.createBrowserContext proxyServer.
type EgressProxy struct {
	ln       net.Listener
	server   *http.Server
	dialer   *net.Dialer
	username string
	password string

	mu      sync.Mutex
	tunnels map[net.Conn]struct{}
	closed  bool
}

// NewEgressProxy binds 127.0.0.1:0, generates a random 24-byte proxy
// credential, and starts serving. dialer may be nil (then outbound dials are
// unrestricted — used by tests reaching httptest loopback upstreams).
func NewEgressProxy(dialer *net.Dialer) (*EgressProxy, error) {
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	cred := make([]byte, 18) // 18 bytes -> 24 base64 chars across two fields
	if _, err := rand.Read(cred); err != nil {
		return nil, fmt.Errorf("egress: generate credential: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("egress: listen: %w", err)
	}
	p := &EgressProxy{
		ln:       ln,
		dialer:   dialer,
		username: base64.RawURLEncoding.EncodeToString(cred[:9]),
		password: base64.RawURLEncoding.EncodeToString(cred[9:]),
		tunnels:  make(map[net.Conn]struct{}),
	}
	p.server = &http.Server{
		Handler:           http.HandlerFunc(p.handle),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = p.server.Serve(ln) }()
	return p, nil
}

// ProxyServerURL returns the per-launch credential-embedded proxy URL to hand
// to Chrome (proxyServer), e.g. "http://<user>:<pass>@127.0.0.1:54321".
func (p *EgressProxy) ProxyServerURL() string {
	return fmt.Sprintf("http://%s:%s@127.0.0.1:%d", p.username, p.password, p.AddrPort())
}

// AddrPort returns the bound port (test/manager convenience).
func (p *EgressProxy) AddrPort() int {
	return p.ln.Addr().(*net.TCPAddr).Port
}

// Addr returns the full bound address.
func (p *EgressProxy) Addr() string { return p.ln.Addr().String() }

// Credential returns "username:password" (test-only auth inspection).
func (p *EgressProxy) Credential() string { return p.username + ":" + p.password }

// Close stops the listener and closes all active tunnels.
func (p *EgressProxy) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	tunnels := p.tunnels
	p.tunnels = make(map[net.Conn]struct{})
	p.mu.Unlock()

	for c := range tunnels {
		_ = c.Close()
	}
	return p.server.Close()
}

func (p *EgressProxy) handle(w http.ResponseWriter, r *http.Request) {
	// Belt-and-braces peer check: only loopback clients may use the proxy.
	if ip := loopbackPeer(r.RemoteAddr); ip == "" {
		http.Error(w, "proxy: non-loopback peer refused", http.StatusForbidden)
		return
	}
	user, pass, ok := parseProxyAuth(r.Header.Get("Proxy-Authorization"))
	if !ok || user != p.username || pass != p.password {
		w.Header().Set("Proxy-Authenticate", `Basic realm="`+proxyRealm+`"`)
		w.WriteHeader(http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	// Check for WebSocket upgrade before stripping hop-by-hop headers,
	// otherwise the Upgrade header would already be gone.
	isUpgrade := isUpgradeRequest(r)
	if isUpgrade {
		p.handleUpgrade(w, r)
		return
	}
	stripHopByHop(r.Header)
	p.handlePlain(w, r)
}

// parseProxyAuth decodes a Proxy-Authorization: Basic <base64> header.
func parseProxyAuth(header string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header[len(prefix):]))
	if err != nil {
		return "", "", false
	}
	s := string(decoded)
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

func loopbackPeer(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip != nil && ip.IsLoopback() {
		return host
	}
	return ""
}

func isUpgradeRequest(r *http.Request) bool {
	for _, v := range r.Header.Values("Upgrade") {
		if strings.EqualFold(strings.TrimSpace(v), "websocket") {
			return true
		}
	}
	return false
}

// handlePlain forwards an absolute-URI HTTP request to the upstream, relaying
// status/headers/body. Dialer policy blocks map to 403; other failures to 502.
func (p *EgressProxy) handlePlain(w http.ResponseWriter, r *http.Request) {
	req := r.Clone(r.Context())
	req.RequestURI = ""
	req.URL.Scheme = r.URL.Scheme
	req.URL.Host = r.URL.Host
	// Ensure the Host header matches the upstream, not the proxy.
	req.Host = r.URL.Host

	tr := &http.Transport{DialContext: p.dialer.DialContext, Proxy: nil, DisableCompression: true}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		p.classifyDialErr(w, err)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// classifyDialErr writes the proxy's dial-failure response: 403 for a policy
// block (ErrBlocked), 502 otherwise.
func (p *EgressProxy) classifyDialErr(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrBlocked) {
		http.Error(w, "blocked: private address", http.StatusForbidden)
		return
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		http.Error(w, "proxy closing", http.StatusServiceUnavailable)
		return
	}
	http.Error(w, "proxy: dial failed", http.StatusBadGateway)
}

// handleUpgrade tunnels an Upgrade (WebSocket) request: dial the upstream, relay
// the request, then byte-pipe both directions once the upstream answers 101.
func (p *EgressProxy) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Host
	if target == "" {
		target = r.Host
	}
	up, err := p.dialer.DialContext(r.Context(), "tcp", target)
	if err != nil {
		p.classifyDialErr(w, err)
		return
	}
	p.trackTunnel(up)
	defer p.untrackTunnel(up)

	req := r.Clone(r.Context())
	req.RequestURI = r.URL.RequestURI()
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	if err := req.Write(up); err != nil {
		http.Error(w, "proxy: upgrade write failed", http.StatusBadGateway)
		return
	}
	br := bufio.NewReader(up)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		http.Error(w, "proxy: upgrade response failed", http.StatusBadGateway)
		return
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		http.Error(w, "proxy: upstream refused upgrade", resp.StatusCode)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "proxy: hijack unsupported", http.StatusInternalServerError)
		return
	}
	clientRW, clientW, err := hj.Hijack()
	if err != nil {
		http.Error(w, "proxy: hijack failed", http.StatusInternalServerError)
		return
	}
	// Relay the 101 response (status line + relevant headers) to the client.
	fmt.Fprintf(clientRW, "HTTP/1.1 101 Switching Protocols\r\n")
	fmt.Fprintf(clientRW, "Upgrade: websocket\r\n")
	fmt.Fprintf(clientRW, "Connection: Upgrade\r\n")
	for k, vs := range resp.Header {
		if strings.EqualFold(k, "Upgrade") || strings.EqualFold(k, "Connection") {
			continue
		}
		for _, v := range vs {
			fmt.Fprintf(clientRW, "%s: %s\r\n", k, v)
		}
	}
	fmt.Fprint(clientRW, "\r\n")
	if err := clientW.Flush(); err != nil {
		_ = up.Close()
		_ = clientRW.Close()
		return
	}
	// Bidirectional pipe. Use br (buffered reader wrapping up) for the
	// upstream→client direction so any bytes already buffered beyond the
	// 101 headers (early WebSocket frames) are not lost.
	go func() { _, _ = io.Copy(up, clientRW); up.Close(); _ = clientRW.Close() }()
	go func() { _, _ = io.Copy(clientRW, br); up.Close(); _ = clientRW.Close() }()
}

// handleConnect establishes a CONNECT tunnel to the requested host.
func (p *EgressProxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	up, err := p.dialer.DialContext(r.Context(), "tcp", host)
	if err != nil {
		p.classifyDialErr(w, err)
		return
	}
	p.trackTunnel(up)
	defer p.untrackTunnel(up)

	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = up.Close()
		http.Error(w, "proxy: hijack unsupported", http.StatusInternalServerError)
		return
	}
	clientRW, clientW, err := hj.Hijack()
	if err != nil {
		_ = up.Close()
		http.Error(w, "proxy: hijack failed", http.StatusInternalServerError)
		return
	}
	fmt.Fprint(clientRW, "HTTP/1.1 200 Connection Established\r\n\r\n")
	if err := clientW.Flush(); err != nil {
		_ = up.Close()
		_ = clientRW.Close()
		return
	}
	go func() { _, _ = io.Copy(up, clientRW); _ = up.Close(); _ = clientRW.Close() }()
	go func() { _, _ = io.Copy(clientRW, up); _ = up.Close(); _ = clientRW.Close() }()
}

func (p *EgressProxy) trackTunnel(c net.Conn) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = c.Close()
		return
	}
	p.tunnels[c] = struct{}{}
	p.mu.Unlock()
}

func (p *EgressProxy) untrackTunnel(c net.Conn) {
	p.mu.Lock()
	delete(p.tunnels, c)
	p.mu.Unlock()
}

// stripHopByHop removes hop-by-hop headers a proxy must not forward.
func stripHopByHop(h http.Header) {
	for _, k := range []string{
		"Proxy-Authorization",
		"Proxy-Connection",
		"Connection",
		"Keep-Alive",
		"TE",
		"Trailer",
		"Transfer-Encoding",
	} {
		h.Del(k)
	}
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
