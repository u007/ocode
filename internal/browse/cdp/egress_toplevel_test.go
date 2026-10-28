package cdp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
)

// fakeResolver answers top-level name lookups from a fixed table and counts
// them, so tests can prove a name was looked up exactly once (or never).
type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]netip.Addr
	calls   map[string]int
}

func newFakeResolver(answers map[string][]netip.Addr) *fakeResolver {
	return &fakeResolver{answers: answers, calls: make(map[string]int)}
}

func (f *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[host]++
	addrs, ok := f.answers[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return addrs, nil
}

func (f *fakeResolver) callCount(host string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[host]
}

func (f *fakeResolver) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

// pinRecorder stands in for the pinned connect. It records every address the
// proxy tries, refuses the ones in refuse, and routes successful dials to
// redirect (a real loopback listener), so no test ever reaches a real host.
type pinRecorder struct {
	mu       sync.Mutex
	dialed   []string
	refuse   map[string]bool
	redirect string
}

func (r *pinRecorder) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	r.mu.Lock()
	r.dialed = append(r.dialed, addr)
	refuse := r.refuse[addr]
	r.mu.Unlock()
	if refuse {
		return nil, errors.New("test: connection refused")
	}
	return (&net.Dialer{}).DialContext(ctx, network, r.redirect)
}

func (r *pinRecorder) dialedAddrs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.dialed...)
}

func newTopLevelProxy(t *testing.T, res *fakeResolver, rec *pinRecorder) *EgressProxy {
	t.Helper()
	p, err := NewEgressProxy(newRefuseAllDialer())
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	p.resolver = res
	if rec != nil {
		p.dialPinned = rec.dial
	}
	return p
}

func plainGet(t *testing.T, p *EgressProxy, hostport string) (*http.Response, string) {
	t.Helper()
	raw := fmt.Sprintf("GET http://%s/ HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\nConnection: close\r\n\r\n",
		hostport, hostport, proxyAuthHeader(p.Credential()))
	return dialProxyRaw(t, p.Addr(), raw)
}

// A top-level host whose name resolves to a metadata or link-local address is
// refused before any connect. Covers the rebinding shape: public at first
// lookup, metadata at dial, both answered here by a fake resolver.
func TestTopLevelRefusesMetadataAnswers(t *testing.T) {
	res := newFakeResolver(map[string][]netip.Addr{
		"rebind-v4.example":        {netip.MustParseAddr("169.254.169.254")},
		"rebind-v6.example":        {netip.MustParseAddr("fd00:ec2::254")},
		"rebind-mapped.example":    {netip.MustParseAddr("::ffff:169.254.169.254")},
		"rebind-alibaba.example":   {netip.MustParseAddr("100.100.100.200")},
		"rebind-ll6.example":       {netip.MustParseAddr("fe80::1")},
		"mixed.example":            {netip.MustParseAddr("203.0.113.5"), netip.MustParseAddr("169.254.169.254")},
		"metadata.google.internal": {netip.MustParseAddr("203.0.113.5")},
	})
	rec := &pinRecorder{refuse: map[string]bool{}}
	p := newTopLevelProxy(t, res, rec)

	for _, name := range []string{
		"rebind-v4.example", "rebind-v6.example", "rebind-mapped.example",
		"rebind-alibaba.example", "rebind-ll6.example", "mixed.example",
		"metadata.google.internal",
	} {
		t.Run(name, func(t *testing.T) {
			p.AllowHost(name + ":80")
			defer p.ReleaseHost(name + ":80")
			resp, body := plainGet(t, p, name)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d body %q, want 403", resp.StatusCode, body)
			}
			if resp.Header.Get("X-Ocode-Blocked") == "" {
				t.Fatalf("missing X-Ocode-Blocked header on block")
			}
		})
	}

	if got := rec.dialedAddrs(); len(got) != 0 {
		t.Fatalf("pinned connect reached %v; a denied name must never dial", got)
	}
	if n := res.callCount("metadata.google.internal"); n != 0 {
		t.Fatalf("metadata hostname was looked up %d times; must be refused before lookup", n)
	}
}

