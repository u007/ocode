package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jamesmercstudio/ocode/internal/agent"
)

func TestProjectSlug(t *testing.T) {
	slug1 := getProjectSlug()
	if slug1 == "" {
		t.Error("expected non-empty slug")
	}

	origWd, _ := os.Getwd()
	os.Chdir("/")
	slug2 := getProjectSlug()
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
