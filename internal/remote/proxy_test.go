package remote

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewAPIProxy_ForwardsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	proxy, err := NewAPIProxy(srv.URL, "remote-tok", nil)
	if err != nil {
		t.Fatalf("NewAPIProxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/models", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if gotPath != "/api/models" {
		t.Errorf("got path %q, want %q", gotPath, "/api/models")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestNewAPIProxy_StripsLocalTokenQuery(t *testing.T) {
	var gotToken string
	var gotOther string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.URL.Query().Get("token")
		gotOther = r.URL.Query().Get("other")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	proxy, err := NewAPIProxy(srv.URL, "remote-tok", nil)
	if err != nil {
		t.Fatalf("NewAPIProxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/models?token=local-secret&other=keep", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if gotToken != "" {
		t.Errorf("remote received query token %q, want empty (stripped)", gotToken)
	}
	if gotOther != "keep" {
		t.Errorf("other query param lost: got %q, want %q", gotOther, "keep")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestNewAPIProxy_ReplacesAuthorization(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	proxy, err := NewAPIProxy(srv.URL, "remote-secret-999", nil)
	if err != nil {
		t.Fatalf("NewAPIProxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer local-token")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if gotAuth != "Bearer remote-secret-999" {
		t.Errorf("got Authorization %q, want %q", gotAuth, "Bearer remote-secret-999")
	}
}

func TestNewAPIProxy_ClosedBackend_502AndOnErr(t *testing.T) {
	// Create a server and immediately close it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	var errCount int
	var lastErr error
	rec := httptest.NewRecorder()
	// Pin the ordering contract: onError must run BEFORE the 502 is written,
	// so it observes the recorder's default 200 code.
	var codeInOnErr int
	proxy, err := NewAPIProxy(closedURL, "tok", func(e error) {
		codeInOnErr = rec.Code
		errCount++
		lastErr = e
	})
	if err != nil {
		t.Fatalf("NewAPIProxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	proxy.ServeHTTP(rec, req)

	if codeInOnErr != http.StatusOK {
		t.Errorf("onError saw status %d, want %d (default before 502 write)", codeInOnErr, http.StatusOK)
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("got %d, want %d (502)", rec.Code, http.StatusBadGateway)
	}
	if errCount != 1 {
		t.Errorf("onError called %d times, want 1", errCount)
	}
	if lastErr == nil {
		t.Error("onError received nil error")
	}
}

func TestNewAPIProxy_OnErrorNil(t *testing.T) {
	// Closed backend with nil onError — must return 502 and not panic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	proxy, err := NewAPIProxy(closedURL, "tok", nil)
	if err != nil {
		t.Fatalf("NewAPIProxy: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("got %d, want %d (502)", rec.Code, http.StatusBadGateway)
	}
}

func TestNewAPIProxy_FlushSSE(t *testing.T) {
	// The first SSE chunk must reach the client while the backend handler is
	// still blocked — a proxy that buffers the whole stream would hang here.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement Flusher")
			return
		}
		w.Write([]byte("data: first-chunk\n\n"))
		flusher.Flush()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
			t.Error("backend timed out waiting for release")
		}
		w.Write([]byte("data: second-chunk\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	proxy, err := NewAPIProxy(srv.URL, "tok", nil)
	if err != nil {
		t.Fatalf("NewAPIProxy: %v", err)
	}
	// A real listener + client so transport flushing is observable
	// (httptest.ResponseRecorder buffers everything).
	liveSrv := httptest.NewServer(proxy)
	defer liveSrv.Close()

	resp, err := http.Get(liveSrv.URL + "/api/stream")
	if err != nil {
		t.Fatalf("GET /api/stream: %v", err)
	}
	defer resp.Body.Close()

	// Read one line at a time: io.Copy with an io.LimitReader would block
	// until the byte limit or EOF and could never observe an incremental flush.
	br := bufio.NewReader(resp.Body)
	type lineResult struct {
		line string
		err  error
	}
	first := make(chan lineResult, 1)
	go func() {
		line, err := br.ReadString('\n')
		first <- lineResult{line, err}
	}()

	select {
	case r := <-first:
		if r.err != nil {
			t.Fatalf("read first line: %v", r.err)
		}
		if !strings.Contains(r.line, "first-chunk") {
			t.Errorf("first line = %q, want it to contain %q", r.line, "first-chunk")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first chunk before the backend returned — flush broken")
	}

	// The first chunk arrived while the backend was still blocked; release it
	// and confirm the rest of the stream still arrives.
	close(release)
	rest, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("read rest of body: %v", err)
	}
	if !strings.Contains(string(rest), "second-chunk") {
		t.Errorf("rest of body = %q, want it to contain %q", string(rest), "second-chunk")
	}
}

func TestNewAPIProxy_InvalidURL(t *testing.T) {
	proxy, err := NewAPIProxy("://bad", "", nil)
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
	if proxy != nil {
		t.Errorf("expected nil proxy, got %v", proxy)
	}
}

func TestInjectAuth_StripsTokenAndSetsBearer(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/x?token=local&keep=1", nil)
	req.Header.Set("Authorization", "Bearer local-tok")

	InjectAuth(req, "remote-xyz")

	if req.URL.Query().Get("token") != "" {
		t.Errorf("token query param not stripped")
	}
	if req.URL.Query().Get("keep") != "1" {
		t.Errorf("other query params lost")
	}
	if req.Header.Get("Authorization") != "Bearer remote-xyz" {
		t.Errorf("Authorization = %q, want %q", req.Header.Get("Authorization"), "Bearer remote-xyz")
	}
}

func TestInjectAuth_EmptyRemoteToken_NoAuth(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/x?token=local", nil)
	req.Header.Set("Authorization", "Bearer local-tok")

	InjectAuth(req, "")

	if req.URL.Query().Get("token") != "" {
		t.Errorf("token query param not stripped")
	}
	if req.Header.Get("Authorization") != "" {
		t.Errorf("Authorization should be empty for empty remote token, got %q", req.Header.Get("Authorization"))
	}
}
