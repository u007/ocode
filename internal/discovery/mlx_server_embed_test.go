package discovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteMLXServerScriptCreatesCacheDir(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "nested", "discovery-cache")
	path, err := WriteMLXServerScript(cacheDir)
	if err != nil {
		t.Fatalf("WriteMLXServerScript: %v", err)
	}
	if path != filepath.Join(cacheDir, "mlx_embed_server.py") {
		t.Fatalf("unexpected path %q", path)
	}
	if _, err := os.Stat(cacheDir); err != nil {
		t.Fatalf("expected cache dir to exist: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if len(data) == 0 || !strings.Contains(string(data), "mlx_lm") {
		t.Fatalf("unexpected script contents")
	}
}
