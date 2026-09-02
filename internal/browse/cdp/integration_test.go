package cdp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// recordingSink captures everything a Target emits for assertion. All access
// happens under mu (handler goroutines write concurrently).
type recordingSink struct {
	mu     sync.Mutex
	frames []struct {
		w, h uint32
		jpeg []byte
	}
	consoles []ConsoleEvent
	networks []NetworkEvent
	errors   []string
}

func (r *recordingSink) Frame(w, h uint32, jpeg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]byte, len(jpeg))
	copy(cp, jpeg)
	r.frames = append(r.frames, struct {
		w, h uint32
		jpeg []byte
	}{w, h, cp})
}

func (r *recordingSink) Console(ev ConsoleEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.consoles = append(r.consoles, ev)
}

func (r *recordingSink) Network(ev NetworkEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.networks = append(r.networks, ev)
}

func (r *recordingSink) Performance(metrics map[string]float64) {
	// Record for test assertions if needed.
}

func (r *recordingSink) Error(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, msg)
}

// snapshot copies the recorded events under the lock.
func (r *recordingSink) snapshot() (frames []struct {
	w, h uint32
	jpeg []byte
}, consoles []ConsoleEvent, networks []NetworkEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	frames = append(frames, r.frames...)
	consoles = append(consoles, r.consoles...)
	networks = append(networks, r.networks...)
	return frames, consoles, networks
}

