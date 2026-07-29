// Package sync provides encrypted config sync between ocode machines via
// the kakiit server. It implements device-code login, debounced background
// push, startup pull-merge, and a 3-way JSON merge with per-blob-type
// conflict policy.
package sync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
)

// DefaultBaseURL returns the kakiit sync server URL: OCODE_SYNC_URL if set,
// otherwise the production instance. Shared by every caller of NewClient
// (TUI, web/desktop server) so they all resolve the same override.
func DefaultBaseURL() string {
	if v := os.Getenv("OCODE_SYNC_URL"); v != "" {
		return v
	}
	return "https://kakiit.com"
}

// BlobType identifies which config file is being synced.
type BlobType string

const (
	BlobTypeConfig BlobType = "ocodeconfig"
	BlobTypeAuth   BlobType = "authsecrets"
)

// syncDir returns the ocode sync data directory (~/.local/share/opencode/sync).
func syncDir() (string, error) {
	base, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "sync")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// SnapshotPath returns the path to the local snapshot file for the given blob
// type. The snapshot tracks the last-known server version to detect conflicts.
func SnapshotPath(blob BlobType) (string, error) {
	dir, err := syncDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, string(blob)+".snapshot.json"), nil
}

// TokenPath returns the path to the stored sync bearer token file.
func TokenPath() (string, error) {
	dir, err := syncDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token"), nil
}

// writeLocalConfigFileAtomic writes a merged blob to a local config file
// (ocodeconfig.json or auth.json) via write-tmp-then-rename, matching
// internal/auth.persistLocked's own atomicity. auth.json in particular can
// be written concurrently by a separate opencode CLI installation sharing
// the same data directory (see internal/auth's opencodeLegacyAuthPath) — a
// plain os.WriteFile could leave that file (or ours) reading a torn write.
func writeLocalConfigFileAtomic(path string, data []byte) error {
	tmp := path + ".sync-tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// localConfigPathFor returns the path to the actual local config file that
// corresponds to the given blob type.
func localConfigPathFor(blob BlobType) (string, error) {
	switch blob {
	case BlobTypeConfig:
		return config.ActiveOcodeConfigPath()
	case BlobTypeAuth:
		return auth.AuthPath()
	default:
		return "", fmt.Errorf("unknown blob type: %s", blob)
	}
}

// SaveToken persists the sync bearer token at 0600 permissions.
func SaveToken(token string) error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0600)
}

// LoadToken reads the stored sync bearer token. A missing file is not an
// error — it means the user hasn't logged in.
func LoadToken() (string, bool, error) {
	path, err := TokenPath()
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// ClearToken deletes the locally stored sync bearer token. A missing file
// is not an error — logout is idempotent.
func ClearToken() error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
