//go:build !windows

// (appended tests)
package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/u007/ocode/internal/projects"
)

func TestTerminalWSCwdAllowed(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	requirePTY(t)
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(root); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, true)
	h.projects = store

	srv := httptest.NewServer(http.HandlerFunc(h.HandleTerminalWS))
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?project_path=" + url.QueryEscape(root) + "&cwd=" + url.QueryEscape(sub)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v (status %v)", err, respStatus(resp))
	}
	if resp != nil {
		resp.Body.Close()
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("pwd\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var seen strings.Builder
	want := "/" + filepath.Base(sub)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
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

func TestTerminalWSCwdRejectedOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(root); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, true)
	h.projects = store

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/terminal/ws?project_path="+url.QueryEscape(root)+"&cwd="+url.QueryEscape(outside), nil)
	h.HandleTerminalWS(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestTerminalWSCwdSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	store, err := projects.NewStoreAt(filepath.Join(t.TempDir(), "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(root); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, true)
	h.projects = store

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/terminal/ws?project_path="+url.QueryEscape(root)+"&cwd="+url.QueryEscape(link), nil)
	h.HandleTerminalWS(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// A relative cwd resolves against the handler's work dir (the project root),
// never the process cwd. The directory name is unique to this test, so it
// cannot exist under the package directory the test process runs in.
func TestTerminalWSRelativeCwdResolvesUnderWorkDir(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	requirePTY(t)
	root := t.TempDir()
	const rel = "relative-cwd-probe"
	sub := filepath.Join(root, rel)
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedSub, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.workDir = root
	h.SetTerminalAccessPolicy(false, true)

	srv := httptest.NewServer(http.HandlerFunc(h.HandleTerminalWS))
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "?cwd=" + url.QueryEscape(rel)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v (status %v)", err, respStatus(resp))
	}
	if resp != nil {
		resp.Body.Close()
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("pwd\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var seen strings.Builder
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read failed before seeing pwd output (got %q): %v", seen.String(), err)
		}
		seen.Write(data)
		if strings.Contains(seen.String(), resolvedSub) {
			return
		}
	}
}

// A relative cwd that climbs out of the project root is refused by the same
// containment check as an absolute one.
func TestTerminalWSRelativeCwdEscapeRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	rel, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	h.workDir = root
	h.SetTerminalAccessPolicy(false, true)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/terminal/ws?cwd="+url.QueryEscape(rel), nil)
	h.HandleTerminalWS(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (cwd %q)", w.Code, http.StatusForbidden, rel)
	}
}

func respStatus(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