func TestTopLevelRefusesMetadataIPLiteral(t *testing.T) {
	rec := &pinRecorder{refuse: map[string]bool{}}
	p := newTopLevelProxy(t, newFakeResolver(nil), rec)

	for _, literal := range []string{
		"169.254.169.254",
		"[::ffff:169.254.169.254]",
		"[fd00:ec2::254]",
		"100.100.100.200",
		"[fe80::1]",
	} {
		t.Run(literal, func(t *testing.T) {
			p.AllowHost(literal + ":80")
			defer p.ReleaseHost(literal + ":80")
			if resp, body := plainGet(t, p, literal); resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d body %q, want 403", resp.StatusCode, body)
			}
		})
	}
	if got := rec.dialedAddrs(); len(got) != 0 {
		t.Fatalf("pinned connect reached %v for an IP literal the deny set covers", got)
	}
}

// The CONNECT (https) path uses the same top-level resolution.
func TestTopLevelRefusesMetadataConnect(t *testing.T) {
	res := newFakeResolver(map[string][]netip.Addr{
		"rebind.example": {netip.MustParseAddr("169.254.169.254")},
	})
	rec := &pinRecorder{refuse: map[string]bool{}}
	p := newTopLevelProxy(t, res, rec)
	p.AllowHost("rebind.example:443")

	raw := fmt.Sprintf("CONNECT rebind.example:443 HTTP/1.1\r\nHost: rebind.example:443\r\nProxy-Authorization: %s\r\n\r\n",
		proxyAuthHeader(p.Credential()))
	resp, body := dialProxyRaw(t, p.Addr(), raw)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d body %q, want 403", resp.StatusCode, body)
	}
	if got := rec.dialedAddrs(); len(got) != 0 {
		t.Fatalf("pinned connect reached %v; must never dial", got)
	}
}

// A public name is looked up once and dialed at the checked IP literal, never
// by name, so a second resolution cannot change the destination.
func TestTopLevelPinsCheckedAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	res := newFakeResolver(map[string][]netip.Addr{
		"public.example": {netip.MustParseAddr("203.0.113.5")},
	})
	rec := &pinRecorder{refuse: map[string]bool{}, redirect: upstream.Listener.Addr().String()}
	p := newTopLevelProxy(t, res, rec)
	p.AllowHost("public.example:80")

	resp, body := plainGet(t, p, "public.example")
	if resp.StatusCode != http.StatusOK || body != "ok" {
		t.Fatalf("status = %d body %q, want 200 ok", resp.StatusCode, body)
	}
	if got := rec.dialedAddrs(); len(got) != 1 || got[0] != "203.0.113.5:80" {
		t.Fatalf("dialed %v, want exactly [203.0.113.5:80]", got)
	}
	if n := res.callCount("public.example"); n != 1 {
		t.Fatalf("name looked up %d times, want exactly 1", n)
	}
}

// When the first checked address refuses, the remaining checked addresses are
// tried in order without a second lookup (dual-stack hosts keep working).
func TestTopLevelFallsThroughCheckedAddresses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	res := newFakeResolver(map[string][]netip.Addr{
		"dual.example": {netip.MustParseAddr("203.0.113.9"), netip.MustParseAddr("203.0.113.5")},
	})
	rec := &pinRecorder{
		refuse:   map[string]bool{"203.0.113.9:80": true},
		redirect: upstream.Listener.Addr().String(),
	}
	p := newTopLevelProxy(t, res, rec)
	p.AllowHost("dual.example:80")

	resp, body := plainGet(t, p, "dual.example")
	if resp.StatusCode != http.StatusOK || body != "ok" {
		t.Fatalf("status = %d body %q, want 200 ok", resp.StatusCode, body)
	}
	want := []string{"203.0.113.9:80", "203.0.113.5:80"}
	if got := rec.dialedAddrs(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("dialed %v, want %v", got, want)
	}
	if n := res.callCount("dual.example"); n != 1 {
		t.Fatalf("name looked up %d times, want exactly 1", n)
	}
}

// Loopback is allowed on the top-level path as before. This runs the real
// pinned connect (its Control hook included) against a loopback upstream, with
// an IPv6 loopback answer first to exercise the in-order fallback.
func TestTopLevelLoopbackPreviewStillWorks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	_, port, err := net.SplitHostPort(upstream.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split upstream addr: %v", err)
	}

	res := newFakeResolver(map[string][]netip.Addr{
		"preview.example": {netip.MustParseAddr("::1"), netip.MustParseAddr("127.0.0.1")},
	})
	p := newTopLevelProxy(t, res, nil)
	hostport := "preview.example:" + port
	p.AllowHost(hostport)

	resp, body := plainGet(t, p, hostport)
	if resp.StatusCode != http.StatusOK || body != "ok" {
		t.Fatalf("status = %d body %q, want 200 ok", resp.StatusCode, body)
	}
}

