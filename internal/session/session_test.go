package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
)

func TestProjectSlug(t *testing.T) {
	slug1 := ProjectSlug()
	if slug1 == "" {
		t.Error("expected non-empty slug")
	}

	origWd, _ := os.Getwd()
	os.Chdir("/")
	slug2 := ProjectSlug()
	os.Chdir(origWd)

	if slug1 == slug2 {
		t.Errorf("expected different slugs for different directories, got %s and %s", slug1, slug2)
	}
}

func TestSaveAndLoadPreservesMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	meta := map[string]any{"prompt_tokens": 12, "completion_tokens": 34, "total_tokens": 46, "spend": 0.035}
	if err := Save("session-1", "", []agent.Message{{Role: "user", Content: "hi"}}, meta); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	sess, err := Load("session-1")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if sess.Metadata == nil || sess.Metadata["total_tokens"] != 46.0 {
		t.Fatalf("expected metadata to persist, got %#v", sess.Metadata)
	}
}

func TestNewSessionIDUsesCanonicalPrefix(t *testing.T) {
	id := NewSessionID()
	if !strings.HasPrefix(id, "ses_") {
		t.Fatalf("expected canonical ses_ prefix, got %q", id)
	}
	if len(id) != len("ses_2006-01-02-150405") {
		t.Fatalf("expected timestamp-style session id, got %q", id)
	}
}

func TestLoadFallsBackToLegacyBareTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	legacyID := "2025-05-26-102317"
	if err := Save(legacyID, "Legacy", []agent.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	sess, err := Load("ses_" + legacyID)
	if err != nil {
		t.Fatalf("Load with canonical prefix failed: %v", err)
	}
	if sess.ID != legacyID {
		t.Fatalf("expected stored legacy id preserved, got %q", sess.ID)
	}
}

func TestLoadFallsBackToCanonicalPrefixedID(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	canonicalID := "ses_2025-05-26-102317"
	if err := Save(canonicalID, "Canonical", []agent.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	sess, err := Load("2025-05-26-102317")
	if err != nil {
		t.Fatalf("Load with legacy bare id failed: %v", err)
	}
	if sess.ID != canonicalID {
		t.Fatalf("expected stored canonical id preserved, got %q", sess.ID)
	}
}

func TestSaveGeneratesCanonicalPrefixedIDWhenBlank(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	if err := Save("", "Generated", []agent.Message{{Role: "user", Content: "hello"}}, nil); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	dir, err := GetStorageDir()
	if err != nil {
		t.Fatalf("GetStorageDir failed: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || entry.Name() == "index.json" {
			continue
		}
		if !strings.HasPrefix(entry.Name(), "ses_") {
			t.Fatalf("expected generated session file to use ses_ prefix, got %q", entry.Name())
		}
		return
	}
	t.Fatal("expected generated session file")
}

func TestSaveWritesOcodeSessionFormatWithSidebarMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	meta := map[string]any{
		"input_tokens":  12,
		"output_tokens": 34,
		"billed_tokens": 46,
		"cached_tokens": 9,
		"spend":         0.035,
	}
	if err := Save("session-format", "Demo", []agent.Message{{Role: "user", Content: "hi"}}, meta); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	dir, err := GetStorageDir()
	if err != nil {
		t.Fatalf("GetStorageDir failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "session-format.json"))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal raw session failed: %v", err)
	}
	for _, key := range []string{"id", "title", "messages", "created_at", "updated_at", "metadata"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected opencode session format to include %q, got %#v", key, got)
		}
	}
	metadata, ok := got["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected metadata object, got %#v", got["metadata"])
	}
	for key, want := range map[string]float64{
		"input_tokens":  12,
		"output_tokens": 34,
		"billed_tokens": 46,
		"cached_tokens": 9,
		"spend":         0.035,
	} {
		if metadata[key] != want {
			t.Fatalf("expected metadata[%q]=%v, got %#v", key, want, metadata[key])
		}
	}
}

