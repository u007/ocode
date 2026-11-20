package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestBlockedFetchHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"LOCALHOST", true},
		{"localhost.", true},
		{"api.localhost", true},
		{"127.0.0.1", true},
		{"127.255.0.9", true},
		{"::1", true},
		{"::ffff:127.0.0.1", true},
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"fc00::1", true},
		{"fd12:3456::1", true},
		{"::ffff:10.0.0.1", true},
		{"169.254.169.254", true},
		{"fe80::1", true},
		{"fe80::1%en0", true},
		{"0.0.0.0", true},
		{"::", true},
		{"172.32.0.1", false},
		{"100.64.0.1", false},
		{"8.8.8.8", false},
		{"2001:4860:4860::8888", false},
		{"example.com", false},
		{"EXAMPLE.COM", false},
		{"example.com.", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := blockedFetchHost(tc.host); got != tc.want {
			t.Errorf("blockedFetchHost(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestBlockedRequestURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"http://127.0.0.1:9222/json", true},
		{"http://[::1]:9222/json/version", true},
		{"https://192.168.0.10/admin", true},
		{"http://localhost:3000/", true},
		{"https://example.com/", false},
		{"data:text/html,hi", false},
		{"about:blank", false},
		{"http://[::1/", true}, // unparseable: fail closed
	}
	resolve := staticResolver(map[string][]netip.Addr{
		"example.com": {netip.MustParseAddr("93.184.216.34")},
	})
	for _, tc := range cases {
		if got := blockedRequest(context.Background(), resolve, tc.url); got != tc.want {
			t.Errorf("blockedRequest(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

// attachTestManager builds an attach-mode Manager whose dial returns the
// stub's pipe connection, so the fake peer sees every CDP call.
func attachTestManager(t *testing.T, stub *stubChrome) *Manager {
	t.Helper()
	m := NewAttachManager(ManagerOptions{Log: log.New(io.Discard, "", 0)},
		func(context.Context) (*Conn, error) { return stub.Conn(), nil })
	m.resolveHost = testResolver
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	return m
}

// testResolver answers example.com with a public address and nothing else.
// Any other name fails, which attach mode treats as blocked, so no test
// touches the network.
var testResolver = staticResolver(map[string][]netip.Addr{
	"example.com": {netip.MustParseAddr("93.184.216.34")},
})

// staticResolver returns a resolveFunc backed by a fixed table. Unknown names
// fail.
func staticResolver(table map[string][]netip.Addr) resolveFunc {
	return func(_ context.Context, host string) ([]netip.Addr, error) {
		if addrs, ok := table[host]; ok {
			return addrs, nil
		}
		return nil, errors.New("no such host: " + host)
	}
}

func TestAttachModeSharesOneBrowserContextWithoutProxy(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	a, _, err := m.EnsureHeadless(ctx, "tab:a")
	if err != nil {
		t.Fatalf("attach a: %v", err)
	}
	b, _, err := m.EnsureHeadless(ctx, "tab:b")
	if err != nil {
		t.Fatalf("attach b: %v", err)
	}

	creates := stub.CallsFor("Target.createBrowserContext")
	if len(creates) != 1 {
		t.Fatalf("Target.createBrowserContext sent %d times, want exactly 1", len(creates))
	}
	for _, c := range creates {
		if strings.Contains(string(c.Params), "proxyServer") {
			t.Fatalf("createBrowserContext carried a proxy parameter: %s", c.Params)
		}
	}
	if a.browserContextID == "" || a.browserContextID != b.browserContextID {
		t.Fatalf("tabs do not share a browser context: %q vs %q", a.browserContextID, b.browserContextID)
	}
	if !a.sharedContext || !b.sharedContext {
		t.Fatal("attach-mode tabs must be marked sharedContext so their context is never disposed")
	}

	enables := stub.CallsFor("Fetch.enable")
	if len(enables) != 2 {
		t.Fatalf("Fetch.enable sent %d times, want once per tab", len(enables))
	}
	for _, c := range enables {
		var p map[string]json.RawMessage
		_ = json.Unmarshal(c.Params, &p)
		if _, ok := p["handleAuthRequests"]; ok {
			t.Fatalf("attach mode installed the proxy auth handler: %s", c.Params)
		}
		if _, ok := p["patterns"]; !ok {
			t.Fatalf("Fetch.enable has no patterns: %s", c.Params)
		}
	}

	if err := m.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if n := len(stub.CallsFor("Browser.close")); n != 0 {
		t.Fatalf("attach-mode Close sent Browser.close %d times; the browser is not ours to stop", n)
	}
	// Close disposes the one shared context it created, and only that one.
	disposed := stub.CallsFor("Target.disposeBrowserContext")
	if len(disposed) != 1 || paramOf(disposed[0], "browserContextId") != a.browserContextID {
		t.Fatalf("Close disposed %v, want exactly the shared context %q", disposed, a.browserContextID)
	}
}

func TestAttachModeBlocksPrivateRequestsAndContinuesPublic(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	tab, _, err := m.EnsureHeadless(ctx, "tab:net")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	stub.InjectEvent(tab.sessionID, "Fetch.requestPaused", map[string]any{
		"requestId": "r-block",
		"request":   map[string]string{"url": "http://127.0.0.1:9222/json"},
	})
	stub.InjectEvent(tab.sessionID, "Fetch.requestPaused", map[string]any{
		"requestId": "r-ok",
		"request":   map[string]string{"url": "https://example.com/"},
	})

	waitFor := func(method, requestID string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			for _, c := range stub.CallsFor(method) {
				var p map[string]string
				_ = json.Unmarshal(c.Params, &p)
				if p["requestId"] == requestID && c.SessionID == tab.sessionID {
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("no %s for %s; calls=%v", method, requestID, stub.Calls())
	}
	waitFor("Fetch.failRequest", "r-block")
	waitFor("Fetch.continueRequest", "r-ok")

	for _, c := range stub.CallsFor("Fetch.failRequest") {
		var p map[string]string
		_ = json.Unmarshal(c.Params, &p)
		if p["requestId"] == "r-ok" {
			t.Fatal("public request was failed")
		}
		if p["errorReason"] != "BlockedByClient" {
			t.Fatalf("errorReason = %q", p["errorReason"])
		}
	}
	for _, c := range stub.CallsFor("Fetch.continueRequest") {
		var p map[string]string
		_ = json.Unmarshal(c.Params, &p)
		if p["requestId"] == "r-block" {
			t.Fatal("private request was continued")
		}
	}
}

func TestAttachModeDialFailureNeverLaunchesChrome(t *testing.T) {
	dialErr := errors.New("connection refused")
	m := NewAttachManager(ManagerOptions{}, func(context.Context) (*Conn, error) { return nil, dialErr })
	_, _, err := m.EnsureHeadless(context.Background(), "tab:x")
	if !errors.Is(err, dialErr) {
		t.Fatalf("EnsureHeadless error = %v, want the dial error", err)
	}
	if errors.Is(err, ErrChromeNotFound) {
		t.Fatal("attach mode reported a missing local Chrome")
	}

	none := NewAttachManager(ManagerOptions{}, nil)
	if _, _, err := none.EnsureHeadless(context.Background(), "tab:y"); err == nil {
		t.Fatal("attach mode with no dial function succeeded")
	}
}
