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
	if existing, err := os.ReadFile(dest); err == nil {
		if sha256Hex(existing) == a.SHA256 {
			return nil
		}
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
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s: %w", a.URL, err)
	}
	if got := sha256Hex(data); got != a.SHA256 {
		return fmt.Errorf("sha256 mismatch for %s: got %s want %s", a.Dest, got, a.SHA256)
	}
	tmp := dest + ".tmp"
	mode := os.FileMode(0o644)
	if a.Exec {
		mode = 0o755
	}
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write artifact tmp: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("rename artifact: %w", err)
	}
	return nil
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
