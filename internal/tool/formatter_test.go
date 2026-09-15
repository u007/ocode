package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestFormatFile_BuiltinGoFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc  main( ){}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := FormatFile(context.Background(), path, map[string]config.FormatterConfig{}); err != nil {
		t.Fatalf("FormatFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "package main\n\nfunc main() {}\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatFile_ExplicitConfigOverridesBuiltin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	content := "package main\nfunc  main( ){}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Explicit empty command for "go" disables formatting entirely,
	// even though a builtin gofmt fallback exists.
	formatters := map[string]config.FormatterConfig{
		"go": {Command: ""},
	}
	if err := FormatFile(context.Background(), path, formatters); err != nil {
		t.Fatalf("FormatFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("expected content unchanged, got %q", got)
	}
}

func TestFormatFile_NoBuiltinForUnknownExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	content := "hello   world\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := FormatFile(context.Background(), path, map[string]config.FormatterConfig{}); err != nil {
		t.Fatalf("FormatFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("expected content unchanged, got %q", got)
	}
}

func TestFormatFile_BuiltinFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	if err := FormatFile(context.Background(), path, nil); err == nil {
		t.Fatal("FormatFile swallowed a missing builtin formatter")
	}
}
