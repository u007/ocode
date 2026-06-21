package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// EnsureArtifact downloads a into dir/a.Dest if missing or sha-mismatched, verifies
// sha256, and writes atomically (temp + rename). Re-download is skipped when the
// cached file already matches.
func EnsureArtifact(a Artifact, dir string) error {
	dest := filepath.Join(dir, a.Dest)
	if a.URL == "" {
		return fmt.Errorf("artifact %s: URL not pinned in manifest", a.Dest)
	}
	// Artifacts are large (model GGUFs run to hundreds of MB–multi-GB), so both
	// the cache check and the download stream through the hasher — never buffer
	// the whole file in memory.
	if got, err := sha256File(dest); err == nil && got == a.SHA256 {
		return nil
	}
	if a.SHA256 == "" {
		return fmt.Errorf("artifact %s: SHA256 not pinned in manifest (refusing to download unverified)", a.Dest)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir artifact dir: %w", err)
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(a.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", a.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", a.URL, resp.StatusCode)
	}
	mode := os.FileMode(0o644)
	if a.Exec {
		mode = 0o755
	}
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("open artifact tmp: %w", err)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("download %s: %w", a.URL, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close artifact tmp: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		os.Remove(tmp)
		return fmt.Errorf("sha256 mismatch for %s: got %s want %s", a.Dest, got, a.SHA256)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("rename artifact: %w", err)
	}
	return nil
}

// sha256File streams a file through sha256 (no full-file buffering).
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