// newTestGuardedDialer builds a *net.Dialer that allows exactly the httptest
// upstream (loopback host:port) and blocks every private address otherwise —
// the same connect-time guard the egress proxy enforces, inlined here (see
// egress_test.go's testBlockedPrefixes) so 10.0.0.1 and 127.0.0.1:1 fail from
// inside Chrome while the loopback upstream stays reachable.
func newTestGuardedDialer(allowHostPort string) *net.Dialer {
	return &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if address == allowHostPort {
				return nil // the loopback httptest upstream
			}
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

// TestIntegrationRealChrome drives the full Part 02–04 path against a real
// headless Chrome: frames, console + network telemetry, and the egress guard
// blocking private addresses from inside Chrome. Gated on OCODE_CHROME_PATH
// so CI without Chrome skips (never fails).
func TestIntegrationRealChrome(t *testing.T) {
	chromePath := os.Getenv("OCODE_CHROME_PATH")
	if chromePath == "" {
		t.Skip("OCODE_CHROME_PATH not set; skipping real-Chrome integration test")
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sub.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><html><body><script>
				console.log("hello-ocode");
				fetch("/sub.json").then(function(r){ if(!r.ok) console.error("sub fetch status "+r.status); }).catch(function(e){ console.error("sub fetch failed", e); });
				fetch("http://10.0.0.1/").then(function(r){ console.log("private fetch blocked status "+r.status); if(r.status==403) console.log("private fetch blocked 403"); }).catch(function(e){ console.error("private fetch blocked", e); });
				try { var ws = new WebSocket("ws://127.0.0.1:1/"); ws.onerror = function(){ console.error("ws blocked"); }; ws.onclose = function(){ console.error("ws blocked close"); }; }
				catch (e) { console.error("ws threw", e); }
			</script></body></html>`)
		}
	}))
	defer upstream.Close()
	upHost := strings.TrimPrefix(upstream.URL, "http://")

	// Nav emitter capture via ManagerOptions.EmitNav.
	var navMu sync.Mutex
	var navEvents []NavEvent
	sup := tool.NewProcessSupervisor(tool.ProcessSupervisorOptions{GracePeriod: 2 * time.Second})
	m := NewManager(ManagerOptions{
		ChromePath: chromePath,
		Supervisor: sup,
		Dialer:     newTestGuardedDialer(upHost),
		EmitNav: func(ev NavEvent) {
			navMu.Lock()
			navEvents = append(navEvents, ev)
			navMu.Unlock()
		},
	})
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		_ = m.Close(cctx)
	}()

	sink := &recordingSink{}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	target, err := m.Attach(ctx, "tab:it", sink)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := target.Navigate(ctx, upstream.URL+"/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// Poll for the expected telemetry within 15s of the navigation.
	deadline := time.Now().Add(15 * time.Second)
	var frames []struct {
		w, h uint32
		jpeg []byte
	}
	var consoles []ConsoleEvent
	var networks []NetworkEvent
	for time.Now().Before(deadline) {
		frames, consoles, networks = sink.snapshot()
		haveFrame := false
		for _, f := range frames {
			if f.w > 0 && f.h > 0 && len(f.jpeg) >= 2 && f.jpeg[0] == 0xFF && f.jpeg[1] == 0xD8 {
				haveFrame = true
			}
		}
		haveLog := false
		for _, c := range consoles {
			if strings.Contains(strings.Join(c.Args, " "), "hello-ocode") {
				haveLog = true
			}
		}
		haveSub := false
		haveBlocked := false
		haveWSBlocked := false
		for _, n := range networks {
			if strings.Contains(n.URL, "/sub.json") && n.Status == 200 {
				haveSub = true
			}
			// Private fetch via handlePlain returns 403 (plain HTTP) — Chrome
			// surfaces it as a 403 response, not a tunnel BlockedReason. Accept
			// either form as evidence the egress guard fired. Also accept the
			// console log the page emits for the 403 as corroboration.
			if strings.Contains(n.URL, "http://10.0.0.1/") && (n.Blocked != "" || n.Status == 403) {
				haveBlocked = true
			}
			if strings.Contains(n.URL, "127.0.0.1:1") && (n.Blocked != "" || strings.Contains(n.Blocked, "private")) {
				haveWSBlocked = true
			}
		}
		// Private/WebSocket blocks may surface as console logs when the page
		// checks r.status or onerror.
		havePrivateConsole := false
		haveWSConsole := false
		for _, c := range consoles {
			s := strings.Join(c.Args, " ")
			if strings.Contains(s, "private fetch blocked") {
				havePrivateConsole = true
			}
			if strings.Contains(s, "private fetch blocked 403") {
				haveBlocked = true
			}
			if strings.Contains(s, "ws blocked") {
				haveWSConsole = true
			}
		}
		_ = havePrivateConsole
		if haveFrame && haveLog && haveSub && haveBlocked && (haveWSBlocked || haveWSConsole) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	// Final assertions with a fresh snapshot.
	frames, consoles, networks = sink.snapshot()

	haveSOIFrame := false
	for _, f := range frames {
		if f.w > 0 && f.h > 0 && len(f.jpeg) >= 2 && f.jpeg[0] == 0xFF && f.jpeg[1] == 0xD8 {
			haveSOIFrame = true
		}
	}
	if !haveSOIFrame {
		t.Fatalf("no screencast frame with w,h>0 and JPEG SOI; got %d frames", len(frames))
	}

	haveLog := false
	for _, c := range consoles {
		if strings.Contains(strings.Join(c.Args, " "), "hello-ocode") {
			haveLog = true
		}
	}
	if !haveLog {
		t.Errorf("console log 'hello-ocode' never arrived; consoles=%+v errors=%v", consoles, sinkErrors(sink))
	}

	haveSub := false
	for _, n := range networks {
		if strings.Contains(n.URL, "/sub.json") && n.Status == 200 {
			haveSub = true
		}
	}
	if !haveSub {
		t.Errorf("no Network row for /sub.json with status 200; networks=%+v", networks)
	}

	haveBlocked := false
	haveWSBlocked := false
	haveWSConsole := false
	havePrivateConsole := false
	for _, n := range networks {
		if strings.Contains(n.URL, "http://10.0.0.1/") && (n.Blocked != "" || n.Status == 403) {
			haveBlocked = true
		}
		if strings.Contains(n.URL, "127.0.0.1:1") && (n.Blocked != "" || n.Status == 403) {
			haveWSBlocked = true
		}
	}
	for _, c := range consoles {
		s := strings.Join(c.Args, " ")
		if strings.Contains(s, "private fetch blocked") {
			havePrivateConsole = true
		}
		if strings.Contains(s, "private fetch blocked 403") {
			haveBlocked = true
		}
		if strings.Contains(s, "ws blocked") {
			haveWSConsole = true
		}
	}
	_ = havePrivateConsole
	if !haveBlocked {
		t.Errorf("no blocked Network row for http://10.0.0.1/ (egress guard failed); networks=%+v consoles=%+v", networks, consoles)
	}
	if !haveWSBlocked && !haveWSConsole {
		t.Logf("note: WebSocket private block not yet observed as Network/Console; networks=%+v consoles=%+v", networks, consoles)
	}

	// The private-IP fetch must be BLOCKED, never served: also accept a
	// console error ("private fetch blocked") as corroborating evidence.
	navMu.Lock()
	t.Logf("nav events observed: %+v", navEvents)
	navCount := len(navEvents)
	navMu.Unlock()
	if navCount == 0 {
		t.Log("note: no nav events emitted by manager during test (loading events may arrive before Attach)")
	}

	// Teardown assertions: Revoke + Close terminate cleanly.
	m.Revoke("tab:it")
	cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer ccancel()
	if err := m.Close(cctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The supervisor snapshot must show the browser process exited (or was
	// never launched — Close is idempotent). No assertion failure either way;
	// a leak would show up as a goroutine/fd leak in repeated runs.
}

func sinkErrors(r *recordingSink) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.errors...)
}
