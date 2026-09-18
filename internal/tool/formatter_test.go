package tool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/snapshot"
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

// TestFormatFile_RegistersPostWriteFingerprintForUndo covers the write-tool
// flow: tool write → formatter rewrite → undo. The formatter must register its
// rewrite as the post-write state, otherwise undo would see the formatted
// content as an external modification and refuse (or clobber it).
func TestFormatFile_RegistersPostWriteFingerprintForUndo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	original := "package main\n\nfunc original() {}\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore(snapshot.NewAgentID(), filepath.Join(dir, "snapshots"))
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc-fmt")

	// The write tool: back up the original, write the (unformatted) content,
	// then register the write.
	if err := store.Backup(path, "tc-fmt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\nfunc  new( ){}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store.RegisterWrite(path, "tc-fmt")

	// The formatter rewrite: must re-register the final content.
	if err := FormatFile(ctx, path, map[string]config.FormatterConfig{}); err != nil {
		t.Fatalf("FormatFile: %v", err)
	}

	// No external edit: undo must succeed and restore the original content.
	if _, err := store.UndoByToolCallID("tc-fmt", 5); err != nil {
		t.Fatalf("undo after formatter rewrite: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("file = %q, want %q", got, original)
	}
}

// TestFormatFile_UndoRefusesAfterExternalEdit verifies the formatter-registered
// fingerprint catches a genuine external edit after the formatter finished.
func TestFormatFile_UndoRefusesAfterExternalEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc original() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	store := snapshot.NewStore(snapshot.NewAgentID(), filepath.Join(dir, "snapshots"))
	ctx := snapshot.WithStore(context.Background(), store)
	ctx = snapshot.WithToolCallID(ctx, "tc-fmt")

	if err := store.Backup(path, "tc-fmt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\nfunc  new( ){}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store.RegisterWrite(path, "tc-fmt")
	if err := FormatFile(ctx, path, map[string]config.FormatterConfig{}); err != nil {
		t.Fatalf("FormatFile: %v", err)
	}

	if err := os.WriteFile(path, []byte("package main\n\n// user edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UndoByToolCallID("tc-fmt", 5); !errors.Is(err, snapshot.ErrConflict) {
		t.Fatalf("expected ErrConflict after external edit, got %v", err)
	}
}
