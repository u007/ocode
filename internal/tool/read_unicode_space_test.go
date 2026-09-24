package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real-world trigger: macOS writes screenshot filenames with U+202F
// (NARROW NO-BREAK SPACE) before AM/PM, and a model re-emits that path with a
// plain ASCII space. The read must still find the file.
func TestReadToolRecoversUnicodeSpaceFilename(t *testing.T) {
	dir := t.TempDir()
	uploads := filepath.Join(dir, ".ocode", "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(uploads, "Screenshot 2026-09-23 at 11.00.05\u202fPM.txt")
	if err := os.WriteFile(real, []byte("hello from the screenshot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	args, _ := json.Marshal(map[string]any{
		"path": ".ocode/uploads/Screenshot 2026-09-23 at 11.00.05 PM.txt",
	})
	out, err := (ReadTool{}).Execute(args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "hello from the screenshot") {
		t.Fatalf("read output = %q", out)
	}
}

// The same recovery must apply on the vision path (ExecuteImage), which the
// agent uses when a vision-capable model reads an image.
func TestReadToolExecuteImageRecoversUnicodeSpaceFilename(t *testing.T) {
	dir := t.TempDir()
	// A 1x1 PNG so sniffImageMIME recognises it; ExecuteImage does not decode.
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
	real := filepath.Join(dir, "Screenshot 2026-09-23 at 6.44.05\u202fPM.png")
	if err := os.WriteFile(real, png, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	args, _ := json.Marshal(map[string]any{
		"path": "Screenshot 2026-09-23 at 6.44.05 PM.png",
	})
	raw, mime, err := (ReadTool{}).ExecuteImage(args)
	if err != nil {
		t.Fatalf("ExecuteImage: %v", err)
	}
	if len(raw) != len(png) {
		t.Fatalf("image bytes = %d, want %d", len(raw), len(png))
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", mime)
	}
}

// A genuinely missing file must still fail, and the failure must name the
// resolved path rather than silently reading a neighbour.
func TestReadToolDoesNotFabricateMissingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "present.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	args, _ := json.Marshal(map[string]any{"path": "absent.txt"})
	if _, err := (ReadTool{}).Execute(args); err == nil {
		t.Fatal("expected an error for a genuinely missing file")
	}
}

// Read-only scope: a write must NOT be redirected onto a differently-spaced
// existing file, or a create-new-file flow would silently clobber it.
func TestConfinedReadPathIsReadOnlyScope(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "a\u202fb.txt")
	if err := os.WriteFile(real, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	// confinedPath (used by write/edit) keeps the literal missing path.
	literal, err := confinedPath(t.Context(), filepath.Join(dir, "a b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Lstat(literal); statErr == nil {
		t.Fatalf("confinedPath must not redirect a missing literal path, got %q", literal)
	}
	// confinedReadPath recovers it.
	recovered, err := confinedReadPath(t.Context(), filepath.Join(dir, "a b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if recovered != real {
		t.Fatalf("confinedReadPath = %q, want %q", recovered, real)
	}
}

// The desktop app is launched from Finder with cwd "/", so a relative read must
// anchor on the session project root (WithWorkDir) — never the process cwd.
func TestReadToolUsesContextWorkDir(t *testing.T) {
	projDir := t.TempDir()
	otherDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projDir, "note.txt"), []byte("from project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	origWd, _ := os.Getwd()
	if err := os.Chdir(otherDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	args, _ := json.Marshal(map[string]any{"path": "note.txt"})

	// Without a workdir the process cwd wins and the read misses.
	if _, err := (ReadTool{}).Execute(args); err == nil {
		t.Fatal("expected a relative read with no workdir to miss (cwd anchor)")
	}
	// With the session workdir in context it resolves against the project root.
	out, err := (ReadTool{}).ExecuteCtx(WithWorkDir(context.Background(), projDir), args)
	if err != nil {
		t.Fatalf("ExecuteCtx: %v", err)
	}
	if !strings.Contains(out, "from project") {
		t.Fatalf("out = %q", out)
	}
}

// Same anchoring for the vision byte read (ExecuteImageCtx).
func TestReadToolExecuteImageUsesContextWorkDir(t *testing.T) {
	projDir := t.TempDir()
	otherDir := t.TempDir()
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
	if err := os.WriteFile(filepath.Join(projDir, "shot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	origWd, _ := os.Getwd()
	if err := os.Chdir(otherDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	args, _ := json.Marshal(map[string]any{"path": "shot.png"})
	if _, _, err := (ReadTool{}).ExecuteImage(args); err == nil {
		t.Fatal("expected a relative image read with no workdir to miss")
	}
	raw, mime, err := (ReadTool{}).ExecuteImageCtx(WithWorkDir(context.Background(), projDir), args)
	if err != nil {
		t.Fatalf("ExecuteImageCtx: %v", err)
	}
	if len(raw) != len(png) || mime != "image/png" {
		t.Fatalf("raw=%d mime=%q", len(raw), mime)
	}
}
