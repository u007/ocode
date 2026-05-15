package runcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePromptFromArg(t *testing.T) {
	p, err := resolvePrompt("hello", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != "hello" {
		t.Errorf("expected 'hello', got %q", p)
	}
}

func TestResolvePromptFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("file content"), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := resolvePrompt("", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != "file content" {
		t.Errorf("expected 'file content', got %q", p)
	}
}

func TestResolvePromptFromNonexistentFile(t *testing.T) {
	_, err := resolvePrompt("", "/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestResolvePromptEmpty(t *testing.T) {
	p, err := resolvePrompt("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != "" {
		t.Errorf("expected empty string, got %q", p)
	}
}
