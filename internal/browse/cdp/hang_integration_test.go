package cdp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// realChromeHarness boots headless Chrome against an httptest upstream that
// serves the given HTML at "/" and returns an attached, navigated target.
func realChromeHarness(t *testing.T, html string, opts func(*ManagerOptions)) (*Manager, *Target, *recordingSink) {
	t.Helper()
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome integration test")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/popup" {
			fmt.Fprint(w, `<!doctype html><html><body><h1>popup</h1></body></html>`)
			return
		}
		fmt.Fprint(w, html)
	}))
	t.Cleanup(upstream.Close)
	upHost := strings.TrimPrefix(upstream.URL, "http://")

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 2 * time.Second})
	mo := ManagerOptions{
		ChromePath: chromePath,
		Supervisor: sup,
		Dialer:     newTestGuardedDialer(upHost),
	}
	if opts != nil {
		opts(&mo)
	}
	m := NewManager(mo)
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_ = m.Close(cctx)
	})

	sink := &recordingSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	target, err := m.Attach(ctx, "tab:hang", sink)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := target.Navigate(ctx, upstream.URL+"/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	waitConsole(t, sink, "ready", 10*time.Second)
	return m, target, sink
}

func waitConsole(t *testing.T, sink *recordingSink, needle string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		_, consoles, _ := sink.snapshot()
		for _, c := range consoles {
			if strings.Contains(strings.Join(c.Args, " "), needle) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("console %q not seen within %v", needle, d)
}

func click(t *testing.T, target *Target, x, y float64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, kind := range []string{"move", "down", "up"} {
		ev := MouseEvent{Kind: kind, X: x, Y: y, Button: "left", ClickCount: 1}
		if kind == "down" {
			ev.Buttons = 1
		}
		if err := target.Mouse(ctx, ev); err != nil {
			t.Fatalf("Mouse %s: %v", kind, err)
		}
	}
}

// evalBounded is the probe: a renderer-bound round-trip with a short deadline.
// If the renderer is blocked (JS dialog) or the session is otherwise wedged,
// this returns ctx.Err() instead of hanging the caller forever.
func evalBounded(target *Target) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := target.ScrollY(ctx)
	return err
}

