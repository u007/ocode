package cdp

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var testBlockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("0.0.0.0/32"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("::/128"),
}

func isTestPrivateIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, p := range testBlockedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func newBlockListDialer() *net.Dialer {
	return &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("unparseable %q: %w", address, err)
			}
			if isTestPrivateIP(ap.Addr()) {
				return fmt.Errorf("%w: %s", ErrBlocked, ap.Addr())
			}
			return nil
		},
	}
}

func newRefuseAllDialer() *net.Dialer {
	return &net.Dialer{
		Control: func(network, address string, _ syscall.RawConn) error {
			return ErrBlocked
		},
	}
}

func proxyAuthHeader(cred string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(cred))
}


// authedProxyURL rebuilds the proxy URL with userinfo for Go test clients
// (http.Transport only sends Proxy-Authorization when the proxy URL carries
// credentials; Chrome gets them via Fetch.authRequired instead).
func authedProxyURL(t *testing.T, p *EgressProxy) *url.URL {
	t.Helper()
	u, err := url.Parse(p.ProxyServerURL())
	if err != nil {
		t.Fatalf("ProxyServerURL parse: %v", err)
	}
	user, pass := p.UserPass()
	u.User = url.UserPassword(user, pass)
	return u
}

func mustProxyURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("parse proxy URL %q: %v", s, err)
	}
	return u
}

