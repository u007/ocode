package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/umlinfo"
)

const umlFixtureMain = `package main

// Greeter greets people.
type Greeter interface {
	Greet(name string) string
}

// Person is a human.
type Person struct{ Name string }

// Greet says hello.
func (p *Person) Greet(name string) string { return "hi " + name }

func main() {}
`

func newUmlFixture(t *testing.T) (string, *Handler) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/example/umlfixture\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(umlFixtureMain), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewHandler()
	h.workDir = root
	return root, h
}

func TestHandleUmlReturns202ThenReady(t *testing.T) {
	root, h := newUmlFixture(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/uml?project="+root, nil)
	h.HandleUml(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("first call: expected 202, got %d body %s", w.Code, w.Body.String())
	}

	// Wait for the background build to finish.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !h.umlIndexer.Building(root) {
			if idx, ok := h.umlIndexer.Get(root, mustFingerprint(t, h, root)); ok {
				_ = idx
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/uml?project="+root, nil)
	h.HandleUml(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("second call: expected 200, got %d body %s", w.Code, w.Body.String())
	}
	var resp umlResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.State != "ready" || resp.Index == nil {
		t.Fatalf("expected ready with index, got state=%q", resp.State)
	}
	names := map[string]bool{}
	for _, s := range resp.Index.Symbols {
		names[s.Name] = true
	}
	for _, want := range []string{"Greeter", "Person", "Greet", "main"} {
		if !names[want] {
			t.Errorf("symbol %q missing from index", want)
		}
	}
	if resp.Index.Monorepo != "none" {
		t.Errorf("monorepo = %q, want none", resp.Index.Monorepo)
	}
}

func TestHandleUmlRefusesHostAndOutsideRoot(t *testing.T) {
	root, h := newUmlFixture(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/uml?project="+root+"&host=somehost", nil)
	h.HandleUml(w, r)
	// An unknown host is not connected and must never be answered from this
	// machine's index.
	if w.Code != http.StatusBadGateway {
		t.Fatalf("unknown remote host: expected 502, got %d body %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "\"ready\"") {
		t.Fatalf("a remote request must never be answered with a local index, got %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/uml?project="+t.TempDir(), nil)
	h.HandleUml(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("outside root: expected 403, got %d body %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/uml", nil)
	h.HandleUml(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing project: expected 400, got %d", w.Code)
	}
}

func TestHandleUmlStatus(t *testing.T) {
	root, h := newUmlFixture(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/uml/status?project="+root, nil)
	h.HandleUmlStatus(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "\"state\":\"idle\"") {
		t.Fatalf("expected idle state, got %s", w.Body.String())
	}
}

func mustFingerprint(t *testing.T, h *Handler, root string) string {
	t.Helper()
	ctx := t.Context()
	fp, err := umlinfo.Fingerprint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// A remote project's UML request is proxied to the host's own server: that
// process indexes its own filesystem, and the local server must not answer with
// a local index (the paths only exist on the other machine).
func TestHandleUmlProxiesToTheRemoteHost(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	h := pulseRemoteTestHandler(t, f)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/uml?project="+pulseRemoteTestPath+"&host="+pulseRemoteTestHost, nil)
	h.HandleUml(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from the host, got %d body %s", w.Code, w.Body.String())
	}
	var resp umlResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.State != "ready" || resp.Index == nil || len(resp.Index.Symbols) != 1 || resp.Index.Symbols[0].Name != "Greeter" {
		t.Fatalf("expected the host's index, got %+v", resp)
	}
	if resp.Index.Root != pulseRemoteTestPath {
		t.Fatalf("the index must describe the host's project, got root %q", resp.Index.Root)
	}

	// The host param is consumed here; the host sees only its own project path.
	f.mu.Lock()
	queries := append([]string(nil), f.umlQueries...)
	f.mu.Unlock()
	if len(queries) != 1 || queries[0] != "project="+strings.ReplaceAll(pulseRemoteTestPath, "/", "%2F") {
		t.Fatalf("the host must receive only project, got %v", queries)
	}
}

// A host still building its index answers 202; the local server passes that
// through so the client's poll loop is identical for local and remote projects.
func TestHandleUmlRemoteIndexingPassesThrough(t *testing.T) {
	f := newPulseRemoteFake(t, "", 80, 24)
	f.umlIndexing = true
	h := pulseRemoteTestHandler(t, f)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/uml?project="+pulseRemoteTestPath+"&host="+pulseRemoteTestHost, nil)
	h.HandleUml(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body %s", w.Code, w.Body.String())
	}
	// The host answers 202, which decodes like a 200; its state is what drives
	// the client's poll, so the local answer must carry that through unchanged.
	if !strings.Contains(w.Body.String(), "\"indexing\"") {
		t.Fatalf("expected an indexing state, got %s", w.Body.String())
	}
}
