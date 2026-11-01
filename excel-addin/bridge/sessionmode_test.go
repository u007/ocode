package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// leakyStatus is ocode's TUIStatus-shaped body. Every SECRET value must stay on the bridge side. mode is a JSON value
// (quoted, null, or a raw fragment) for the permission_mode field.
func leakyStatus(mode string) string {
	return `{"cwd":"/SECRET/cwd","extra_allowed_paths":["/SECRET/extra"],"lsp_servers":[{"command":"SECRET-LSP"}],` +
		`"model_prompt":"SECRET-PROMPT","modified_files":["SECRET-FILE"],"permission_mode":` + mode + `}`
}

// paneGet sends one GET to the bridge and returns the status and the trimmed body. withKey sets the pane key.
func paneGet(t *testing.T, url string, withKey bool) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if withKey {
		req.Header.Set("X-Bridge-Key", key)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, strings.TrimSpace(string(b))
}

func TestSessionModeReturnsOnlyTheModeOcodeReports(t *testing.T) {
	for _, mode := range []string{"normal", "yolo", "locked", "sandbox"} {
		t.Run(mode, func(t *testing.T) {
			ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
				tailJSON(w, http.StatusOK, leakyStatus(`"`+mode+`"`))
			})
			s := newLocalBridge(t, ocode.URL, t.TempDir())
			code, body := paneGet(t, s.URL+"/bridge/session-mode/ses_1", true)
			if code != http.StatusOK || body != `{"permission_mode":"`+mode+`"}` {
				t.Fatalf("got %d %s, want 200 with only the mode", code, body)
			}
			reqs := fo.requests()
			if len(reqs) != 1 || reqs[0].method != http.MethodGet || reqs[0].path != "/api/sessions/ses_1/status" || reqs[0].auth != "Bearer ocode-secret" {
				t.Fatalf("ocode saw %+v, want one GET of the status route with the ocode token", reqs)
			}
		})
	}
}

// TestSessionModeFailsClosed: anything other than a known mode is a failure, and ocode's text (which carries secrets
// in these fixtures) is never echoed to the pane.
func TestSessionModeFailsClosed(t *testing.T) {
	const shortUnusable = "ocode did not report a usable permission mode for this chat"
	cases := []struct {
		name     string
		status   int
		body     string
		want     int
		wantBody string
	}{
		{"unknown mode", http.StatusOK, leakyStatus(`"auto"`), http.StatusBadGateway, shortUnusable},
		{"empty mode", http.StatusOK, leakyStatus(`""`), http.StatusBadGateway, shortUnusable},
		{"null mode", http.StatusOK, leakyStatus(`null`), http.StatusBadGateway, shortUnusable},
		{"field missing", http.StatusOK, `{"cwd":"/SECRET/cwd","model_prompt":"SECRET-PROMPT"}`, http.StatusBadGateway, shortUnusable},
		{"not json", http.StatusOK, `<html>SECRET-PROMPT`, http.StatusBadGateway, shortUnusable},
		{"ocode 500", http.StatusInternalServerError, `boom SECRET-PROMPT /SECRET/cwd`, http.StatusBadGateway, shortUnusable},
		{"ocode 404", http.StatusNotFound, `{"error":"session not found SECRET-PROMPT"}`, http.StatusNotFound, "ocode has no such chat"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
				tailJSON(w, c.status, c.body)
			})
			s := newLocalBridge(t, ocode.URL, t.TempDir())
			code, body := paneGet(t, s.URL+"/bridge/session-mode/ses_1", true)
			if code != c.want {
				t.Fatalf("status %d, want %d (%s)", code, c.want, body)
			}
			if body != `{"error":"`+c.wantBody+`"}` {
				t.Fatalf("body %q, want the short message %q", body, c.wantBody)
			}
			if len(fo.requests()) != 1 {
				t.Fatalf("ocode saw %d requests, want exactly the status GET", len(fo.requests()))
			}
		})
	}
}

func TestSessionModeWhenOcodeIsUnreachable(t *testing.T) {
	s := newLocalBridge(t, "http://127.0.0.1:1", t.TempDir())
	code, body := paneGet(t, s.URL+"/bridge/session-mode/ses_1", true)
	if code != http.StatusBadGateway || body != `{"error":"ocode did not report a usable permission mode for this chat"}` {
		t.Fatalf("got %d %s, want 502 with the short message", code, body)
	}
}

// TestSessionModeRefusesBadIDsBeforeAskingOcode: the id reaches ocode's URL path, so only the session-id shape is
// accepted. A dot segment (%2e%2e decodes to "..") is refused by that shape check, and nothing reaches ocode in any case.
func TestSessionModeRefusesBadIDsBeforeAskingOcode(t *testing.T) {
	cases := []struct {
		name, id string
		want     int
	}{
		{"dot", "a.b", http.StatusBadRequest},
		{"space", "a%20b", http.StatusBadRequest},
		{"tilde", "a~b", http.StatusBadRequest},
		{"too long", strings.Repeat("a", 129), http.StatusBadRequest},
		{"dot segment", "%2e%2e", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
				tailJSON(w, http.StatusOK, leakyStatus(`"normal"`))
			})
			s := newLocalBridge(t, ocode.URL, t.TempDir())
			code, _ := paneGet(t, s.URL+"/bridge/session-mode/"+c.id, true)
			if code != c.want {
				t.Fatalf("status %d, want %d", code, c.want)
			}
			if n := len(fo.requests()); n != 0 {
				t.Fatalf("ocode saw %d requests for a refused id", n)
			}
		})
	}
}

func TestSessionModeNeedsThePaneKey(t *testing.T) {
	ocode, fo := startFakeOcode(t, func(w http.ResponseWriter, r *http.Request) {
		tailJSON(w, http.StatusOK, leakyStatus(`"normal"`))
	})
	s := newLocalBridge(t, ocode.URL, t.TempDir())
	code, _ := paneGet(t, s.URL+"/bridge/session-mode/ses_1", false)
	if code != http.StatusUnauthorized {
		t.Fatalf("without the pane key: status %d, want 401", code)
	}
	if n := len(fo.requests()); n != 0 {
		t.Fatalf("unauthenticated request reached ocode %d times", n)
	}
}