func TestSaveAndLoadPreservesMessageImages(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	messages := []agent.Message{{
		Role:    "user",
		Content: "describe this",
		Images: []agent.Image{{
			Path:     "screenshot.png",
			MIMEType: "image/png",
			Data:     "aW1n",
		}},
	}}
	if err := Save("session-images", "", messages, nil); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	sess, err := Load("session-images")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(sess.Messages) != 1 || len(sess.Messages[0].Images) != 1 {
		t.Fatalf("expected image to persist, got %#v", sess.Messages)
	}
	img := sess.Messages[0].Images[0]
	if img.Path != "screenshot.png" || img.MIMEType != "image/png" || img.Data != "aW1n" {
		t.Fatalf("unexpected image data: %#v", img)
	}
}

func TestLoadRemovesIncompleteToolRequests(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	completedCall := agent.ToolCall{ID: "call-complete", Type: "function"}
	completedCall.Function.Name = "read"
	completedCall.Function.Arguments = `{"filePath":"README.md"}`

	incompleteCall := agent.ToolCall{ID: "call-incomplete", Type: "function"}
	incompleteCall.Function.Name = "bash"
	incompleteCall.Function.Arguments = `{"command":"go test ./..."}`

	questionCall := agent.ToolCall{ID: "call-question", Type: "function"}
	questionCall.Function.Name = "question"
	questionCall.Function.Arguments = `{}`

	permissionCall := agent.ToolCall{ID: "call-permission", Type: "function"}
	permissionCall.Function.Name = "bash"
	permissionCall.Function.Arguments = `{"command":"git status"}`

	messages := []agent.Message{
		{Role: "user", Content: "start"},
		{Role: "assistant", ToolCalls: []agent.ToolCall{completedCall, incompleteCall}},
		{Role: "tool", ToolID: "call-complete", Content: "read result"},
		{Role: "assistant", Content: "need input", ToolCalls: []agent.ToolCall{questionCall}},
		{Role: "tool", ToolID: "call-question", Content: "WAITING_FOR_USER_RESPONSE"},
		{Role: "assistant", ToolCalls: []agent.ToolCall{permissionCall}},
		{Role: "tool", ToolID: "call-permission", Content: `PERMISSION_ASK:{"tool_name":"bash"}`},
	}
	if err := Save("session-tools", "", messages, nil); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	sess, err := Load("session-tools")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(sess.Messages) != 4 {
		t.Fatalf("expected incomplete placeholder tool message to be removed, got %#v", sess.Messages)
	}
	if len(sess.Messages[1].ToolCalls) != 1 || sess.Messages[1].ToolCalls[0].ID != "call-complete" {
		t.Fatalf("expected only completed tool call to remain, got %#v", sess.Messages[1].ToolCalls)
	}
	if len(sess.Messages[3].ToolCalls) != 0 || sess.Messages[3].Content != "need input" {
		t.Fatalf("expected question tool call stripped while keeping assistant text, got %#v", sess.Messages[3])
	}
}

func TestListRemovesIncompleteToolRequests(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(tmpDir)

	completedCall := agent.ToolCall{ID: "call-complete", Type: "function"}
	completedCall.Function.Name = "read"
	completedCall.Function.Arguments = `{"filePath":"README.md"}`

	incompleteCall := agent.ToolCall{ID: "call-interrupted", Type: "function"}
	incompleteCall.Function.Name = "bash"
	incompleteCall.Function.Arguments = `{"command":"sleep 60"}`

	messages := []agent.Message{
		{Role: "user", Content: "start"},
		{Role: "assistant", ToolCalls: []agent.ToolCall{completedCall, incompleteCall}},
		{Role: "tool", ToolID: "call-complete", Content: "read result"},
	}
	if err := Save("session-list-tools", "", messages, nil); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	sessions, err := List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if len(sessions[0].Messages) != 3 {
		t.Fatalf("expected messages to remain except incomplete tool calls, got %#v", sessions[0].Messages)
	}
	calls := sessions[0].Messages[1].ToolCalls
	if len(calls) != 1 || calls[0].ID != "call-complete" {
		t.Fatalf("expected List to strip interrupted tool call, got %#v", calls)
	}
}

