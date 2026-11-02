//go:build repro

package browse

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// reproPage runs immediately and reports the pathname the page sees. The
// report is a root-relative fetch, so it also exercises the capture bundle's
// rerouting for the mode under test.
const reproPage = `<!doctype html><html><head><title>repro</title></head><body><script>
fetch('/__report?p=' + encodeURIComponent(location.pathname));
</script></body></html>`

// reproSPAOrigin is a non-empty postMessage target. capture.js does nothing
// without one, so the bundle would otherwise be absent from the page under
// test. The value matches no real parent, so the page's telemetry is dropped.
const reproSPAOrigin = "http://127.0.0.1:1"

// reproChromePath returns the headless Chrome binary, or skips the test.
func reproChromePath(t *testing.T) string {
	t.Helper()
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		chromePath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	if _, err := os.Stat(chromePath); err != nil {
		t.Skipf("chrome not found at %s (set OCODE_CHROME_PATH)", chromePath)
	}
	return chromePath
}

// reproUpstream serves reproPage on loopback. It returns the upstream's
// host:port and a channel of the pathnames the page reports via /__report.
func reproUpstream(t *testing.T) (string, <-chan string) {
	t.Helper()
	return reproUpstreamServing(t, reproPage)
}

// reproUpstreamServing serves page for every path except /__report on loopback.
// It returns the upstream's host:port and a channel of the pathnames the page
// reports via /__report.
func reproUpstreamServing(t *testing.T, page string) (string, <-chan string) {
	t.Helper()
	reports := make(chan string, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__report" {
			select {
			case reports <- r.URL.Query().Get("p"):
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, page)
	}))
	t.Cleanup(upstream.Close)
	host := strings.TrimPrefix(upstream.URL, "http://")
	t.Logf("upstream host=%s", host)
	return host, reports
}

// reproOpenInChrome starts headless Chrome on url under a process supervisor
// and returns the path of its log. Cleanup shuts the supervisor down and waits
// for the browser to exit.
func reproOpenInChrome(t *testing.T, chromePath, url string) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "chrome.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create chrome log: %v", err)
	}
	t.Cleanup(func() { _ = logFile.Close() })

	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{})
	cmd := exec.Command(chromePath,
		"--headless=new", "--disable-gpu", "--no-first-run",
		"--user-data-dir="+t.TempDir(),
		url)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{ID: "repro-chrome", Name: "repro chrome"}); err != nil {
		t.Fatalf("start chrome: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		if err := sup.Shutdown(context.Background()); err != nil {
			t.Logf("supervisor shutdown: %v", err)
		}
		select {
		case err := <-exited:
			if err != nil {
				t.Logf("chrome exited: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Logf("chrome still running 5s after supervisor shutdown")
		}
	})
	return logPath
}

// TestReproPrefixModePathnameIsProxyPath pins TODAY's prefix-mode behavior, a
// known limitation: a page served through /b/{stateKey}/http/{host}/... sees
// the proxy path as location.pathname, not its upstream path. Client routers
// with a root basepath (TanStack Router SPA mode) fail on that path. The
// root-mounted tab origin (TestReproTabOriginPageSeesUpstreamPathname) fixes
// it for local clients. Real browse.Server on a real listener, real headless
// Chrome, local grant.
func TestReproPrefixModePathnameIsProxyPath(t *testing.T) {
	chromePath := reproChromePath(t)
	host, reports := reproUpstream(t)

	s := New("tok", nil)
	ln, base, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() { _ = http.Serve(ln, s.Handler()) }()
	t.Logf("browse origin=%s", base)

	const stateKey = "tab:repro"
	grant := s.MintGrant(stateKey, reproSPAOrigin)
	start := base + "/b/" + stateKey + "/http/" + host + "/app/dashboard?__grant=" + grant
	t.Logf("launch url=%s", start)
	logPath := reproOpenInChrome(t, chromePath, start)

	want := "/b/" + stateKey + "/http/" + host + "/app/dashboard"
	select {
	case got := <-reports:
		t.Logf("reported p=%q", got)
		if got != want {
			t.Fatalf("page saw location.pathname=%q, want proxy path %q", got, want)
		}
	case <-time.After(20 * time.Second):
		logged, _ := os.ReadFile(logPath)
		t.Fatalf("no /__report from page within 20s; chrome log:\n%s", logged)
	}
}