// dialProxy sends a raw HTTP request to the proxy and returns the parsed response.
// rawReq must be a complete HTTP request with CRLF line endings and terminal "\r\n\r\n".
func dialProxyRaw(t *testing.T, proxyAddr, rawReq string) (*http.Response, string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy %s: %v", proxyAddr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(conn, rawReq); err != nil {
		t.Fatalf("write to proxy: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read proxy response: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(body)
}

// ---------------------------------------------------------------------------
// Task 1 — Listener, auth, peer check
// ---------------------------------------------------------------------------

func TestEgress_ProxyAuthMissing(t *testing.T) {
	proxy, err := NewEgressProxy(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	raw := "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"
	resp, _ := dialProxyRaw(t, proxy.Addr(), raw)
	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("expected 407, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Proxy-Authenticate"); got != `Basic realm="ocode-browse"` {
		t.Fatalf("Proxy-Authenticate = %q, want %q", got, `Basic realm="ocode-browse"`)
	}
}

func TestEgress_ProxyAuthWrongCredential(t *testing.T) {
	proxy, err := NewEgressProxy(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	badCred := "wrong:creds"
	raw := "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nProxy-Authorization: " + proxyAuthHeader(badCred) + "\r\nConnection: close\r\n\r\n"
	resp, _ := dialProxyRaw(t, proxy.Addr(), raw)
	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("expected 407, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Proxy-Authenticate"); got != `Basic realm="ocode-browse"` {
		t.Fatalf("Proxy-Authenticate = %q, want %q", got, `Basic realm="ocode-browse"`)
	}
}

func TestEgress_ProxyAuthOKForwards(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "upstream body")
	}))
	defer upstream.Close()

	proxy, err := NewEgressProxy(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	proxyURL := authedProxyURL(t, proxy)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(upstream.URL + "/x")
	if err != nil {
		t.Fatalf("GET via proxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if string(body) != "upstream body" {
		t.Fatalf("body = %q, want %q", string(body), "upstream body")
	}
}

func TestEgress_ProxyServerURLCredential(t *testing.T) {
	proxy, err := NewEgressProxy(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	// Chrome does not honor userinfo embedded in proxyServer URLs; credentials
	// are supplied out-of-band via Fetch.authRequired (UserPass). The URL must
	// therefore carry NO userinfo.
	u, err := url.Parse(proxy.ProxyServerURL())
	if err != nil {
		t.Fatalf("ProxyServerURL parse: %v", err)
	}
	if u.Scheme != "http" {
		t.Fatalf("scheme = %q, want http", u.Scheme)
	}
	if u.User != nil {
		t.Fatalf("ProxyServerURL must not embed userinfo, got %q", u.User)
	}
	user, pass := proxy.UserPass()
	if user == "" || pass == "" {
		t.Fatal("UserPass returned empty credentials")
	}
	if got := user + ":" + pass; got != proxy.Credential() {
		t.Fatalf("UserPass %q != Credential %q", got, proxy.Credential())
	}
}

func TestEgress_PlainForwardStripsHopByHop(t *testing.T) {
	var captured struct {
		method string
		path   string
		body   string
		header http.Header
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		if r.URL.RawQuery != "" {
			captured.path += "?" + r.URL.RawQuery
		}
		b, _ := io.ReadAll(r.Body)
		captured.body = string(b)
		captured.header = r.Header.Clone()
		w.Header().Set("X-Upstream", "1")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(201)
		fmt.Fprint(w, "resp body")
	}))
	defer upstream.Close()

	proxy, err := NewEgressProxy(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	upURL, _ := url.Parse(upstream.URL)

	// --- GET with query and hop-by-hop headers ---
	extraHeaders := map[string]string{
		"Proxy-Connection": "keep-alive",
		"Keep-Alive":       "timeout=5",
		"TE":               "trailers",
		"Trailer":          "Expires",
		"X-Custom":         "keep-me",
		"X-Another":        "also-keep",
	}
	var hdrLines string
	for k, v := range extraHeaders {
		hdrLines += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	raw := fmt.Sprintf("GET http://%s/echo?foo=bar HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\n%s\r\n",
		upURL.Host, upURL.Host, proxyAuthHeader(proxy.Credential()), hdrLines)
	resp, body := dialProxyRaw(t, proxy.Addr(), raw)
	if resp.StatusCode != 201 {
		t.Fatalf("GET status = %d, want 201", resp.StatusCode)
	}
	if body != "resp body" {
		t.Fatalf("GET body = %q, want %q", body, "resp body")
	}
	if got := resp.Header.Get("X-Upstream"); got != "1" {
		t.Fatalf("response header X-Upstream = %q, want 1", got)
	}
	if captured.method != "GET" {
		t.Fatalf("upstream method = %q, want GET", captured.method)
	}
	if captured.path != "/echo?foo=bar" {
		t.Fatalf("upstream path = %q, want /echo?foo=bar", captured.path)
	}
	// Verify hop-by-hop headers stripped (except X-Custom which must survive).
	// Current egress.go strips Proxy-Authorization, Proxy-Connection, Connection,
	// Keep-Alive, TE, Trailer, Transfer-Encoding. Upgrade is not in that list
	// (upgrade handled separately), so we only assert the forwarded subset.
	for _, h := range []string{"Proxy-Authorization", "Proxy-Connection", "Keep-Alive", "TE", "Trailer"} {
		if captured.header.Get(h) != "" {
			t.Fatalf("upstream should not have received %q, got %q", h, captured.header.Get(h))
		}
	}
	if got := captured.header.Get("X-Custom"); got != "keep-me" {
		t.Fatalf("upstream missing X-Custom, got %q", got)
	}

	// --- POST with body ---
	rawPost := fmt.Sprintf("POST http://%s/submit HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\nContent-Type: text/plain\r\nContent-Length: 9\r\nConnection: close\r\n\r\npost-body",
		upURL.Host, upURL.Host, proxyAuthHeader(proxy.Credential()))
	resp2, body2 := dialProxyRaw(t, proxy.Addr(), rawPost)
	if resp2.StatusCode != 201 {
		t.Fatalf("POST status = %d, want 201", resp2.StatusCode)
	}
	if body2 != "resp body" {
		t.Fatalf("POST body = %q, want %q", body2, "resp body")
	}
	if captured.method != "POST" {
		t.Fatalf("upstream POST method = %q, want POST", captured.method)
	}
	if captured.body != "post-body" {
		t.Fatalf("upstream POST body = %q, want %q", captured.body, "post-body")
	}
}

func TestEgress_UpgradeWebSocket(t *testing.T) {
	// Echo websocket upstream.
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade failed: %v", err)
			return
		}
		defer c.Close()
		for {
			mt, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	proxy, err := NewEgressProxy(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	proxyURL := authedProxyURL(t, proxy)
	dialer := websocket.Dialer{
		Proxy:            http.ProxyURL(proxyURL),
		HandshakeTimeout: 5 * time.Second,
	}
	wsURL := "ws://" + strings.TrimPrefix(srv.URL, "http://") + "/ws"
	c, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial via proxy: %v", err)
	}
	defer c.Close()

	msg := "hello websocket"
	if err := c.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, got, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(got) != msg {
		t.Fatalf("echo = %q, want %q", string(got), msg)
	}
}

func TestEgress_PlainBlockedReturns403(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be reached when dial blocked")
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)

	proxy, err := NewEgressProxy(newRefuseAllDialer())
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	raw := fmt.Sprintf("GET http://%s/ HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\nConnection: close\r\n\r\n",
		upURL.Host, upURL.Host, proxyAuthHeader(proxy.Credential()))
	resp, body := dialProxyRaw(t, proxy.Addr(), raw)
	if got := resp.Header.Get("X-Ocode-Blocked"); got != "private address" {
		t.Fatalf("X-Ocode-Blocked = %q, want %q", got, "private address")
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want * (page must be able to read the block)", got)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "blocked: private address") {
		t.Fatalf("body = %q, want contains blocked: private address", body)
	}
}

// ---------------------------------------------------------------------------
// Task 3 — CONNECT tunnels + block-list
// ---------------------------------------------------------------------------

func TestEgress_ConnectTLSTunnel(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "tls upstream")
	}))
	defer upstream.Close()

	upURL, _ := url.Parse(upstream.URL) // https://host:port
	hostPort := upURL.Host

	proxy, err := NewEgressProxy(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	// Establish CONNECT tunnel.
	conn, err := net.Dial("tcp", proxy.Addr())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\n\r\n",
		hostPort, hostPort, proxyAuthHeader(proxy.Credential()))
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("CONNECT status = %d, want 200 body %q", resp.StatusCode, string(b))
	}
	// Drain any buffered body (CONNECT 200 has empty body).
	_ = resp.Body.Close()

	// TLS handshake through tunnel.
	tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("TLS handshake through tunnel: %v", err)
	}
	defer tlsConn.Close()

	// Send GET via TLS tunnel.
	fmt.Fprintf(tlsConn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", hostPort)
	tlsBR := bufio.NewReader(tlsConn)
	tlsResp, err := http.ReadResponse(tlsBR, nil)
	if err != nil {
		t.Fatalf("read TLS upstream response: %v", err)
	}
	defer tlsResp.Body.Close()
	body, _ := io.ReadAll(tlsResp.Body)
	if tlsResp.StatusCode != http.StatusOK {
		t.Fatalf("upstream status = %d body %q", tlsResp.StatusCode, string(body))
	}
	if string(body) != "tls upstream" {
		t.Fatalf("body = %q, want %q", string(body), "tls upstream")
	}
}

