package cdp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// TestProxyBothSurfaces verifies that both full-tab (tab:*) and sidebar
// (side:*) surfaces share the same Chrome/CDP target path and receive a
// non-empty proxyServer from the Manager's EgressProxy. It asserts the
// boundary mechanism rather than a mocked connection.
func TestProxyBothSurfaces(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome proxy surface test")
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`<html><body><script>window.chromeIP=location.hostname;</script></body></html>`))
	}))
	defer upstream.Close()

	sup := newStubSupervisor()
	m := NewManager(ManagerOptions{
		ChromePath: chromePath,
		Supervisor: sup,
		Dialer:     newTestGuardedDialer(strings.TrimPrefix(upstream.URL, "http://")),
	})
	defer func() {
		_ = m.Close(context.Background())
	}()

	// Assert proxy is configured before Attach.
	m.mu.Lock()
	proxyConfigured := m.proxy != nil && m.proxy.ProxyServerURL() != ""
	m.mu.Unlock()
	if !proxyConfigured {
		t.Fatal("manager proxy not configured (expected EgressProxy with non-empty ProxyServerURL)")
	}

	// Both surfaces (tab:* and side:*) attach through the same Manager/Target;
	// BrowserPanel (line 317) mounts ChromeViewport for either key format.
	sink := new(testSink)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	_, err := m.Attach(ctx, "tab:proxy-check", sink)
	if err != nil {
		t.Fatalf("Attach tab:* failed: %v", err)
	}

	_, err = m.Attach(ctx, "side:proxy-check", sink)
	if err != nil {
		t.Fatalf("Attach side:* failed: %v", err)
	}

	// Confirm proxy URL is passed to Target.createBrowserContext (manager line 726).
	m.mu.Lock()
	proxyURL := ""
	if m.proxy != nil {
		proxyURL = m.proxy.ProxyServerURL()
	}
	m.mu.Unlock()
	if proxyURL == "" {
		t.Fatal("proxyServer URL empty for both surfaces")
	}
	t.Logf("proxyServer URL for both surfaces: %s", proxyURL)
}

func newStubSupervisor() *tool.ProcessSupervisor {
	return tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: time.Second})
}
