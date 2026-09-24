//go:build !windows

package vault

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/sys/unix"
)

// fileLock owns an exclusive advisory lock over a vault file.
type fileLock struct {
	f *os.File
}

// acquireFileLock takes an exclusive, cross-process advisory lock for path on a
// `<path>.lock` sidecar. This is what makes the vault's load-modify-write safe
// when more than one ocode process (desktop shell, CLI, TUI) edits the same
// vault concurrently: the lock serialises the read-merge-write so no process's
// update is lost. It blocks until the lock becomes available.
func acquireFileLock(path string) (*fileLock, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("vault: open lock file: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		if cerr := f.Close(); cerr != nil {
			log.Printf("vault: close lock file after flock failure: %v", cerr)
		}
		return nil, fmt.Errorf("vault: flock: %w", err)
	}
	return &fileLock{f: f}, nil
}

// release drops the lock and closes the sidecar. Safe to call once; a nil
// receiver is a no-op.
func (l *fileLock) release() {
	if l == nil || l.f == nil {
		return
	}
	if err := unix.Flock(int(l.f.Fd()), unix.LOCK_UN); err != nil {
		log.Printf("vault: unlock: %v", err)
	}
	if err := l.f.Close(); err != nil {
		log.Printf("vault: close lock file: %v", err)
	}
	l.f = nil
}
