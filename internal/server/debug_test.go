package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDebugPath(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Path=%q RawPath=%q Pattern=%q PathValue(host)=%q", r.URL.Path, r.URL.RawPath, r.Pattern, r.PathValue("host"))
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/remote/user@realhost/browse/b/tab:x/https/example.com/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	t.Logf("status=%d", resp.StatusCode)
}