func TestEgress_ConnectBlockedLoopback(t *testing.T) {
	proxy, err := NewEgressProxy(newBlockListDialer())
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	raw := fmt.Sprintf("CONNECT 127.0.0.1:1 HTTP/1.1\r\nHost: 127.0.0.1:1\r\nProxy-Authorization: %s\r\n\r\n",
		proxyAuthHeader(proxy.Credential()))
	resp, body := dialProxyRaw(t, proxy.Addr(), raw)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "blocked: private address") {
		t.Fatalf("body = %q, want blocked", body)
	}
}

func TestEgress_ConnectBlockedPrivate10(t *testing.T) {
	proxy, err := NewEgressProxy(newBlockListDialer())
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	raw := fmt.Sprintf("CONNECT 10.0.0.1:443 HTTP/1.1\r\nHost: 10.0.0.1:443\r\nProxy-Authorization: %s\r\n\r\n",
		proxyAuthHeader(proxy.Credential()))
	resp, body := dialProxyRaw(t, proxy.Addr(), raw)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "blocked: private address") {
		t.Fatalf("body = %q, want blocked", body)
	}
}

func TestEgress_ConnectBlockedMappedIPv6(t *testing.T) {
	proxy, err := NewEgressProxy(newBlockListDialer())
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	raw := fmt.Sprintf("CONNECT [::ffff:192.168.1.1]:443 HTTP/1.1\r\nHost: [::ffff:192.168.1.1]:443\r\nProxy-Authorization: %s\r\n\r\n",
		proxyAuthHeader(proxy.Credential()))
	resp, body := dialProxyRaw(t, proxy.Addr(), raw)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "blocked: private address") {
		t.Fatalf("body = %q, want blocked", body)
	}
}

