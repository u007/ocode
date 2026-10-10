package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// The handler tests in handler_bash_rules_test.go call HandleSetBashRules
// directly. That cannot catch a missing or shadowed ROUTE — the failure mode is
// a 404 from the SPA fallback, which is how a settings write that "saves" and
// changes nothing would present. So drive the real mux.
func newBashRulesRouter(t *testing.T) *Server {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	srv := New("127.0.0.1:0", "", "", nil)
	srv.handler.mu.Lock()
	srv.handler.cfg = &config.Config{}
	srv.handler.mu.Unlock()
	return srv
}

func TestBashRulesRouteIsRegistered(t *testing.T) {
	srv := newBashRulesRouter(t)
	h := srv.serveHandler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/permissions/bash-rules",
		strings.NewReader(`{"set":{"git push":"deny"}}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/permissions/bash-rules = %d body=%s (a 404 means the route is missing)", rec.Code, rec.Body.String())
	}

	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if got := cfg.Permissions.Bash.Prefixes["git push"]; got != "deny" {
		t.Fatalf("router PUT did not persist: %#v", cfg.Permissions.Bash.Prefixes)
	}
}

// The route sits behind authMiddleware like its sibling: these endpoints mutate
// the live permission state of every agent, so an unauthenticated write must not
// reach the handler. With no credentials configured the middleware passes
// through, so drive it with a server that has an auth token set.
func TestBashRulesRouteRejectsUnauthenticatedWrite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	srv := New("127.0.0.1:0", "ban-user", "ban-pass", nil)
	srv.handler.mu.Lock()
	srv.handler.cfg = &config.Config{}
	srv.handler.mu.Unlock()
	h := srv.serveHandler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/permissions/bash-rules",
		strings.NewReader(`{"set":{"git push":"deny"}}`))
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("unauthenticated PUT was accepted: %s", rec.Body.String())
	}
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Logf("unauthenticated PUT status = %d (not 200, which is the contract)", rec.Code)
	}

	cfg, err := config.LoadOcodeConfigCopy()
	if err != nil {
		t.Fatalf("LoadOcodeConfigCopy: %v", err)
	}
	if len(cfg.Permissions.Bash.Prefixes) != 0 {
		t.Fatalf("an unauthenticated write reached disk: %#v", cfg.Permissions.Bash.Prefixes)
	}

	// With credentials it must succeed, proving the 401 was the auth layer and
	// not a broken route.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/permissions/bash-rules",
		strings.NewReader(`{"set":{"git push":"deny"}}`))
	req.SetBasicAuth("ban-user", "ban-pass")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated PUT = %d body=%s", rec.Code, rec.Body.String())
	}
}
