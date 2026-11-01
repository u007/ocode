package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/session"
	"github.com/u007/ocode/internal/tool"
)

func TestPinnedMCPServersFromMetadata(t *testing.T) {
	cases := []struct {
		name    string
		md      map[string]any
		want    []string
		wantErr bool
	}{
		{name: "absent key is no pins", md: map[string]any{"model": "x"}, want: nil},
		{name: "in-memory string slice", md: map[string]any{pinnedMCPServersMetadataKey: []string{"excel"}}, want: []string{"excel"}},
		{name: "JSON round-trip yields any slice", md: map[string]any{pinnedMCPServersMetadataKey: []any{"excel", "notion"}}, want: []string{"excel", "notion"}},
		{name: "empty any slice", md: map[string]any{pinnedMCPServersMetadataKey: []any{}}, want: []string{}},
		{name: "non-string element is an error", md: map[string]any{pinnedMCPServersMetadataKey: []any{"excel", 3.0}}, wantErr: true},
		{name: "scalar string is an error", md: map[string]any{pinnedMCPServersMetadataKey: "excel"}, wantErr: true},
		{name: "null is an error", md: map[string]any{pinnedMCPServersMetadataKey: nil}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pinnedMCPServersFromMetadata(tc.md)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("names = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHandleChatPersistsPinnedMCPServersAtCreation mirrors the model pin test:
// the pins a request names when it creates a session must reach the persisted
// metadata, where buildAgentSession reads them back on every rebuild.
func TestHandleChatPersistsPinnedMCPServersAtCreation(t *testing.T) {
	h := NewHandler()
	if h.cfg == nil {
		t.Skip("no config loaded")
	}
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	h.cfg.Model = "openai/global-default"
	h.cfg.MCP = map[string]config.MCPConfig{"excel": {}}

	raw, _ := json.Marshal(map[string]any{
		"content":            "hello",
		"async":              true,
		"pinned_mcp_servers": []string{"excel"},
	})
	rec := httptest.NewRecorder()
	h.HandleChat(rec, httptest.NewRequest("POST", "/api/chat", bytes.NewReader(raw)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("chat status %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.SessionID == "" {
		t.Fatalf("decode chat response: %v (%s)", err, rec.Body.String())
	}

	s, err := session.LoadForDir(proj, resp.SessionID)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	got, err := pinnedMCPServersFromMetadata(s.Metadata)
	if err != nil {
		t.Fatalf("persisted pins unreadable: %v (metadata %v)", err, s.Metadata)
	}
	if want := []string{"excel"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted pinned MCP servers = %v, want %v", got, want)
	}
}

// TestBuildAgentSessionRestoresPinnedMCPServers pins the rebuild path: the pins
// saved at creation must reach the agent every time buildAgentSession runs, so
// a restarted or evicted session keeps its MCP servers attached.
func TestBuildAgentSessionRestoresPinnedMCPServers(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	const id = "ses_pinned_mcp_restore"
	h.sessions.Register(id, proj)
	if err := h.saveSession(id, "", nil, map[string]any{
		pinnedMCPServersMetadataKey: []string{"excel"},
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err != nil {
		t.Fatalf("build agent: %v (stage %s)", err, stage)
	}
	defer as.agent.Shutdown()
	if got, want := as.agent.PinnedMCPServers(), []string{"excel"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuilt agent pinned MCP servers = %v, want %v", got, want)
	}
}

// TestHandleChatRejectsInvalidPinnedMCPServers pins the creation-time check: a
// pin the configuration cannot honour is a 400, and the rejected request leaves
// no session behind.
func TestHandleChatRejectsInvalidPinnedMCPServers(t *testing.T) {
	cases := []struct {
		name    string
		pins    []string
		wantMsg string
	}{
		{name: "unknown server", pins: []string{"nope"}, wantMsg: "is not configured"},
		{name: "duplicate server", pins: []string{"excel", "excel"}, wantMsg: "listed more than once"},
		{name: "empty name", pins: []string{""}, wantMsg: "name is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler()
			if h.cfg == nil {
				t.Skip("no config loaded")
			}
			proj := t.TempDir()
			h.projects = newTestProjectStore(t, proj)
			h.SetWorkDir(proj)
			session.SetWorkDir(proj)
			t.Cleanup(func() { session.SetWorkDir("") })
			h.cfg.Model = "openai/global-default"
			h.cfg.MCP = map[string]config.MCPConfig{"excel": {}}

			raw, _ := json.Marshal(map[string]any{
				"content":            "hello",
				"async":              true,
				"pinned_mcp_servers": tc.pins,
			})
			rec := httptest.NewRecorder()
			h.HandleChat(rec, httptest.NewRequest("POST", "/api/chat", bytes.NewReader(raw)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("chat status %d, want 400 (%s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantMsg) {
				t.Fatalf("error body %q does not contain %q", rec.Body.String(), tc.wantMsg)
			}
			refs, err := session.ListRefsForDir(proj)
			if err != nil {
				t.Fatalf("list sessions: %v", err)
			}
			if len(refs) != 0 {
				t.Fatalf("rejected request left %d session(s) behind", len(refs))
			}
		})
	}
}

// TestBuildAgentSessionRejectsMalformedPinnedMCPServers pins the rebuild path's
// failure mode: a malformed pin list must fail the build with a named stage,
// never produce an agent that silently runs unpinned.
func TestBuildAgentSessionRejectsMalformedPinnedMCPServers(t *testing.T) {
	h := NewHandler()
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	const id = "ses_pinned_mcp_malformed"
	h.sessions.Register(id, proj)
	if err := h.saveSession(id, "", nil, map[string]any{
		pinnedMCPServersMetadataKey: "excel",
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	ready := make(chan struct{})
	close(ready)
	h.mcpCache = &mcpCache{ready: ready, tools: []tool.Tool{}, errs: nil}

	as, stage, err := h.buildAgentSession(id, "opencode-go/deepseek-v4-flash", nil, proj)
	if err == nil {
		as.agent.Shutdown()
		t.Fatalf("build succeeded with malformed pinned MCP servers")
	}
	if as != nil {
		t.Fatalf("failed build returned a session")
	}
	if stage != "pinned-mcp" {
		t.Fatalf("stage = %q, want pinned-mcp (err %v)", stage, err)
	}
}