func TestListAllIncludesOnlyCurrentDirClaudeSessions(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	t.Setenv("HOME", tmpHome)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	currentClaudeDir := filepath.Join(tmpHome, ".claude", "projects", claudeProjectSlug(wd))
	otherClaudeDir := filepath.Join(tmpHome, ".claude", "projects", claudeProjectSlug(filepath.Join(tmpHome, "other")))
	if err := os.MkdirAll(currentClaudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherClaudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(currentClaudeDir, "claude-current.jsonl"), []byte(`{"type":"user","message":{"role":"user","content":"current project"},"timestamp":"2026-05-16T10:00:00Z"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherClaudeDir, "claude-other.jsonl"), []byte(`{"type":"user","message":{"role":"user","content":"other project"},"timestamp":"2026-05-16T11:00:00Z"}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	refs, err := ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected one current-dir Claude session, got %#v", refs)
	}
	if refs[0].ID != "claude:claude-current" || refs[0].Source != SourceClaude {
		t.Fatalf("expected current Claude ref, got %#v", refs[0])
	}
}

func TestCloneClaudeSessionSavesOcodeSession(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	t.Setenv("HOME", tmpHome)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	claudeDir := filepath.Join(tmpHome, ".claude", "projects", claudeProjectSlug(wd))
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"hello from claude"},"timestamp":"2026-05-16T10:00:00Z"}`,
		`{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-7","content":[{"type":"text","text":"hello back"}]},"timestamp":"2026-05-16T10:01:00Z"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(claudeDir, "claude-1.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sess, err := CloneClaudeSession("claude-1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID != "claude-claude-1" {
		t.Fatalf("expected cloned session id, got %s", sess.ID)
	}
	if len(sess.Messages) != 2 || sess.Messages[0].Content != "hello from claude" || sess.Messages[1].Content != "hello back" {
		t.Fatalf("unexpected cloned messages: %#v", sess.Messages)
	}
	if sess.Metadata["claude_original_session_id"] != "claude-1" {
		t.Fatalf("expected original Claude metadata, got %#v", sess.Metadata)
	}

	refs, err := ListAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref.ID == "claude:claude-1" {
			t.Fatalf("expected cloned Claude session to hide raw Claude ref, got %#v", refs)
		}
	}
}

func TestAppendClaudeSessionWritesClaudeJsonl(t *testing.T) {
	tmpHome := t.TempDir()
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	t.Setenv("HOME", tmpHome)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	path, err := AppendClaudeSession("session-claude", []agent.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there", Model: "claude-sonnet-4"},
	})
	if err != nil {
		t.Fatalf("AppendClaudeSession failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read exported file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two JSONL lines, got %d: %q", len(lines), string(data))
	}

	var first, second map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first line is not valid JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("second line is not valid JSON: %v", err)
	}

	if first["type"] != "user" || second["type"] != "assistant" {
		t.Fatalf("expected user then assistant lines, got %#v %#v", first["type"], second["type"])
	}
	if first["uuid"] == "" || second["parentUuid"] != first["uuid"] {
		t.Fatalf("expected parent UUID chain, got first=%#v second=%#v", first["uuid"], second["parentUuid"])
	}
	wantSessionUUID := sessionUUIDv5("session-claude")
	if first["sessionId"] != wantSessionUUID {
		t.Fatalf("expected sessionId to be derived UUIDv5 %q, got %#v", wantSessionUUID, first["sessionId"])
	}
	if filepath.Base(path) != wantSessionUUID+".jsonl" {
		t.Fatalf("expected filename to use UUID, got %q", filepath.Base(path))
	}

	msg, ok := first["message"].(map[string]any)
	if !ok || msg["content"] != "hello" {
		t.Fatalf("expected first message content to be plain text, got %#v", first["message"])
	}
	if _, ok := second["message"].(map[string]any); !ok {
		t.Fatalf("expected assistant message object, got %#v", second["message"])
	}

	if _, err := AppendClaudeSession("session-claude", []agent.Message{{Role: "user", Content: "follow-up"}}); err != nil {
		t.Fatalf("second AppendClaudeSession failed: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to reread exported file: %v", err)
	}
	lines = strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected append-only file to grow to three lines, got %d: %q", len(lines), string(data))
	}
	var third map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &third); err != nil {
		t.Fatalf("third line is not valid JSON: %v", err)
	}
	if third["parentUuid"] != second["uuid"] {
		t.Fatalf("expected appended line to chain from prior UUID, got parent=%#v want=%#v", third["parentUuid"], second["uuid"])
	}
	if third["uuid"] == first["uuid"] || third["uuid"] == second["uuid"] {
		t.Fatalf("appended entry uuid collides with earlier entry: third=%#v first=%#v second=%#v", third["uuid"], first["uuid"], second["uuid"])
	}
}