// Pins the current LAN/CGNAT/ULA policy on the top-level path: allowed, as
// before. Only the two metadata addresses and link-local are newly denied.
func TestTopLevelLANStillAllowed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	cases := map[string]string{
		"lan.example":   "192.168.1.10",
		"ten.example":   "10.1.2.3",
		"cgnat.example": "100.64.0.5",
		"ula.example":   "fd12::5",
	}
	answers := make(map[string][]netip.Addr, len(cases))
	for name, ip := range cases {
		answers[name] = []netip.Addr{netip.MustParseAddr(ip)}
	}
	res := newFakeResolver(answers)
	rec := &pinRecorder{refuse: map[string]bool{}, redirect: upstream.Listener.Addr().String()}
	p := newTopLevelProxy(t, res, rec)

	for name, ip := range cases {
		t.Run(name, func(t *testing.T) {
			p.AllowHost(name + ":80")
			defer p.ReleaseHost(name + ":80")
			if resp, body := plainGet(t, p, name); resp.StatusCode != http.StatusOK || body != "ok" {
				t.Fatalf("status = %d body %q, want 200 ok", resp.StatusCode, body)
			}
			want := net.JoinHostPort(ip, "80")
			found := false
			for _, got := range rec.dialedAddrs() {
				if got == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("dialed %v, want %s among them", rec.dialedAddrs(), want)
			}
		})
	}
}

// Pins the policy path for a host that is NOT a top-level navigation: a LAN
// address stays refused, as the policy dialer decides. Unchanged by this fix.
func TestPolicyHostLANStillRefused(t *testing.T) {
	p, err := NewEgressProxy(newBlockListDialer())
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer p.Close()

	raw := fmt.Sprintf("CONNECT 192.168.1.10:443 HTTP/1.1\r\nHost: 192.168.1.10:443\r\nProxy-Authorization: %s\r\n\r\n",
		proxyAuthHeader(p.Credential()))
	resp, body := dialProxyRaw(t, p.Addr(), raw)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d body %q, want 403", resp.StatusCode, body)
	}
}

// resolveTopLevel refuses a zoned link-local literal. netip.Prefix.Contains
// would miss fe80::1%en0 without the zone strip in upstreamDenied.
func TestResolveTopLevelRefusesZonedLinkLocal(t *testing.T) {
	p, err := NewEgressProxy(newRefuseAllDialer())
	if err != nil {
		t.Fatalf("NewEgressProxy: %v", err)
	}
	defer p.Close()
	res := newFakeResolver(nil)
	p.resolver = res

	if _, err := p.resolveTopLevel(context.Background(), "fe80::1%en0"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("zoned link-local: err = %v, want ErrBlocked", err)
	}
	if n := res.totalCalls(); n != 0 {
		t.Fatalf("IP literal must not reach the resolver, got %d calls", n)
	}
}

// The connect-time hook sees the address the socket dials. It denies the
// link-local and metadata set and allows everything else the policy permits.
func TestUpstreamControlDeniesLinkLocalAndMetadata(t *testing.T) {
	denied := []string{
		"169.254.169.254:80",
		"[::ffff:169.254.169.254]:80",
		"[fd00:ec2::254]:80",
		"100.100.100.200:80",
		"[fe80::1]:443",
		"[fe80::1%en0]:80",
	}
	for _, addr := range denied {
		if err := denyUpstreamControl("tcp", addr, nil); !errors.Is(err, ErrBlocked) {
			t.Errorf("Control(%s) = %v, want ErrBlocked", addr, err)
		}
	}
	allowed := []string{
		"203.0.113.5:80",
		"192.168.1.10:80",
		"100.64.0.5:80",
		"[fd12::5]:80",
		"127.0.0.1:80",
		"[::1]:80",
	}
	for _, addr := range allowed {
		if err := denyUpstreamControl("tcp", addr, nil); err != nil {
			t.Errorf("Control(%s) = %v, want nil", addr, err)
		}
	}
}
