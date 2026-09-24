package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/u007/ocode/internal/tool"
)

// fakeImageTool implements both ImageResultTool and ContextualImageResultTool
// so the helper must pick the context-aware variant.
type fakeImageTool struct{ usedCtx bool }

func (f *fakeImageTool) Name() string                       { return "fakeimage" }
func (f *fakeImageTool) Description() string                { return "" }
func (f *fakeImageTool) Definition() map[string]interface{} { return nil }
func (f *fakeImageTool) Parallel() bool                     { return true }
func (f *fakeImageTool) Execute(json.RawMessage) (string, error) {
	return "", nil
}
func (f *fakeImageTool) ExecuteImage(json.RawMessage) ([]byte, string, error) {
	return []byte("plain"), "image/plain", nil
}
func (f *fakeImageTool) ExecuteImageCtx(context.Context, json.RawMessage) ([]byte, string, error) {
	f.usedCtx = true
	return []byte("ctx"), "image/ctx", nil
}

// fakePlainImageTool implements only ImageResultTool; the helper must fall back.
type fakePlainImageTool struct{}

func (f *fakePlainImageTool) Name() string                       { return "fakeplain" }
func (f *fakePlainImageTool) Description() string                { return "" }
func (f *fakePlainImageTool) Definition() map[string]interface{} { return nil }
func (f *fakePlainImageTool) Parallel() bool                     { return true }
func (f *fakePlainImageTool) Execute(json.RawMessage) (string, error) {
	return "", nil
}
func (f *fakePlainImageTool) ExecuteImage(json.RawMessage) ([]byte, string, error) {
	return []byte("plain"), "image/plain", nil
}

// The read tool's vision byte read must go through ExecuteImageCtx so the
// session workdir (project root) anchors a relative image path instead of the
// process cwd.
func TestExecuteImageWithContextPrefersContextualVariant(t *testing.T) {
	ctx := tool.WithWorkDir(context.Background(), "/tmp")

	ft := &fakeImageTool{}
	raw, mime, err := executeImageWithContext(ctx, ft, nil)
	if err != nil {
		t.Fatalf("executeImageWithContext: %v", err)
	}
	if !ft.usedCtx || string(raw) != "ctx" || mime != "image/ctx" {
		t.Fatalf("expected the contextual variant, got usedCtx=%v raw=%q mime=%q", ft.usedCtx, raw, mime)
	}

	// A tool without the contextual variant still works via ExecuteImage.
	raw, mime, err = executeImageWithContext(ctx, &fakePlainImageTool{}, nil)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if string(raw) != "plain" || mime != "image/plain" {
		t.Fatalf("fallback raw=%q mime=%q", raw, mime)
	}
}
