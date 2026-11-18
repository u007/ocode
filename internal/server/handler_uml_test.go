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
	if w.Code != http.StatusBadRequest {
		t.Fatalf("host param: expected 400, got %d", w.Code)
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
