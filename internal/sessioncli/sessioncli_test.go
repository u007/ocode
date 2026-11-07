package sessioncli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeServer records every request and answers with status.
func fakeServer(t *testing.T, status int, seen func(r *http.Request, body []byte)) (*httptest.Server, *int64) {
	t.Helper()
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		body, _ := io.ReadAll(r.Body)
		if seen != nil {
			seen(r, body)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestSendRefusedWithoutYes(t *testing.T) {
	srv, hits := fakeServer(t, http.StatusAccepted, nil)
	err := Run([]string{"send", "--url", srv.URL, "ses_1", "hello"})
	if err == nil {
		t.Fatal("send without --yes succeeded; want refusal")
	}
	if atomic.LoadInt64(hits) != 0 {
		t.Fatalf("refused send still made %d request(s)", *hits)
	}
}

func TestQueueBodyIsValidJSONForAnyContent(t *testing.T) {
	content := "héllo \x01 \"quoted\" \\ back\tslash\nnewline ✓"
	var got map[string]any
	srv, _ := fakeServer(t, http.StatusAccepted, func(r *http.Request, body []byte) {
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("body is not valid JSON: %v (%q)", err, body)
		}
	})
	if err := Run([]string{"queue", "--url", srv.URL, "--yes", "ses_1", content}); err != nil {
		t.Fatalf("queue: %v", err)
	}
	if got["content"] != content {
		t.Fatalf("content round-trip = %q, want %q", got["content"], content)
	}
}

func TestSessionIDIsPathEscaped(t *testing.T) {
	var path string
	srv, _ := fakeServer(t, http.StatusAccepted, func(r *http.Request, _ []byte) {
		path = r.URL.EscapedPath()
	})
	if err := Run([]string{"queue", "--url", srv.URL, "--yes", "a/b", "x"}); err != nil {
		t.Fatalf("queue: %v", err)
	}
	if path != "/api/sessions/a%2Fb/queue" {
		t.Fatalf("path = %q, want the id escaped as one segment", path)
	}
}

func TestBasicAuthFromEnv(t *testing.T) {
	t.Setenv("OPENCODE_SERVER_USERNAME", "")
	t.Setenv("OPENCODE_SERVER_PASSWORD", "s3cret")
	var user, pass string
	var ok bool
	srv, _ := fakeServer(t, http.StatusOK, func(r *http.Request, _ []byte) {
		user, pass, ok = r.BasicAuth()
	})
	if err := Run([]string{"status", "--url", srv.URL, "ses_1"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !ok || user != "opencode" || pass != "s3cret" {
		t.Fatalf("basic auth = (%q, %q, %v), want (opencode, s3cret, true)", user, pass, ok)
	}
}

func TestHTTPErrorIsReturned(t *testing.T) {
	srv, _ := fakeServer(t, http.StatusConflict, nil)
	if err := Run([]string{"queue", "--url", srv.URL, "--yes", "ses_1", "x"}); err == nil {
		t.Fatal("HTTP 409 returned nil error; want failure")
	}
}

func TestBaseURLResolution(t *testing.T) {
	if got := (connFlags{}).baseURL(); got != "http://localhost:4096" {
		t.Fatalf("default base = %q", got)
	}
	if got := (connFlags{port: 8080}).baseURL(); got != "http://localhost:8080" {
		t.Fatalf("port base = %q", got)
	}
	if got := (connFlags{url: "https://ocode.example/"}).baseURL(); got != "https://ocode.example" {
		t.Fatalf("url base = %q", got)
	}
}

func TestEmptyContentRefused(t *testing.T) {
	srv, hits := fakeServer(t, http.StatusAccepted, nil)
	if err := Run([]string{"queue", "--url", srv.URL, "--yes", "ses_1", "   "}); err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("blank content error = %v", err)
	}
	if atomic.LoadInt64(hits) != 0 {
		t.Fatal("blank content still reached the server")
	}
}