// TestReproTabOriginPageSeesUpstreamPathname is the fix: a page served by a
// root-mounted tab origin sees its upstream path as location.pathname. The
// listener and grant are the real flow the SPA uses, and Chrome is real.
func TestReproTabOriginPageSeesUpstreamPathname(t *testing.T) {
	chromePath := reproChromePath(t)
	host, reports := reproUpstream(t)

	s := New("tok", nil)
	defer func() {
		if err := s.Close(context.Background()); err != nil {
			t.Errorf("close browse server: %v", err)
		}
	}()
	const stateKey = "tab:repro"
	_, base, err := s.ListenTab(stateKey, "http", host, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("listen tab: %v", err)
	}
	t.Logf("tab origin=%s", base)

	grant := s.MintGrant(stateKey, reproSPAOrigin)
	start := base + "/app/dashboard?__grant=" + grant
	t.Logf("launch url=%s", start)
	logPath := reproOpenInChrome(t, chromePath, start)

	select {
	case got := <-reports:
		t.Logf("reported p=%q", got)
		if got != "/app/dashboard" {
			t.Fatalf("page saw location.pathname=%q, want %q", got, "/app/dashboard")
		}
	case <-time.After(20 * time.Second):
		logged, _ := os.ReadFile(logPath)
		t.Fatalf("no /__report from page within 20s; chrome log:\n%s", logged)
	}
}

// reproForeignPage is served on the tab origin. It fetches a second upstream by
// absolute URL, as an app does, and reports its pathname only after that fetch
// has settled. The report therefore proves the page survived the foreign fetch.
const reproForeignPage = `<!doctype html><html><head><title>repro</title></head><body><script>
fetch('%s').catch(function () {}).then(function () {
  fetch('/__report?p=' + encodeURIComponent(location.pathname));
});
</script></body></html>`

// reproSecondSeen is one request the second upstream received, as it saw it.
type reproSecondSeen struct {
	host    string
	path    string
	query   string
	xff     string
	referer string
}

// TestReproTabOriginForeignSubresourceThroughProxy: a page on a tab origin
// fetches a second upstream by absolute URL. The fetch must reach that upstream
// through the reserved route, which the proxy serves. The second upstream sees
// the foreign Host and path, and the X-Forwarded-For that only the proxy adds. A
// direct cross-origin fetch would carry neither. The page also still reports its
// own upstream path.
func TestReproTabOriginForeignSubresourceThroughProxy(t *testing.T) {
	chromePath := reproChromePath(t)

	second := make(chan reproSecondSeen, 4)
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case second <- reproSecondSeen{host: r.Host, path: r.URL.Path, query: r.URL.RawQuery, xff: r.Header.Get("X-Forwarded-For"), referer: r.Header.Get("Referer")}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(foreign.Close)
	foreignHost := strings.TrimPrefix(foreign.URL, "http://")

	host, reports := reproUpstreamServing(t, fmt.Sprintf(reproForeignPage, "http://"+foreignHost+"/api/ping?from=page"))

	s := New("tok", nil)
	defer func() {
		if err := s.Close(context.Background()); err != nil {
			t.Errorf("close browse server: %v", err)
		}
	}()
	const stateKey = "tab:repro"
	_, base, err := s.ListenTab(stateKey, "http", host, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("listen tab: %v", err)
	}
	t.Logf("tab origin=%s binding=%s foreign=%s", base, host, foreignHost)

	grant := s.MintGrant(stateKey, reproSPAOrigin)
	start := base + "/app/dashboard?__grant=" + grant
	logPath := reproOpenInChrome(t, chromePath, start)

	select {
	case got := <-reports:
		t.Logf("reported p=%q", got)
		if got != "/app/dashboard" {
			t.Fatalf("page saw location.pathname=%q, want %q", got, "/app/dashboard")
		}
	case <-time.After(20 * time.Second):
		logged, _ := os.ReadFile(logPath)
		t.Fatalf("no /__report from page within 20s; chrome log:\n%s", logged)
	}

	select {
	case got := <-second:
		t.Logf("second upstream saw host=%q path=%q query=%q xff=%q referer=%q", got.host, got.path, got.query, got.xff, got.referer)
		if got.host != foreignHost || got.path != "/api/ping" || got.query != "from=page" {
			t.Fatalf("second upstream saw host=%q path=%q query=%q, want %q /api/ping from=page", got.host, got.path, got.query, foreignHost)
		}
		if got.xff == "" {
			t.Fatal("second upstream got the request directly from Chrome, not through the proxy (no X-Forwarded-For)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second upstream received no request from the page")
	}
}
