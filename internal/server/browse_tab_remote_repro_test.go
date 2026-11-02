//go:build repro

package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// remoteReproSPA stands in for a TanStack Router SPA in SPA mode: the server answers
// every path with the same index document, and the router decides from
// location.pathname. The page reports that pathname via a root-relative fetch, so the
// report also goes through the forwarded tab.
const remoteReproSPA = `<!doctype html><html><head><title>spa</title></head><body><script>
fetch('/__report?p=' + encodeURIComponent(location.pathname));
</script></body></html>`

// remoteReproChrome returns the headless Chrome binary, or skips the test.
func remoteReproChrome(t *testing.T) string {
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

// remoteReproOpen starts headless Chrome on url under a process supervisor. Cleanup
// shuts the supervisor down and waits for the browser to exit. It returns the path of
// the browser's log.
func remoteReproOpen(t *testing.T, chromePath, url string) string {
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
	if _, err := tool.StartSupervised(sup, cmd, tool.ProcessRegistration{ID: "remote-repro-chrome", Name: "remote repro chrome"}); err != nil {
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

// TestReproRemoteTabPageSeesUpstreamPathname: a TanStack-style SPA on a saved remote
// project, opened through the forwarded tab, sees its upstream path as
// location.pathname. The listener, the grant, the redirect and the cookie are the
// real flow the SPA uses; the host is the fixture's browse origin.
func TestReproRemoteTabPageSeesUpstreamPathname(t *testing.T) {
	chromePath := remoteReproChrome(t)
	reports := make(chan string, 4)
	f := newRemoteTabFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__report" {
			select {
			case reports <- r.URL.Query().Get("p"):
			default:
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(remoteReproSPA))
	}))

	rep := f.openTab(t, "tab:repro-remote", "http://"+f.upHost+"/app/dashboard")
	t.Logf("forwarded tab origin=%s", rep.Origin)
	logPath := remoteReproOpen(t, chromePath, rep.URL)

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
