//go:build !windows

package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/u007/ocode/internal/projects"
)

// terminalTestServer returns an httptest server serving only the terminal ws
// endpoint, plus its ws:// URL. The direct handler is explicitly configured as
// loopback-safe so these tests exercise the pty bridge without a Server route.
func terminalTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, true)

	srv := httptest.NewServer(http.HandlerFunc(h.HandleTerminalWS))
	t.Cleanup(srv.Close)
	return srv, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestTerminalShellCommandUsesLoginMode(t *testing.T) {
	cmd := terminalShellCommand("/bin/zsh")
	want := []string{"/bin/zsh", "-l"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("command args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("command args = %v, want %v", cmd.Args, want)
		}
	}
}

// project_path must be one of the server's registered roots — anything else
// would let a client spawn a shell in an arbitrary directory.
func TestTerminalWSRejectsUnregisteredProjectPath(t *testing.T) {
	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, true)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/terminal/ws?project_path=/somewhere/else", nil)
	h.HandleTerminalWS(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// A project_path matching a registered project root starts the shell in that
// directory instead of the server workdir.
func TestTerminalWSSpawnsShellInRequestedProject(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	otherRoot := t.TempDir()

	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, true)
	// Swap in a temp-backed store so the test never writes the user's real
	// projects.json.
	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatalf("projects.NewStoreAt: %v", err)
	}
	if err := store.Add(otherRoot); err != nil {
		t.Fatalf("register project: %v", err)
	}
	h.projects = store

	srv := httptest.NewServer(http.HandlerFunc(h.HandleTerminalWS))
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?project_path=" + url.QueryEscape(otherRoot)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("pwd\n")); err != nil {
		t.Fatalf("command write failed: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var seen strings.Builder
	// macOS tempdirs resolve through /private symlinks, so match on the
	// unique dir suffix rather than the full path. The typed line echo is
	// just "pwd", so a single occurrence is the shell's actual output.
	want := "/" + filepath.Base(otherRoot)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline failed: %v", err)
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read failed before seeing pwd output (got %q): %v", seen.String(), err)
		}
		seen.Write(data)
		if strings.Contains(seen.String(), want) {
			return
		}
	}
}

func TestTerminalWSReadLimit(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	_, wsURL := terminalTestServer(t)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", terminalMaxMessageSize+1))); err != nil {
		t.Fatalf("write oversized frame: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected oversized websocket frame to close the terminal")
	}
}

// The pty bridge must refuse cross-origin upgrades (CSWSH): a page on another
// site must not be able to drive a shell from a logged-in user's browser.
func TestTerminalWSRejectsCrossOrigin(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	_, wsURL := terminalTestServer(t)

	hdr := http.Header{}
	hdr.Set("Origin", "http://evil.example.com")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err == nil {
		conn.Close()
		t.Fatal("expected dial to fail for a cross-origin request")
	}
	if resp == nil {
		t.Fatalf("expected an HTTP response, got dial error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// A same-origin Origin header (what the SPA actually sends) is accepted.
func TestTerminalWSAcceptsSameOrigin(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	srv, wsURL := terminalTestServer(t)

	hdr := http.Header{}
	hdr.Set("Origin", srv.URL)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err != nil {
		t.Fatalf("same-origin dial failed: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	conn.Close()
}

func TestTerminalWSEchoesShellOutput(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	_, wsURL := terminalTestServer(t)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":80,"rows":24}`)); err != nil {
		t.Fatalf("resize write failed: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("echo ocode-terminal-ok\n")); err != nil {
		t.Fatalf("command write failed: %v", err)
	}

	// Hard deadline so a broken bridge is a test failure, not a hang. The pty
	// echoes the typed line back too, so scan frames until the command's
	// output appears on a line of its own.
	deadline := time.Now().Add(10 * time.Second)
	var seen strings.Builder
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatalf("SetReadDeadline failed: %v", err)
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read failed before seeing command output (got %q): %v", seen.String(), err)
		}
		seen.Write(data)
		if strings.Count(seen.String(), "ocode-terminal-ok") >= 2 {
			// First occurrence is the pty echo of the typed line, second is
			// the shell's actual output.
			return
		}
	}
}

// Closing the client connection must tear down the pty and return the handler
// promptly rather than leaving the read loop blocked forever.
func TestTerminalWSClientCloseDoesNotHang(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	srv, wsURL := terminalTestServer(t)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// srv.Close() blocks until all outstanding handlers return.
	done := make(chan struct{})
	go func() {
		srv.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not return after client closed the connection")
	}
}
