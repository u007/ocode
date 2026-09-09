package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CacheDir returns the verified TTS cache path. An invalid component returns
// an empty string; callers that need the error should use CacheDirChecked.
func CacheDir(root string, engine EngineID, voice, version string) string {
	dir, err := CacheDirChecked(root, engine, voice, version)
	if err != nil {
		return ""
	}
	return dir
}

// CacheDirChecked builds a cache path only from validated path components. It
// keeps future artifact adapters from turning a voice/model identifier into a
// path traversal primitive.
func CacheDirChecked(root string, engine EngineID, voice, version string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("cache root is empty")
	}
	if _, ok := EngineForHost(engine); !ok {
		return "", fmt.Errorf("unknown TTS engine %q", engine)
	}
	if err := ValidateVoiceID(voice); err != nil {
		return "", err
	}
	if err := validateCacheComponent("version", version); err != nil {
		return "", err
	}
	base := filepath.Clean(root)
	dir := filepath.Join(base, "models", "tts", string(engine), voice, version, Host())
	rel, err := filepath.Rel(base, dir)
	if err != nil || rel == ".." || (len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("cache path escapes root")
	}
	return dir, nil
}

func validateCacheComponent(label, value string) error {
	if value == "" || value == "." || value == ".." {
		return fmt.Errorf("%s must be a non-empty path component", label)
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("%s contains an invalid path character", label)
	}
	return nil
}

func InstallVerified(src, dst, expectedSHA256 string) error {
	return InstallVerifiedMode(src, dst, expectedSHA256, 0o644)
}

// InstallVerifiedMode is the manifest-aware variant of InstallVerified. The
// mode is applied to the temporary file before its atomic rename so executable
// runtime artifacts never appear at the destination with incomplete metadata.
func InstallVerifiedMode(src, dst, expectedSHA256 string, mode os.FileMode) error {
	if expectedSHA256 == "" {
		return fmt.Errorf("missing checksum for %s", dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source artifact: %w", err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tts-install-*")
	if err != nil {
		return fmt.Errorf("create temporary artifact: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), in); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copy artifact: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync artifact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close artifact: %w", err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != expectedSHA256 {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", dst, got, expectedSHA256)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("set artifact mode: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("install artifact atomically: %w", err)
	}
	return nil
}
