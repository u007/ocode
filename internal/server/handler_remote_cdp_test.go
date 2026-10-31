package server

import (
	"net/http/httptest"
	"testing"
)

// The remote CDP route is a path rewrite, so it is tested as one: a full proxy
// harness needs a live remote workspace, and this is the part that would fail
// silently. The `host` query parameter MUST be stripped (the host's own handler
// reads it as a remote request of its own), and the two path values the proxy
// consumes must be set.
func TestPrepareRemoteCDPRequestStripsHostAndSetsPath(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/browse/cdp/tab%3A1?__grant=g&host=james%40217.216.72.49", nil)
	r.SetPathValue("stateKey", "tab:1")

	prepareRemoteCDPRequest(r, "james@217.216.72.49")

	if got := r.PathValue("host"); got != "james@217.216.72.49" {
		t.Fatalf("host path value = %q, want the host argument", got)
	}
	if got := r.PathValue("rest"); got != "browse/cdp/tab:1" {
		t.Fatalf("rest path value = %q, want browse/cdp/tab:1", got)
	}
	q := r.URL.Query()
	if got := q.Get("host"); got != "" {
		t.Fatalf("host query param was forwarded to the remote handler: %q", got)
	}
	if got := q.Get("__grant"); got != "g" {
		t.Fatalf("__grant = %q, want it preserved", got)
	}
}
