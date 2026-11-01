package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// A leaked pane key must not be able to start an agent without permission prompts, or anywhere outside the workspaces.
func TestChatAndProjectBodiesAreConstrainedToTheWorkspace(t *testing.T) {
	root := t.TempDir()
	var gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, "{}")
	}))
	defer up.Close()
	h, _, err := NewHandler(Config{OcodeURL: up.URL, PaneKey: key, WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	post := func(path, body string) int {
		gotBody = ""
		req, _ := http.NewRequest("POST", s.URL+path, strings.NewReader(body))
		req.Header.Set("X-Bridge-Key", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	ws := filepath.Join(root, "wb-1")

	good := `{"content":"hi","project_path":"` + ws + `","permission_mode":"normal","async":true}`
	if c := post("/api/chat", good); c != 200 || gotBody != good {
		t.Fatalf("a normal pane chat must be forwarded byte for byte: %d %q", c, gotBody)
	}
	if c := post("/api/chat", `{"content":"hi","project_path":"`+ws+`"}`); c != 200 {
		t.Fatalf("no permission_mode is fine (ocode's default): %d", c)
	}
	// yolo is allowed on purpose: the pane's permission picker offers it behind a confirm (RESUME.md, 2026-11-01).
	yolo := `{"content":"hi","project_path":"` + ws + `","permission_mode":"yolo"}`
	if c := post("/api/chat", yolo); c != 200 || gotBody != yolo {
		t.Fatalf("a yolo pane chat must be forwarded byte for byte: %d %q", c, gotBody)
	}
	for name, body := range map[string]string{
		"sandbox":                `{"content":"x","project_path":"` + ws + `","permission_mode":"sandbox"}`,
		"unknown mode":           `{"content":"x","project_path":"` + ws + `","permission_mode":"auto"}`,
		"mode case variant":      `{"content":"x","project_path":"` + ws + `","permission_mode":"normal","PERMISSION_MODE":"sandbox"}`,
		"path case variant":      `{"content":"x","project_path":"` + ws + `","PROJECT_PATH":"/etc","permission_mode":"normal"}`,
		"mode only case variant": `{"content":"x","project_path":"` + ws + `","Permission_Mode":"yolo"}`,
		"outside root":           `{"content":"x","project_path":"/etc","permission_mode":"normal"}`,
		"root itself":            `{"content":"x","project_path":"` + root + `"}`,
		"dotdot escape":          `{"content":"x","project_path":"` + root + `/../..","permission_mode":"normal"}`,
		"unclean":                `{"content":"x","project_path":"` + ws + `/./x"}`,
		"home":                   `{"content":"x","project_path":"~/x"}`,
		"relative":               `{"content":"x","project_path":"wb-1"}`,
		"missing path":           `{"content":"x"}`,
		"wrong type":             `{"content":"x","project_path":5}`,
		"not json":               `nope`,
		"sibling prefix":         `{"content":"x","project_path":"` + root + `-evil/x"}`,
		"mode wrong type":        `{"content":"x","project_path":"` + ws + `","permission_mode":true}`,
	} {
		if c := post("/api/chat", body); c != 403 || gotBody != "" {
			t.Errorf("%s: want 403 and nothing forwarded, got %d (forwarded %q)", name, c, gotBody)
		}
	}

	if c := post("/api/projects", `{"path":"`+ws+`"}`); c != 200 {
		t.Fatalf("registering a workspace must work: %d", c)
	}
	for name, body := range map[string]string{
		"outside":           `{"path":"/home"}`,
		"remote host":       `{"path":"` + ws + `","host":"evil.example"}`,
		"port":              `{"path":"` + ws + `","port":22}`,
		"empty":             `{}`,
		"path case variant": `{"path":"` + ws + `","PATH":"/home"}`,
		"host case variant": `{"path":"` + ws + `","Host":"evil.example"}`,
		"port case variant": `{"path":"` + ws + `","PORT":22}`,
	} {
		if c := post("/api/projects", body); c != 403 || gotBody != "" {
			t.Errorf("projects %s: want 403 and nothing forwarded, got %d", name, c)
		}
	}
}

// PUT /api/permissions/mode sets the permission mode of one live session. Its body is checked as strictly as a
// chat's: a mode from the allowed set, a session id in the id alphabet, and no key that ocode would bind to
// "mode" or "session_id" under another spelling. Other keys are ignored by ocode and forwarded unchanged.
func TestPermissionModeBodyIsConstrained(t *testing.T) {
	var gotPath, gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath = r.Method + " " + r.URL.Path
		gotBody = string(b)
		io.WriteString(w, "{}")
	}))
	defer up.Close()
	h, _, err := NewHandler(Config{OcodeURL: up.URL, PaneKey: key, WorkspaceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	put := func(body string) int {
		gotPath, gotBody = "", ""
		req, _ := http.NewRequest("PUT", s.URL+"/api/permissions/mode", strings.NewReader(body))
		req.Header.Set("X-Bridge-Key", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	for _, tc := range []struct{ name, body string }{
		{"normal", `{"mode":"normal","session_id":"ses_abc-1.2_X"}`},
		{"locked", `{"mode":"locked","session_id":"ses_abc"}`},
		{"yolo", `{"mode":"yolo","session_id":"ses_abc"}`},
		{"other keys are ignored by ocode", `{"mode":"normal","session_id":"ses_abc","note":5}`},
	} {
		if c := put(tc.body); c != 200 || gotBody != tc.body || gotPath != "PUT /api/permissions/mode" {
			t.Errorf("%s: want 200 and the body forwarded byte for byte, got %d (forwarded %q to %q)", tc.name, c, gotBody, gotPath)
		}
	}

	for name, body := range map[string]string{
		"sandbox":                 `{"mode":"sandbox","session_id":"ses_abc"}`,
		"empty mode":              `{"mode":"","session_id":"ses_abc"}`,
		"missing mode":            `{"session_id":"ses_abc"}`,
		"null mode":               `{"mode":null,"session_id":"ses_abc"}`,
		"mode wrong type":         `{"mode":true,"session_id":"ses_abc"}`,
		"mode case variant":       `{"mode":"normal","MODE":"sandbox","session_id":"ses_abc"}`,
		"only capitalised mode":   `{"Mode":"yolo","session_id":"ses_abc"}`,
		"missing session id":      `{"mode":"normal"}`,
		"empty session id":        `{"mode":"normal","session_id":""}`,
		"null session id":         `{"mode":"normal","session_id":null}`,
		"session id wrong type":   `{"mode":"normal","session_id":5}`,
		"session id with slash":   `{"mode":"normal","session_id":"ses/abc"}`,
		"session id with space":   `{"mode":"normal","session_id":"ses abc"}`,
		"session id newline":      `{"mode":"normal","session_id":"ses_abc\n"}`,
		"session id case variant": `{"mode":"normal","SESSION_ID":"ses_other","session_id":"ses_abc"}`,
		"long s session id":       "{\"mode\":\"normal\",\"session_id\":\"ses_abc\",\"\u017Fession_id\":\"ses_other\"}",
		"not json":                `mode=normal`,
		"json array":              `[]`,
		"trailing text":           `{"mode":"normal","session_id":"ses_abc"} x`,
	} {
		if c := put(body); c != 403 || gotBody != "" {
			t.Errorf("%s: want 403 and nothing forwarded, got %d (forwarded %q)", name, c, gotBody)
		}
	}
}

func TestChatIsRefusedWhenNoWorkspaceRootIsKnown(t *testing.T) {
	h := allowAPI("", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("must not forward") }))
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(`{"project_path":"/anything/at/all"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("fail closed: got %d", rec.Code)
	}
}

// A pane chat may pin only the excel MCP server. A pin attaches every tool of the named server from turn one, so
// absent or null is the only other value accepted, and a key that differs from pinned_mcp_servers only by case or
// Unicode folding is refused like the other checked keys.
func TestChatPinnedMCPServersBodyIsConstrained(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "wb-1")
	var gotBody string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, "{}")
	}))
	defer up.Close()
	h, _, err := NewHandler(Config{OcodeURL: up.URL, PaneKey: key, WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(h)
	defer s.Close()
	post := func(body string) int {
		gotBody = ""
		req, _ := http.NewRequest("POST", s.URL+"/api/chat", strings.NewReader(body))
		req.Header.Set("X-Bridge-Key", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// extra is spliced in after permission_mode, so each case is a complete chat body.
	chat := func(extra string) string {
		return `{"content":"hi","project_path":"` + ws + `","permission_mode":"normal"` + extra + `}`
	}

	for _, tc := range []struct{ name, extra string }{
		{"absent", ""},
		{"null", `,"pinned_mcp_servers":null`},
		{"excel", `,"pinned_mcp_servers":["excel"]`},
	} {
		body := chat(tc.extra)
		if c := post(body); c != 200 || gotBody != body {
			t.Errorf("%s: want 200 and the body forwarded byte for byte, got %d (forwarded %q)", tc.name, c, gotBody)
		}
	}

	for _, tc := range []struct{ name, extra string }{
		{"empty array", `,"pinned_mcp_servers":[]`},
		{"other server", `,"pinned_mcp_servers":["filesystem"]`},
		{"excel and another", `,"pinned_mcp_servers":["excel","filesystem"]`},
		{"excel twice", `,"pinned_mcp_servers":["excel","excel"]`},
		{"name with a space", `,"pinned_mcp_servers":["excel "]`},
		{"capitalised name", `,"pinned_mcp_servers":["Excel"]`},
		{"string instead of array", `,"pinned_mcp_servers":"excel"`},
		{"object instead of array", `,"pinned_mcp_servers":{"excel":true}`},
		{"null element", `,"pinned_mcp_servers":[null]`},
		{"number element", `,"pinned_mcp_servers":[1]`},
		{"nested array", `,"pinned_mcp_servers":[["excel"]]`},
		{"boolean", `,"pinned_mcp_servers":true`},
		{"exact key twice, refused value last", `,"pinned_mcp_servers":["excel"],"pinned_mcp_servers":["filesystem"]`},
		{"upper-case key next to the exact key", `,"pinned_mcp_servers":["excel"],"PINNED_MCP_SERVERS":["excel"]`},
		{"upper-case key alone", `,"PINNED_MCP_SERVERS":["excel"]`},
		{"upper-case null key", `,"PINNED_MCP_SERVERS":null`},
		{"mixed-case key with a refused value", `,"Pinned_Mcp_Servers":["filesystem"]`},
		{"long s key", "," + `"pinned_mcp_` + "ſ" + `ervers":["filesystem"]`},
	} {
		body := chat(tc.extra)
		if c := post(body); c != 403 || gotBody != "" {
			t.Errorf("%s: want 403 and nothing forwarded, got %d (forwarded %q)", tc.name, c, gotBody)
		}
	}
}
