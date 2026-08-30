package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/u007/ocode/internal/scheduler"
)

func TestSchedulerRoutesUseCORS(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := New("127.0.0.1:0", "", "", nil)
	svc := scheduler.NewService(filepath.Join(t.TempDir(), "jobs.json"))
	srv.SetScheduler(svc)

	const origin = "http://localhost:5173"
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/cron"},
		{http.MethodPost, "/api/cron"},
		{http.MethodGet, "/api/cron/missing"},
		{http.MethodPatch, "/api/cron/missing"},
		{http.MethodDelete, "/api/cron/missing"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Origin", origin)
			res := httptest.NewRecorder()
			srv.serveHandler().ServeHTTP(res, req)
			if got := res.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, origin)
			}
		})
	}

	req := httptest.NewRequest(http.MethodOptions, "/api/cron", nil)
	req.Header.Set("Origin", origin)
	res := httptest.NewRecorder()
	srv.serveHandler().ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want %d", res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("OPTIONS Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
}

func TestCORSPolicyAuthAndNonAPIHandling(t *testing.T) {
	srv := New("127.0.0.1:0", "user", "secret", nil)
	srv.mux.HandleFunc("GET /api/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv.mux.HandleFunc("GET /non-api-test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	const allowedOrigin = "http://localhost:5173"
	request := func(method, path, origin string, auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Origin", origin)
		if auth {
			req.SetBasicAuth("user", "secret")
		}
		res := httptest.NewRecorder()
		srv.serveHandler().ServeHTTP(res, req)
		return res
	}

	res := request(http.MethodGet, "/api/test", allowedOrigin, true)
	if res.Code != http.StatusNoContent {
		t.Fatalf("authenticated API status = %d, want %d", res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Fatalf("authenticated API Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
	}

	res = request(http.MethodOptions, "/api/test", allowedOrigin, false)
	if res.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Fatalf("preflight Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
	}

	const disallowedOrigin = "https://evil.example"
	res = request(http.MethodGet, "/api/test", disallowedOrigin, true)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed API Access-Control-Allow-Origin = %q, want empty", got)
	}

	res = request(http.MethodGet, "/non-api-test", disallowedOrigin, false)
	if res.Code != http.StatusNoContent {
		t.Fatalf("non-API status = %d, want %d", res.Code, http.StatusNoContent)
	}
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("non-API Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestServeUsesCORSForLateSchedulerRoutes(t *testing.T) {
	srv := New("127.0.0.1:0", "", "", nil)
	svc := scheduler.NewService(filepath.Join(t.TempDir(), "jobs.json"))
	srv.SetScheduler(svc)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ln) }()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
		select {
		case err := <-serveDone:
			if err != nil && err != http.ErrServerClosed {
				t.Errorf("serve: %v", err)
			}
		case <-ctx.Done():
			t.Errorf("serve did not stop: %v", ctx.Err())
		}
	})

	client := &http.Client{Timeout: 200 * time.Millisecond}
	var res *http.Response
	for range 50 {
		req, reqErr := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/api/cron", nil)
		if reqErr != nil {
			t.Fatal(reqErr)
		}
		req.Header.Set("Origin", "http://localhost:5173")
		res, err = client.Do(req)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want allowed origin", got)
	}
}
