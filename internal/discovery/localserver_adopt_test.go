package discovery

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEnsureLocalServer_adoptSetsReady guards the fix for the stale `local: none`
// status: when EnsureLocalServer adopts an already-running healthy server
// (instead of spawning one), it must report status "ready". Previously only the
// spawn+wait path called setStatus, so an adopted server left the persisted
// status stuck at "none" forever.
func TestEnsureLocalServer_adoptSetsReady(t *testing.T) {
	man, ok := ManifestForModel("local/bge-m3")
	if !ok {
		t.Skip("no local/bge-m3 manifest on this host")
	}
	served := man.ExpectedServeID()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == man.HealthPath {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"data":[{"id":%q}]}`, served)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	// Reset the package singleton so the in-process fast path doesn't short-circuit
	// the adopt path we want to exercise.
	StopLocalServer()
	defer StopLocalServer()

	var statuses []string
	setStatus := func(s string) { statuses = append(statuses, s) }
	spawn := func(string) error { t.Fatal("must adopt, not spawn"); return nil }

	base, dim, err := EnsureLocalServer(spawn, "local/bge-m3", t.TempDir(), setStatus,
		LocalServerOptions{UserBaseURL: srv.URL})
	if err != nil {
		t.Fatalf("adopt failed: %v", err)
	}
	if base != srv.URL {
		t.Fatalf("base = %q, want %q", base, srv.URL)
	}
	if dim != man.Dim {
		t.Fatalf("dim = %d, want %d", dim, man.Dim)
	}
	ready := false
	for _, s := range statuses {
		if s == "ready" {
			ready = true
		}
	}
	if !ready {
		t.Fatalf("adopt path did not set status \"ready\"; got %v", statuses)
	}
}
