package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/tool"
)

// newRCResolveServer builds a Server with a registered external session so the
// RC resolve routes are wired behind authMiddleware. The RC bridge token is the
// same secret as the server password (matching production wiring), so a Bearer
// request with that password authenticates.
func newRCResolveServer(t *testing.T, register bool) *Server {
	t.Helper()
	s := New("localhost:0", "", "test-secret", nil)
	if register {
		resolveCh := make(chan RCResolution, 1)
		s.RegisterExternalSession("tui-sess", "test-model", make(chan RCRequest, 1), resolveCh, "test-secret")
	}
	return s
}

func bearer(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
}

func TestHandleRCPermissionResolve(t *testing.T) {
	s := newRCResolveServer(t, true)

	// Valid "allow" forwards to the bridge.
	body := `{"request_id":"r1","decision":"allow"}`
	req := httptest.NewRequest("POST", "/api/rc/permission/resolve", strings.NewReader(body))
	bearer(req, "test-secret")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	select {
	case res := <-s.rc().ResolveCh:
		if res.RequestID != "r1" || res.Decision != "allow" {
			t.Fatalf("unexpected resolution: %+v", res)
		}
	case <-time.After(time.Second):
		t.Fatalf("no resolution forwarded to the bridge")
	}

	// Invalid decision is rejected with 400.
	body = `{"request_id":"r2","decision":"maybe"}`
	req = httptest.NewRequest("POST", "/api/rc/permission/resolve", strings.NewReader(body))
	bearer(req, "test-secret")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}

	// Missing fields are rejected with 400.
	req = httptest.NewRequest("POST", "/api/rc/permission/resolve", strings.NewReader(`{"decision":"allow"}`))
	bearer(req, "test-secret")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing request_id", rec.Code)
	}

	// No active bridge returns 404. A separate server (registered=false) has no
	// rc bridge; auth is still required by the middleware.
	s2 := newRCResolveServer(t, false)
	req = httptest.NewRequest("POST", "/api/rc/permission/resolve", strings.NewReader(`{"request_id":"r3","decision":"allow"}`))
	bearer(req, "test-secret")
	rec = httptest.NewRecorder()
	s2.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when no bridge", rec.Code)
	}
}

func TestHandleRCPermissionResolveUnauthorized(t *testing.T) {
	s := newRCResolveServer(t, true)
	body := `{"request_id":"r1","decision":"allow"}`

	// No token at all -> 401.
	req := httptest.NewRequest("POST", "/api/rc/permission/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 without token", rec.Code)
	}

	// Wrong token -> 401.
	req = httptest.NewRequest("POST", "/api/rc/permission/resolve", strings.NewReader(body))
	bearer(req, "wrong")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 with wrong token", rec.Code)
	}

	// Correct bearer -> 200 and forwarded.
	req = httptest.NewRequest("POST", "/api/rc/permission/resolve", strings.NewReader(body))
	bearer(req, "test-secret")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with correct token (body=%s)", rec.Code, rec.Body.String())
	}
	select {
	case <-s.rc().ResolveCh:
	case <-time.After(time.Second):
		t.Fatalf("no resolution forwarded to the bridge")
	}
}

func TestHandleRCQuestionAnswer(t *testing.T) {
	s := newRCResolveServer(t, true)

	body := `{"request_id":"r1","answers":[{"question":"q","answers":[{"label":"Staging","text":"Staging","custom":false}]}]}`
	req := httptest.NewRequest("POST", "/api/rc/question/answer", strings.NewReader(body))
	bearer(req, "test-secret")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	select {
	case res := <-s.rc().ResolveCh:
		if res.RequestID != "r1" || len(res.Answers) != 1 || len(res.Answers[0].Answers) != 1 || res.Answers[0].Answers[0].Label != "Staging" {
			t.Fatalf("unexpected resolution: %+v", res)
		}
	case <-time.After(time.Second):
		t.Fatalf("no resolution forwarded to the bridge")
	}

	// Missing request_id is rejected with 400.
	req = httptest.NewRequest("POST", "/api/rc/question/answer", strings.NewReader(`{"answers":[]}`))
	bearer(req, "test-secret")
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing request_id", rec.Code)
	}

	// No active bridge returns 404.
	s2 := newRCResolveServer(t, false)
	req = httptest.NewRequest("POST", "/api/rc/question/answer", strings.NewReader(`{"request_id":"r2","answers":[]}`))
	bearer(req, "test-secret")
	rec = httptest.NewRecorder()
	s2.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when no bridge", rec.Code)
	}

	// Malformed answers still parse (empty slice is valid).
	var decoded struct {
		Answers []tool.QuestionAnswer `json:"answers"`
	}
	_ = json.Unmarshal([]byte(`{"answers":[]}`), &decoded)
}
