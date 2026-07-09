package discovery

import (
	_ "embed"
	"os"
	"path/filepath"
)

// mlxServerScript is the bundled OpenAI-compatible embeddings server used by
// the MLX local backend (Apple-Silicon only). It is written to the cache dir on
// first use and spawned via `python3`.
//
//go:embed mlx_embed_server.py
var mlxServerScript string

// WriteMLXServerScript writes the bundled MLX server script into cacheDir and
// returns its path. Idempotent: skips the write if the file already exists.
func WriteMLXServerScript(cacheDir string) (string, error) {
	path := filepath.Join(cacheDir, "mlx_embed_server.py")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(mlxServerScript), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
