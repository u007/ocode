package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/u007/ocode/internal/filelock"
)

// WithDownloadLock serializes all TTS artifact work across processes. The OS
// advisory lock is the ownership record: it is released by the kernel when a
// holder exits, so recovery never removes a live holder's lock file based on
// mtime. The token is useful for diagnostics while the lock is held.
func WithDownloadLock(root string, fn func(ownerToken string) error) error {
	if fn == nil {
		return fmt.Errorf("nil download lock callback")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create download lock directory: %w", err)
	}
	lockPath := filepath.Join(root, "tts-download.lock")
	return filelock.WithFileLock(lockPath, func() error {
		token := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
		if err := os.WriteFile(lockPath+".owner", []byte(token), 0o644); err != nil {
			return fmt.Errorf("write download owner: %w", err)
		}
		defer os.Remove(lockPath + ".owner")
		return fn(token)
	})
}