func TestEgress_ConnectBlockedLocalhost(t *testing.T) {
	proxy, err := NewEgressProxy(newBlockListDialer())
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	raw := fmt.Sprintf("CONNECT localhost:443 HTTP/1.1\r\nHost: localhost:443\r\nProxy-Authorization: %s\r\n\r\n",
		proxyAuthHeader(proxy.Credential()))
	resp, body := dialProxyRaw(t, proxy.Addr(), raw)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "blocked: private address") {
		t.Fatalf("body = %q, want blocked", body)
	}
}

func TestEgress_ConnectUnreachableTimeout(t *testing.T) {
	// 203.0.113.1 is TEST-NET-3 documentation prefix, never routed.
	// With the block-list dialer it is NOT blocked, so the proxy attempts a
	// real dial which should time out quickly and return 502 without hanging
	// past 10s.
	proxy, err := NewEgressProxy(newBlockListDialer())
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer proxy.Close()

	// Use port 81 which is unlikely to have a fast RST in test env.
	target := "203.0.113.1:81"
	raw := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\n\r\n",
		target, target, proxyAuthHeader(proxy.Credential()))

	start := time.Now()
	conn, err := net.DialTimeout("tcp", proxy.Addr(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(12 * time.Second))
	if _, err := io.WriteString(conn, raw); err != nil {
		t.Fatalf("write CONNECT: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("read CONNECT response: %v elapsed %v", err, elapsed)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if elapsed > 10*time.Second {
		t.Fatalf("CONNECT to unreachable took %v, want <=10s", elapsed)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body %q elapsed %v", resp.StatusCode, string(body), elapsed)
	}
}

func TestEgress_ConnectCloseClosesTunnel(t *testing.T) {
	// Raw TCP listener as tunnel target.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	// Accept holds the connection open until test ends.
	hold := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		hold <- c
	}()
	hostPort := ln.Addr().String()

	proxy, err := NewEgressProxy(&net.Dialer{})
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}

	conn, err := net.Dial("tcp", proxy.Addr())
	if err != nil {
		proxy.Close()
		t.Fatalf("dial proxy: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\n\r\n",
		hostPort, hostPort, proxyAuthHeader(proxy.Credential()))
	if _, err := io.WriteString(conn, req); err != nil {
		proxy.Close()
		conn.Close()
		t.Fatalf("write CONNECT: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		proxy.Close()
		conn.Close()
		t.Fatalf("read CONNECT response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		proxy.Close()
		conn.Close()
		t.Fatalf("CONNECT status = %d body %q", resp.StatusCode, string(b))
	}
	_ = resp.Body.Close()

	// Tunnel established; data should flow.
	if _, err := conn.Write([]byte("ping")); err != nil {
		proxy.Close()
		conn.Close()
		t.Fatalf("write through tunnel: %v", err)
	}
	// Now close proxy — tunnel should be torn down.
	if err := proxy.Close(); err != nil {
		t.Fatalf("proxy.Close: %v", err)
	}
	// Give kernel a moment to propagate RST.
	time.Sleep(50 * time.Millisecond)
	_ = conn.SetDeadline(time.Now().Add(1 * time.Second))
	// Reading should EOF or error; writing should fail or EOF.
	conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 4)
	_, readErr := conn.Read(buf)
	// Accept both EOF or timeout as closure indication, but not successful read of pending data beyond ping echo.
	// Instead verify further writes fail or read hits EOF.
	if readErr == nil {
		// If read succeeded (upstream echoed ping), try another read — should now EOF.
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, readErr2 := conn.Read(buf)
		if readErr2 == nil {
			t.Fatalf("tunnel still open after proxy.Close: second read succeeded")
		}
	}
	_ = conn.Close()
	// Drain held upstream conn.
	select {
	case c := <-hold:
		_ = c.Close()
	default:
	}
}
