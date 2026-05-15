package session

import (
	"os"
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
