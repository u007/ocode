package cdp

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitForCall polls the stub until a call to method satisfying match has been
// received. Calls are recorded when they arrive, so a call held by a gated
// reply still counts.
func waitForCall(t *testing.T, stub *stubChrome, method string, match func(stubCall) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, c := range stub.CallsFor(method) {
			if match(c) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s matching the expectation; calls=%v", method, stub.Calls())
}

// paramOf returns a string parameter of a recorded call.
func paramOf(c stubCall, key string) string {
	var p map[string]any
	_ = json.Unmarshal(c.Params, &p)
	s, _ := p[key].(string)
	return s
}

func hasTarget(m *Manager, targetID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.targets {
		if t.targetID == targetID {
			return true
		}
	}
	return false
}

func TestBlockedRequestNameResolution(t *testing.T) {
	resolve := staticResolver(map[string][]netip.Addr{
		"evil.test":   {netip.MustParseAddr("127.0.0.1")},
		"mixed.test":  {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.9")},
		"public.test": {netip.MustParseAddr("93.184.216.34")},
		"v6.test":     {netip.MustParseAddr("fd00::1")},
		"meta.test":   {netip.MustParseAddr("169.254.169.254")},
	})
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"public name resolving to loopback", "https://evil.test/", true},
		{"ws public name resolving to loopback", "ws://evil.test/socket", true},
		{"one public and one private address", "https://mixed.test/", true},
		{"ULA IPv6 address", "https://v6.test/", true},
		{"link-local metadata address", "https://meta.test/", true},
		{"public name resolving to public only", "https://public.test/", false},
		{"upper case name with trailing dot", "https://PUBLIC.TEST./", false},
		{"resolver error fails closed", "https://unknown.test/", true},
		{"file scheme", "file:///etc/passwd", true},
		{"chrome scheme", "chrome://version", true},
		{"chrome extension scheme", "chrome-extension://abcdef/page.html", true},
		{"view-source scheme", "view-source:https://public.test/", true},
		{"about blank continues", "about:blank", false},
		{"data URL continues", "data:text/html,hi", false},
		{"blob URL continues", "blob:https://public.test/6f1e", false},
		{"https without host fails closed", "https:///path", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := blockedRequest(context.Background(), resolve, tc.url); got != tc.want {
				t.Fatalf("blockedRequest(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestBlockedRequestNilResolverFailsClosedForNames(t *testing.T) {
	if !blockedRequest(context.Background(), nil, "https://public.test/") {
		t.Fatal("a name with no resolver must be blocked")
	}
	if blockedRequest(context.Background(), nil, "https://93.184.216.34/") {
		t.Fatal("a public literal needs no resolver")
	}
}

func TestBlockedRequestResolverTimeoutFailsClosed(t *testing.T) {
	slow := func(ctx context.Context, _ string) ([]netip.Addr, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	start := time.Now()
	if !blockedRequest(context.Background(), slow, "https://slow.test/") {
		t.Fatal("a lookup that times out must block the request")
	}
	if elapsed := time.Since(start); elapsed < attachResolveTimeout-200*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("lookup returned after %v, want about %v", elapsed, attachResolveTimeout)
	}
}

func TestAttachFetchEnableFailureFailsAttach(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	stub.SetFailure("Fetch.enable", "guard unavailable")
	m := attachTestManager(t, stub)

	tab, _, err := m.EnsureHeadless(context.Background(), "tab:noguard")
	if err == nil {
		t.Fatalf("attach succeeded without request guard (tab=%v)", tab)
	}
	if n := len(stub.CallsFor("Target.closeTarget")); n == 0 {
		t.Fatal("the unguarded tab was not closed")
	}
	if len(m.TargetKeys()) != 0 {
		t.Fatal("the unguarded tab was registered")
	}
}

func TestAttachPopupGuard(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()
	anchor, _, err := m.EnsureHeadless(ctx, "tab:anchor")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	popup := func(targetID, url string) {
		stub.InjectEvent("", "Target.attachedToTarget", map[string]any{
			"sessionId":          "sess-" + targetID,
			"waitingForDebugger": false,
			"targetInfo": map[string]string{
				"targetId":         targetID,
				"type":             "page",
				"url":              url,
				"browserContextId": anchor.browserContextID,
			},
		})
	}

	popup("popup-blocked", "http://169.254.169.254/")
	waitForCall(t, stub, "Target.closeTarget", func(c stubCall) bool {
		return paramOf(c, "targetId") == "popup-blocked"
	})
	if hasTarget(m, "popup-blocked") {
		t.Fatal("blocked popup was registered as a tab")
	}

	popup("popup-ok", "https://example.com/")
	deadline := time.Now().Add(5 * time.Second)
	for !hasTarget(m, "popup-ok") {
		if time.Now().After(deadline) {
			t.Fatal("allowed popup was not registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, c := range stub.CallsFor("Target.closeTarget") {
		if paramOf(c, "targetId") == "popup-ok" {
			t.Fatal("allowed popup was closed")
		}
	}
}

func TestAttachChildSessionIsGuarded(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	tab, _, err := m.EnsureHeadless(context.Background(), "tab:child")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	stub.InjectEvent(tab.sessionID, "Target.attachedToTarget", map[string]any{
		"sessionId":          "child-1",
		"waitingForDebugger": true,
		"targetInfo":         map[string]string{"type": "iframe", "targetId": "frame-1"},
	})
	waitForCall(t, stub, "Fetch.enable", func(c stubCall) bool { return c.SessionID == "child-1" })
	waitForCall(t, stub, "Target.setAutoAttach", func(c stubCall) bool {
		return c.SessionID == "child-1" && strings.Contains(string(c.Params), `"waitForDebuggerOnStart":true`)
	})
	waitForCall(t, stub, "Runtime.runIfWaitingForDebugger", func(c stubCall) bool { return c.SessionID == "child-1" })

	stub.InjectEvent("child-1", "Fetch.requestPaused", map[string]any{
		"requestId": "c-block",
		"request":   map[string]string{"url": "http://10.0.0.5/"},
	})
	waitForCall(t, stub, "Fetch.failRequest", func(c stubCall) bool {
		return c.SessionID == "child-1" && paramOf(c, "requestId") == "c-block"
	})
}

func TestAttachChildThatCannotBeGuardedIsDetached(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	tab, _, err := m.EnsureHeadless(context.Background(), "tab:badchild")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	// Only the child's Fetch.enable fails; the page's guard already succeeded.
	stub.SetFailure("Fetch.enable", "no fetch for this worker")
	stub.InjectEvent(tab.sessionID, "Target.attachedToTarget", map[string]any{
		"sessionId":          "child-bad",
		"waitingForDebugger": true,
		"targetInfo":         map[string]string{"type": "worker", "targetId": "w-1"},
	})
	waitForCall(t, stub, "Target.detachFromTarget", func(c stubCall) bool {
		return paramOf(c, "sessionId") == "child-bad"
	})
	for _, c := range stub.CallsFor("Runtime.runIfWaitingForDebugger") {
		if c.SessionID == "child-bad" {
			t.Fatal("a child that could not be guarded was resumed")
		}
	}
}

func TestAttachPausedRequestsAnsweredConcurrently(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	tab, _, err := m.EnsureHeadless(context.Background(), "tab:concurrent")
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	// Hold the reply to the first request's continue. The stub keeps reading
	// meanwhile, so a second answer can only be received if the first request
	// does not block the handler.
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	stub.SetReplyGate(func(method string, params json.RawMessage) <-chan struct{} {
		if method == "Fetch.continueRequest" && strings.Contains(string(params), `"r-first"`) {
			return gate
		}
		return nil
	})

	stub.InjectEvent(tab.sessionID, "Fetch.requestPaused", map[string]any{
		"requestId": "r-first",
		"request":   map[string]string{"url": "https://example.com/"},
	})
	waitForCall(t, stub, "Fetch.continueRequest", func(c stubCall) bool { return paramOf(c, "requestId") == "r-first" })

	stub.InjectEvent(tab.sessionID, "Fetch.requestPaused", map[string]any{
		"requestId": "r-second",
		"request":   map[string]string{"url": "http://10.0.0.5/"},
	})
	waitForCall(t, stub, "Fetch.failRequest", func(c stubCall) bool { return paramOf(c, "requestId") == "r-second" })
	release()
}

func TestAttachCloseClosesTabsThenDisposesContextNeverBrowserClose(t *testing.T) {
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
	if err := m.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}

	lastClose, dispose := -1, []int{}
	for i, c := range stub.Calls() {
		switch c.Method {
		case "Target.closeTarget":
			if id := paramOf(c, "targetId"); id == a.targetID || id == b.targetID {
				lastClose = i
			}
		case "Target.disposeBrowserContext":
			dispose = append(dispose, i)
			if got := paramOf(c, "browserContextId"); got != a.browserContextID {
				t.Fatalf("disposed %q, want the shared context %q", got, a.browserContextID)
			}
		case "Browser.close":
			t.Fatal("attach-mode Close sent Browser.close")
		}
	}
	if lastClose < 0 {
		t.Fatal("tabs were not closed")
	}
	if len(dispose) != 1 {
		t.Fatalf("disposed the shared context %d times, want once", len(dispose))
	}
	if dispose[0] < lastClose {
		t.Fatal("the shared context was disposed before its tabs were closed")
	}
}

func TestAttachRedialDisposesStaleContextAndIgnoresStaleExit(t *testing.T) {
	stub1 := newStubChrome()
	defer stub1.Close()
	stub2 := newStubChrome()
	defer stub2.Close()
	stub2.SetResult("Target.createBrowserContext", map[string]string{"browserContextId": "ctx-redial"})
	dials := []*stubChrome{stub1, stub2}
	dialed := 0
	m := NewAttachManager(ManagerOptions{}, func(context.Context) (*Conn, error) {
		s := dials[dialed]
		dialed++
		return s.Conn(), nil
	})
	m.resolveHost = testResolver
	defer func() { _ = m.Close(context.Background()) }()
	ctx := context.Background()

	a, _, err := m.EnsureHeadless(ctx, "tab:a")
	if err != nil {
		t.Fatalf("attach a: %v", err)
	}
	stale := a.browserContextID

	stub1.Close() // the first connection drops
	select {
	case <-stub1.Conn().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("first connection did not end")
	}
	b, _, err := m.EnsureHeadless(ctx, "tab:b")
	if err != nil {
		t.Fatalf("redial attach b: %v", err)
	}
	if dialed != 2 {
		t.Fatalf("dialed %d times, want 2", dialed)
	}
	if b.browserContextID != "ctx-redial" {
		t.Fatalf("tab after redial uses context %q, want the new one", b.browserContextID)
	}
	waitForCall(t, stub2, "Target.disposeBrowserContext", func(c stubCall) bool {
		return paramOf(c, "browserContextId") == stale
	})

	// A late exit notice for the dropped connection must not clear the new one.
	m.handleChromeExit(stub1.Conn())
	m.mu.Lock()
	current, shared := m.conn, m.sharedBC
	m.mu.Unlock()
	if current != stub2.Conn() {
		t.Fatal("a stale exit replaced the current connection")
	}
	if shared != "ctx-redial" {
		t.Fatalf("a stale exit cleared the shared context: %q", shared)
	}
	if !hasTarget(m, b.targetID) {
		t.Fatal("a stale exit dropped the new tab")
	}
}

func TestAttachConcurrentAttachesCreateOneContext(t *testing.T) {
	stub := newStubChrome()
	defer stub.Close()
	m := attachTestManager(t, stub)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, key := range []string{"tab:c1", "tab:c2"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			if _, _, err := m.EnsureHeadless(ctx, key); err != nil {
				errs <- err
			}
		}(key)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent attach: %v", err)
	}
	if n := len(stub.CallsFor("Target.createBrowserContext")); n != 1 {
		t.Fatalf("concurrent attaches created %d shared contexts, want 1", n)
	}
}
