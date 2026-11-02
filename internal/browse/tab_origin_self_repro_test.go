//go:build repro

package browse

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// reproSelfLinkPage is the binding's "/" document. Its anchor is an absolute URL
// on the tab origin itself, so the server rewrite sees a foreign-looking link.
// The script clicks it once the page has loaded.
const reproSelfLinkPage = `<!doctype html><html><head><title>self</title></head><body><a id="self" href="%s/x">self</a><script>setTimeout(function () { document.getElementById('self').click(); }, 50);</script></body></html>`

// reproSelfTargetPage is the binding's "/x" document. It reports its pathname.
const reproSelfTargetPage = `<!doctype html><html><head><title>x</title></head><body><script>fetch('/__report?p=' + encodeURIComponent(location.pathname));</script></body></html>`

// TestReproTabOriginSelfLinkNavigatesToOwnPath: a static link to the tab origin
// itself is followed in Chrome and lands on the binding's own path. The document
// is redirected to /x rather than served under the reserved route, so the page's
// pathname is the upstream path.
func TestReproTabOriginSelfLinkNavigatesToOwnPath(t *testing.T) {
	chromePath := reproChromePath(t)
	reports := make(chan string, 4)
	var selfBase atomic.Value // the tab origin, known only after ListenTab
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__report":
			select {
			case reports <- r.URL.Query().Get("p"):
			default:
			}
			w.WriteHeader(http.StatusNoContent)
		case "/x":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, reproSelfTargetPage)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, reproSelfLinkPage, selfBase.Load().(string))
		}
	}))
	t.Cleanup(upstream.Close)

	s := New("tok", nil)
	defer func() {
		if err := s.Close(context.Background()); err != nil {
			t.Errorf("close browse server: %v", err)
		}
	}()
	const stateKey = "tab:selfrepro"
	_, base, err := s.ListenTab(stateKey, "http", tabAddr(upstream.URL), "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("listen tab: %v", err)
	}
	selfBase.Store(base)
	t.Logf("tab origin=%s", base)

	grant := s.MintGrant(stateKey, reproSPAOrigin)
	logPath := reproOpenInChrome(t, chromePath, base+"/?__grant="+grant)

	select {
	case got := <-reports:
		t.Logf("reported p=%q", got)
		if got != "/x" {
			t.Fatalf("self-link page saw location.pathname=%q, want %q", got, "/x")
		}
	case <-time.After(20 * time.Second):
		logged, _ := os.ReadFile(logPath)
		t.Fatalf("no /__report from the self-link target within 20s; chrome log:\n%s", logged)
	}
}
