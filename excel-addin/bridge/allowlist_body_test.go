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
	for name, body := range map[string]string{
		"yolo":            `{"content":"x","project_path":"` + ws + `","permission_mode":"yolo"}`,
		"sandbox":         `{"content":"x","project_path":"` + ws + `","permission_mode":"sandbox"}`,
		"outside root":    `{"content":"x","project_path":"/etc","permission_mode":"normal"}`,
		"root itself":     `{"content":"x","project_path":"` + root + `"}`,
		"dotdot escape":   `{"content":"x","project_path":"` + root + `/../..","permission_mode":"normal"}`,
		"unclean":         `{"content":"x","project_path":"` + ws + `/./x"}`,
		"home":            `{"content":"x","project_path":"~/x"}`,
		"relative":        `{"content":"x","project_path":"wb-1"}`,
		"missing path":    `{"content":"x"}`,
		"wrong type":      `{"content":"x","project_path":5}`,
		"not json":        `nope`,
		"sibling prefix":  `{"content":"x","project_path":"` + root + `-evil/x"}`,
		"mode wrong type": `{"content":"x","project_path":"` + ws + `","permission_mode":true}`,
	} {
		if c := post("/api/chat", body); c != 403 || gotBody != "" {
			t.Errorf("%s: want 403 and nothing forwarded, got %d (forwarded %q)", name, c, gotBody)
		}
	}

	if c := post("/api/projects", `{"path":"`+ws+`"}`); c != 200 {
		t.Fatalf("registering a workspace must work: %d", c)
	}
	for name, body := range map[string]string{
		"outside":     `{"path":"/home"}`,
		"remote host": `{"path":"` + ws + `","host":"evil.example"}`,
		"port":        `{"path":"` + ws + `","port":22}`,
		"empty":       `{}`,
	} {
		if c := post("/api/projects", body); c != 403 || gotBody != "" {
			t.Errorf("projects %s: want 403 and nothing forwarded, got %d", name, c)
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