// popupCase drives one popup-opening page and asserts the opener stays
// responsive, a NewTabEvent fires, and the new tab reports its final URL.
func popupCase(t *testing.T, html, doneNeedle string, clickFn func(*testing.T, *Target)) {
	var mu sync.Mutex
	var newTabs []NewTabEvent
	var navs []NavEvent
	_, target, sink := realChromeHarness(t, html, func(o *ManagerOptions) {
		o.EmitNewTab = func(ev NewTabEvent) {
			mu.Lock()
			newTabs = append(newTabs, ev)
			mu.Unlock()
		}
		o.EmitNav = func(ev NavEvent) {
			mu.Lock()
			navs = append(navs, ev)
			mu.Unlock()
		}
	})

	clickFn(t, target)
	if doneNeedle != "" {
		waitConsole(t, sink, doneNeedle, 5*time.Second)
	}
	if err := evalBounded(target); err != nil {
		t.Fatalf("opener wedged after opening popup: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		var key string
		if len(newTabs) > 0 {
			key = newTabs[0].StateKey
		}
		ok := false
		for _, n := range navs {
			if n.StateKey == key && key != "" && strings.HasSuffix(n.URL, "/popup") {
				ok = true
			}
		}
		mu.Unlock()
		if ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("no NewTabEvent+nav to /popup; newTabs=%+v navs=%+v", newTabs, navs)
}

// TestIntegrationPopupFromClick: a JS window.open on click must surface a
// NewTabEvent and must not wedge the opener.
func TestIntegrationPopupFromClick(t *testing.T) {
	html := `<!doctype html><html><body style="margin:0">
	<button style="position:fixed;left:0;top:0;width:200px;height:100px"
	 onclick="window.open('/popup'); console.log('opened')">open</button>
	<script>console.log("ready")</script></body></html>`
	popupCase(t, html, "opened", func(t *testing.T, target *Target) { click(t, target, 50, 50) })
}

// TestIntegrationMiddleClickLink: middle-click on a link opens it in a new
// background target, which must surface like window.open does.
func TestIntegrationMiddleClickLink(t *testing.T) {
	html := `<!doctype html><html><body style="margin:0">
	<a href="/popup" style="position:fixed;left:0;top:0;width:200px;height:100px;display:block">link</a>
	<script>console.log("ready")</script></body></html>`
	popupCase(t, html, "", func(t *testing.T, target *Target) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, kind := range []string{"move", "down", "up"} {
			ev := MouseEvent{Kind: kind, X: 50, Y: 50, Button: "middle", ClickCount: 1}
			if kind == "down" {
				ev.Buttons = 4
			}
			if err := target.Mouse(ctx, ev); err != nil {
				t.Fatalf("Mouse %s: %v", kind, err)
			}
		}
	})
}

// TestIntegrationAlertDoesNotWedge: alert() blocks the renderer until the
// client answers Page.handleJavaScriptDialog. Without a handler the tab's
// input path hangs forever.
func TestIntegrationAlertDoesNotWedge(t *testing.T) {
	html := `<!doctype html><html><body style="margin:0">
	<button style="position:fixed;left:0;top:0;width:200px;height:100px"
	 onclick="console.log('alerting'); alert('hi'); console.log('after-alert')">alert</button>
	<script>console.log("ready")</script></body></html>`
	_, target, sink := realChromeHarness(t, html, nil)

	click(t, target, 50, 50)
	waitConsole(t, sink, "alerting", 5*time.Second)

	if err := evalBounded(target); err != nil {
		t.Fatalf("tab wedged by alert(): %v", err)
	}
	waitConsole(t, sink, "after-alert", 5*time.Second)
}

// openPopupTab returns the stateKey of a popup opened from the harness page.
func openPopupTab(t *testing.T) (*Manager, *Target, string, *[]NavEvent, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var newTabs []NewTabEvent
	var navs []NavEvent
	html := `<!doctype html><html><body style="margin:0">
	<button style="position:fixed;left:0;top:0;width:200px;height:100px"
	 onclick="window.open('/popup'); console.log('opened')">open</button>
	<script>console.log("ready")</script></body></html>`
	m, target, sink := realChromeHarness(t, html, func(o *ManagerOptions) {
		o.EmitNewTab = func(ev NewTabEvent) { mu.Lock(); newTabs = append(newTabs, ev); mu.Unlock() }
		o.EmitNav = func(ev NavEvent) { mu.Lock(); navs = append(navs, ev); mu.Unlock() }
	})
	click(t, target, 50, 50)
	waitConsole(t, sink, "opened", 5*time.Second)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(newTabs)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(newTabs) == 0 {
		t.Fatal("no popup tab")
	}
	return m, target, newTabs[0].StateKey, &navs, &mu
}

// TestIntegrationRevokePopupKeepsOpener: closing a popup tab must not dispose
// the opener's shared browser context.
func TestIntegrationRevokePopupKeepsOpener(t *testing.T) {
	m, opener, popupKey, _, _ := openPopupTab(t)
	m.Revoke(popupKey)
	if err := evalBounded(opener); err != nil {
		t.Fatalf("opener died after revoking popup: %v", err)
	}
	m.mu.Lock()
	_, still := m.targets[popupKey]
	m.mu.Unlock()
	if still {
		t.Fatal("popup still registered after Revoke")
	}
}

// TestIntegrationPopupClosesItself: window.close() in the popup must drop the
// tab and tell the SPA, not leave a dead session behind.
func TestIntegrationPopupClosesItself(t *testing.T) {
	m, _, popupKey, navs, mu := openPopupTab(t)
	m.mu.Lock()
	popup := m.targets[popupKey]
	m.mu.Unlock()
	if popup == nil {
		t.Fatal("popup target missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = popup.conn.Call(ctx, popup.sessionID, "Runtime.evaluate", map[string]any{"expression": "window.close()"}, nil)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		closed := false
		for _, n := range *navs {
			if n.StateKey == popupKey && n.Error == "tab closed" {
				closed = true
			}
		}
		mu.Unlock()
		m.mu.Lock()
		_, still := m.targets[popupKey]
		m.mu.Unlock()
		if closed && !still {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("popup not dropped after window.close()")
}
